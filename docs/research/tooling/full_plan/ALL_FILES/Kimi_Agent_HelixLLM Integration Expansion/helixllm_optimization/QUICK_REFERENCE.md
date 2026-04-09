# HelixLLM Quick Reference Guide

## One-Line Commands

### Setup
```bash
# Complete setup (Linux)
./setup.sh

# Complete setup (Windows)
setup.bat
```

### Model Download
```bash
# Download recommended models
python 11_download_models.py --download-all

# Download specific model
python 11_download_models.py --download qwen2.5-1.5b-instruct-q4_k_m
```

### Server
```bash
# Start server with auto-detection
python 10_helixllm_server.py

# Start with specific profile
python 10_helixllm_server.py --profile consumer_6gb

# Start with custom config
python 10_helixllm_server.py --config my_config.json
```

### Benchmark
```bash
# Run full benchmark
python 09_benchmark.py

# Benchmark with custom models
python 09_benchmark.py /path/to/llm.gguf /path/to/embedding.gguf
```

### Health Check
```bash
# Run optimization checklist
python 08_optimization_checklist.py

# Check hardware
python 06_hardware_detection.py
```

## API Examples

### Generate Text
```bash
curl -X POST http://localhost:8080/generate \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Explain machine learning",
    "max_tokens": 256,
    "temperature": 0.7
  }'
```

### Generate Embeddings
```bash
curl -X POST http://localhost:8080/embed \
  -H "Content-Type: application/json" \
  -d '{
    "texts": ["Hello world", "Machine learning"],
    "batch_size": 32
  }'
```

### Health Check
```bash
curl http://localhost:8080/health
```

## Python API

### Basic Usage
```python
from 04_model_loader import HelixLLM

helix = HelixLLM()
helix.initialize(
    llm_path="models/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf",
    embedding_path="models/nomic-embed-text-v1.5.Q4_K_M.gguf"
)

# Generate
result = helix.generate("Hello", max_tokens=50)
print(result['text'])

# Embed
embeddings = helix.embed(["Text 1", "Text 2"])

helix.shutdown()
```

## Configuration Presets

### Consumer 6GB VRAM
```python
from 05_runtime_config import PresetConfigs
config = PresetConfigs.consumer_6gb()
```

### Consumer 8GB VRAM
```python
config = PresetConfigs.consumer_8gb()
```

### CPU-Only
```python
config = PresetConfigs.cpu_only()
```

## Environment Variables

### Essential
```bash
export CUDA_HOME=/usr/local/cuda
export PATH=$CUDA_HOME/bin:$PATH
export LD_LIBRARY_PATH=$CUDA_HOME/lib64:$LD_LIBRARY_PATH
```

### Performance
```bash
export LLAMA_CUDA_FORCE_MMQ=1
export LLAMA_CUDA_F16=1
export OMP_NUM_THREADS=16
```

## Troubleshooting

### GPU Not Detected
```bash
# Check drivers
nvidia-smi

# Check CUDA
nvcc --version

# Reinstall llama-cpp-python
CMAKE_ARGS="-DLLAMA_CUDA=on" pip install llama-cpp-python --force-reinstall --no-cache-dir
```

### Out of Memory
```python
# Reduce GPU layers
config.gpu_layers = 20  # Instead of -1

# Reduce context
config.context_size = 2048  # Instead of 4096
```

### Slow Performance
```bash
# Check CPU governor
sudo cpufreq-set -g performance

# Monitor GPU
nvidia-smi dmon
```

## File Locations

| Type | Location |
|------|----------|
| Virtual Environment | `~/helixllm_env` |
| Environment Config | `~/.config/helixllm/environment.sh` |
| Profiles | `~/.config/helixllm/profiles/` |
| Benchmarks | `~/.config/helixllm/benchmarks/` |
| Models | `./models/` |
| Hardware Profile | `~/.config/helixllm/hardware_profile.json` |

## Performance Targets

| Metric | Target | Excellent |
|--------|--------|-----------|
| Token Generation | 150+ TPS | 250+ TPS |
| Embedding | 10+ docs/sec | 15+ docs/sec |
| Retrieval | <50ms | <25ms |

## Common Issues

### ImportError
```bash
# Activate virtual environment
source ~/helixllm_env/bin/activate

# Source environment
source ~/.config/helixllm/environment.sh
```

### CUDA Errors
```bash
# Clear CUDA cache
python -c "import torch; torch.cuda.empty_cache()"

# Check GPU memory
nvidia-smi
```

### Permission Denied
```bash
chmod +x *.sh
```
