#!/usr/bin/env python3
"""
HelixLLM Hardware Detection and Auto-Configuration
Automatically detects hardware and configures optimal settings
"""

import os
import sys
import json
import subprocess
import platform
import psutil
from typing import Dict, Any, Optional, Tuple, List
from dataclasses import dataclass, asdict
from pathlib import Path


@dataclass
class HardwareProfile:
    """Complete hardware profile"""
    # CPU
    cpu_name: str = "Unknown"
    cpu_physical_cores: int = 0
    cpu_logical_cores: int = 0
    cpu_frequency_mhz: float = 0.0
    cpu_architecture: str = "Unknown"
    
    # GPU
    gpu_available: bool = False
    gpu_name: str = "None"
    gpu_memory_total_mb: int = 0
    gpu_memory_free_mb: int = 0
    gpu_compute_capability: str = ""
    gpu_driver_version: str = ""
    gpu_utilization: float = 0.0
    
    # Memory
    ram_total_gb: float = 0.0
    ram_available_gb: float = 0.0
    
    # Storage
    storage_type: str = "Unknown"
    storage_speed_mbps: float = 0.0
    
    # OS
    os_name: str = "Unknown"
    os_version: str = "Unknown"
    
    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


class HardwareDetector:
    """Detects system hardware capabilities"""
    
    def __init__(self):
        self.profile = HardwareProfile()
        self._detect_all()
    
    def _detect_all(self):
        """Run all detection methods"""
        self._detect_cpu()
        self._detect_gpu()
        self._detect_memory()
        self._detect_storage()
        self._detect_os()
    
    def _detect_cpu(self):
        """Detect CPU information"""
        try:
            # Basic CPU info
            self.profile.cpu_physical_cores = psutil.cpu_count(logical=False) or 4
            self.profile.cpu_logical_cores = psutil.cpu_count(logical=True) or 4
            
            freq = psutil.cpu_freq()
            if freq:
                self.profile.cpu_frequency_mhz = freq.max or freq.current
            
            self.profile.cpu_architecture = platform.machine()
            
            # Try to get CPU name
            if platform.system() == "Linux":
                try:
                    with open('/proc/cpuinfo', 'r') as f:
                        for line in f:
                            if 'model name' in line:
                                self.profile.cpu_name = line.split(':')[1].strip()
                                break
                except:
                    pass
            elif platform.system() == "Windows":
                try:
                    import winreg
                    key = winreg.OpenKey(winreg.HKEY_LOCAL_MACHINE, 
                                        r"HARDWARE\DESCRIPTION\System\CentralProcessor\0")
                    self.profile.cpu_name = winreg.QueryValueEx(key, "ProcessorNameString")[0]
                except:
                    pass
            elif platform.system() == "Darwin":
                try:
                    result = subprocess.run(['sysctl', '-n', 'machdep.cpu.brand_string'],
                                          capture_output=True, text=True)
                    if result.returncode == 0:
                        self.profile.cpu_name = result.stdout.strip()
                except:
                    pass
                    
        except Exception as e:
            print(f"CPU detection warning: {e}")
    
    def _detect_gpu(self):
        """Detect GPU information using nvidia-smi"""
        try:
            # Check if nvidia-smi is available
            result = subprocess.run(['nvidia-smi', '--query-gpu=name,memory.total,memory.free,'
                                    'memory.used,compute_cap,driver_version,utilization.gpu,'
                                    'clocks.current.sm,temperature.gpu,power.draw',
                                    '--format=csv,noheader,nounits'],
                                  capture_output=True, text=True)
            
            if result.returncode == 0:
                lines = result.stdout.strip().split('\n')
                if lines and lines[0]:
                    parts = [p.strip() for p in lines[0].split(',')]
                    if len(parts) >= 10:
                        self.profile.gpu_available = True
                        self.profile.gpu_name = parts[0]
                        self.profile.gpu_memory_total_mb = int(float(parts[1]))
                        self.profile.gpu_memory_free_mb = int(float(parts[2]))
                        self.profile.gpu_compute_capability = parts[4]
                        self.profile.gpu_driver_version = parts[5]
                        self.profile.gpu_utilization = float(parts[6])
            else:
                # Try alternative detection methods
                self._detect_gpu_alternative()
                
        except FileNotFoundError:
            # nvidia-smi not found, try alternatives
            self._detect_gpu_alternative()
        except Exception as e:
            print(f"GPU detection warning: {e}")
    
    def _detect_gpu_alternative(self):
        """Alternative GPU detection methods"""
        # Try ROCm for AMD GPUs
        try:
            result = subprocess.run(['rocm-smi', '--showproductname'],
                                  capture_output=True, text=True)
            if result.returncode == 0:
                self.profile.gpu_available = True
                self.profile.gpu_name = "AMD GPU (ROCm)"
        except:
            pass
        
        # Try checking for CUDA libraries
        try:
            import ctypes
            cuda_lib = ctypes.CDLL('libcuda.so')
            self.profile.gpu_available = True
            if not self.profile.gpu_name or self.profile.gpu_name == "None":
                self.profile.gpu_name = "CUDA-capable GPU"
        except:
            pass
    
    def _detect_memory(self):
        """Detect system memory"""
        try:
            mem = psutil.virtual_memory()
            self.profile.ram_total_gb = mem.total / (1024**3)
            self.profile.ram_available_gb = mem.available / (1024**3)
        except Exception as e:
            print(f"Memory detection warning: {e}")
    
    def _detect_storage(self):
        """Detect storage type and speed"""
        try:
            # Detect storage type
            if platform.system() == "Linux":
                # Check if root is on SSD/NVMe
                result = subprocess.run(['lsblk', '-d', '-o', 'NAME,ROTA,TYPE', '-n'],
                                      capture_output=True, text=True)
                if result.returncode == 0:
                    for line in result.stdout.strip().split('\n'):
                        if 'nvme' in line.lower():
                            self.profile.storage_type = "NVMe SSD"
                            break
                        elif '0' in line and 'disk' in line:
                            self.profile.storage_type = "SSD"
                            break
                        elif '1' in line and 'disk' in line:
                            self.profile.storage_type = "HDD"
                            break
            
            # Estimate speed based on type
            if self.profile.storage_type == "NVMe SSD":
                self.profile.storage_speed_mbps = 3500  # Typical Gen3 NVMe
            elif self.profile.storage_type == "SSD":
                self.profile.storage_speed_mbps = 500   # Typical SATA SSD
            elif self.profile.storage_type == "HDD":
                self.profile.storage_speed_mbps = 150   # Typical HDD
                
        except Exception as e:
            print(f"Storage detection warning: {e}")
    
    def _detect_os(self):
        """Detect operating system"""
        self.profile.os_name = platform.system()
        self.profile.os_version = platform.release()
    
    def get_profile(self) -> HardwareProfile:
        """Get the detected hardware profile"""
        return self.profile
    
    def print_summary(self):
        """Print hardware summary"""
        print("\n" + "="*60)
        print("         HelixLLM Hardware Detection Report")
        print("="*60)
        
        print("\n[CPU]")
        print(f"  Name: {self.profile.cpu_name}")
        print(f"  Architecture: {self.profile.cpu_architecture}")
        print(f"  Physical Cores: {self.profile.cpu_physical_cores}")
        print(f"  Logical Cores: {self.profile.cpu_logical_cores}")
        print(f"  Max Frequency: {self.profile.cpu_frequency_mhz:.0f} MHz")
        
        print("\n[GPU]")
        if self.profile.gpu_available:
            print(f"  Name: {self.profile.gpu_name}")
            print(f"  Compute Capability: {self.profile.gpu_compute_capability}")
            print(f"  Driver Version: {self.profile.gpu_driver_version}")
            print(f"  Total Memory: {self.profile.gpu_memory_total_mb / 1024:.2f} GB")
            print(f"  Free Memory: {self.profile.gpu_memory_free_mb / 1024:.2f} GB")
            print(f"  Utilization: {self.profile.gpu_utilization:.1f}%")
        else:
            print("  No GPU detected - CPU-only mode")
        
        print("\n[Memory]")
        print(f"  Total RAM: {self.profile.ram_total_gb:.2f} GB")
        print(f"  Available RAM: {self.profile.ram_available_gb:.2f} GB")
        
        print("\n[Storage]")
        print(f"  Type: {self.profile.storage_type}")
        print(f"  Estimated Speed: {self.profile.storage_speed_mbps:.0f} MB/s")
        
        print("\n[Operating System]")
        print(f"  Name: {self.profile.os_name}")
        print(f"  Version: {self.profile.os_version}")
        
        print("="*60 + "\n")


