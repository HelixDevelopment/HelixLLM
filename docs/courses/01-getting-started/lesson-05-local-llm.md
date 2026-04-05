# Lesson 5: Local LLM Setup

**Duration:** 25 minutes
**Prerequisites:** Lesson 4 (Configuration)
**Learning Objectives:**
- Set up llama.cpp as a local inference backend
- Download and configure GGUF model files
- Compare CPU and GPU inference performance
- Verify local inference through the HelixLLM API

---

## Scene 1: Why Local Inference? (3 min)

**Narration:** "Local inference means running an LLM on your own hardware -- no API keys, no cloud dependency, no data leaving your network. HelixLLM uses llama.cpp as its local inference backend. llama.cpp is an optimized C++ inference engine that runs quantized models in the GGUF format on CPUs and GPUs."

**Screen:** Comparison table of local versus cloud inference.

| Aspect | Local (llama.cpp) | Cloud (OpenAI/Anthropic) |
|--------|-------------------|--------------------------|
| Privacy | Complete -- data never leaves your machine | Data sent to provider |
| Latency | Depends on hardware | Depends on network + load |
| Cost | Hardware only | Per-token pricing |
| Models | Open-weight GGUF models | Provider's model catalog |
| Availability | Always on, no rate limits | Subject to API limits |

**Key points:**
- Local inference gives complete data privacy
- GGUF is the model format used by llama.cpp
- Quantized models (Q4, Q5, Q8) reduce memory at minimal quality loss
- GPU acceleration dramatically improves throughput

---

## Scene 2: Setting Up llama.cpp (6 min)

**Narration:** "The easiest way to run llama.cpp is as a container. The HelixLLM control plane can deploy it automatically, but let us set it up manually first to understand how it works."

**Demo steps:**

```bash
# Pull the llama.cpp server image (CUDA variant for NVIDIA GPUs)
podman pull ghcr.io/ggml-org/llama.cpp:server-cuda

# Or the CPU-only variant
podman pull ghcr.io/ggml-org/llama.cpp:server
```

**Narration:** "Create a directory for your models and download a GGUF model file. I will use the Llama 3.1 70B model in Q4_K_M quantization, which offers a good balance between quality and memory usage."

```bash
# Create a models directory
mkdir -p ~/models

# Download a GGUF model (example using wget)
# The actual URL depends on the model provider (e.g., HuggingFace)
wget -O ~/models/Llama-3.1-70B-Instruct-Q4_K_M.gguf \
  "https://huggingface.co/bartowski/Meta-Llama-3.1-70B-Instruct-GGUF/resolve/main/Meta-Llama-3.1-70B-Instruct-Q4_K_M.gguf"
```

**Narration:** "Now start the llama.cpp server. We mount the models directory and expose the RPC port that HelixLLM expects."

```bash
# Start llama.cpp server with GPU acceleration
podman run -d \
  --name llama-cpp \
  --gpus all \
  -v ~/models:/models:ro \
  -p 50052:8080 \
  ghcr.io/ggml-org/llama.cpp:server-cuda \
  -m /models/Llama-3.1-70B-Instruct-Q4_K_M.gguf \
  --host 0.0.0.0 \
  --port 8080 \
  -ngl 99
```

**Key points:**
- `-ngl 99` offloads all layers to GPU (reduce for partial offload)
- The model file can be 40-70 GB depending on quantization
- Port 50052 is the default `HELIX_LLM_LOCAL_RPC_PORT`
- Use the CPU-only image if you do not have an NVIDIA GPU

---

## Scene 3: Configuring HelixLLM for Local Inference (4 min)

**Narration:** "Now we configure HelixLLM to use the local llama.cpp server as its default provider."

**Demo steps:**

```bash
# Edit .env to configure local inference
cat >> .env << 'EOF'
HELIX_LLM_DEFAULT_PROVIDER=local
HELIX_LLM_LOCAL_MODEL=Llama-3.1-70B-Instruct-Q4_K_M
HELIX_LLM_LOCAL_RPC_PORT=50052
EOF
```

**Narration:** "The three key variables are: DEFAULT_PROVIDER set to local, LOCAL_MODEL set to the model name that matches your GGUF file, and LOCAL_RPC_PORT pointing to the llama.cpp server."

```bash
# Start HelixLLM
make dev
```

