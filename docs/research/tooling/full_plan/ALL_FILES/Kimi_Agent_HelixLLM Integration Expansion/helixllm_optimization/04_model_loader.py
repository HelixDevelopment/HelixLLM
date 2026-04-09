#!/usr/bin/env python3
"""
HelixLLM Optimized Model Loader
Maximizes performance on consumer hardware (6GB VRAM, 32GB RAM)
Target: 150-300+ tokens/sec, 10-20 docs/sec embeddings, <50ms retrieval
"""

import os
import sys
import json
import time
import psutil
from pathlib import Path
from typing import Optional, Dict, Any, Tuple
from dataclasses import dataclass, field, asdict

# llama-cpp imports
from llama_cpp import Llama, LlamaCache

# Color codes for terminal output
class Colors:
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    BLUE = '\033[0;34m'
    CYAN = '\033[0;36m'
    NC = '\033[0m'

@dataclass
class ModelConfig:
    """Configuration for model loading optimization"""
    # Model paths
    model_path: str = ""
    embedding_model_path: str = ""
    
    # GPU Configuration
    n_gpu_layers: int = -1  # -1 = offload all layers to GPU
    main_gpu: int = 0
    tensor_split: Optional[list] = None
    
    # Context and Batch Settings
    n_ctx: int = 4096       # Optimal context window
    n_batch: int = 512      # Batch size for prompt processing
    n_ubatch: int = 512     # Micro-batch size
    
    # Threading
    n_threads: int = -1     # -1 = auto-detect
    n_threads_batch: int = -1
    
    # Memory Management
    use_mmap: bool = True   # Use memory mapping
    use_mlock: bool = False # Lock memory (requires permissions)
    
    # Optimization Flags
    offload_kqv: bool = True    # Offload KQV to GPU
    flash_attn: bool = True     # Use Flash Attention
    
    # Quantization
    type_k: int = 1  # FP16 for keys
    type_v: int = 1  # FP16 for values
    
    # Cache Settings
    cache_size: int = 4096      # KV cache size
    
    # Verbosity
    verbose: bool = True
    
    def __post_init__(self):
        if self.n_threads == -1:
            self.n_threads = self._optimal_thread_count()
        if self.n_threads_batch == -1:
            self.n_threads_batch = self.n_threads
    
    @staticmethod
    def _optimal_thread_count() -> int:
        """Calculate optimal thread count based on CPU"""
        cpu_count = os.cpu_count() or 4
        # Leave 2 cores free for system
        return max(2, cpu_count - 2)


