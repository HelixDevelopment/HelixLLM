#!/usr/bin/env python3
"""
HelixLLM Runtime Configuration
Optimized runtime settings for maximum performance
"""

import os
import sys
import json
import threading
from typing import Dict, Any, Optional, List
from dataclasses import dataclass, field, asdict
from pathlib import Path


@dataclass
class RuntimeConfig:
    """Complete runtime configuration for HelixLLM"""
    
    # ============================================
    # GPU Configuration
    # ============================================
    gpu_layers: int = -1           # -1 = all layers on GPU
    main_gpu: int = 0              # Primary GPU index
    tensor_split: Optional[List[float]] = None  # Multi-GPU split
    
    # ============================================
    # Context and Batch Settings
    # ============================================
    context_size: int = 4096       # Context window size
    batch_size: int = 512          # Prompt processing batch
    ubatch_size: int = 512         # Micro-batch size
    
    # ============================================
    # Threading Configuration
    # ============================================
    n_threads: int = -1            # -1 = auto-detect
    n_threads_batch: int = -1      # Batch processing threads
    
    # ============================================
    # Memory Management
    # ============================================
    use_mmap: bool = True          # Memory mapping
    use_mlock: bool = False        # Lock memory (needs permissions)
    
    # ============================================
    # Optimization Flags
    # ============================================
    offload_kqv: bool = True       # Offload KQV attention to GPU
    flash_attn: bool = True        # Use Flash Attention
    
    # ============================================
    # Quantization Settings
    # ============================================
    type_k: int = 1                # Key type (1=FP16)
    type_v: int = 1                # Value type (1=FP16)
    
    # ============================================
    # Cache Configuration
    # ============================================
    cache_size: int = 4096         # KV cache size in MB
    cache_type: str = "f16"        # Cache type: f16, q8_0, q4_0
    
    # ============================================
    # Generation Parameters
    # ============================================
    temperature: float = 0.7
    top_p: float = 0.9
    top_k: int = 40
    repeat_penalty: float = 1.1
    presence_penalty: float = 0.0
    frequency_penalty: float = 0.0
    
    # ============================================
    # Performance Tuning
    # ============================================
    prompt_cache: bool = True      # Enable prompt caching
    seed: int = -1                 # Random seed (-1 = random)
    
    def __post_init__(self):
        if self.n_threads == -1:
            self.n_threads = self._optimal_threads()
        if self.n_threads_batch == -1:
            self.n_threads_batch = self.n_threads
    
    @staticmethod
    def _optimal_threads() -> int:
        """Calculate optimal thread count"""
        cpu_count = os.cpu_count() or 4
        # Leave 2 cores for system
        return max(2, min(cpu_count - 2, 16))
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary"""
        return asdict(self)
    
    def save(self, path: str):
        """Save configuration to JSON file"""
        with open(path, 'w') as f:
            json.dump(self.to_dict(), f, indent=2)
    
    @classmethod
    def load(cls, path: str) -> 'RuntimeConfig':
        """Load configuration from JSON file"""
        with open(path, 'r') as f:
            data = json.load(f)
        return cls(**data)


class EnvironmentConfigurator:
    """Configures environment variables for optimal performance"""
    
    # Optimal environment variables for different scenarios
    ENV_CONFIGS = {
        'cuda': {
            'LLAMA_CUDA_FORCE_MMQ': '1',
            'LLAMA_CUDA_MMV_Y': '1',
            'LLAMA_CUDA_F16': '1',
            'LLAMA_CUDA_DMMV_X': '32',
            'LLAMA_CUDA_DMMV_Y': '1',
            'LLAMA_CUDA_KQUANTS_ITER': '2',
            'LLAMA_CUDA_PEER_MAX_BATCH_SIZE': '128',
            'GGML_CUDA_NO_PINNED': '0',
            'GGML_CUDA_MEMORY_POOL': '1',
        },
        'cpu': {
            'OMP_NUM_THREADS': '16',
            'OPENBLAS_NUM_THREADS': '16',
            'MKL_NUM_THREADS': '16',
            'VECLIB_MAXIMUM_THREADS': '16',
            'NUMEXPR_NUM_THREADS': '16',
        },
        'memory': {
            'PYTORCH_CUDA_ALLOC_CONF': 'max_split_size_mb:512',
        },
        'python': {
            'PYTHONUNBUFFERED': '1',
            'PYTHONDONTWRITEBYTECODE': '1',
        }
    }
    
    @classmethod
    def apply(cls, mode: str = 'all'):
        """Apply environment configuration"""
        configs_to_apply = []
        
        if mode == 'all':
            configs_to_apply = ['cuda', 'cpu', 'memory', 'python']
        else:
            configs_to_apply = [mode]
        
        for config_name in configs_to_apply:
            if config_name in cls.ENV_CONFIGS:
                for key, value in cls.ENV_CONFIGS[config_name].items():
                    os.environ[key] = value
                    print(f"  Set {key}={value}")
    
    @classmethod
    def get_shell_script(cls) -> str:
        """Generate shell script with environment variables"""
        lines = ["#!/bin/bash", "# HelixLLM Environment Configuration", ""]
        
        for config_name, vars in cls.ENV_CONFIGS.items():
            lines.append(f"# {config_name.upper()} Settings")
            for key, value in vars.items():
                lines.append(f'export {key}="{value}"')
            lines.append("")
        
        return "\n".join(lines)


class PresetConfigs:
    """Predefined configurations for common scenarios"""
    
    @staticmethod
    def consumer_6gb() -> RuntimeConfig:
        """Optimized for 6GB VRAM consumer GPU"""
        return RuntimeConfig(
            gpu_layers=-1,           # Offload all layers
            context_size=4096,       # Standard context
            batch_size=512,
            ubatch_size=512,
            n_threads=14,            # For 16-core CPU
            n_threads_batch=16,
            use_mmap=True,
            use_mlock=False,
            offload_kqv=True,
            flash_attn=True,
            cache_size=4096,
            cache_type="f16",
        )
    
    @staticmethod
    def consumer_8gb() -> RuntimeConfig:
        """Optimized for 8GB VRAM consumer GPU"""
        return RuntimeConfig(
            gpu_layers=-1,
            context_size=8192,       # Larger context
            batch_size=1024,
            ubatch_size=1024,
            n_threads=14,
            n_threads_batch=16,
            use_mmap=True,
            use_mlock=False,
            offload_kqv=True,
            flash_attn=True,
            cache_size=8192,
            cache_type="f16",
        )
    
    @staticmethod
    def consumer_12gb() -> RuntimeConfig:
        """Optimized for 12GB VRAM GPU"""
        return RuntimeConfig(
            gpu_layers=-1,
            context_size=16384,      # Very large context
            batch_size=2048,
            ubatch_size=2048,
            n_threads=14,
            n_threads_batch=16,
            use_mmap=True,
            use_mlock=True,          # Can afford to lock memory
            offload_kqv=True,
            flash_attn=True,
            cache_size=16384,
            cache_type="f16",
        )
    
    @staticmethod
    def cpu_only() -> RuntimeConfig:
        """Optimized for CPU-only operation"""
        return RuntimeConfig(
            gpu_layers=0,            # No GPU
            context_size=4096,
            batch_size=256,
            ubatch_size=256,
            n_threads=-1,            # Auto-detect all cores
            n_threads_batch=-1,
            use_mmap=True,           # Essential for CPU
            use_mlock=False,
            offload_kqv=False,
            flash_attn=False,
            cache_size=2048,
            cache_type="q8_0",       # Smaller cache
        )
    
    @staticmethod
    def embedding() -> RuntimeConfig:
        """Optimized for embedding generation"""
        return RuntimeConfig(
            gpu_layers=-1,
            context_size=2048,       # Smaller context for embeddings
            batch_size=1024,         # Larger batches
            ubatch_size=1024,
            n_threads=14,
            n_threads_batch=16,
            use_mmap=True,
            use_mlock=False,
            offload_kqv=True,
            flash_attn=True,
            cache_size=2048,
            cache_type="f16",
        )


class ConfigManager:
    """Manages configuration files and profiles"""
    
    def __init__(self, config_dir: str = None):
        if config_dir is None:
            config_dir = os.path.expanduser("~/.config/helixllm")
        self.config_dir = Path(config_dir)
        self.config_dir.mkdir(parents=True, exist_ok=True)
        self.profiles_dir = self.config_dir / "profiles"
        self.profiles_dir.mkdir(exist_ok=True)
    
    def save_profile(self, name: str, config: RuntimeConfig):
        """Save a configuration profile"""
        profile_path = self.profiles_dir / f"{name}.json"
        config.save(str(profile_path))
        print(f"Profile saved: {profile_path}")
    
    def load_profile(self, name: str) -> RuntimeConfig:
        """Load a configuration profile"""
        profile_path = self.profiles_dir / f"{name}.json"
        if not profile_path.exists():
            raise FileNotFoundError(f"Profile not found: {name}")
        return RuntimeConfig.load(str(profile_path))
    
    def list_profiles(self) -> List[str]:
        """List available profiles"""
        profiles = []
        for f in self.profiles_dir.glob("*.json"):
            profiles.append(f.stem)
        return profiles
    
    def get_active_profile(self) -> Optional[str]:
        """Get the currently active profile name"""
        active_file = self.config_dir / "active_profile.txt"
        if active_file.exists():
            return active_file.read_text().strip()
        return None
    
    def set_active_profile(self, name: str):
        """Set the active profile"""
        active_file = self.config_dir / "active_profile.txt"
        active_file.write_text(name)
    
    def create_default_profiles(self):
        """Create default configuration profiles"""
        presets = {
            'consumer_6gb': PresetConfigs.consumer_6gb(),
            'consumer_8gb': PresetConfigs.consumer_8gb(),
            'consumer_12gb': PresetConfigs.consumer_12gb(),
            'cpu_only': PresetConfigs.cpu_only(),
            'embedding': PresetConfigs.embedding(),
        }
        
        for name, config in presets.items():
            self.save_profile(name, config)
        
        print(f"Created {len(presets)} default profiles")


def generate_server_config(config: RuntimeConfig, host: str = "0.0.0.0", port: int = 8080) -> Dict[str, Any]:
    """Generate configuration for llama-server"""
    return {
        "host": host,
        "port": port,
        "model": "",  # To be filled
        "n_gpu_layers": config.gpu_layers,
        "main_gpu": config.main_gpu,
        "tensor_split": config.tensor_split,
        "ctx_size": config.context_size,
        "n_batch": config.batch_size,
        "n_ubatch": config.ubatch_size,
        "threads": config.n_threads,
        "threads_batch": config.n_threads_batch,
        "use_mmap": config.use_mmap,
        "use_mlock": config.use_mlock,
        "flash_attn": config.flash_attn,
        "cache_type_k": config.cache_type,
        "cache_type_v": config.cache_type,
        "seed": config.seed,
    }


def print_config_summary(config: RuntimeConfig):
    """Print configuration summary"""
    print("\n" + "="*50)
    print("HelixLLM Runtime Configuration")
    print("="*50)
    
    print("\n[GPU Settings]")
    print(f"  GPU Layers: {config.gpu_layers} ({'All' if config.gpu_layers == -1 else config.gpu_layers})")
    print(f"  Main GPU: {config.main_gpu}")
    
    print("\n[Context & Batch]")
    print(f"  Context Size: {config.context_size}")
    print(f"  Batch Size: {config.batch_size}")
    print(f"  Micro-batch Size: {config.ubatch_size}")
    
    print("\n[Threading]")
    print(f"  Threads: {config.n_threads}")
    print(f"  Batch Threads: {config.n_threads_batch}")
    
    print("\n[Memory]")
    print(f"  Use MMAP: {config.use_mmap}")
    print(f"  Use MLOCK: {config.use_mlock}")
    
    print("\n[Optimizations]")
    print(f"  Offload KQV: {config.offload_kqv}")
    print(f"  Flash Attention: {config.flash_attn}")
    
    print("\n[Cache]")
    print(f"  Cache Size: {config.cache_size} MB")
    print(f"  Cache Type: {config.cache_type}")
    
    print("\n[Generation]")
    print(f"  Temperature: {config.temperature}")
    print(f"  Top P: {config.top_p}")
    print(f"  Top K: {config.top_k}")
    print(f"  Repeat Penalty: {config.repeat_penalty}")
    
    print("="*50 + "\n")


# Main execution
if __name__ == "__main__":
    print("HelixLLM Runtime Configuration Generator")
    print("="*50)
    
    # Create config manager
    manager = ConfigManager()
    
    # Create default profiles
    print("\nCreating default profiles...")
    manager.create_default_profiles()
    
    # Print available profiles
    print("\nAvailable profiles:")
    for profile in manager.list_profiles():
        print(f"  - {profile}")
    
    # Show example configurations
    print("\n" + "="*50)
    print("Example: Consumer 6GB GPU Configuration")
    print("="*50)
    config_6gb = PresetConfigs.consumer_6gb()
    print_config_summary(config_6gb)
    
    # Generate environment script
    print("\nGenerating environment configuration script...")
    env_script = EnvironmentConfigurator.get_shell_script()
    env_path = manager.config_dir / "environment.sh"
    with open(env_path, 'w') as f:
        f.write(env_script)
    print(f"Saved to: {env_path}")
    
    # Generate server config example
    print("\nGenerating server configuration...")
    server_config = generate_server_config(config_6gb)
    server_path = manager.config_dir / "server_config.json"
    with open(server_path, 'w') as f:
        json.dump(server_config, f, indent=2)
    print(f"Saved to: {server_path}")
    
    print("\nConfiguration setup complete!")
    print(f"Configuration directory: {manager.config_dir}")
