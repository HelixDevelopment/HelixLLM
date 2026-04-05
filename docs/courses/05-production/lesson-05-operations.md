# Lesson 5: Operations

**Duration:** 25 minutes
**Prerequisites:** Lessons 1-4 (Containerization, Multi-Host, Monitoring, Security)
**Learning Objectives:**
- Scale HelixLLM horizontally by adding hosts and vertically by upgrading hardware
- Implement backup and disaster recovery procedures for all data stores
- Perform graceful upgrades and rolling deployments
- Troubleshoot common production issues

---

## Scene 1: Scaling Strategies (5 min)

**Narration:** "HelixLLM supports both vertical and horizontal scaling. Vertical scaling means upgrading individual host hardware. Horizontal scaling means adding more hosts to the cluster."

**Screen:** Show scaling options.

**Vertical Scaling:**

| Resource | Impact |
|----------|--------|
| More RAM | Support larger models and more concurrent sessions |
| Better GPU | Faster inference (tokens per second) |
| More CPU cores | Higher concurrent throughput |
| Faster storage | Faster model loading and vector search |

**Horizontal Scaling:**

```bash
# Add a new host to the cluster
# 1. Set up SSH access
ssh-copy-id -i ~/.ssh/id_ed25519 milosvasic@newhost.local

# 2. Update HELIX_HOSTS
HELIX_HOSTS=nezha.local,thinker.local,amber.local,newhost.local

# 3. Probe and rebalance
curl -sk -X POST https://localhost:8443/internal/cluster/probe
curl -sk -X POST https://localhost:8443/internal/cluster/rebalance
```

**Narration:** "Gateway scaling deserves special mention. Run multiple gateway instances behind a load balancer, each connecting to the same Brain and Knowledge services. This distributes HTTP traffic across instances."

**Key points:**
- Vertical: more RAM, better GPU, faster storage
- Horizontal: add hosts to `HELIX_HOSTS`, probe, rebalance
- Gateway: multiple instances behind a load balancer
- Brain: GPU-heavy, scale vertically with better GPUs
- Knowledge: memory-heavy, scale with more RAM or distributed vector DB

---

## Scene 2: Backup and Recovery (6 min)

**Narration:** "A production deployment has several stateful components that need regular backups: configuration, TLS certificates, the PostgreSQL database, and the vector store."

**Demo steps:**

```bash
# 1. Back up configuration and certificates
cp .env .env.backup.$(date +%Y%m%d)
cp -r certs/ certs.backup.$(date +%Y%m%d)/

# 2. Back up PostgreSQL
pg_dump -h localhost -U helix helixllm > helixllm-backup-$(date +%Y%m%d).sql

# 3. Back up Qdrant (snapshot)
curl -X POST http://localhost:6333/collections/default/snapshots

# 4. Back up Redis (if persistence is enabled)
podman exec redis redis-cli BGSAVE
```

**Narration:** "For disaster recovery, restore in reverse order."

```bash
# Restore PostgreSQL
psql -h localhost -U helix helixllm < helixllm-backup-20260405.sql

# Restore configuration
cp .env.backup.20260405 .env
cp -r certs.backup.20260405/ certs/

# Restart services
make dev
```

**Narration:** "For the knowledge base, keep source documents available so you can re-ingest if the vector store is lost. Re-ingestion is a valid recovery strategy."

**Key points:**
- Back up: `.env`, `certs/`, PostgreSQL, vector store snapshots
- PostgreSQL: `pg_dump` and `psql` for backup and restore
- Qdrant: snapshot API for point-in-time backups
- Knowledge base: re-ingest from source documents if needed
- Automate backups with cron jobs in production

---

## Scene 3: Graceful Upgrades (5 min)

**Narration:** "HelixLLM handles SIGTERM and SIGINT for graceful shutdown. In-flight requests complete before the process exits. This enables zero-downtime upgrades."

**Demo steps:**

```bash
# Single-host upgrade procedure
# 1. Build the new version
make build

# 2. Send SIGTERM to the running process (graceful shutdown)
kill -SIGTERM $(pgrep helixllm)

# 3. Wait for graceful shutdown (in-flight requests complete)

# 4. Start the new version
./bin/helixllm
```

**Narration:** "For multi-host clusters, perform a rolling upgrade -- one host at a time."

