# Troubleshooting Guide

## Light Local LLM System - Common Issues and Solutions

---

## Quick Diagnostic Commands

```bash
# Check all service status
make status

# Check health of all services
make health

# View recent logs
make logs

# Check Docker resources
docker stats --no-stream

# Check disk space
df -h

# Check memory usage
free -h
```

---

## Issue Categories

### 1. Service Won't Start

#### Symptom: Container keeps restarting

```bash
# Check container status
docker-compose -f docker/docker-compose.yml ps

# View container logs
docker-compose -f docker/docker-compose.yml logs <service-name>

# Check for port conflicts
netstat -tlnp | grep <port>
```

**Common Causes:**

| Cause | Solution |
|-------|----------|
| Port already in use | Change port in docker-compose.yml or stop conflicting service |
| Insufficient memory | Increase Docker memory limit or add RAM |
| Missing environment variables | Check .env file exists and is complete |
| Dependency not ready | Check dependent services are healthy |
| Image pull failed | Run `docker-compose pull` manually |

**Fix Steps:**

```bash
# 1. Stop all services
make stop

# 2. Check for port conflicts
sudo lsof -i :8080
sudo lsof -i :11434
sudo lsof -i :8000

# 3. Kill conflicting processes if safe
sudo kill -9 <PID>

# 4. Pull latest images
docker-compose -f docker/docker-compose.yml pull

# 5. Start services
make start

# 6. Check status
make status
```

---

### 2. Ollama Issues

#### Symptom: Model download fails

```bash
# Check Ollama logs
make logs-ollama

# Manual model pull
docker exec llm-ollama ollama pull llama3.2

# Check available models
docker exec llm-ollama ollama list
```

**Solutions:**

```bash
# 1. Check internet connectivity from container
docker exec llm-ollama ping -c 3 google.com

# 2. Restart Ollama
make restart-service ollama

# 3. Pull model manually with verbose output
docker exec llm-ollama ollama pull llama3.2 --verbose

# 4. Check disk space
df -h

# 5. If disk full, clean up
make prune
```

#### Symptom: LLM inference is very slow

**Check GPU availability:**

```bash
# Check if GPU is detected
docker exec llm-ollama nvidia-smi

# Check Ollama logs for GPU messages
make logs-ollama | grep -i gpu
```

**Solutions:**

```bash
# 1. Verify GPU support is enabled
cat docker/.env | grep CUDA

# 2. Check NVIDIA Docker runtime
docker info | grep -i nvidia

# 3. Use smaller model
# Edit docker/.env:
# OLLAMA_MODEL=llama3.2:1b

# 4. Restart Ollama
make restart-service ollama
```

---

### 3. ChromaDB Issues

#### Symptom: Cannot connect to ChromaDB

```bash
# Check ChromaDB health
curl http://localhost:8000/api/v1/heartbeat

# Check ChromaDB logs
make logs-chroma
```

**Solutions:**

```bash
# 1. Restart ChromaDB
make restart-service chromadb

# 2. Check volume permissions
docker volume inspect llm_chroma-data

# 3. Verify network connectivity
docker network inspect llm-network

# 4. Check for corruption
# Backup and recreate volume if needed
docker volume rm llm_chroma-data  # WARNING: Data loss!
```

#### Symptom: Vector search returns no results

```bash
# Check collection exists
curl http://localhost:8000/api/v1/collections

# Check document count
curl http://localhost:8000/api/v1/collections/documents/count
```

**Solutions:**

```bash
# 1. Verify documents are indexed
# Check RAG service logs
make logs-rag | grep -i index

# 2. Reindex documents if needed
# Use RAG service API to reindex

# 3. Check embedding model is working
```

---

### 4. RAG Service Issues

#### Symptom: RAG queries timeout

```bash
# Check RAG service logs
make logs-rag

# Check ChromaDB connection from RAG
docker exec llm-rag-service curl http://chromadb:8000/api/v1/heartbeat
```

**Solutions:**

```bash
# 1. Check ChromaDB is healthy
make health

# 2. Restart RAG service
make restart-service rag-service

# 3. Check embedding model download
make logs-rag | grep -i embedding

# 4. Increase timeout in client
# Update request timeout to 60+ seconds
```