class AutoConfigurator:
    """Automatically configures settings based on hardware"""
    
    def __init__(self, hardware_profile: HardwareProfile):
        self.profile = hardware_profile
        self.config = {}
    
    def generate_config(self) -> Dict[str, Any]:
        """Generate optimal configuration"""
        self.config = {
            'hardware_profile': self.profile.to_dict(),
            'llm_config': self._generate_llm_config(),
            'embedding_config': self._generate_embedding_config(),
            'environment_variables': self._generate_env_vars(),
            'recommendations': self._generate_recommendations(),
        }
        return self.config
    
    def _generate_llm_config(self) -> Dict[str, Any]:
        """Generate LLM configuration"""
        config = {
            'n_gpu_layers': -1,
            'n_ctx': 4096,
            'n_batch': 512,
            'n_ubatch': 512,
            'n_threads': max(2, self.profile.cpu_logical_cores - 2),
            'n_threads_batch': self.profile.cpu_logical_cores,
            'use_mmap': True,
            'use_mlock': False,
            'offload_kqv': True,
            'flash_attn': True,
            'cache_size': 4096,
        }
        
        if self.profile.gpu_available:
            gpu_mem_gb = self.profile.gpu_memory_total_mb / 1024
            
            # Adjust based on GPU memory
            if gpu_mem_gb >= 12:
                config['n_ctx'] = 16384
                config['n_batch'] = 2048
                config['use_mlock'] = True
                config['cache_size'] = 8192
            elif gpu_mem_gb >= 8:
                config['n_ctx'] = 8192
                config['n_batch'] = 1024
                config['cache_size'] = 4096
            elif gpu_mem_gb >= 6:
                config['n_ctx'] = 4096
                config['n_batch'] = 512
                config['cache_size'] = 4096
            else:
                config['n_ctx'] = 2048
                config['n_batch'] = 256
                config['n_gpu_layers'] = max(0, int((gpu_mem_gb - 0.5) * 20))
        else:
            # CPU-only mode
            config['n_gpu_layers'] = 0
            config['n_ctx'] = 4096
            config['n_batch'] = 256
            config['offload_kqv'] = False
            config['flash_attn'] = False
        
        return config
    
    def _generate_embedding_config(self) -> Dict[str, Any]:
        """Generate embedding model configuration"""
        config = {
            'n_gpu_layers': -1,
            'n_ctx': 2048,
            'n_batch': 1024,
            'n_threads': max(2, self.profile.cpu_logical_cores - 2),
            'embedding': True,
        }
        
        if not self.profile.gpu_available:
            config['n_gpu_layers'] = 0
            config['n_batch'] = 512
        
        return config
    
    def _generate_env_vars(self) -> Dict[str, str]:
        """Generate environment variables"""
        env_vars = {
            'OMP_NUM_THREADS': str(self.profile.cpu_logical_cores),
            'OPENBLAS_NUM_THREADS': str(self.profile.cpu_logical_cores),
            'MKL_NUM_THREADS': str(self.profile.cpu_logical_cores),
            'PYTHONUNBUFFERED': '1',
        }
        
        if self.profile.gpu_available:
            env_vars.update({
                'LLAMA_CUDA_FORCE_MMQ': '1',
                'LLAMA_CUDA_F16': '1',
                'LLAMA_CUDA_DMMV_X': '32',
                'GGML_CUDA_MEMORY_POOL': '1',
            })
        
        return env_vars
    
    def _generate_recommendations(self) -> List[str]:
        """Generate optimization recommendations"""
        recommendations = []
        
        if self.profile.gpu_available:
            gpu_mem_gb = self.profile.gpu_memory_total_mb / 1024
            
            if gpu_mem_gb < 6:
                recommendations.append(
                    "Limited VRAM detected. Consider using Q4_K_M or Q5_K_M quantized models."
                )
            
            if self.profile.gpu_memory_free_mb < 1024:
                recommendations.append(
                    "Low free GPU memory. Close other GPU applications for better performance."
                )
        else:
            recommendations.append(
                "No GPU detected. Performance will be CPU-limited. Consider using smaller models."
            )
        
        if self.profile.ram_total_gb < 16:
            recommendations.append(
                "Limited system RAM. Enable swap or use smaller context windows."
            )
        
        if self.profile.storage_type == "HDD":
            recommendations.append(
                "HDD detected. Model loading will be slower. Consider upgrading to SSD."
            )
        
        # CPU-specific recommendations
        if self.profile.cpu_physical_cores < 8:
            recommendations.append(
                "Few CPU cores detected. Consider reducing n_threads for better responsiveness."
            )
        
        return recommendations
    
    def save_config(self, path: str):
        """Save configuration to file"""
        with open(path, 'w') as f:
            json.dump(self.config, f, indent=2)
        print(f"Configuration saved to: {path}")
    
    def print_config(self):
        """Print generated configuration"""
        print("\n" + "="*60)
        print("         Generated Configuration")
        print("="*60)
        
        print("\n[LLM Configuration]")
        for key, value in self.config['llm_config'].items():
            print(f"  {key}: {value}")
        
        print("\n[Embedding Configuration]")
        for key, value in self.config['embedding_config'].items():
            print(f"  {key}: {value}")
        
        print("\n[Environment Variables]")
        for key, value in self.config['environment_variables'].items():
            print(f"  {key}={value}")
        
        print("\n[Recommendations]")
        for i, rec in enumerate(self.config['recommendations'], 1):
            print(f"  {i}. {rec}")
        
        print("="*60 + "\n")


