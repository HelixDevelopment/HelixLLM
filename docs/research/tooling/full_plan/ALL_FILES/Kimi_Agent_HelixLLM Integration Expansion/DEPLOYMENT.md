# HelixLLM + HelixAgent Deployment Guide
# =======================================

## Table of Contents
1. [Prerequisites](#prerequisites)
2. [Hardware Requirements](#hardware-requirements)
3. [Installation](#installation)
4. [Configuration](#configuration)
5. [Running the System](#running-the-system)
6. [Monitoring](#monitoring)
7. [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required Software
- Docker Engine 24.0+ with Docker Compose
- NVIDIA Docker Runtime (for GPU support)
- NVIDIA drivers 525.60.13+ (for CUDA 12.1)

### Optional Software
- curl (for health checks)
- make (for convenience commands)

---

## Hardware Requirements

### Minimum Requirements
| Component | Specification |
|-----------|--------------|
| CPU | 4 cores (x86_64) |
| RAM | 16 GB |
| GPU | 4 GB VRAM |
| Storage | 10 GB SSD |

### Recommended Specifications (Target)
| Component | Specification |
|-----------|--------------|
| CPU | AMD Ryzen 9 (16 cores) |
| RAM | 32 GB DDR4/DDR5 |
| GPU | NVIDIA RTX 3060 (6GB VRAM) |
| Storage | 50 GB NVMe SSD |

### Model Storage Requirements
| Model | Quantization | Size | VRAM Required |
|-------|-------------|------|---------------|
| 1.5B | Q4_K_M | ~1 GB | 2-3 GB |
| 1.5B | Q5_K_M | ~1.2 GB | 2.5-3.5 GB |
| 1.5B | Q8_0 | ~1.5 GB | 3-4 GB |

---

## Installation

### 1. Clone the Repository

```bash
git clone https://github.com/HelixDevelopment/HelixLLM.git
git clone https://github.com/HelixDevelopment/HelixAgent.git
cd HelixLLM
```

### 2. Download Model

Download a GGUF model file (e.g., Helix-1.5B-Q4_K_M.gguf):

```bash
mkdir -p models
cd models

# Download from HuggingFace (example)
wget https://huggingface.co/helix/helix-1.5b-gguf/resolve/main/helix-1.5b-q4_k_m.gguf

cd ..
```

### 3. Configure Environment

Create a `.env` file:

```bash
cat > .env << 'EOF'
# Model Configuration
HELIX_MODEL_PATH=./models
HELIX_MODEL_FILE=helix-1.5b-q4_k_m.gguf
HELIX_GPU_LAYERS=-1
HELIX_CONTEXT_LENGTH=4096
HELIX_CPU_THREADS=16

# API Configuration
HELIX_PORT=8000
HELIX_AUTH_ENABLED=false
HELIX_LOG_LEVEL=info

# GPU Configuration
CUDA_VISIBLE_DEVICES=0

# Storage
HELIX_CONFIG_PATH=./config.yaml
EOF
```

### 4. Build Docker Image

```bash
docker-compose build
```

---

## Configuration

### Basic Configuration (config.yaml)

```yaml
model:
  path: "/app/models/helix-1.5b-q4_k_m.gguf"
  context_length: 4096
  gpu_layers: -1
  cpu_threads: 16
  batch_size: 512

embedding:
  model_name: "nomic-ai/nomic-embed-text-v1.5"
  device: "cuda"
  normalize: true

storage:
  chroma_path: "/app/data/chroma"
  collection: "default"

rag:
  enabled: true
  max_context_tokens: 1500
  top_k: 5

api:
  host: "0.0.0.0"
  port: 8000
  rate_limit: 60
```

### Advanced Configuration

See `config.example.yaml` for all available options.

---

## Running the System

### Start All Services

```bash
docker-compose up -d
```

### Start with Monitoring

```bash
docker-compose --profile monitoring up -d
```

### Start with Redis Caching

```bash
docker-compose --profile with-redis up -d
```

### View Logs

```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f helixllm
```

### Stop Services

```bash
docker-compose down
```

### Stop and Remove Data

```bash
docker-compose down -v
```

---

## Verification

### Health Check

```bash
curl http://localhost:8000/health
```

Expected response:
```json
{
  "status": "healthy",
  "model_loaded": true,
  "gpu_available": true,
  "version": "1.0.0"
}
```

### Test Chat Completion

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-1.5b",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ]
  }'
```

### Test Embeddings

```bash
curl -X POST http://localhost:8000/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "nomic-embed-text",
    "input": ["Hello world"]
  }'
```

---

## Monitoring

### Prometheus Metrics

Access Prometheus at: http://localhost:9090

Key metrics:
- `helix_tokens_generated_total`
- `helix_request_duration_seconds`
- `helix_active_sessions`
- `helix_gpu_memory_used_bytes`

### Grafana Dashboard

Access Grafana at: http://localhost:3000

Default credentials:
- Username: admin
- Password: admin

### Custom Metrics Endpoint

```bash
curl http://localhost:8000/metrics
```

---

## Performance Tuning

### For 6GB VRAM (RTX 3060)

```yaml
model:
  gpu_layers: -1  # All layers on GPU
  context_length: 4096
  batch_size: 512

embedding:
  device: "cuda"  # Embeddings on GPU
```

### For CPU-Only Mode

```yaml
model:
  gpu_layers: 0  # No GPU layers
  cpu_threads: 16  # Use all cores
  context_length: 2048  # Reduce for memory
```

### Expected Performance

| Configuration | Tokens/Second | Notes |
|--------------|---------------|-------|
| GPU (all layers) | 150-300+ | RTX 3060 6GB |
| GPU (partial) | 80-150 | Mixed CPU/GPU |
| CPU only | 20-50 | Ryzen 9 16 cores |

---

## Troubleshooting

### Issue: Model fails to load

**Symptoms:** Container exits with "Model file not found"

**Solution:**
```bash
# Check model file exists
ls -la models/

# Verify volume mount
docker-compose exec helixllm ls -la /app/models/
```

### Issue: GPU not detected

**Symptoms:** "No GPU detected. Running on CPU only."

**Solution:**
```bash
# Check NVIDIA runtime
docker run --rm --gpus all nvidia/cuda:12.1.0-base-ubuntu22.04 nvidia-smi

# Restart with GPU support
docker-compose down
docker-compose up -d
```

### Issue: Out of memory

**Symptoms:** Container killed with OOM error

**Solution:**
```bash
# Reduce context length
export HELIX_CONTEXT_LENGTH=2048

# Reduce GPU layers
export HELIX_GPU_LAYERS=20

docker-compose up -d
```

### Issue: Slow inference

**Symptoms:** Low tokens/second

**Solution:**
1. Check GPU utilization: `nvidia-smi`
2. Verify all layers on GPU: Check logs for "offloaded X/XX layers"
3. Increase batch size if memory allows
4. Enable Flash Attention if supported

### Issue: ChromaDB connection failed

**Symptoms:** "Failed to connect to ChromaDB"

**Solution:**
```bash
# Check ChromaDB is running
docker-compose ps chromadb

# Check ChromaDB logs
docker-compose logs chromadb

# Restart ChromaDB
docker-compose restart chromadb
```

---

## API Examples

### Chat Completion with Tools

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-1.5b",
    "messages": [
      {"role": "user", "content": "What is the weather in New York?"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "Get weather for a location",
          "parameters": {
            "type": "object",
            "properties": {
              "location": {"type": "string"},
              "unit": {"type": "string", "enum": ["celsius", "fahrenheit"]}
            },
            "required": ["location"]
          }
        }
      }
    ]
  }'
```

### Streaming Response

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-1.5b",
    "messages": [
      {"role": "user", "content": "Tell me a story"}
    ],
    "stream": true
  }'
```

### Document Ingestion

```bash
curl -X POST http://localhost:8000/v1/documents \
  -H "Content-Type: multipart/form-data" \
  -F "file=@document.pdf" \
  -F "metadata={"source":"user_upload"}"
```

### RAG Query

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "helix-1.5b",
    "messages": [
      {"role": "system", "content": "Use the provided context to answer."},
      {"role": "user", "content": "What does the documentation say about authentication?"}
    ],
    "use_rag": true
  }'
```

---

## Security Considerations

### Enable Authentication

```yaml
# config.yaml
api:
  auth_enabled: true
  keys:
    - "sk-your-secure-api-key"
```

### Use HTTPS

Deploy behind a reverse proxy (nginx, traefik) with SSL termination.

### Network Isolation

```yaml
# docker-compose.yml
networks:
  helix_network:
    internal: true  # No external access
```

---

## Backup and Recovery

### Backup Data

```bash
# Backup ChromaDB
docker run --rm -v helix_chroma_data:/data -v $(pwd):/backup alpine tar czf /backup/chroma_backup.tar.gz -C /data .

# Backup configuration
cp config.yaml config.yaml.backup
```

### Restore Data

```bash
# Restore ChromaDB
docker run --rm -v helix_chroma_data:/data -v $(pwd):/backup alpine tar xzf /backup/chroma_backup.tar.gz -C /data

# Restart services
docker-compose restart
```

---

## Support

For issues and feature requests:
- GitHub Issues: https://github.com/HelixDevelopment/HelixLLM/issues
- Documentation: https://docs.helix.dev
- Discord: https://discord.gg/helixdev
