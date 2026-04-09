# HelixLLM Optimization Suite - Complete Summary

## Overview

This suite provides a complete optimization system for running HelixLLM on consumer hardware with maximum performance.

**Target Hardware:**
- AMD Ryzen 9 CPU (or equivalent)
- 32GB RAM
- RTX GPU with 6GB VRAM
- 2TB NVMe SSD

**Expected Performance:**
- Token Generation: 150-300+ tokens/second
- Embedding Generation: 10-20 documents/second
- Retrieval Latency: <50ms

---

## Complete File Listing

### Setup Scripts

| File | Description | Platform |
|------|-------------|----------|
| `setup.sh` | Master setup script for Linux | Linux |
| `setup.bat` | Master setup script for Windows | Windows |
| `01_environment_setup.sh` | CUDA, drivers, Python environment | Linux |
| `02_build_llama_cpp.sh` | llama.cpp build with optimizations | Linux |
| `03_install_llama_cpp_python.sh` | llama-cpp-python installation | Linux |

### Python Modules

| File | Description | Key Features |
|------|-------------|--------------|
| `04_model_loader.py` | Optimized model loading | GPU offloading, hardware profiling, HelixLLM class |
| `05_runtime_config.py` | Runtime configuration | Presets, config management, environment setup |
| `06_hardware_detection.py` | Hardware detection | Auto-configuration, GPU detection, recommendations |
| `07_performance_monitor.py` | Performance monitoring | Token tracking, GPU monitoring, benchmarks |
| `08_optimization_checklist.py` | Optimization checks | Pre-run, runtime, post-run optimizations |
| `09_benchmark.py` | Benchmark suite | LLM, embedding, retrieval benchmarks |
| `10_helixllm_server.py` | Inference server | FastAPI REST API, streaming, health checks |
| `11_download_models.py` | Model downloader | HuggingFace integration, progress tracking |

### Documentation

| File | Description |
|------|-------------|
| `README.md` | Complete documentation |
| `QUICK_REFERENCE.md` | Quick command reference |
| `SUMMARY.md` | This file |

---

## Key Configuration Values

### GPU Layer Offloading (6GB VRAM)

| Setting | VRAM Used | Performance |
|---------|-----------|-------------|
| `n_gpu_layers=-1` (all) | ~4.5GB | Fastest |
| `n_gpu_layers=20` | ~3GB | Good balance |
| `n_gpu_layers=10` | ~2GB | More CPU fallback |

### Context Window Sizes

| VRAM | Recommended Context |
|------|---------------------|
| 12GB+ | 16384 |
| 8GB | 8192 |
| 6GB | 4096 |
| 4GB | 2048 |

### Batch Sizes

| VRAM | n_batch | n_ubatch |
|------|---------|----------|
| 12GB+ | 2048 | 2048 |
| 8GB | 1024 | 1024 |
| 6GB | 512 | 512 |
| 4GB | 256 | 256 |

### Thread Configuration

| CPU Cores | n_threads | n_threads_batch |
|-----------|-----------|-----------------|
| 16 | 14 | 16 |
| 12 | 10 | 12 |
| 8 | 6 | 8 |
| 4 | 4 | 4 |

---

## Environment Variables

### Essential
```bash
export CUDA_HOME=/usr/local/cuda
export PATH=$CUDA_HOME/bin:$PATH
export LD_LIBRARY_PATH=$CUDA_HOME/lib64:$LD_LIBRARY_PATH
```

### CUDA Optimization
```bash
export LLAMA_CUDA_FORCE_MMQ=1
export LLAMA_CUDA_MMV_Y=1
export LLAMA_CUDA_F16=1
export LLAMA_CUDA_DMMV_X=32
export LLAMA_CUDA_DMMV_Y=1
export LLAMA_CUDA_KQUANTS_ITER=2
export LLAMA_CUDA_PEER_MAX_BATCH_SIZE=128
export GGML_CUDA_MEMORY_POOL=1
```

### CPU Threading
```bash
export OMP_NUM_THREADS=16
export OPENBLAS_NUM_THREADS=16
export MKL_NUM_THREADS=16
```

### Python
```bash
export PYTHONUNBUFFERED=1
export PYTHONDONTWRITEBYTECODE=1
```

---

## CMake Build Flags

```bash
# CUDA
-DLLAMA_CUDA=ON
-DLLAMA_CUDA_F16=ON
-DLLAMA_CUDA_FORCE_MMQ=ON
-DCMAKE_CUDA_ARCHITECTURES=75;80;86;89

# BLAS
-DLLAMA_BLAS=ON
-DLLAMA_BLAS_VENDOR=OpenBLAS

# CPU Optimizations
-DLLAMA_NATIVE=ON
-DLLAMA_AVX=ON
-DLLAMA_AVX2=ON
-DLLAMA_FMA=ON
-DLLAMA_F16C=ON

# Threading
-DLLAMA_OPENMP=ON
```

