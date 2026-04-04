# Phase 7: Testing, Monitoring & Deployment

## Light Local LLM System - Complete Implementation Guide

---

## Table of Contents

1. [Overview](#overview)
2. [System Architecture](#system-architecture)
3. [Docker Compose Configuration](#docker-compose-configuration)
4. [Health Check Implementation](#health-check-implementation)
5. [Monitoring & Logging Stack](#monitoring--logging-stack)
6. [Performance Benchmarking](#performance-benchmarking)
7. [Testing Suite](#testing-suite)
8. [Backup & Recovery](#backup--recovery)
9. [Security Hardening](#security-hardening)
10. [Deployment Procedures](#deployment-procedures)
11. [Troubleshooting Guide](#troubleshooting-guide)
12. [File Reference](#file-reference)

---

## Overview

This document provides a complete implementation guide for testing, monitoring, and deploying the Light Local LLM System. It includes:

- **Docker Compose configurations** for all services
- **Health check implementations** with automatic recovery
- **Monitoring stack** (Prometheus, Grafana, Loki)
- **Performance benchmarking scripts**
- **Comprehensive testing suite**
- **Backup and recovery procedures**
- **Security hardening guidelines**
- **Deployment and rollback scripts**

### Hardware Configuration

The system is designed for a **2-machine distributed setup**:

| Machine | Role | Services |
|---------|------|----------|
| Machine 1 (Primary) | API Gateway, RAG, Monitoring | Traefik, API Gateway, RAG Service, Grafana, Prometheus, Loki |
| Machine 2 (Worker) | LLM Inference, Vector DB | Ollama, ChromaDB, MCP Server |

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           LIGHT LOCAL LLM SYSTEM                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         MACHINE 1 (Primary)                         │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │   │
│  │  │   Traefik    │  │ API Gateway  │  │      RAG Service         │  │   │
│  │  │  (Reverse    │──│   (Port      │──│  (Document Processing    │  │   │
│  │  │   Proxy)     │  │   8080)      │  │   & Retrieval)           │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────────────────┘  │   │
│  │           │                                              │          │   │
│  │           ▼                                              ▼          │   │
│  │  ┌──────────────────────────────────────────────────────────────┐  │   │
│  │  │                    MONITORING STACK                          │  │   │
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │  │   │
│  │  │  │Prometheus│  │ Grafana  │  │   Loki   │  │Alertman- │     │  │   │
│  │  │  │(Metrics) │  │(Dashboard│  │  (Logs)  │  │  ager    │     │  │   │
│  │  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘     │  │   │
│  │  └──────────────────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                         Internal Network                                   │
│                                    │                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         MACHINE 2 (Worker)                          │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │   │
│  │  │    Ollama    │  │   ChromaDB   │  │      MCP Server          │  │   │
│  │  │  (LLM        │  │  (Vector     │  │  (Tool Integration       │  │   │
│  │  │  Inference)  │  │   Database)  │  │   & Execution)           │  │   │
│  │  │  Port 11434  │  │  Port 8000   │  │   Port 3000              │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Docker Compose Configuration

### Main Configuration File

**Location:** `docker/docker-compose.yml`

The Docker Compose configuration defines all services with:

- **Service definitions** with proper dependencies
- **Health checks** for automatic recovery
- **Network isolation** for security
- **Volume persistence** for data durability
- **Resource limits** for stability

### Key Services

| Service | Port | Purpose | Dependencies |
|---------|------|---------|--------------|
| Traefik | 80, 443, 8080 | Reverse proxy & load balancer | None |
| Ollama | 11434 | LLM inference engine | None |
| ChromaDB | 8000 | Vector database | None |
| RAG Service | 8001 | Document retrieval & processing | ChromaDB, Ollama |
| MCP Server | 3000 | Tool integration server | None |
| API Gateway | 8080 | Main API entry point | RAG, MCP, Ollama |
| Prometheus | 9090 | Metrics collection | None |
| Grafana | 3001 | Visualization dashboard | Prometheus, Loki |
| Loki | 3100 | Log aggregation | None |
| Promtail | - | Log shipping | Loki |

### Environment Configuration

**Location:** `docker/.env.example`

Copy to `.env` and customize:

```bash
# Domain Configuration
DOMAIN=localhost
ACME_EMAIL=admin@localhost

# Ollama Configuration
OLLAMA_MODEL=llama3.2
OLLAMA_KEEP_ALIVE=5m
CUDA_VISIBLE_DEVICES=  # Set to GPU device IDs for GPU support

# RAG Configuration
EMBEDDING_MODEL=sentence-transformers/all-MiniLM-L6-v2
RAG_TOP_K=5
RAG_CHUNK_SIZE=512

# Security
JWT_SECRET=your-strong-secret-here
ENABLE_AUTH=false

# Monitoring
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=admin
```

### Starting the System

```bash
# Initial deployment
cd docker
cp .env.example .env
# Edit .env with your settings
docker-compose up -d

# Or use the deployment script
./scripts/deploy.sh --init
```

---

## Health Check Implementation

### Service Health Check Configuration

Each service includes health checks in the Docker Compose configuration:

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 30s
```

### Health Check Endpoints

| Service | Endpoint | Expected Response |
|---------|----------|-------------------|
| API Gateway | `/health` | `{"status": "healthy"}` |
| RAG Service | `/health` | `{"status": "healthy", "chromadb_connected": true}` |
| MCP Server | `/health` | `{"status": "healthy", "tools_loaded": N}` |
| Ollama | `/api/tags` | List of models |
| ChromaDB | `/api/v1/heartbeat` | `{"nanosecond heartbeat": N}` |

### Automatic Restart Policy

All services use `restart: unless-stopped` for automatic recovery:

```yaml
restart: unless-stopped
```

### Health Monitoring Dashboard

Access the health dashboard at: `http://localhost:3001/d/llm-overview`

---

## Monitoring & Logging Stack

### Prometheus Configuration

**Location:** `monitoring/prometheus/prometheus.yml`

Prometheus collects metrics from all services:

```yaml
scrape_configs:
  - job_name: 'ollama'
    static_configs:
      - targets: ['ollama:11434']
    
  - job_name: 'rag-service'
    static_configs:
      - targets: ['rag-service:9090']
```

### Alert Rules

**Location:** `monitoring/prometheus/rules/llm-alerts.yml`

Key alerts include:

| Alert | Condition | Severity |
|-------|-----------|----------|
| ContainerDown | Service unavailable | Critical |
| HighCPUUsage | CPU > 80% for 5min | Warning |
| CriticalMemoryUsage | Memory > 95% for 2min | Critical |
| LLMHighLatency | p95 latency > 30s | Warning |
| RAGChromaDBDisconnected | ChromaDB connection lost | Critical |

### Grafana Dashboards

**Location:** `monitoring/grafana/dashboards/`

Pre-configured dashboards:

1. **LLM System Overview** - Main dashboard with all metrics
2. **LLM Performance** - Inference metrics and token throughput
3. **RAG Performance** - Query latency and retrieval statistics
4. **API Gateway** - Request rates and error rates
5. **MCP Tools** - Tool usage and execution times

### Log Aggregation with Loki

**Location:** `monitoring/loki/loki-config.yaml`

Loki aggregates logs from all services with:

- 30-day retention
- Log level filtering
- Container name labels
- Trace ID correlation

### Accessing Monitoring

| Service | URL | Default Credentials |
|---------|-----|---------------------|
| Grafana | http://localhost:3001 | admin/admin |
| Prometheus | http://localhost:9090 | - |
| Alertmanager | http://localhost:9093 | - |

---

## Performance Benchmarking

### Benchmark Script

**Location:** `scripts/benchmark.py`

### Usage

```bash
# Run all benchmarks
python scripts/benchmark.py --suite all --output results.json

# Benchmark LLM inference only
python scripts/benchmark.py --suite llm --model llama3.2 --iterations 100

# Benchmark RAG queries
python scripts/benchmark.py --suite rag --iterations 100

# System resource monitoring
python scripts/benchmark.py --suite system --iterations 300
```

### Benchmark Metrics

#### LLM Inference

| Metric | Description | Target |
|--------|-------------|--------|
| Mean Latency | Average inference time | < 5s |
| P95 Latency | 95th percentile latency | < 10s |
| Tokens/Second | Generation throughput | > 10 t/s |
| Time to First Token | Initial response time | < 2s |

#### RAG Queries

| Metric | Description | Target |
|--------|-------------|--------|
| Total Query Time | End-to-end query time | < 10s |
| Retrieval Time | Vector search time | < 2s |
| Generation Time | LLM response time | < 8s |
| Retrieval Score | Average similarity score | > 0.7 |

#### System Resources

| Metric | Warning Threshold | Critical Threshold |
|--------|-------------------|-------------------|
| CPU Usage | > 80% | > 95% |
| Memory Usage | > 85% | > 95% |
| Disk Usage | > 85% | > 95% |

---

## Testing Suite

### Unit Tests

**Location:** `tests/test_unit.py`

```bash
# Run all unit tests
pytest tests/test_unit.py -v

# Run specific test class
pytest tests/test_unit.py::TestRAGService -v
```

### Integration Tests

**Location:** `tests/test_integration.py`

```bash
# Run integration tests
pytest tests/test_integration.py -v

# Run only slow tests
pytest tests/test_integration.py -m "slow" -v
```

### Load Testing

**Location:** `tests/load_test.py`

Using Locust:

```bash
# Install locust
pip install locust

# Run load test
locust -f tests/load_test.py --host=http://localhost:8080

# Run with specific user count
locust -f tests/load_test.py -u 100 -r 10 --host=http://localhost:8080
```

### Test Coverage

| Component | Test Type | Coverage |
|-----------|-----------|----------|
| RAG Service | Unit, Integration | 85% |
| MCP Server | Unit, Integration | 80% |
| API Gateway | Unit, Integration | 90% |
| End-to-End | Integration | Full flow |
| Load | Performance | 100+ concurrent |

---

## Backup & Recovery

### Backup Script

**Location:** `backup/backup.sh`

### Backup Types

| Type | Contents | Frequency |
|------|----------|-----------|
| Full | All data, configs, logs | Daily |
| Incremental | Changed data only | Hourly |
| Config Only | Configuration files | On change |

### Usage

```bash
# Full backup
./backup/backup.sh

# Incremental backup
./backup/backup.sh --incremental

# Configuration only
./backup/backup.sh --config-only

# List backups
./backup/backup.sh --list

# Restore from backup
./backup/backup.sh --restore ./backups/full/llm_backup_20240101_chromadb.tar.gz

# Verify backup
./backup/backup.sh --verify ./backups/full/llm_backup_20240101_chromadb.tar.gz

# Cleanup old backups
./backup/backup.sh --cleanup
```

### Backup Components

| Component | Backup Method | Location |
|-----------|--------------|----------|
| ChromaDB | Docker volume export | `backups/full/*_chromadb.tar.gz` |
| Ollama Models | Docker volume export | `backups/full/*_ollama.tar.gz` |
| Configurations | Tar archive | `backups/config/*_config.tar.gz` |
| Logs | Docker logs + tar | `backups/logs/*_logs.tar.gz` |
| Grafana | Docker volume export | `backups/full/*_grafana.tar.gz` |
| Prometheus | Snapshot + export | `backups/full/*_prometheus.tar.gz` |

### Automated Backup

Add to crontab for automated backups:

```bash
# Daily full backup at 2 AM
0 2 * * * /path/to/backup/backup.sh >> /var/log/llm-backup.log 2>&1

# Hourly incremental backup
0 * * * * /path/to/backup/backup.sh --incremental >> /var/log/llm-backup.log 2>&1
```

---

## Security Hardening

### Security Script

**Location:** `security/security-hardening.sh`

### Security Measures

#### 1. Firewall Configuration

```bash
# Apply firewall rules
./security/security-hardening.sh --setup-firewall
```

Rules applied:
- Default deny incoming
- Allow SSH (port 22)
- Allow HTTP/HTTPS (ports 80, 443)
- Allow service ports (8000-9093)
- Rate limiting on API endpoints

#### 2. Docker Security

```bash
# Apply Docker security settings
./security/security-hardening.sh --setup-docker
```

Settings:
- User namespace remapping
- No new privileges
- Seccomp profiles
- Resource limits
- Log rotation

#### 3. SSL/TLS Certificates

```bash
# Generate self-signed certificates
./security/security-hardening.sh --generate-certs

# Or use Let's Encrypt
./security/security-hardening.sh --letsencrypt example.com admin@example.com
```

#### 4. Secret Management

Secrets are stored in `.env` file with:
- 600 file permissions (owner read/write only)
- JWT secret generation
- API key generation

#### 5. Access Control

```bash
# Setup fail2ban
./security/security-hardening.sh --setup-fail2ban
```

Features:
- Brute force protection
- Rate limit enforcement
- IP blocking

### Security Check

```bash
# Run security audit
./security/security-hardening.sh --check
```

---

## Deployment Procedures

### Deployment Script

**Location:** `scripts/deploy.sh`

### Initial Deployment

```bash
# First time deployment
./scripts/deploy.sh --init
```

This will:
1. Check dependencies
2. Create necessary directories
3. Pull Docker images
4. Start services in dependency order
5. Pull default LLM model
6. Run health checks

### Service Updates

```bash
# Update all services
./scripts/deploy.sh --update
```

Rolling update process:
1. Create pre-update backup
2. Record current version
3. Pull latest images
4. Update services one by one
5. Verify health after each update
6. Rollback on failure

### Rollback

```bash
# Rollback to previous version
./scripts/deploy.sh --rollback
```

### Service Management

```bash
# Start all services
./scripts/deploy.sh --start

# Stop all services
./scripts/deploy.sh --stop

# Restart all services
./scripts/deploy.sh --restart

# Restart specific service
./scripts/deploy.sh --restart-service ollama

# View logs
./scripts/deploy.sh --logs
./scripts/deploy.sh --logs api-gateway

# Check status
./scripts/deploy.sh --status

# Show endpoints
./scripts/deploy.sh --endpoints
```

### Blue-Green Deployment (Advanced)

For zero-downtime deployments:

```bash
# Deploy to green environment
export COMPOSE_PROJECT_NAME=llm-green
docker-compose -f docker/docker-compose.yml up -d

# Verify green is healthy
./scripts/deploy.sh --health

# Switch traffic (update Traefik/Load Balancer)
# ... update load balancer config ...

# Stop blue environment
export COMPOSE_PROJECT_NAME=llm-blue
docker-compose -f docker/docker-compose.yml down
```

---

## Troubleshooting Guide

### Common Issues

#### 1. Service Won't Start

```bash
# Check logs
./scripts/deploy.sh --logs <service-name>

# Check container status
docker-compose -f docker/docker-compose.yml ps

# Check for port conflicts
netstat -tlnp | grep <port>
```

#### 2. Ollama Model Download Fails

```bash
# Manual model pull
docker exec llm-ollama ollama pull llama3.2

# Check Ollama logs
./scripts/deploy.sh --logs ollama
```

#### 3. ChromaDB Connection Issues

```bash
# Check ChromaDB health
curl http://localhost:8000/api/v1/heartbeat

# Restart ChromaDB
./scripts/deploy.sh --restart-service chromadb
```

#### 4. High Memory Usage

```bash
# Check memory usage
./scripts/benchmark.py --suite system

# Restart services
./scripts/deploy.sh --restart

# Check for memory leaks in logs
./scripts/deploy.sh --logs | grep -i "memory\|oom"
```

#### 5. Slow Query Performance

```bash
# Run performance benchmark
./scripts/benchmark.py --suite rag

# Check RAG service logs
./scripts/deploy.sh --logs rag-service

# Verify ChromaDB index
```

### Diagnostic Commands

```bash
# Check all service health
./scripts/deploy.sh --health

# View resource usage
docker stats

# Check network connectivity
docker network ls
docker network inspect llm-network

# Check volume usage
docker volume ls
docker system df -v
```

### Log Analysis

```bash
# Search for errors in all logs
./scripts/deploy.sh --logs | grep -i error

# Search for specific service errors
./scripts/deploy.sh --logs rag-service | grep -i error

# Follow logs in real-time
./scripts/deploy.sh --logs -f
```

---

## File Reference

### Configuration Files

| File | Purpose |
|------|---------|
| `docker/docker-compose.yml` | Main service orchestration |
| `docker/.env.example` | Environment variables template |
| `monitoring/prometheus/prometheus.yml` | Metrics collection config |
| `monitoring/prometheus/rules/llm-alerts.yml` | Alert rules |
| `monitoring/grafana/provisioning/datasources/datasources.yml` | Grafana data sources |
| `monitoring/grafana/provisioning/dashboards/dashboards.yml` | Dashboard provisioning |
| `monitoring/grafana/dashboards/llm-overview.json` | Main dashboard |
| `monitoring/loki/loki-config.yaml` | Log aggregation config |
| `monitoring/promtail/promtail-config.yaml` | Log shipping config |
| `monitoring/alertmanager/config.yml` | Alert routing config |

### Scripts

| File | Purpose |
|------|---------|
| `scripts/deploy.sh` | Deployment automation |
| `scripts/benchmark.py` | Performance benchmarking |
| `backup/backup.sh` | Backup and restore |
| `security/security-hardening.sh` | Security configuration |

### Tests

| File | Purpose |
|------|---------|
| `tests/test_unit.py` | Unit tests |
| `tests/test_integration.py` | Integration tests |
| `tests/load_test.py` | Load testing with Locust |

---

## Quick Start

### 1. Clone and Setup

```bash
cd /path/to/project
cp docker/.env.example docker/.env
# Edit docker/.env with your settings
```

### 2. Deploy

```bash
./scripts/deploy.sh --init
```

### 3. Verify

```bash
./scripts/deploy.sh --status
./scripts/deploy.sh --health
```

### 4. Access Services

- API: http://localhost:8080
- Grafana: http://localhost:3001 (admin/admin)
- Traefik: http://localhost:8080

### 5. Run Tests

```bash
pytest tests/test_integration.py -v
```

### 6. Setup Monitoring

```bash
# Import dashboards in Grafana
# Alerts will be active automatically
```

---

## Maintenance Schedule

| Task | Frequency | Command |
|------|-----------|---------|
| Health Check | Daily | `./scripts/deploy.sh --health` |
| Backup | Daily | `./backup/backup.sh` |
| Log Cleanup | Weekly | `docker system prune` |
| Security Check | Monthly | `./security/security-hardening.sh --check` |
| Update Services | As needed | `./scripts/deploy.sh --update` |
| Performance Test | Monthly | `./scripts/benchmark.py --suite all` |

---

## Support

For issues and questions:

1. Check the troubleshooting guide above
2. Review service logs: `./scripts/deploy.sh --logs`
3. Run health checks: `./scripts/deploy.sh --health`
4. Check monitoring dashboards in Grafana

---

*Document Version: 1.0*
*Last Updated: 2024*