```bash
# Rolling upgrade for multi-host
# 1. Probe to confirm current cluster health
curl -sk -X POST https://localhost:8443/internal/cluster/probe

# 2. Upgrade Worker 1: stop, deploy new image, verify
ssh milosvasic@thinker.local "podman stop helixllm && podman pull helixllm:v2.0 && podman run -d ..."
curl -sk https://localhost:8443/internal/health

# 3. Upgrade Worker 2: same process
ssh milosvasic@amber.local "podman stop helixllm && podman pull helixllm:v2.0 && podman run -d ..."
curl -sk https://localhost:8443/internal/health

# 4. Upgrade Primary last
# ... same process

# 5. Rebalance after all hosts are updated
curl -sk -X POST https://localhost:8443/internal/cluster/rebalance
```

**Key points:**
- SIGTERM triggers graceful shutdown -- in-flight requests complete
- Single-host: stop, replace binary, start
- Multi-host: rolling upgrade one host at a time
- Verify health after each host upgrade before proceeding
- Rebalance after all hosts are updated

---

## Scene 4: Resource Requirements (4 min)

**Narration:** "Let me cover the hardware requirements so you can plan your deployment."

**Screen:** Show the resource tables.

**Minimum (Single Host, Local Inference):**

| Resource | Requirement |
|----------|-------------|
| CPU | 8 cores |
| RAM | 32 GB |
| Disk | 50 GB (for models) |
| GPU | Recommended (NVIDIA, Apple Silicon, or AMD) |

**Recommended (Multi-Host Production):**

| Host Role | CPU | RAM | GPU | Disk |
|-----------|-----|-----|-----|------|
| Gateway + Control | 4 cores | 8 GB | None | 20 GB |
| Brain (Inference) | 16 cores | 64 GB | NVIDIA A100/RTX 4090 | 100 GB |
| Knowledge + Agents | 8 cores | 32 GB | None | 50 GB |

**External Services:**

| Service | Memory | Disk |
|---------|--------|------|
| PostgreSQL | 1 GB | 10 GB |
| Redis | 512 MB | 1 GB |
| Qdrant | 4-8 GB | 20 GB |
| Kafka | 2 GB | 10 GB |

**Key points:**
- GGUF models are large: 5 GB (8B) to 70 GB (70B Q4)
- GPU VRAM determines the largest model you can run
- Vector databases need RAM proportional to the number of vectors
- Plan storage for model files, vector data, and PostgreSQL

---

## Scene 5: Troubleshooting (5 min)

**Narration:** "Let me walk through the most common production issues and how to diagnose them."

**Screen:** Show the troubleshooting guide.

| Symptom | Likely Cause | Diagnosis | Fix |
|---------|-------------|-----------|-----|
| 503 on health check | Subsystem unhealthy | Check `/internal/health` response | Restart failed service |
| Slow responses | LLM inference bottleneck | Check `llm_request_duration_seconds` | Upgrade GPU, reduce model size |
| 429 Too Many Requests | Rate limit hit | Check rate limit config | Increase limit or add gateway instances |
| Connection refused | Server not running | Check process and port | Start server, check TLS config |
| SSH probe fails | Key not authorized | Test with `ssh -v` | Fix authorized_keys |
| Empty model list | No providers configured | Check API keys in .env | Set provider keys |

**Demo steps:**

```bash
# Diagnostic commands
# Check health with details
curl -sk https://localhost:8443/internal/health | python3 -m json.tool

# Check cluster status
curl -sk https://localhost:8443/internal/cluster/status | python3 -m json.tool

# Check logs for errors
# HELIX_LOG_FORMAT=json HELIX_LOG_LEVEL=debug ./bin/helixllm 2>&1 | grep '"level":"error"'

# Test SSH connectivity
ssh -v -i ~/.ssh/id_ed25519 milosvasic@thinker.local "hostname"

# Verify container runtime
podman ps -a
```

**Key points:**
- Always start diagnosis with the health endpoint
- Check logs with debug level for detailed information
- Use the cluster status endpoint for multi-host issues
- Test SSH connectivity independently of HelixLLM
- Monitor metrics dashboards for gradual degradation

---

## Exercises

1. Practice the single-host upgrade procedure: build a new binary, gracefully stop the running server, and start the new version -- verify no requests are dropped during the transition
2. Set up automated PostgreSQL backups with a cron job that runs `pg_dump` daily and retains the last 7 backups
3. Simulate a common production issue (e.g., stop Qdrant while HelixLLM is running) and observe the health endpoint response, then restart the service and verify recovery