---

## Model Recommendations

### For 6GB VRAM

| Model | Size | Purpose |
|-------|------|---------|
| Qwen2.5-1.5B-Instruct-Q4_K_M | ~1GB | General LLM |
| nomic-embed-text-v1.5.Q4_K_M | ~300MB | Embeddings |

### For 8GB VRAM

| Model | Size | Purpose |
|-------|------|---------|
| Qwen2.5-3B-Instruct-Q4_K_M | ~1.9GB | General LLM |
| nomic-embed-text-v1.5.f16 | ~500MB | Embeddings |

---

## API Endpoints

### REST API (Server)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/generate` | POST | Generate text |
| `/embed` | POST | Generate embeddings |
| `/stats` | GET | Server statistics |

### Python API

```python
from 04_model_loader import HelixLLM

helix = HelixLLM()
helix.initialize(llm_path="...", embedding_path="...")

# Generate text
result = helix.generate(prompt, max_tokens=256)

# Generate embeddings
embeddings = helix.embed(texts)

# Benchmark
results = helix.benchmark()

helix.shutdown()
```

---

## Performance Monitoring

### Metrics Tracked

| Metric | Description |
|--------|-------------|
| tokens_per_second | Generation speed |
| time_to_first_token | Initial latency |
| inter_token_latency | Time between tokens |
| vram_used_mb | GPU memory usage |
| gpu_utilization | GPU utilization % |
| gpu_temperature | GPU temperature |

### Usage

```python
from 07_performance_monitor import PerformanceMonitor

monitor = PerformanceMonitor()
monitor.start_generation()
# ... generate ...
monitor.end_generation()
stats = monitor.get_statistics()
```

---

## Troubleshooting

### Common Issues

| Issue | Solution |
|-------|----------|
| GPU not detected | Check `nvidia-smi`, reinstall drivers |
| CUDA errors | Verify CUDA installation, check versions |
| Out of memory | Reduce `n_gpu_layers`, `n_ctx`, `n_batch` |
| Slow performance | Check CPU governor, enable performance mode |
| Import errors | Activate virtual environment |

### Verification Commands

```bash
# Check GPU
nvidia-smi

# Check CUDA
nvcc --version

# Check Python packages
python -c "from llama_cpp import Llama; print('OK')"

# Run checklist
python 08_optimization_checklist.py
```

---

## Directory Structure

```
helixllm_optimization/
├── setup.sh                    # Master setup (Linux)
├── setup.bat                   # Master setup (Windows)
├── 01_environment_setup.sh     # Environment setup
├── 02_build_llama_cpp.sh       # Build script
├── 03_install_llama_cpp_python.sh  # Python package install
├── 04_model_loader.py          # Model loading
├── 05_runtime_config.py        # Configuration
├── 06_hardware_detection.py    # Hardware detection
├── 07_performance_monitor.py   # Performance monitoring
├── 08_optimization_checklist.py # Optimization checks
├── 09_benchmark.py             # Benchmark suite
├── 10_helixllm_server.py       # Inference server
├── 11_download_models.py       # Model downloader
├── README.md                   # Full documentation
├── QUICK_REFERENCE.md          # Quick reference
└── SUMMARY.md                  # This file

~/.config/helixllm/
├── environment.sh              # Environment variables
├── profiles/                   # Configuration profiles
│   ├── consumer_6gb.json
│   ├── consumer_8gb.json
│   ├── consumer_12gb.json
│   ├── cpu_only.json
│   └── embedding.json
├── benchmarks/                 # Benchmark results
└── hardware_profile.json       # Detected hardware

~/helixllm_env/                 # Python virtual environment

models/                         # Downloaded models
├── Qwen2.5-1.5B-Instruct-Q4_K_M.gguf
└── nomic-embed-text-v1.5.Q4_K_M.gguf
```

---

## Quick Start Commands

```bash
# Complete setup
./setup.sh

# Download models
python 11_download_models.py --download-all

# Run benchmark
python 09_benchmark.py

# Start server
python 10_helixllm_server.py

# Check health
curl http://localhost:8080/health
```

---

## Performance Targets vs Expected

| Metric | Target | Expected (6GB) | Expected (8GB) |
|--------|--------|----------------|----------------|
| Token Generation | 150+ TPS | 180-220 TPS | 220-280 TPS |
| Embedding | 10+ docs/sec | 12-16 docs/sec | 16-20 docs/sec |
| Retrieval | <50ms | 20-35ms | 15-25ms |

---

## License

MIT License - See LICENSE file for details.