**Narration:** "When HelixLLM starts, it registers the local provider and the model appears in the /v1/models listing."

```bash
# Verify the local model is available
curl -sk https://localhost:8443/v1/models | python3 -m json.tool
```

**Key points:**
- `HELIX_LLM_DEFAULT_PROVIDER=local` makes all requests use llama.cpp by default
- The model name in `HELIX_LLM_LOCAL_MODEL` is what clients use in API requests
- The RPC port must match the port exposed by the llama.cpp container

---

## Scene 4: Testing Local Inference (4 min)

**Narration:** "Let us verify local inference is working by sending a chat completion through the HelixLLM API."

**Demo steps:**

```bash
# Chat completion using the local model
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "What are the benefits of running LLMs locally?"}
    ],
    "temperature": 0.7,
    "max_tokens": 512
  }' | python3 -m json.tool
```

**Narration:** "The response comes from your local hardware. No data left your machine. Let us also try streaming."

```bash
# Streaming local inference
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "Count from 1 to 10."}
    ],
    "stream": true
  }'
```

**Narration:** "You can see the tokens arriving one by one from llama.cpp, streamed through HelixLLM to your client as SSE events."

**Key points:**
- The API is identical whether using local or cloud providers
- Clients cannot tell the difference between local and cloud inference
- Streaming works with local inference just like cloud providers

---

## Scene 5: GPU vs CPU Performance (5 min)

**Narration:** "The hardware you use makes a dramatic difference in inference speed. Let me show you the comparison."

**Screen:** Performance comparison table.

| Hardware | Model | Tokens/sec | First Token | Memory |
|----------|-------|------------|-------------|--------|
| NVIDIA RTX 4090 (24 GB) | Llama-3.1-70B Q4_K_M | ~40 t/s | ~200 ms | ~40 GB |
| Apple M3 Max (36 GB) | Llama-3.1-70B Q4_K_M | ~20 t/s | ~400 ms | ~40 GB |
| CPU only (32 cores) | Llama-3.1-70B Q4_K_M | ~5 t/s | ~2000 ms | ~40 GB |
| NVIDIA RTX 4090 | Llama-3.1-8B Q4_K_M | ~120 t/s | ~50 ms | ~5 GB |

**Narration:** "For GPU inference, the ngl flag controls how many model layers are offloaded to the GPU. Setting it to 99 means all layers. If your GPU does not have enough VRAM, reduce this number for partial offloading -- the remaining layers run on CPU."

**Demo steps:**

```bash
# CPU-only inference (no GPU offload)
podman run -d --name llama-cpu \
  -v ~/models:/models:ro \
  -p 50053:8080 \
  ghcr.io/ggml-org/llama.cpp:server \
  -m /models/Llama-3.1-70B-Instruct-Q4_K_M.gguf \
  --host 0.0.0.0 --port 8080 -ngl 0

# Partial GPU offload (30 layers on GPU, rest on CPU)
podman run -d --name llama-partial \
  --gpus all \
  -v ~/models:/models:ro \
  -p 50054:8080 \
  ghcr.io/ggml-org/llama.cpp:server-cuda \
  -m /models/Llama-3.1-70B-Instruct-Q4_K_M.gguf \
  --host 0.0.0.0 --port 8080 -ngl 30
```

**Key points:**
- GPU acceleration provides 5-20x speedup over CPU-only
- Partial GPU offload is useful when VRAM is limited
- Smaller quantizations (Q4) use less memory but slightly less quality
- For interactive use, aim for at least 20 tokens per second

---

## Scene 6: What's Next (1 min)

**Narration:** "You now have HelixLLM running with a fully local LLM backend. No cloud dependencies, no API keys, complete privacy. In the next course, we will explore the API in depth -- OpenAI compatibility, Anthropic compatibility, streaming patterns, and embeddings."

---

## Exercises

1. Download a small GGUF model (such as Llama-3.1-8B Q4_K_M) and run it with llama.cpp, then send a chat completion through HelixLLM
2. Compare inference speed by running the same prompt with `-ngl 0` (CPU only) and `-ngl 99` (full GPU offload) and note the tokens-per-second difference
3. Configure `HELIX_LLM_DEFAULT_PROVIDER=auto` and set up both local and one cloud provider, then send requests with different model names to observe the routing