#### Symptom: Low retrieval quality

```bash
# Check retrieval scores in logs
make logs-rag | grep -i score
```

**Solutions:**

```bash
# 1. Adjust top_k parameter
# Increase from 5 to 10

# 2. Check chunk size
# Smaller chunks (256) for precise retrieval

# 3. Verify documents are relevant
# Review indexed documents

# 4. Reindex with better embeddings
# Use larger embedding model
```

---

### 5. API Gateway Issues

#### Symptom: API returns 500 errors

```bash
# Check API Gateway logs
make logs-api

# Test health endpoint
curl http://localhost:8080/health
```

**Solutions:**

```bash
# 1. Check backend services
make health

# 2. Restart API Gateway
make restart-service api-gateway

# 3. Check rate limiting
make logs-api | grep -i rate

# 4. Verify JWT secret is set
cat docker/.env | grep JWT_SECRET
```

#### Symptom: Rate limit exceeded

```bash
# Check rate limit configuration
cat docker/.env | grep RATE_LIMIT
```

**Solutions:**

```bash
# 1. Increase rate limits in .env
# RATE_LIMIT_REQUESTS=200
# RATE_LIMIT_WINDOW=60

# 2. Restart API Gateway
make restart-service api-gateway

# 3. Implement client-side rate limiting
```

---

### 6. High Resource Usage

#### Symptom: System is slow/unresponsive

```bash
# Check resource usage
top
htop

# Check Docker stats
docker stats --no-stream

# Check memory
free -h

# Check disk
df -h
```

**Solutions:**

```bash
# 1. Identify high resource container
docker stats --no-stream --format "table {{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}"

# 2. Restart problematic service
make restart-service <service-name>

# 3. Scale down if needed
# Edit docker-compose.yml to reduce replicas

# 4. Clean up Docker
docker system prune -f
docker volume prune -f

# 5. Add more resources
# Increase Docker Desktop memory limit
# Or add RAM to host
```

#### Symptom: Out of memory errors

```bash
# Check OOM events
dmesg | grep -i "out of memory"

# Check container restarts
docker-compose ps | grep Restarting
```

**Solutions:**

```bash
# 1. Add swap space
sudo fallocate -l 4G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile

# 2. Reduce Ollama memory usage
# In docker-compose.yml, add:
# deploy:
#   resources:
#     limits:
#       memory: 4G

# 3. Use smaller model
# OLLAMA_MODEL=llama3.2:1b

# 4. Restart services
make restart
```

---

### 7. Network Issues

#### Symptom: Services cannot communicate

```bash
# Check network
docker network ls
docker network inspect llm-network

# Test connectivity between containers
docker exec llm-api-gateway ping -c 3 chromadb
docker exec llm-rag-service ping -c 3 ollama
```

**Solutions:**

```bash
# 1. Recreate network
docker network rm llm-network
docker-compose up -d

# 2. Check firewall rules
sudo ufw status
sudo iptables -L

# 3. Verify service names in docker-compose.yml
# Services must use container names for DNS resolution

# 4. Restart all services
make restart
```

---

### 8. Monitoring Issues

#### Symptom: No metrics in Grafana

```bash
# Check Prometheus targets
curl http://localhost:9090/api/v1/targets

# Check Prometheus logs
docker-compose logs prometheus
```

**Solutions:**

```bash
# 1. Check Prometheus configuration
# Verify prometheus.yml is valid

# 2. Restart Prometheus
make restart-service prometheus

# 3. Check Grafana data source
curl http://localhost:3001/api/datasources

# 4. Verify network connectivity
docker exec llm-prometheus wget -qO- http://rag-service:9090/metrics
```

#### Symptom: No logs in Loki

```bash
# Check Promtail status
docker-compose logs promtail

# Check Loki status
curl http://localhost:3100/ready
```

**Solutions:**

```bash
# 1. Restart Promtail
make restart-service promtail

# 2. Check log file permissions
ls -la /var/log/

# 3. Verify Promtail configuration
# Check promtail-config.yml

# 4. Restart Loki
make restart-service loki
```

