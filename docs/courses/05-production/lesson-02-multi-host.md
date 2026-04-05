# Lesson 2: Multi-Host Deployment

**Duration:** 30 minutes
**Prerequisites:** Lesson 1 (Containerization)
**Learning Objectives:**
- Set up SSH key-based authentication between cluster hosts
- Configure the control plane with host definitions and scheduling strategies
- Probe hosts to discover hardware capabilities
- Deploy and manage services across multiple machines

---

## Scene 1: Multi-Host Architecture (5 min)

**Narration:** "In a multi-host deployment, HelixLLM distributes its layers across separate machines. The control plane runs on a primary host and manages workers via SSH. Each worker runs containers for the services assigned to it by the scheduler."

**Screen:** Show the multi-host topology.

```
Primary Host (nezha.local)
  - Control Plane
  - Gateway
  - Manages cluster via SSH
       |
       +--- Worker 1 (thinker.local)
       |      - Brain (llama.cpp with GPU)
       |      - 32 cores, 128 GB RAM, A100 GPU
       |
       +--- Worker 2 (amber.local)
              - Knowledge (Qdrant)
              - Agents
              - 8 cores, 32 GB RAM
```

**Key points:**
- One primary host runs the control plane and gateway
- Worker hosts run specific services (brain, knowledge, agents)
- All communication between the control plane and workers uses SSH
- Each host needs Podman or Docker installed
- Hosts must be reachable by hostname (DNS or /etc/hosts)

---

## Scene 2: SSH Setup (6 min)

**Narration:** "The control plane connects to worker hosts via SSH with key-based authentication. No passwords -- everything must be automated."

**Demo steps:**

```bash
# Step 1: Generate an Ed25519 SSH key (if you do not have one)
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ""

# Step 2: Copy the key to each worker host
ssh-copy-id -i ~/.ssh/id_ed25519 milosvasic@thinker.local
ssh-copy-id -i ~/.ssh/id_ed25519 milosvasic@amber.local

# Step 3: Verify passwordless access
ssh milosvasic@thinker.local "hostname && uname -m"
ssh milosvasic@amber.local "hostname && uname -m"
```

**Narration:** "Both commands should print the hostname without any password prompt. If you see a password prompt, check that the public key was copied correctly and that the SSH service allows key-based authentication."

```bash
# Verify container runtime is available on workers
ssh milosvasic@thinker.local "podman --version || docker --version"
ssh milosvasic@amber.local "podman --version || docker --version"
```

**Key points:**
- Ed25519 keys are recommended (fast and secure)
- The SSH user needs permission to run container commands
- Verify passwordless access before configuring HelixLLM
- Ensure Podman or Docker is installed on every worker
- Key permissions must be correct: `chmod 600 ~/.ssh/id_ed25519`

---

## Scene 3: Cluster Configuration (5 min)

**Narration:** "Configure the cluster by setting host and SSH variables in your .env file."

**Demo steps:**

```bash
# Configure cluster in .env
HELIX_MODE=full
HELIX_HOSTS=nezha.local,thinker.local,amber.local
HELIX_SSH_USER=milosvasic
HELIX_SSH_KEY=~/.ssh/id_ed25519
HELIX_SCHEDULE_STRATEGY=auto
HELIX_CONTAINER_RUNTIME=auto
```

**Narration:** "HELIX_HOSTS is a comma-separated list of all hosts in the cluster, including the primary. The SSH user and key are used for all remote connections. The schedule strategy controls how services are placed on hosts."

**Screen:** Show the scheduling strategies table.

| Strategy | Behavior | Best For |
|----------|----------|----------|
| `auto` | Selects per-service based on requirements | General use (recommended) |
| `binpack` | Pack onto fewest hosts | Saving resources |
| `spread` | Distribute across all hosts | High availability |
| `gpu-affinity` | Place on host with best GPU | LLM inference |
| `memory-first` | Place on host with most RAM | Vector databases |
| `latency` | Place near clients | Gateways, caches |

**Narration:** "The auto strategy is recommended for most deployments. It evaluates each service's requirements -- GPU needs, memory, latency sensitivity -- and selects the best strategy automatically."

**Key points:**
- `HELIX_HOSTS` lists all cluster members
- `HELIX_SSH_USER` and `HELIX_SSH_KEY` for authentication
- `HELIX_SCHEDULE_STRATEGY=auto` is the recommended default
- The container runtime is auto-detected on each host

---

## Scene 4: Probing and Deploying (8 min)