class HardwareProfiler:
    """Profiles hardware and recommends optimal settings"""
    
    def __init__(self):
        self.cpu_info = self._get_cpu_info()
        self.gpu_info = self._get_gpu_info()
        self.memory_info = self._get_memory_info()
    
    def _get_cpu_info(self) -> Dict[str, Any]:
        """Get CPU information"""
        info = {
            'physical_cores': psutil.cpu_count(logical=False) or 4,
            'logical_cores': psutil.cpu_count(logical=True) or 4,
            'frequency_mhz': psutil.cpu_freq().max if psutil.cpu_freq() else 0,
        }
        
        # Try to get CPU name
        try:
            with open('/proc/cpuinfo', 'r') as f:
                for line in f:
                    if 'model name' in line:
                        info['name'] = line.split(':')[1].strip()
                        break
        except:
            info['name'] = 'Unknown'
        
        return info
    
    def _get_gpu_info(self) -> Dict[str, Any]:
        """Get GPU information using nvidia-smi"""
        info = {'available': False}
        
        try:
            import subprocess
            result = subprocess.run(
                ['nvidia-smi', '--query-gpu=name,memory.total,memory.free,memory.used,compute_cap,utilization.gpu,clocks.current.sm',
                 '--format=csv,noheader,nounits'],
                capture_output=True, text=True
            )
            
            if result.returncode == 0:
                lines = result.stdout.strip().split('\n')
                if lines:
                    parts = [p.strip() for p in lines[0].split(',')]
                    if len(parts) >= 7:
                        info = {
                            'available': True,
                            'name': parts[0],
                            'memory_total_mb': int(float(parts[1])),
                            'memory_free_mb': int(float(parts[2])),
                            'memory_used_mb': int(float(parts[3])),
                            'compute_cap': parts[4],
                            'utilization': float(parts[5]),
                            'clock_mhz': int(float(parts[6])),
                        }
                        info['memory_total_gb'] = info['memory_total_mb'] / 1024
                        info['memory_free_gb'] = info['memory_free_mb'] / 1024
        except Exception as e:
            info['error'] = str(e)
        
        return info
    
    def _get_memory_info(self) -> Dict[str, Any]:
        """Get system memory information"""
        mem = psutil.virtual_memory()
        return {
            'total_gb': mem.total / (1024**3),
            'available_gb': mem.available / (1024**3),
            'used_gb': mem.used / (1024**3),
            'percent_used': mem.percent,
        }
    
    def print_summary(self):
        """Print hardware summary"""
        print(f"\n{Colors.CYAN}╔══════════════════════════════════════════════════════════════╗{Colors.NC}")
        print(f"{Colors.CYAN}║{Colors.NC}              HelixLLM Hardware Profile                       {Colors.CYAN}║{Colors.NC}")
        print(f"{Colors.CYAN}╚══════════════════════════════════════════════════════════════╝{Colors.NC}\n")
        
        # CPU
        print(f"{Colors.BLUE}CPU:{Colors.NC}")
        print(f"  Name: {self.cpu_info.get('name', 'Unknown')}")
        print(f"  Physical Cores: {self.cpu_info['physical_cores']}")
        print(f"  Logical Cores: {self.cpu_info['logical_cores']}")
        print(f"  Max Frequency: {self.cpu_info['frequency_mhz']:.0f} MHz")
        
        # GPU
        print(f"\n{Colors.BLUE}GPU:{Colors.NC}")
        if self.gpu_info['available']:
            print(f"  Name: {self.gpu_info['name']}")
            print(f"  Compute Capability: {self.gpu_info['compute_cap']}")
            print(f"  Total Memory: {self.gpu_info['memory_total_gb']:.2f} GB")
            print(f"  Free Memory: {self.gpu_info['memory_free_gb']:.2f} GB")
            print(f"  Used Memory: {self.gpu_info['memory_used_mb']} MB")
            print(f"  Utilization: {self.gpu_info['utilization']:.1f}%")
            print(f"  Clock: {self.gpu_info['clock_mhz']} MHz")
        else:
            print(f"  {Colors.YELLOW}No GPU detected - will use CPU only{Colors.NC}")
        
        # Memory
        print(f"\n{Colors.BLUE}System Memory:{Colors.NC}")
        print(f"  Total: {self.memory_info['total_gb']:.2f} GB")
        print(f"  Available: {self.memory_info['available_gb']:.2f} GB")
        print(f"  Used: {self.memory_info['used_gb']:.2f} GB ({self.memory_info['percent_used']:.1f}%)")
        
        print()
    
    def recommend_config(self, model_size_gb: float = 1.0) -> ModelConfig:
        """Recommend optimal configuration based on hardware"""
        config = ModelConfig()
        
        # Thread configuration
        config.n_threads = max(2, self.cpu_info['logical_cores'] - 2)
        config.n_threads_batch = self.cpu_info['logical_cores']
        
        if self.gpu_info['available']:
            gpu_mem_gb = self.gpu_info['memory_total_gb']
            free_mem_gb = self.gpu_info['memory_free_gb']
            
            # Calculate optimal GPU layers based on available VRAM
            # Rough estimate: 1B params ~ 0.5GB for Q4_K_M
            model_vram_needed = model_size_gb * 0.6  # Add overhead
            
            if free_mem_gb >= model_vram_needed * 1.5:
                # Plenty of VRAM - offload everything
                config.n_gpu_layers = -1
                config.n_ctx = 8192  # Can use larger context
            elif free_mem_gb >= model_vram_needed:
                # Just enough VRAM - offload everything with smaller context
                config.n_gpu_layers = -1
                config.n_ctx = 4096
            else:
                # Limited VRAM - calculate layers to offload
                # Assume ~50MB per layer for 1.5B model
                layers_can_fit = int((free_mem_gb - 0.5) * 1024 / 50)
                config.n_gpu_layers = max(0, layers_can_fit)
                config.n_ctx = 4096
            
            # Adjust batch size based on VRAM
            if gpu_mem_gb >= 8:
                config.n_batch = 1024
                config.n_ubatch = 1024
            elif gpu_mem_gb >= 6:
                config.n_batch = 512
                config.n_ubatch = 512
            else:
                config.n_batch = 256
                config.n_ubatch = 256
        else:
            # CPU-only mode
            config.n_gpu_layers = 0
            config.n_ctx = 4096
            config.n_batch = 256
            config.n_ubatch = 256
            config.use_mmap = True  # Essential for CPU mode
            config.use_mlock = False
        
        return config