---

### 9. Backup Issues

#### Symptom: Backup fails

```bash
# Run backup with verbose output
./backup/backup.sh 2>&1 | tee backup.log

# Check disk space
df -h
```

**Solutions:**

```bash
# 1. Ensure backup directory exists
mkdir -p backups/full backups/config backups/logs

# 2. Check permissions
chmod +x backup/backup.sh

# 3. Run config-only backup first
./backup/backup.sh --config-only

# 4. Check container is running
docker-compose ps | grep chromadb
```

---

### 10. Security Issues

#### Symptom: Unauthorized access attempts

```bash
# Check API Gateway logs for auth failures
make logs-api | grep -i "unauthorized\|forbidden"

# Check fail2ban status
sudo fail2ban-client status
```

**Solutions:**

```bash
# 1. Enable authentication
# Edit docker/.env:
# ENABLE_AUTH=true

# 2. Generate strong JWT secret
openssl rand -hex 32

# 3. Restart API Gateway
make restart-service api-gateway

# 4. Check firewall rules
sudo ufw status verbose

# 5. Review access logs
make logs-traefik | grep -i "access"
```

---

## Recovery Procedures

### Complete System Recovery

```bash
# 1. Stop all services
make stop

# 2. Backup current state (if possible)
./backup/backup.sh --config-only

# 3. Clean up Docker
docker system prune -a -f --volumes

# 4. Reinitialize
make init

# 5. Restore data if backup exists
./backup/backup.sh --restore <backup-file>

# 6. Verify
make health
```

### Partial Service Recovery

```bash
# 1. Identify failed service
make status

# 2. Check logs
make logs <service-name>

# 3. Restart service
make restart-service <service-name>

# 4. Verify
make health
```

### Database Recovery

```bash
# 1. Stop dependent services
make stop

# 2. Remove corrupted volume
# WARNING: Data loss if no backup!
docker volume rm llm_chroma-data

# 3. Recreate volume
docker volume create llm_chroma-data

# 4. Restore from backup
./backup/backup.sh --restore <chromadb-backup>

# 5. Start services
make start
```

---

## Performance Tuning

### Optimize LLM Performance

```bash
# 1. Use GPU acceleration
# Set in docker/.env:
# CUDA_VISIBLE_DEVICES=0

# 2. Use quantized models
# OLLAMA_MODEL=llama3.2:q4_0

# 3. Increase context window
# In Ollama options:
# "num_ctx": 4096

# 4. Enable model caching
# OLLAMA_KEEP_ALIVE=24h
```

### Optimize RAG Performance

```bash
# 1. Use GPU for embeddings
# EMBEDDING_DEVICE=cuda

# 2. Adjust chunk size
# Smaller chunks = faster retrieval
# RAG_CHUNK_SIZE=256

# 3. Reduce top_k
# RAG_TOP_K=3

# 4. Enable caching
# Add Redis cache configuration
```

### Optimize System Performance

```bash
# 1. Increase Docker resources
# Docker Desktop > Settings > Resources

# 2. Use SSD for data volumes
# Mount volumes on fast storage

# 3. Enable swap
sudo swapon -a

# 4. Tune kernel parameters
# /etc/sysctl.conf:
# vm.swappiness=10
# vm.dirty_ratio=40
```

---

## Getting Help

If issues persist:

1. **Collect diagnostic information:**
```bash
# System info
uname -a
docker version
docker-compose version

# Service status
make status > diagnostic.txt
make health >> diagnostic.txt

# Recent logs
make logs >> diagnostic.txt 2>&1

# Resource usage
docker stats --no-stream >> diagnostic.txt
free -h >> diagnostic.txt
df -h >> diagnostic.txt
```

2. **Check documentation:**
- README.md - General information
- docs/architecture.md - System design
- This troubleshooting guide

3. **Review monitoring:**
- Grafana dashboards
- Prometheus alerts
- Service logs

4. **Common fixes to try:**
- Restart services: `make restart`
- Clean Docker: `make prune`
- Update images: `docker-compose pull`
- Reinitialize: `make init`
