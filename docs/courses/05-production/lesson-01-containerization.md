# Lesson 1: Containerization

**Duration:** 25 minutes
**Prerequisites:** Course 1 (Getting Started)
**Learning Objectives:**
- Build a container image for HelixLLM using the multi-stage Containerfile
- Run HelixLLM as a container with proper volume mounts and networking
- Deploy the full stack with compose including external services
- Configure GPU passthrough for local inference containers

---

## Scene 1: The Containerfile (5 min)

**Narration:** "HelixLLM uses a multi-stage container build. The first stage compiles the Go binary with the full toolchain, and the second stage copies just the binary into a minimal runtime image. This produces a small, secure image with no compiler or source code."

**Screen:** Show the Containerfile structure.

```dockerfile
# Stage 1: Builder
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY submodules/ submodules/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/bin/helixllm ./cmd/helixllm

# Stage 2: Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/bin/helixllm /usr/local/bin/helixllm
EXPOSE 8443
ENTRYPOINT ["helixllm"]
```

**Narration:** "The builder stage uses the Go Alpine image with the full toolchain. It copies the submodules and source code, then builds with the same flags as make build. The runtime stage starts from bare Alpine, adds CA certificates for TLS, and copies just the binary."

**Key points:**
- Multi-stage build keeps the final image small
- `CGO_ENABLED=0` produces a statically-linked binary
- `-ldflags="-s -w"` strips debug symbols for smaller size
- Runtime image contains only the binary and CA certificates
- Compatible with both Podman and Docker (no Docker-specific features)

---

## Scene 2: Building the Image (4 min)

**Narration:** "The make container target auto-detects whether you have Podman or Docker and builds the image."

**Demo steps:**

```bash
# Build the container image
make container
```

**Narration:** "This produces an image tagged as helixllm:dev. Let me verify it was created."

```bash
# List images (Podman)
podman images | grep helixllm

# Or with Docker
docker images | grep helixllm
```

**Expected output:**

```
localhost/helixllm    dev    abc123def456    2 minutes ago    45 MB
```

**Narration:** "The image is about 45 megabytes -- that is the Go binary plus Alpine base. Compare that to an image with the full Go toolchain, which would be over 500 megabytes."

**Key points:**
- `make container` auto-detects Podman or Docker
- Image tagged as `helixllm:dev`
- Override the runtime with `HELIX_CONTAINER_RUNTIME=podman` or `docker`
- Push to a registry with `make container-push`

---

## Scene 3: Running as a Container (6 min)

**Narration:** "To run HelixLLM as a container, you need to mount the TLS certificates and configuration, and expose the server port."

**Demo steps:**

```bash
# Generate certificates first
make certs

# Run the container
podman run -d \
  --name helixllm \
  -p 8443:8443 \
  -v ./certs:/app/certs:ro,z \
  -v ./.env:/app/.env:ro,z \
  helixllm:dev
```

**Narration:** "The -v flags mount the certificates and .env file as read-only volumes. The z suffix is for SELinux relabeling on systems like Fedora. Port 8443 is exposed for both HTTP/3 over UDP and HTTP/2 over TCP."

```bash
# Verify the container is running
podman ps

# Check the logs
podman logs helixllm

# Test health
curl -sk https://localhost:8443/internal/health
```

**Narration:** "For environment variables, you can also pass them directly instead of mounting the .env file."

```bash
# Alternative: pass environment variables directly
podman run -d \
  --name helixllm \
  -p 8443:8443 \
  -v ./certs:/app/certs:ro,z \
  -e HELIX_MODE=full \
  -e HELIX_PORT=8443 \
  -e HELIX_LLM_DEFAULT_PROVIDER=local \
  -e HELIX_LOG_LEVEL=info \
  helixllm:dev
```

**Key points:**
- Mount certificates as read-only volumes
- Mount `.env` or pass environment variables with `-e`
- Expose port 8443 for both TCP and UDP
- Use `podman logs` to verify startup
- Container health checks can use the `/internal/health` endpoint

---

## Scene 4: Compose Deployment (6 min)

**Narration:** "For a full deployment with external services like Qdrant, PostgreSQL, and Redis, use a compose file."

**Screen:** Show the compose configuration.

```yaml
# deploy/compose.yaml
services:
  helixllm:
    image: helixllm:dev
    ports:
      - "8443:8443"
    volumes:
      - ./certs:/app/certs:ro
      - ./.env:/app/.env:ro
    depends_on:
      qdrant:
        condition: service_healthy
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

  qdrant:
    image: qdrant/qdrant
    ports:
      - "6333:6333"
    volumes:
      - qdrant-data:/qdrant/storage
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:6333/healthz"]
      interval: 10s
      timeout: 5s
      retries: 3

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: helixllm
      POSTGRES_USER: helix
      POSTGRES_PASSWORD: helix123
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U helix"]
      interval: 10s
      timeout: 5s
      retries: 3

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  qdrant-data:
  pgdata:
```

**Demo steps:**

```bash
# Start the full stack
podman compose -f deploy/compose.yaml up -d

# Check all services
podman compose -f deploy/compose.yaml ps

# Verify HelixLLM connects to all services
curl -sk https://localhost:8443/internal/health | python3 -m json.tool

# Tear down
podman compose -f deploy/compose.yaml down
```

**Key points:**
- Health checks ensure HelixLLM starts after dependencies are ready
- Named volumes persist data across restarts
- Override for development: `podman compose -f compose.yaml -f compose.dev.yaml up`
- Compatible with both `podman compose` and `docker compose`

---

## Scene 5: GPU Passthrough (4 min)

**Narration:** "For local LLM inference, you need to pass the GPU through to the llama.cpp container. Here is how to configure NVIDIA GPU access."

**Screen:** Show GPU configuration.

```yaml
# GPU-enabled llama.cpp service in compose
services:
  llama-cpp:
    image: ghcr.io/ggml-org/llama.cpp:server-cuda
    ports:
      - "50052:8080"
    volumes:
      - ~/models:/models:ro
    command: >
      -m /models/Llama-3.1-70B-Instruct-Q4_K_M.gguf
      --host 0.0.0.0
      --port 8080
      -ngl 99
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
```

**Demo steps:**

```bash
# Verify GPU is accessible in the container
podman run --rm --gpus all nvidia/cuda:12.0-base nvidia-smi

# Start llama.cpp with GPU
podman run -d \
  --name llama-cpp \
  --gpus all \
  -p 50052:8080 \
  -v ~/models:/models:ro \
  ghcr.io/ggml-org/llama.cpp:server-cuda \
  -m /models/Llama-3.1-70B-Instruct-Q4_K_M.gguf \
  --host 0.0.0.0 --port 8080 -ngl 99
```

**Key points:**
- `--gpus all` passes all GPUs to the container
- NVIDIA Container Toolkit must be installed on the host
- Use the `-cuda` image variant for NVIDIA GPU support
- `-ngl 99` offloads all model layers to GPU
- Monitor GPU usage with `nvidia-smi` on the host

---

## Exercises

1. Build the HelixLLM container image with `make container` and run it with mounted certificates, then verify the health endpoint responds
2. Create a compose file with HelixLLM, Qdrant, and Redis, start all services, and verify that document ingestion works through the running stack
3. If you have an NVIDIA GPU, set up llama.cpp as a container with GPU passthrough and verify local inference works through HelixLLM