class OptimizedModelLoader:
    """Optimized loader for LLM and embedding models"""
    
    def __init__(self, config: ModelConfig):
        self.config = config
        self.model = None
        self.cache = None
        self._load_time = 0
    
    def load_model(self, model_path: str, model_type: str = "llm") -> Llama:
        """Load model with optimized settings"""
        if not os.path.exists(model_path):
            raise FileNotFoundError(f"Model not found: {model_path}")
        
        print(f"\n{Colors.BLUE}Loading {model_type} model...{Colors.NC}")
        print(f"  Path: {model_path}")
        print(f"  GPU Layers: {self.config.n_gpu_layers}")
        print(f"  Context Size: {self.config.n_ctx}")
        print(f"  Batch Size: {self.config.n_batch}")
        print(f"  Threads: {self.config.n_threads}")
        
        start_time = time.time()
        
        try:
            # Build kwargs based on model type
            kwargs = {
                'model_path': model_path,
                'n_gpu_layers': self.config.n_gpu_layers,
                'n_ctx': self.config.n_ctx,
                'n_batch': self.config.n_batch,
                'n_ubatch': self.config.n_ubatch,
                'n_threads': self.config.n_threads,
                'n_threads_batch': self.config.n_threads_batch,
                'use_mmap': self.config.use_mmap,
                'use_mlock': self.config.use_mlock,
                'offload_kqv': self.config.offload_kqv,
                'flash_attn': self.config.flash_attn,
                'type_k': self.config.type_k,
                'type_v': self.config.type_v,
                'verbose': self.config.verbose,
            }
            
            # Add embedding-specific settings
            if model_type == "embedding":
                kwargs['embedding'] = True
                kwargs['pooling_type'] = 1  # Mean pooling
            
            self.model = Llama(**kwargs)
            self._load_time = time.time() - start_time
            
            # Setup cache
            if model_type == "llm":
                self.cache = LlamaCache(capacity_bytes=self.config.cache_size * 1024 * 1024)
                self.model.set_cache(self.cache)
            
            print(f"{Colors.GREEN}✓ Model loaded in {self._load_time:.2f}s{Colors.NC}")
            
            # Print model info
            self._print_model_info()
            
            return self.model
            
        except Exception as e:
            print(f"{Colors.RED}✗ Failed to load model: {e}{Colors.NC}")
            raise
    
    def _print_model_info(self):
        """Print loaded model information"""
        if self.model:
            print(f"\n{Colors.CYAN}Model Information:{Colors.NC}")
            try:
                print(f"  Context Size: {self.model.n_ctx()}")
                print(f"  Vocab Size: {self.model.n_vocab()}")
                print(f"  Embedding Size: {self.model.n_embd()}")
            except:
                pass
    
    def get_load_time(self) -> float:
        """Get model loading time"""
        return self._load_time
    
    def unload(self):
        """Unload model and free memory"""
        if self.model:
            del self.model
            self.model = None
            self.cache = None
            import gc
            gc.collect()
            print(f"{Colors.GREEN}✓ Model unloaded{Colors.NC}")