def create_fallback_config() -> Dict[str, Any]:
    """Create a safe fallback configuration"""
    return {
        'hardware_profile': {'gpu_available': False},
        'llm_config': {
            'n_gpu_layers': 0,
            'n_ctx': 2048,
            'n_batch': 256,
            'n_threads': 4,
            'use_mmap': True,
            'use_mlock': False,
        },
        'embedding_config': {
            'n_gpu_layers': 0,
            'n_ctx': 1024,
            'n_batch': 256,
            'embedding': True,
        },
        'environment_variables': {
            'OMP_NUM_THREADS': '4',
        },
        'recommendations': [
            'Using safe fallback configuration.',
            'Run hardware detection for optimized settings.',
        ]
    }


# Main execution
if __name__ == "__main__":
    print("HelixLLM Hardware Detection and Auto-Configuration")
    print("="*60)
    
    # Detect hardware
    detector = HardwareDetector()
    detector.print_summary()
    
    # Generate configuration
    configurator = AutoConfigurator(detector.get_profile())
    config = configurator.generate_config()
    configurator.print_config()
    
    # Save configuration
    config_dir = Path.home() / ".config" / "helixllm"
    config_dir.mkdir(parents=True, exist_ok=True)
    
    config_path = config_dir / "auto_config.json"
    configurator.save_config(str(config_path))
    
    # Also save hardware profile
    profile_path = config_dir / "hardware_profile.json"
    with open(profile_path, 'w') as f:
        json.dump(detector.get_profile().to_dict(), f, indent=2)
    print(f"Hardware profile saved to: {profile_path}")
    
    print("\nConfiguration complete!")
    print(f"Use this configuration with: --config {config_path}")
