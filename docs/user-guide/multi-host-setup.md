# Multi-Host Setup

HelixLLM can distribute its subsystems across multiple hosts for production workloads. The control plane probes hosts via SSH, benchmarks their capabilities, and schedules services based on configurable strategies.

## Prerequisites

- All hosts must be reachable from the primary host via SSH
- Key-based SSH authentication (no password prompts)
- Podman or Docker installed on each host
- Hostnames resolvable (DNS or `/etc/hosts`)

## SSH Setup

HelixLLM uses key-based SSH with no password. Set up access from your primary host to each worker:

### 1. Generate an SSH key (if you don't have one)

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ""
```

### 2. Copy the key to each worker host

```bash
ssh-copy-id -i ~/.ssh/id_ed25519 milosvasic@thinker.local
ssh-copy-id -i ~/.ssh/id_ed25519 milosvasic@amber.local
```

### 3. Verify passwordless access

```bash
ssh milosvasic@thinker.local "hostname"
ssh milosvasic@amber.local "hostname"
```

Both commands should print the hostname without prompting for a password.

## Host Configuration

Configure the cluster in `.env`:

```bash
HELIX_HOSTS=nezha.local,thinker.local,amber.local
HELIX_SSH_USER=milosvasic
HELIX_SSH_KEY=~/.ssh/id_ed25519
```

Each host is probed for:
- OS type (Linux, macOS)
- CPU core count
- Available memory
- GPU presence and type (NVIDIA/CUDA, Apple Metal, AMD/ROCm)
- Disk space
- Container runtime (Podman or Docker)
- Network interfaces

## Host Topology Example

| Host | DNS | Role | Hardware |
|------|-----|------|----------|
| Primary | nezha.local | Control plane, gateway | 16 cores, 64GB RAM, RTX 4090 |
| Worker 1 | thinker.local | Brain (LLM inference) | 32 cores, 128GB RAM, A100 |
| Worker 2 | amber.local | Knowledge, agents | 8 cores, 32GB RAM, Apple M3 |

## Scheduling Strategies

Set the strategy in `.env`:

```bash
HELIX_SCHEDULE_STRATEGY=auto
```

| Strategy | Best For | Behavior |
|----------|----------|----------|
| `auto` | General use | Selects the best strategy per service based on its resource profile |
| `binpack` | Low-resource services | Packs onto fewest hosts to minimize resource waste |
| `spread` | High availability | Distributes across all hosts for redundancy |
| `gpu-affinity` | LLM inference | Places on the host with the best GPU |
| `memory-first` | Vector databases, RAG | Places on the host with the most available RAM |
| `latency` | Gateways, caches | Places near clients for lowest latency |

With `auto`, the control plane evaluates each service's requirements and picks the right strategy. For example, llama.cpp gets `gpu-affinity` while Redis gets `latency`.

## Probing Hosts

After configuring hosts, probe them to build capability profiles:

```bash
curl -k -X POST https://localhost:8443/internal/cluster/probe
```

This SSH-connects to each host and collects hardware and software information. The response contains a `HostProfile` for each host.

## Deploying Services

Deploy services to the cluster:

```bash
curl -k -X POST https://localhost:8443/internal/cluster/deploy \
  -H "Content-Type: application/json" \
  -d '{
    "services": [
      {
        "name": "llama-cpp",
        "image": "ghcr.io/ggml-org/llama.cpp:server-cuda",
        "requires_gpu": true,
        "memory_mb": 16384
      },
      {
        "name": "qdrant",
        "image": "qdrant/qdrant",
        "memory_mb": 8192
      },
      {
        "name": "redis",
        "image": "redis:7-alpine",
        "memory_mb": 512
      }
    ]
  }'
```

The control plane:
1. Reads the current host profiles (probes first if none exist)
2. Runs the scheduler to compute placements
3. Deploys containers to the assigned hosts via SSH

## Checking Cluster Status

```bash
curl -k https://localhost:8443/internal/cluster/status
```

Returns host health, deployment status, and service placements.

## Rebalancing

When host conditions change (new hardware, degraded performance, host failure), trigger a rebalance:

```bash
curl -k -X POST https://localhost:8443/internal/cluster/rebalance
```

This re-evaluates all current deployments against the latest host profiles and moves services if a better placement exists.

## External Services

The control plane manages these containerized services:

| Service | Image | Default Strategy |
|---------|-------|-----------------|
| llama.cpp RPC | `ghcr.io/ggml-org/llama.cpp:server-cuda` | gpu-affinity |
| Qdrant | `qdrant/qdrant` | memory-first |
| PostgreSQL | `postgres:16-alpine` | binpack |
| Redis | `redis:7-alpine` | latency |
| Kafka | `bitnami/kafka` | spread |
| Prometheus | `prom/prometheus` | binpack |
| Grafana | `grafana/grafana` | binpack |

## Container Runtime

HelixLLM prefers Podman (rootless, daemonless) over Docker. The runtime is auto-detected:

```bash
HELIX_CONTAINER_RUNTIME=auto    # Checks for podman first, then docker
HELIX_CONTAINER_RUNTIME=podman  # Force Podman
HELIX_CONTAINER_RUNTIME=docker  # Force Docker
```

## Troubleshooting Multi-Host

**SSH connection fails:**
- Verify the key path: `ssh -i ~/.ssh/id_ed25519 user@host`
- Check permissions: `chmod 600 ~/.ssh/id_ed25519`
- Ensure the public key is in `~/.ssh/authorized_keys` on the remote host

**Host shows as unreachable:**
- Confirm hostname resolves: `ping thinker.local`
- Check SSH service is running on the remote host
- Review firewall rules

**Container deployment fails:**
- Verify container runtime is installed on the remote host: `ssh user@host "podman --version"`
- Check disk space on the remote host
- Review container logs: `ssh user@host "podman logs <container-id>"`