class HelixLLM:
    """Main HelixLLM interface with optimized model loading"""
    
    def __init__(self):
        self.profiler = HardwareProfiler()
        self.llm_loader: Optional[OptimizedModelLoader] = None
        self.embedding_loader: Optional[OptimizedModelLoader] = None
        self.llm: Optional[Llama] = None
        self.embedder: Optional[Llama] = None
        self._llm_config: Optional[ModelConfig] = None
        self._embedding_config: Optional[ModelConfig] = None
    
    def initialize(self, 
                   llm_path: Optional[str] = None,
                   embedding_path: Optional[str] = None,
                   llm_config: Optional[ModelConfig] = None,
                   embedding_config: Optional[ModelConfig] = None):
        """Initialize HelixLLM with models"""
        
        # Profile hardware
        self.profiler.print_summary()
        
        # Load LLM
        if llm_path:
            if llm_config is None:
                # Estimate model size (rough: Qwen2.5-1.5B Q4_K_M ~ 1GB)
                llm_config = self.profiler.recommend_config(model_size_gb=1.0)
            
            self._llm_config = llm_config
            self.llm_loader = OptimizedModelLoader(llm_config)
            self.llm = self.llm_loader.load_model(llm_path, model_type="llm")
        
        # Load Embedding Model
        if embedding_path:
            if embedding_config is None:
                # Embedding models are smaller
                embedding_config = self.profiler.recommend_config(model_size_gb=0.3)
                embedding_config.n_ctx = 2048  # Smaller context for embeddings
            
            self._embedding_config = embedding_config
            self.embedding_loader = OptimizedModelLoader(embedding_config)
            self.embedder = self.embedding_loader.load_model(embedding_path, model_type="embedding")
    
    def generate(self, 
                 prompt: str, 
                 max_tokens: int = 512,
                 temperature: float = 0.7,
                 top_p: float = 0.9,
                 top_k: int = 40,
                 repeat_penalty: float = 1.1,
                 stream: bool = False,
                 **kwargs) -> Dict[str, Any]:
        """Generate text with optimized settings"""
        if not self.llm:
            raise RuntimeError("LLM not loaded. Call initialize() first.")
        
        start_time = time.time()
        
        output = self.llm(
            prompt,
            max_tokens=max_tokens,
            temperature=temperature,
            top_p=top_p,
            top_k=top_k,
            repeat_penalty=repeat_penalty,
            stream=stream,
            **kwargs
        )
        
        generation_time = time.time() - start_time
        
        if stream:
            return output  # Return generator
        
        # Calculate tokens per second
        tokens_generated = output['usage']['completion_tokens']
        tokens_per_sec = tokens_generated / generation_time if generation_time > 0 else 0
        
        result = {
            'text': output['choices'][0]['text'],
            'tokens_generated': tokens_generated,
            'generation_time': generation_time,
            'tokens_per_second': tokens_per_sec,
            'prompt_tokens': output['usage']['prompt_tokens'],
            'total_tokens': output['usage']['total_tokens'],
        }
        
        return result
    
    def embed(self, texts: list, batch_size: int = 32) -> list:
        """Generate embeddings with batching"""
        if not self.embedder:
            raise RuntimeError("Embedding model not loaded. Call initialize() first.")
        
        if isinstance(texts, str):
            texts = [texts]
        
        embeddings = []
        start_time = time.time()
        
        # Process in batches
        for i in range(0, len(texts), batch_size):
            batch = texts[i:i + batch_size]
            batch_embeddings = self.embedder.create_embedding(batch)
            
            if isinstance(batch_embeddings, dict):
                batch_embeddings = batch_embeddings['data']
            
            embeddings.extend([e['embedding'] for e in batch_embeddings])
        
        total_time = time.time() - start_time
        docs_per_sec = len(texts) / total_time if total_time > 0 else 0
        
        print(f"{Colors.GREEN}✓ Embedded {len(texts)} documents in {total_time:.2f}s ({docs_per_sec:.1f} docs/sec){Colors.NC}")
        
        return embeddings
    
    def benchmark(self, prompt: str = "Explain quantum computing in simple terms.", 
                  max_tokens: int = 256) -> Dict[str, Any]:
        """Run performance benchmark"""
        if not self.llm:
            raise RuntimeError("LLM not loaded")
        
        print(f"\n{Colors.CYAN}Running Benchmark...{Colors.NC}")
        print(f"  Prompt: '{prompt[:50]}...'")
        print(f"  Max Tokens: {max_tokens}")
        
        # Warmup
        print(f"{Colors.BLUE}Warming up...{Colors.NC}")
        self.generate("Hello", max_tokens=10)
        
        # Benchmark
        print(f"{Colors.BLUE}Running benchmark...{Colors.NC}")
        results = []
        
        for i in range(3):
            result = self.generate(prompt, max_tokens=max_tokens)
            results.append(result)
            print(f"  Run {i+1}: {result['tokens_per_second']:.1f} tokens/sec")
        
        avg_tps = sum(r['tokens_per_second'] for r in results) / len(results)
        avg_time = sum(r['generation_time'] for r in results) / len(results)
        
        print(f"\n{Colors.GREEN}Benchmark Results:{Colors.NC}")
        print(f"  Average: {avg_tps:.1f} tokens/sec")
        print(f"  Average Time: {avg_time:.2f}s")
        
        return {
            'average_tokens_per_second': avg_tps,
            'average_generation_time': avg_time,
            'runs': results,
        }
    
    def shutdown(self):
        """Shutdown and cleanup"""
        if self.llm_loader:
            self.llm_loader.unload()
        if self.embedding_loader:
            self.embedding_loader.unload()
        print(f"{Colors.GREEN}✓ HelixLLM shutdown complete{Colors.NC}")


