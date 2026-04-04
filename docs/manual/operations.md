# Operations Guide

Deploying, managing, and maintaining HelixLLM in production.

## Deployment Options

### Single Host (Full Mode)

The simplest deployment. One process runs all subsystems:

```bash
HELIX_MODE=full ./bin/helixllm
```

Suitable for:
- Development and testing
- Small teams with moderate load
- Single-machine inference with local models

### Multi-Host (Distributed)

For production workloads, distribute subsystems across hosts:

1. **Primary host (nezha.local):** Control plane + gateway
2. **GPU host (thinker.local):** Brain (LLM inference)
3. **Storage host (amber.local):** Knowledge + agents

See [Multi-Host Setup](../user-guide/multi-host-setup.md) for full instructions.

## Container Deployment

### Building the Image

```bash
make container
```

The Containerfile uses multi-stage builds:
- Builder stage with full Go toolchain
- Runtime stage with minimal footprint (Alpine/distroless)

### Running as a Container

```bash
podman run -d \
  --name helixllm \
  -p 8443:8443 \
  -v ./certs:/app/certs:ro \
  -v ./.env:/app/.env:ro \
  helixllm:dev
```

### Compose Deployment

For the full stack with all external services:

```bash
podman compose -f deploy/compose.yaml up -d
```

Development overrides:

```bash
podman compose -f deploy/compose.yaml -f deploy/compose.dev.yaml up -d
```

## Cluster Management

### Probing Hosts

Before deploying, probe all hosts to detect capabilities:

```bash
curl -k -X POST https://localhost:8443/internal/cluster/probe
```

This returns each host's CPU, memory, GPU, OS, and container runtime.

### Deploying Services

Deploy the service stack to the cluster:

```bash
curl -k -X POST https://localhost:8443/internal/cluster/deploy \
  -H "Content-Type: application/json" \
  -d '{
    "services": [
      {"name": "llama-cpp", "image": "ghcr.io/ggml-org/llama.cpp:server-cuda", "requires_gpu": true, "memory_mb": 16384},
      {"name": "qdrant", "image": "qdrant/qdrant", "memory_mb": 8192},
      {"name": "postgres", "image": "postgres:16-alpine", "memory_mb": 1024},
      {"name": "redis", "image": "redis:7-alpine", "memory_mb": 512},
      {"name": "kafka", "image": "bitnami/kafka", "memory_mb": 2048}
    ]
  }'
```

### Checking Status

```bash
curl -k https://localhost:8443/internal/cluster/status
```

### Rebalancing

When host conditions change, rebalance service placement:

```bash
curl -k -X POST https://localhost:8443/internal/cluster/rebalance
```

## Monitoring

### Health Checks

Poll the health endpoint for automated monitoring:

```bash
curl -k https://localhost:8443/internal/health
```

Returns HTTP 200 when healthy, HTTP 503 when unhealthy.

### Prometheus

Configure Prometheus to scrape HelixLLM metrics. The control plane deploys Prometheus as a container with the `binpack` strategy.

### Grafana

Access dashboards at `http://<host>:3001`. The control plane deploys Grafana alongside Prometheus.

### Log Aggregation

With `HELIX_LOG_FORMAT=json`, logs are structured and ready for aggregation:

```bash
HELIX_LOG_FORMAT=json HELIX_LOG_LEVEL=info ./bin/helixllm
```

Loki is deployed by the control plane for centralized log collection.

## Auto-Remediation

The control plane monitor provides automatic recovery:

| Failure | Response |
|---------|----------|
| Container crashed | Restart on the same host |
| Host unreachable | Reschedule services to surviving hosts |
| Performance degraded | Trigger rebalancing |

Monitoring runs on a 30-second interval by default.

## Scaling

### Vertical Scaling

- Add more RAM to support larger models
- Add or upgrade GPUs for faster inference
- Increase CPU cores for higher concurrent throughput

### Horizontal Scaling

- Add more hosts to `HELIX_HOSTS`
- Run `probe` to detect the new host
- Run `deploy` or `rebalance` to distribute load

### Gateway Scaling

Run multiple gateway instances behind a load balancer. Each gateway connects to the same Brain and Knowledge services.

## Backup and Recovery

### Configuration

Back up your `.env` file and TLS certificates:

```bash
cp .env .env.backup
cp -r certs/ certs.backup/
```

### Database

PostgreSQL holds metadata and task queue data:

```bash
pg_dump -h localhost -U helix helixllm > backup.sql
```

Restore:

```bash
psql -h localhost -U helix helixllm < backup.sql
```

### Vector Store

Qdrant supports snapshots:

```bash
curl -X POST http://localhost:6333/collections/default/snapshots
```

### Knowledge Base

Re-ingest documents if the vector store is lost. Keep source documents available for re-ingestion.

## Upgrades

### Binary Upgrade

1. Build the new binary: `make build`
2. Stop the running instance (SIGTERM for graceful shutdown)
3. Replace the binary
4. Start the new instance

The server handles SIGINT and SIGTERM for graceful shutdown -- in-flight requests complete before exit.

### Rolling Upgrade (Multi-Host)

1. Probe hosts: `curl -k -X POST .../internal/cluster/probe`
2. Deploy updated containers one host at a time
3. Verify health after each host
4. Rebalance if needed

### Submodule Updates

```bash
make deps
make test-all
make build
```

## Resource Requirements

### Minimum (Single Host, Local Inference)

- 8 CPU cores
- 32 GB RAM
- 50 GB disk (for models)
- GPU recommended (NVIDIA, Apple Silicon, or AMD)

### Recommended (Multi-Host Production)

| Host Role | CPU | RAM | GPU | Disk |
|-----------|-----|-----|-----|------|
| Gateway + Control | 4 cores | 8 GB | None | 20 GB |
| Brain (Inference) | 16 cores | 64 GB | NVIDIA A100/RTX 4090 | 100 GB |
| Knowledge + Agents | 8 cores | 32 GB | None | 50 GB |

### External Services

| Service | Memory | Disk |
|---------|--------|------|
| PostgreSQL | 1 GB | 10 GB |
| Redis | 512 MB | 1 GB |
| Qdrant | 4-8 GB | 20 GB |
| Kafka | 2 GB | 10 GB |

## Maintenance Tasks

### Rotate TLS Certificates

```bash
rm -rf certs/
make certs
# Restart the server
```

### Rotate API Keys

Update `HELIX_AUTH_API_KEYS` in `.env` and restart. Distribute new keys to clients.

### Clean Up

```bash
make clean    # Remove build artifacts, coverage files, certs
```

### Check Disk Usage

Monitor model storage and vector database disk usage. Large GGUF models can be 40-70 GB each.
