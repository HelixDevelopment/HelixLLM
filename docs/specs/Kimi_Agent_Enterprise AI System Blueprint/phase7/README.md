# Phase 7: Testing, Monitoring & Deployment

## Light Local LLM System - DevOps Implementation

This directory contains the complete DevOps implementation for the Light Local LLM System, including Docker configurations, monitoring stack, testing suite, backup procedures, and deployment scripts.

---

## Directory Structure

```
phase7/
├── docker/                     # Docker Compose configurations
│   ├── docker-compose.yml      # Main orchestration file
│   └── .env.example            # Environment variables template
│
├── monitoring/                 # Monitoring and logging stack
│   ├── prometheus/             # Metrics collection
│   │   ├── prometheus.yml      # Prometheus configuration
│   │   └── rules/              # Alert rules
│   ├── grafana/                # Visualization dashboards
│   │   ├── dashboards/         # Dashboard JSON files
│   │   └── provisioning/       # Auto-provisioning configs
│   ├── loki/                   # Log aggregation
│   ├── promtail/               # Log shipping
│   └── alertmanager/           # Alert routing
│
├── scripts/                    # Automation scripts
│   ├── deploy.sh               # Deployment automation
│   └── benchmark.py            # Performance benchmarking
│
├── tests/                      # Testing suite
│   ├── test_unit.py            # Unit tests
│   ├── test_integration.py     # Integration tests
│   └── load_test.py            # Load testing (Locust)
│
├── backup/                     # Backup and recovery
│   └── backup.sh               # Backup automation script
│
├── security/                   # Security hardening
│   └── security-hardening.sh   # Security configuration
│
├── Makefile                    # Convenience commands
└── README.md                   # This file
```

---

## Quick Start

### 1. Prerequisites

- Docker Engine 20.10+
- Docker Compose 2.0+
- Python 3.8+ (for testing)
- Make (optional, for convenience commands)

### 2. Initial Setup

```bash
# Clone or navigate to the project
cd phase7

# Copy environment template
cp docker/.env.example docker/.env

# Edit environment variables
nano docker/.env
```

### 3. Deploy

```bash
# Using the deployment script
./scripts/deploy.sh --init

# Or using Make
make init
```

### 4. Verify

```bash
# Check status
make status

# Run health checks
make health
```

---

## Service Endpoints

After deployment, services are available at:

| Service | URL | Description |
|---------|-----|-------------|
| API Gateway | http://localhost:8080 | Main API entry point |
| Grafana | http://localhost:3001 | Monitoring dashboards |
| Prometheus | http://localhost:9090 | Metrics collection |
| Ollama | http://localhost:11434 | LLM inference |
| ChromaDB | http://localhost:8000 | Vector database |
| RAG Service | http://localhost:8001 | Document retrieval |
| MCP Server | http://localhost:3000 | Tool integration |
| Traefik | http://localhost:8080 | Reverse proxy dashboard |

---

## Common Commands

### Deployment

```bash
make init          # Initial deployment
make start         # Start services
make stop          # Stop services
make restart       # Restart services
make update        # Update to latest
make rollback      # Rollback deployment
```

### Monitoring

```bash
make status        # Service status
make health        # Health checks
make logs          # View logs
make logs-rag      # RAG service logs
make logs-api      # API Gateway logs
```

### Maintenance

```bash
make backup        # Create backup
make backup-clean  # Clean old backups
make clean         # Clean Docker resources
make prune         # Deep clean (careful!)
```

### Testing

```bash
make test          # Run all tests
make test-unit     # Unit tests only
make test-int      # Integration tests
make benchmark     # Performance benchmarks
```

### Security

```bash
make security         # Apply security hardening
make security-check   # Run security audit
make security-certs   # Generate SSL certificates
```

---

## Configuration

### Environment Variables

Key variables in `docker/.env`:

| Variable | Description | Default |
|----------|-------------|---------|
| `DOMAIN` | Domain name | localhost |
| `OLLAMA_MODEL` | Default LLM model | llama3.2 |
| `JWT_SECRET` | API authentication secret | (generate) |
| `GRAFANA_ADMIN_PASSWORD` | Grafana admin password | admin |
| `ENABLE_AUTH` | Enable API authentication | false |

### Docker Compose Overrides

Create `docker/docker-compose.override.yml` for local customizations:

```yaml
version: '3.8'
services:
  ollama:
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

---

## Monitoring

### Grafana Dashboards

Pre-configured dashboards:

1. **LLM System Overview** - Main system metrics
2. **LLM Performance** - Inference statistics
3. **RAG Performance** - Query metrics
4. **API Gateway** - Request analytics
5. **MCP Tools** - Tool usage stats

### Alerts

Configured alerts for:

- Service availability
- Resource usage (CPU, Memory, Disk)
- LLM performance degradation
- RAG query latency
- API error rates

### Log Aggregation

Logs are aggregated in Loki and searchable in Grafana:

```
{job="rag-service"} |= "error"
{container_name="llm-ollama"} |= "model"
```

---

## Testing

### Unit Tests

```bash
python -m pytest tests/test_unit.py -v
```

### Integration Tests

```bash
python -m pytest tests/test_integration.py -v
```

### Load Testing

```bash
# Install locust
pip install locust

# Run load test
locust -f tests/load_test.py --host=http://localhost:8080
```

Then open http://localhost:8089 to configure and start the test.

---

## Backup & Recovery

### Create Backup

```bash
./backup/backup.sh
```

### Restore from Backup

```bash
./backup/backup.sh --restore ./backups/full/llm_backup_YYYYMMDD_chromadb.tar.gz
```

### Automated Backups

Add to crontab:

```bash
# Daily backup at 2 AM
0 2 * * * /path/to/phase7/backup/backup.sh
```

---

## Security

### Apply Security Hardening

```bash
./security/security-hardening.sh --apply
```

This configures:

- Firewall rules (UFW/iptables)
- Docker security settings
- SSL/TLS certificates
- Secret management
- fail2ban intrusion prevention

### Security Audit

```bash
./security/security-hardening.sh --check
```

---

## Troubleshooting

### Service Won't Start

```bash
# Check logs
make logs

# Check Docker status
docker-compose -f docker/docker-compose.yml ps

# Restart service
./scripts/deploy.sh --restart-service <service-name>
```

### High Resource Usage

```bash
# Check resource usage
docker stats

# Run benchmark
make benchmark
```

### Network Issues

```bash
# Check network
docker network ls
docker network inspect llm-network
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      MACHINE 1 (Primary)                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   Traefik   │  │ API Gateway │  │    RAG Service      │ │
│  │   (Proxy)   │──│  (Port 8080)│──│  (Document RAG)     │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
│         │                                                    │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              MONITORING STACK                        │  │
│  │  Prometheus │ Grafana │ Loki │ Alertmanager          │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
                   Internal Network
                              │
┌─────────────────────────────────────────────────────────────┐
│                      MACHINE 2 (Worker)                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │    Ollama   │  │   ChromaDB  │  │    MCP Server       │ │
│  │  (LLM)      │  │  (Vectors)  │  │   (Tools)           │ │
│  │  Port 11434 │  │  Port 8000  │  │   Port 3000         │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## Contributing

When adding new features:

1. Update relevant configuration files
2. Add tests in `tests/`
3. Update documentation
4. Test deployment with `make init`

---

## License

This project is part of the Light Local LLM System.

---

## Support

For issues:

1. Check logs: `make logs`
2. Run health check: `make health`
3. Review monitoring dashboards
4. Check troubleshooting section above