# Example usage and testing
if __name__ == "__main__":
    print(f"{Colors.CYAN}HelixLLM Model Loader{Colors.NC}")
    print(f"{Colors.CYAN}====================={Colors.NC}\n")
    
    # Example paths (update these to your actual model paths)
    LLM_MODEL = "models/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf"
    EMBEDDING_MODEL = "models/nomic-embed-text-v1.5.Q4_K_M.gguf"
    
    # Check if models exist
    if not os.path.exists(LLM_MODEL):
        print(f"{Colors.YELLOW}Warning: LLM model not found at {LLM_MODEL}{Colors.NC}")
        print(f"{Colors.YELLOW}Please download the model or update the path{Colors.NC}")
        LLM_MODEL = None
    
    if not os.path.exists(EMBEDDING_MODEL):
        print(f"{Colors.YELLOW}Warning: Embedding model not found at {EMBEDDING_MODEL}{Colors.NC}")
        EMBEDDING_MODEL = None
    
    # Initialize HelixLLM
    helix = HelixLLM()
    
    try:
        helix.initialize(
            llm_path=LLM_MODEL,
            embedding_path=EMBEDDING_MODEL,
        )
        
        # Run benchmark if LLM loaded
        if helix.llm:
            benchmark_results = helix.benchmark()
            
            # Test generation
            print(f"\n{Colors.CYAN}Testing Generation:{Colors.NC}")
            result = helix.generate(
                "What is the capital of France?",
                max_tokens=50
            )
            print(f"Response: {result['text']}")
            print(f"Speed: {result['tokens_per_second']:.1f} tokens/sec")
        
        # Test embeddings if loaded
        if helix.embedder:
            print(f"\n{Colors.CYAN}Testing Embeddings:{Colors.NC}")
            texts = [
                "The quick brown fox jumps over the lazy dog.",
                "Machine learning is a subset of artificial intelligence.",
                "Python is a popular programming language.",
            ]
            embeddings = helix.embed(texts)
            print(f"Generated {len(embeddings)} embeddings of dimension {len(embeddings[0])}")
        
    except Exception as e:
        print(f"{Colors.RED}Error: {e}{Colors.NC}")
        import traceback
        traceback.print_exc()
    
    finally:
        helix.shutdown()