**Narration:** "With SSH configured, the first step is probing hosts to discover their capabilities. Then you deploy services and the scheduler places them optimally."

**Demo steps:**

```bash
# Start HelixLLM
make dev

# Probe all configured hosts
curl -sk -X POST https://localhost:8443/internal/cluster/probe | python3 -m json.tool
```

**Expected response:**

```json
{
  "hosts": [
    {
      "hostname": "nezha.local",
      "reachable": true,
      "os": "linux",
      "cpu_cores": 16,
      "memory_mb": 65536,
      "gpu": "NVIDIA RTX 4090",
      "container_runtime": "podman"
    },
    {
      "hostname": "thinker.local",
      "reachable": true,
      "os": "linux",
      "cpu_cores": 32,
      "memory_mb": 131072,
      "gpu": "NVIDIA A100",
      "container_runtime": "podman"
    },
    {
      "hostname": "amber.local",
      "reachable": true,
      "os": "linux",
      "cpu_cores": 8,
      "memory_mb": 32768,
      "gpu": "",
      "container_runtime": "docker"
    }
  ]
}
```

**Narration:** "The probe detected each host's CPU, memory, GPU, and container runtime. Now let us deploy services."

```bash
# Deploy the service stack
curl -sk -X POST https://localhost:8443/internal/cluster/deploy \
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
  }' | python3 -m json.tool
```

**Expected response:**

```json
{
  "deployments": [
    {"service_name": "llama-cpp", "host": "thinker.local", "status": "running"},
    {"service_name": "qdrant", "host": "amber.local", "status": "running"},
    {"service_name": "redis", "host": "nezha.local", "status": "running"}
  ],
  "placements": [
    {"service": "llama-cpp", "host": "thinker.local", "strategy": "gpu-affinity"},
    {"service": "qdrant", "host": "amber.local", "strategy": "memory-first"},
    {"service": "redis", "host": "nezha.local", "strategy": "latency"}
  ]
}
```

**Narration:** "The scheduler placed llama.cpp on the host with the best GPU, Qdrant on the host with the most available memory, and Redis on the primary host for lowest latency. This is the auto strategy in action."

**Key points:**
- Always probe before deploying to get current host capabilities
- The scheduler uses host profiles to make placement decisions
- Each service shows its assigned host and the strategy used
- Services are deployed as containers via SSH on the target host

---

## Scene 5: Monitoring and Rebalancing (6 min)

**Narration:** "Once deployed, monitor the cluster status and rebalance when conditions change."

**Demo steps:**

```bash
# Check cluster status
curl -sk https://localhost:8443/internal/cluster/status | python3 -m json.tool

# Rebalance after adding a new host or changing hardware
curl -sk -X POST https://localhost:8443/internal/cluster/rebalance | python3 -m json.tool
```

**Narration:** "The control plane continuously monitors container health on all hosts. If a container crashes, it restarts automatically on the same host. If a host becomes unreachable, services are rescheduled to surviving hosts."

**Screen:** Show the auto-remediation table.

| Failure | Automatic Response |
|---------|-------------------|
| Container crashed | Restart on same host |
| Host unreachable | Reschedule to surviving hosts |
| Performance degraded | Trigger rebalancing |

**Narration:** "To add a new host to the cluster, add it to HELIX_HOSTS, probe to detect capabilities, and rebalance to redistribute services."

```bash
# Adding a new host
# 1. Set up SSH access to the new host
ssh-copy-id -i ~/.ssh/id_ed25519 milosvasic@newhost.local

# 2. Add to HELIX_HOSTS in .env
# HELIX_HOSTS=nezha.local,thinker.local,amber.local,newhost.local

# 3. Restart, probe, and rebalance
make dev
curl -sk -X POST https://localhost:8443/internal/cluster/probe
curl -sk -X POST https://localhost:8443/internal/cluster/rebalance
```

**Key points:**
- Monitoring runs on a 30-second interval
- Auto-remediation handles container crashes and host failures
- Rebalancing redistributes services based on current conditions
- Add hosts by updating `HELIX_HOSTS`, probing, and rebalancing

---

## Exercises

1. Set up SSH key-based authentication to a second machine (or a local VM), configure it in `HELIX_HOSTS`, and run a probe to verify it is discovered
2. Deploy llama.cpp and Qdrant to the cluster and verify the scheduler places the GPU workload on the host with the best GPU
3. Simulate a host failure by stopping the container runtime on a worker, then observe the control plane's automatic rescheduling behavior
