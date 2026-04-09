#!/usr/bin/env python3
"""
HelixLLM Optimization Checklist
Pre-run checks, runtime optimizations, and post-run cleanup
"""

import os
import sys
import subprocess
import psutil
from typing import List, Dict, Any, Tuple
from dataclasses import dataclass
from enum import Enum


class CheckStatus(Enum):
    PASS = "✓"
    FAIL = "✗"
    WARN = "⚠"
    INFO = "ℹ"


@dataclass
class CheckResult:
    name: str
    status: CheckStatus
    message: str
    recommendation: str = ""


class OptimizationChecklist:
    """Complete optimization checklist for HelixLLM"""
    
    def __init__(self):
        self.results: List[CheckResult] = []
        self.warnings: List[str] = []
        self.recommendations: List[str] = []
    
    # =========================================================================
    # PRE-RUN CHECKS
    # =========================================================================
    
    def run_pre_run_checks(self) -> List[CheckResult]:
        """Run all pre-run checks"""
        print("\n" + "="*60)
        print("         PRE-RUN OPTIMIZATION CHECKLIST")
        print("="*60 + "\n")
        
        self.results = []
        
        # Hardware checks
        self._check_gpu()
        self._check_vram()
        self._check_cpu()
        self._check_ram()
        self._check_storage()
        
        # Software checks
        self._check_cuda()
        self._check_drivers()
        self._check_python()
        self._check_llama_cpp()
        
        # Environment checks
        self._check_env_vars()
        self._check_virtual_env()
        
        # System checks
        self._check_cpu_governor()
        self._check_swappiness()
        self._check_file_descriptors()
        
        self._print_results()
        return self.results
    
    def _check_gpu(self):
        """Check GPU availability"""
        try:
            result = subprocess.run(['nvidia-smi'], capture_output=True, text=True)
            if result.returncode == 0:
                gpu_name = ""
                for line in result.stdout.split('\n'):
                    if 'NVIDIA' in line and 'MiB' not in line:
                        gpu_name = line.strip()
                        break
                self.results.append(CheckResult(
                    name="GPU Detection",
                    status=CheckStatus.PASS,
                    message=f"GPU detected: {gpu_name[:50]}...",
                    recommendation=""
                ))
            else:
                self.results.append(CheckResult(
                    name="GPU Detection",
                    status=CheckStatus.WARN,
                    message="No NVIDIA GPU detected",
                    recommendation="Install NVIDIA drivers or use CPU-only mode"
                ))
        except FileNotFoundError:
            self.results.append(CheckResult(
                name="GPU Detection",
                status=CheckStatus.FAIL,
                message="nvidia-smi not found",
                recommendation="Install NVIDIA drivers"
            ))
    
    def _check_vram(self):
        """Check available VRAM"""
        try:
            result = subprocess.run(
                ['nvidia-smi', '--query-gpu=memory.total,memory.free', 
                 '--format=csv,noheader,nounits'],
                capture_output=True, text=True
            )
            if result.returncode == 0:
                parts = result.stdout.strip().split(',')
                total_mb = int(float(parts[0]))
                free_mb = int(float(parts[1]))
                total_gb = total_mb / 1024
                free_gb = free_mb / 1024
                
                status = CheckStatus.PASS if free_gb >= 4 else CheckStatus.WARN
                
                self.results.append(CheckResult(
                    name="VRAM Availability",
                    status=status,
                    message=f"Total: {total_gb:.1f} GB, Free: {free_gb:.1f} GB",
                    recommendation="Close GPU applications if VRAM is low" if free_gb < 4 else ""
                ))
        except:
            self.results.append(CheckResult(
                name="VRAM Availability",
                status=CheckStatus.INFO,
                message="Could not check VRAM",
                recommendation=""
            ))
    
    def _check_cpu(self):
        """Check CPU configuration"""
        cpu_count = psutil.cpu_count(logical=True)
        physical_cores = psutil.cpu_count(logical=False)
        
        try:
            with open('/proc/cpuinfo', 'r') as f:
                cpu_name = ""
                for line in f:
                    if 'model name' in line:
                        cpu_name = line.split(':')[1].strip()
                        break
        except:
            cpu_name = "Unknown"
        
        self.results.append(CheckResult(
            name="CPU Configuration",
            status=CheckStatus.PASS,
            message=f"{cpu_name[:40]}... ({physical_cores} cores, {cpu_count} threads)",
            recommendation=""
        ))
    
    def _check_ram(self):
        """Check available RAM"""
        mem = psutil.virtual_memory()
        total_gb = mem.total / (1024**3)
        available_gb = mem.available / (1024**3)
        
        status = CheckStatus.PASS if total_gb >= 16 else CheckStatus.WARN
        
        self.results.append(CheckResult(
            name="System RAM",
            status=status,
            message=f"Total: {total_gb:.1f} GB, Available: {available_gb:.1f} GB",
            recommendation="Close applications to free RAM" if available_gb < 4 else ""
        ))
    
    def _check_storage(self):
        """Check storage type and space"""
        try:
            # Check disk space
            disk = psutil.disk_usage('/')
            free_gb = disk.free / (1024**3)
            
            # Try to detect storage type
            storage_type = "Unknown"
            try:
                result = subprocess.run(['lsblk', '-d', '-o', 'NAME,ROTA', '-n'],
                                      capture_output=True, text=True)
                if result.returncode == 0:
                    for line in result.stdout.split('\n'):
                        if 'nvme' in line.lower():
                            storage_type = "NVMe SSD"
                            break
                        elif '0' in line and 'disk' in line:
                            storage_type = "SSD"
                            break
            except:
                pass
            
            status = CheckStatus.PASS if free_gb >= 10 else CheckStatus.WARN
            
            self.results.append(CheckResult(
                name="Storage",
                status=status,
                message=f"{storage_type}, Free: {free_gb:.1f} GB",
                recommendation="Free up disk space" if free_gb < 10 else ""
            ))
        except:
            pass
    
    def _check_cuda(self):
        """Check CUDA installation"""
        try:
            result = subprocess.run(['nvcc', '--version'], capture_output=True, text=True)
            if result.returncode == 0:
                version = ""
                for line in result.stdout.split('\n'):
                    if 'release' in line:
                        version = line.strip()
                        break
                self.results.append(CheckResult(
                    name="CUDA Installation",
                    status=CheckStatus.PASS,
                    message=version[:50],
                    recommendation=""
                ))
            else:
                self.results.append(CheckResult(
                    name="CUDA Installation",
                    status=CheckStatus.FAIL,
                    message="CUDA not properly installed",
                    recommendation="Reinstall CUDA toolkit"
                ))
        except FileNotFoundError:
            self.results.append(CheckResult(
                name="CUDA Installation",
                status=CheckStatus.FAIL,
                message="nvcc not found",
                recommendation="Install CUDA toolkit"
            ))
    
    def _check_drivers(self):
        """Check NVIDIA driver version"""
        try:
            result = subprocess.run(
                ['nvidia-smi', '--query-gpu=driver_version', '--format=csv,noheader'],
                capture_output=True, text=True
            )
            if result.returncode == 0:
                version = result.stdout.strip()
                # Check if version is recent enough
                major = int(version.split('.')[0])
                status = CheckStatus.PASS if major >= 525 else CheckStatus.WARN
                
                self.results.append(CheckResult(
                    name="NVIDIA Driver",
                    status=status,
                    message=f"Version: {version}",
                    recommendation="Update drivers" if major < 525 else ""
                ))
        except:
            pass
    
    def _check_python(self):
        """Check Python version"""
        version = sys.version_info
        status = CheckStatus.PASS if version >= (3, 10) else CheckStatus.WARN
        
        self.results.append(CheckResult(
            name="Python Version",
            status=status,
            message=f"{version.major}.{version.minor}.{version.micro}",
            recommendation="Upgrade to Python 3.10+ for best performance" if version < (3, 10) else ""
        ))
    
    def _check_llama_cpp(self):
        """Check llama-cpp-python installation"""
        try:
            from llama_cpp import Llama
            import llama_cpp
            version = getattr(llama_cpp, '__version__', 'unknown')
            
            self.results.append(CheckResult(
                name="llama-cpp-python",
                status=CheckStatus.PASS,
                message=f"Version: {version}",
                recommendation=""
            ))
        except ImportError:
            self.results.append(CheckResult(
                name="llama-cpp-python",
                status=CheckStatus.FAIL,
                message="Not installed",
                recommendation="Run: pip install llama-cpp-python --upgrade"
            ))
    
    def _check_env_vars(self):
        """Check environment variables"""
        required_vars = ['CUDA_HOME', 'PATH']
        optional_vars = ['LLAMA_CUDA_FORCE_MMQ', 'OMP_NUM_THREADS']
        
        missing = [v for v in required_vars if v not in os.environ]
        
        if missing:
            self.results.append(CheckResult(
                name="Environment Variables",
                status=CheckStatus.WARN,
                message=f"Missing: {', '.join(missing)}",
                recommendation="Source environment configuration script"
            ))
        else:
            self.results.append(CheckResult(
                name="Environment Variables",
                status=CheckStatus.PASS,
                message="All required variables set",
                recommendation=""
            ))
    
    def _check_virtual_env(self):
        """Check if running in virtual environment"""
        in_venv = hasattr(sys, 'real_prefix') or (
            hasattr(sys, 'base_prefix') and sys.base_prefix != sys.prefix
        )
        
        status = CheckStatus.PASS if in_venv else CheckStatus.WARN
        
        self.results.append(CheckResult(
            name="Virtual Environment",
            status=status,
            message="Active" if in_venv else "Not active",
            recommendation="Use virtual environment for isolation" if not in_venv else ""
        ))
    
    def _check_cpu_governor(self):
        """Check CPU governor setting"""
        try:
            governors = set()
            for cpu in range(psutil.cpu_count()):
                path = f'/sys/devices/system/cpu/cpu{cpu}/cpufreq/scaling_governor'
                if os.path.exists(path):
                    with open(path, 'r') as f:
                        governors.add(f.read().strip())
            
            if 'performance' in governors:
                self.results.append(CheckResult(
                    name="CPU Governor",
                    status=CheckStatus.PASS,
                    message="Performance mode enabled",
                    recommendation=""
                ))
            else:
                self.results.append(CheckResult(
                    name="CPU Governor",
                    status=CheckStatus.WARN,
                    message=f"Current: {', '.join(governors)}",
                    recommendation="Set to 'performance' for best results"
                ))
        except:
            pass
    
    def _check_swappiness(self):
        """Check swappiness setting"""
        try:
            with open('/proc/sys/vm/swappiness', 'r') as f:
                swappiness = int(f.read().strip())
            
            status = CheckStatus.PASS if swappiness <= 10 else CheckStatus.INFO
            
            self.results.append(CheckResult(
                name="Swappiness",
                status=status,
                message=f"Current: {swappiness}",
                recommendation="Set to 10 for better performance" if swappiness > 10 else ""
            ))
        except:
            pass
    
    def _check_file_descriptors(self):
        """Check file descriptor limits"""
        try:
            import resource
            soft, hard = resource.getrlimit(resource.RLIMIT_NOFILE)
            
            status = CheckStatus.PASS if soft >= 65536 else CheckStatus.WARN
            
            self.results.append(CheckResult(
                name="File Descriptors",
                status=status,
                message=f"Soft: {soft}, Hard: {hard}",
                recommendation="Increase limit with ulimit -n 65536" if soft < 65536 else ""
            ))
        except:
            pass
    
    # =========================================================================
    # RUNTIME OPTIMIZATIONS
    # =========================================================================
    
    def apply_runtime_optimizations(self):
        """Apply runtime optimizations"""
        print("\n" + "="*60)
        print("         APPLYING RUNTIME OPTIMIZATIONS")
        print("="*60 + "\n")
        
        optimizations_applied = []
        
        # Set CPU affinity (Linux only)
        try:
            os.sched_setaffinity(0, range(psutil.cpu_count()))
            optimizations_applied.append("CPU affinity set to all cores")
        except:
            pass
        
        # Set process priority
        try:
            import psutil
            process = psutil.Process()
            process.nice(-10)
            optimizations_applied.append("Process priority increased")
        except:
            pass
        
        # Set environment variables
        env_vars = {
            'PYTHONUNBUFFERED': '1',
            'PYTHONDONTWRITEBYTECODE': '1',
        }
        for key, value in env_vars.items():
            os.environ[key] = value
            optimizations_applied.append(f"Set {key}={value}")
        
        # Print applied optimizations
        for opt in optimizations_applied:
            print(f"  ✓ {opt}")
        
        if not optimizations_applied:
            print("  No runtime optimizations applied")
        
        print()
        return optimizations_applied
    
    # =========================================================================
    # POST-RUN CLEANUP
    # =========================================================================
    
    def run_post_run_cleanup(self):
        """Run post-run cleanup"""
        print("\n" + "="*60)
        print("         POST-RUN CLEANUP")
        print("="*60 + "\n")
        
        cleanup_actions = []
        
        # Clear Python cache
        try:
            import gc
            gc.collect()
            cleanup_actions.append("Garbage collection completed")
        except:
            pass
        
        # Clear CUDA cache if available
        try:
            import torch
            if torch.cuda.is_available():
                torch.cuda.empty_cache()
                cleanup_actions.append("CUDA cache cleared")
        except:
            pass
        
        # Print cleanup actions
        for action in cleanup_actions:
            print(f"  ✓ {action}")
        
        if not cleanup_actions:
            print("  No cleanup actions needed")
        
        print()
        return cleanup_actions
    
    # =========================================================================
    # UTILITY METHODS
    # =========================================================================
    
    def _print_results(self):
        """Print check results"""
        for result in self.results:
            color = {
                CheckStatus.PASS: '\033[92m',   # Green
                CheckStatus.FAIL: '\033[91m',   # Red
                CheckStatus.WARN: '\033[93m',   # Yellow
                CheckStatus.INFO: '\033[94m',   # Blue
            }.get(result.status, '')
            reset = '\033[0m'
            
            print(f"{color}{result.status.value}{reset} {result.name}")
            print(f"   {result.message}")
            if result.recommendation:
                print(f"   → {result.recommendation}")
            print()
        
        # Summary
        pass_count = sum(1 for r in self.results if r.status == CheckStatus.PASS)
        fail_count = sum(1 for r in self.results if r.status == CheckStatus.FAIL)
        warn_count = sum(1 for r in self.results if r.status == CheckStatus.WARN)
        
        print("-" * 60)
        print(f"Summary: {pass_count} passed, {fail_count} failed, {warn_count} warnings")
        print("=" * 60 + "\n")
    
    def get_optimization_recommendations(self) -> List[str]:
        """Get list of optimization recommendations"""
        recommendations = []
        
        for result in self.results:
            if result.recommendation:
                recommendations.append(f"{result.name}: {result.recommendation}")
        
        return recommendations
    
    def is_ready(self) -> bool:
        """Check if system is ready for optimal performance"""
        for result in self.results:
            if result.status == CheckStatus.FAIL:
                return False
        return True


# Main execution
if __name__ == "__main__":
    checklist = OptimizationChecklist()
    
    # Run pre-run checks
    checklist.run_pre_run_checks()
    
    # Apply runtime optimizations
    checklist.apply_runtime_optimizations()
    
    # Run post-run cleanup (for demonstration)
    checklist.run_post_run_cleanup()
    
    # Final status
    if checklist.is_ready():
        print("\n✓ System is ready for optimal performance!")
    else:
        print("\n⚠ Please address failed checks before running.")
