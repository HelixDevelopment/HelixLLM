#!/usr/bin/env python3
"""
HelixLLM Performance Monitoring System
Tracks token generation speed, memory usage, GPU utilization, and latency
"""

import os
import sys
import time
import json
import threading
import statistics
from typing import Dict, Any, List, Optional, Callable
from dataclasses import dataclass, field
from collections import deque
from datetime import datetime
from pathlib import Path

try:
    import psutil
    PSUTIL_AVAILABLE = True
except ImportError:
    PSUTIL_AVAILABLE = False

try:
    from pynvml import nvmlInit, nvmlShutdown, nvmlDeviceGetHandleByIndex, \
                      nvmlDeviceGetMemoryInfo, nvmlDeviceGetUtilizationRates, \
                      nvmlDeviceGetTemperature, nvmlDeviceGetPowerUsage
    NVML_AVAILABLE = True
except ImportError:
    NVML_AVAILABLE = False


@dataclass
class PerformanceMetrics:
    """Performance metrics snapshot"""
    timestamp: float = field(default_factory=time.time)
    
    # Token metrics
    tokens_generated: int = 0
    generation_time: float = 0.0
    tokens_per_second: float = 0.0
    prompt_tokens: int = 0
    total_tokens: int = 0
    
    # Latency metrics
    time_to_first_token: float = 0.0
    inter_token_latency: float = 0.0
    
    # Memory metrics
    ram_used_gb: float = 0.0
    ram_percent: float = 0.0
    vram_used_mb: float = 0.0
    vram_percent: float = 0.0
    
    # GPU metrics
    gpu_utilization: float = 0.0
    gpu_temperature: float = 0.0
    gpu_power_watts: float = 0.0


class PerformanceMonitor:
    """Monitors performance metrics during inference"""
    
    def __init__(self, history_size: int = 100):
        self.history_size = history_size
        self.metrics_history: deque = deque(maxlen=history_size)
        self.current_metrics = PerformanceMetrics()
        
        # GPU monitoring
        self.gpu_handle = None
        if NVML_AVAILABLE:
            try:
                nvmlInit()
                self.gpu_handle = nvmlDeviceGetHandleByIndex(0)
            except:
                pass
        
        # Running statistics
        self._token_times: List[float] = []
        self._generation_start_time: Optional[float] = None
        self._first_token_time: Optional[float] = None
        
        # Callbacks
        self._on_token_callbacks: List[Callable] = []
        self._on_generation_end_callbacks: List[Callable] = []
    
    def start_generation(self):
        """Mark the start of generation"""
        self._generation_start_time = time.time()
        self._first_token_time = None
        self._token_times = []
        self.current_metrics = PerformanceMetrics()
    
    def on_token(self, token_text: str = ""):
        """Called for each generated token"""
        now = time.time()
        
        if self._first_token_time is None:
            self._first_token_time = now
            self.current_metrics.time_to_first_token = now - self._generation_start_time
        
        self._token_times.append(now)
        
        # Calculate inter-token latency
        if len(self._token_times) > 1:
            self.current_metrics.inter_token_latency = (
                self._token_times[-1] - self._token_times[-2]
            )
        
        # Update metrics
        self.current_metrics.tokens_generated = len(self._token_times)
        
        # Trigger callbacks
        for callback in self._on_token_callbacks:
            callback(self.current_metrics)
    
    def end_generation(self, prompt_tokens: int = 0):
        """Mark the end of generation"""
        if self._generation_start_time is None:
            return
        
        end_time = time.time()
        self.current_metrics.generation_time = end_time - self._generation_start_time
        self.current_metrics.prompt_tokens = prompt_tokens
        self.current_metrics.total_tokens = prompt_tokens + self.current_metrics.tokens_generated
        
        # Calculate tokens per second
        if self.current_metrics.generation_time > 0:
            self.current_metrics.tokens_per_second = (
                self.current_metrics.tokens_generated / self.current_metrics.generation_time
            )
        
        # Collect system metrics
        self._collect_system_metrics()
        
        # Add to history
        self.metrics_history.append(self.current_metrics)
        
        # Trigger callbacks
        for callback in self._on_generation_end_callbacks:
            callback(self.current_metrics)
    
    def _collect_system_metrics(self):
        """Collect system resource metrics"""
        # RAM metrics
        if PSUTIL_AVAILABLE:
            mem = psutil.virtual_memory()
            self.current_metrics.ram_used_gb = mem.used / (1024**3)
            self.current_metrics.ram_percent = mem.percent
        
        # GPU metrics
        if self.gpu_handle:
            try:
                # VRAM
                mem_info = nvmlDeviceGetMemoryInfo(self.gpu_handle)
                self.current_metrics.vram_used_mb = mem_info.used / (1024**2)
                self.current_metrics.vram_percent = (mem_info.used / mem_info.total) * 100
                
                # Utilization
                util = nvmlDeviceGetUtilizationRates(self.gpu_handle)
                self.current_metrics.gpu_utilization = util.gpu
                
                # Temperature
                self.current_metrics.gpu_temperature = nvmlDeviceGetTemperature(
                    self.gpu_handle, 0  # NVML_TEMPERATURE_GPU
                )
                
                # Power
                self.current_metrics.gpu_power_watts = nvmlDeviceGetPowerUsage(self.gpu_handle) / 1000
            except:
                pass
    
    def get_current_metrics(self) -> PerformanceMetrics:
        """Get current metrics"""
        return self.current_metrics
    
    def get_statistics(self) -> Dict[str, Any]:
        """Get statistics from history"""
        if not self.metrics_history:
            return {}
        
        tps_values = [m.tokens_per_second for m in self.metrics_history]
        latency_values = [m.time_to_first_token for m in self.metrics_history]
        
        return {
            'total_generations': len(self.metrics_history),
            'avg_tokens_per_second': statistics.mean(tps_values),
            'min_tokens_per_second': min(tps_values),
            'max_tokens_per_second': max(tps_values),
            'std_tokens_per_second': statistics.stdev(tps_values) if len(tps_values) > 1 else 0,
            'avg_time_to_first_token': statistics.mean(latency_values),
            'avg_vram_used_mb': statistics.mean([m.vram_used_mb for m in self.metrics_history]),
            'avg_gpu_utilization': statistics.mean([m.gpu_utilization for m in self.metrics_history]),
        }
    
    def print_summary(self):
        """Print performance summary"""
        print("\n" + "="*60)
        print("         Performance Summary")
        print("="*60)
        
        if not self.metrics_history:
            print("No data collected yet.")
            return
        
        stats = self.get_statistics()
        
        print(f"\nTotal Generations: {stats['total_generations']}")
        
        print("\n[Token Generation]")
        print(f"  Average: {stats['avg_tokens_per_second']:.1f} tokens/sec")
        print(f"  Min: {stats['min_tokens_per_second']:.1f} tokens/sec")
        print(f"  Max: {stats['max_tokens_per_second']:.1f} tokens/sec")
        print(f"  Std Dev: {stats['std_tokens_per_second']:.1f}")
        
        print("\n[Latency]")
        print(f"  Avg Time to First Token: {stats['avg_time_to_first_token']*1000:.1f} ms")
        
        print("\n[Resource Usage]")
        print(f"  Avg VRAM Used: {stats['avg_vram_used_mb']:.0f} MB")
        print(f"  Avg GPU Utilization: {stats['avg_gpu_utilization']:.1f}%")
        
        print("="*60 + "\n")
    
    def register_on_token(self, callback: Callable):
        """Register callback for token events"""
        self._on_token_callbacks.append(callback)
    
    def register_on_generation_end(self, callback: Callable):
        """Register callback for generation end events"""
        self._on_generation_end_callbacks.append(callback)
    
    def save_report(self, path: str):
        """Save performance report to file"""
        report = {
            'timestamp': datetime.now().isoformat(),
            'statistics': self.get_statistics(),
            'history': [
                {
                    'timestamp': m.timestamp,
                    'tokens_generated': m.tokens_generated,
                    'tokens_per_second': m.tokens_per_second,
                    'generation_time': m.generation_time,
                    'time_to_first_token': m.time_to_first_token,
                    'vram_used_mb': m.vram_used_mb,
                    'gpu_utilization': m.gpu_utilization,
                }
                for m in self.metrics_history
            ]
        }
        
        with open(path, 'w') as f:
            json.dump(report, f, indent=2)
        
        print(f"Performance report saved to: {path}")
    
    def __del__(self):
        """Cleanup"""
        if NVML_AVAILABLE and self.gpu_handle:
            try:
                nvmlShutdown()
            except:
                pass


class RealTimeMonitor:
    """Real-time performance monitoring with live display"""
    
    def __init__(self, update_interval: float = 1.0):
        self.update_interval = update_interval
        self.running = False
        self.monitor_thread: Optional[threading.Thread] = None
        self._metrics_callback: Optional[Callable] = None
    
    def start(self, metrics_callback: Callable = None):
        """Start real-time monitoring"""
        self.running = True
        self._metrics_callback = metrics_callback
        self.monitor_thread = threading.Thread(target=self._monitor_loop)
        self.monitor_thread.daemon = True
        self.monitor_thread.start()
    
    def stop(self):
        """Stop real-time monitoring"""
        self.running = False
        if self.monitor_thread:
            self.monitor_thread.join(timeout=2.0)
    
    def _monitor_loop(self):
        """Main monitoring loop"""
        while self.running:
            metrics = self._collect_metrics()
            
            if self._metrics_callback:
                self._metrics_callback(metrics)
            
            time.sleep(self.update_interval)
    
    def _collect_metrics(self) -> Dict[str, Any]:
        """Collect current system metrics"""
        metrics = {
            'timestamp': time.time(),
            'cpu_percent': 0.0,
            'ram_percent': 0.0,
            'vram_percent': 0.0,
            'gpu_utilization': 0.0,
        }
        
        if PSUTIL_AVAILABLE:
            metrics['cpu_percent'] = psutil.cpu_percent(interval=0.1)
            metrics['ram_percent'] = psutil.virtual_memory().percent
        
        if NVML_AVAILABLE:
            try:
                handle = nvmlDeviceGetHandleByIndex(0)
                mem_info = nvmlDeviceGetMemoryInfo(handle)
                util = nvmlDeviceGetUtilizationRates(handle)
                
                metrics['vram_percent'] = (mem_info.used / mem_info.total) * 100
                metrics['gpu_utilization'] = util.gpu
            except:
                pass
        
        return metrics
    
    def print_live(self, metrics: Dict[str, Any]):
        """Print live metrics (for terminal)"""
        # Clear line and print
        sys.stdout.write('\r')
        sys.stdout.write(
            f"CPU: {metrics['cpu_percent']:5.1f}% | "
            f"RAM: {metrics['ram_percent']:5.1f}% | "
            f"VRAM: {metrics['vram_percent']:5.1f}% | "
            f"GPU: {metrics['gpu_utilization']:5.1f}%"
        )
        sys.stdout.flush()


class BenchmarkSuite:
    """Comprehensive benchmarking suite"""
    
    BENCHMARK_PROMPTS = [
        "Explain the concept of machine learning in simple terms.",
        "Write a short story about a robot learning to paint.",
        "What are the main differences between Python and JavaScript?",
        "Describe the process of photosynthesis.",
        "How does blockchain technology work?",
    ]
    
    def __init__(self, llm, monitor: PerformanceMonitor):
        self.llm = llm
        self.monitor = monitor
    
    def run_benchmark(self, 
                      prompts: List[str] = None,
                      max_tokens: int = 256,
                      warmup: bool = True) -> Dict[str, Any]:
        """Run comprehensive benchmark"""
        if prompts is None:
            prompts = self.BENCHMARK_PROMPTS
        
        print(f"\nRunning benchmark with {len(prompts)} prompts...")
        print(f"Max tokens per generation: {max_tokens}\n")
        
        # Warmup
        if warmup:
            print("Warming up...")
            self.llm("Hello", max_tokens=10)
            print("Warmup complete.\n")
        
        results = []
        
        for i, prompt in enumerate(prompts, 1):
            print(f"[{i}/{len(prompts)}] Testing: '{prompt[:50]}...'")
            
            self.monitor.start_generation()
            
            output = self.llm(
                prompt,
                max_tokens=max_tokens,
                stream=False
            )
            
            self.monitor.end_generation(prompt_tokens=output['usage']['prompt_tokens'])
            
            metrics = self.monitor.get_current_metrics()
            results.append({
                'prompt': prompt,
                'tokens_generated': metrics.tokens_generated,
                'tokens_per_second': metrics.tokens_per_second,
                'generation_time': metrics.generation_time,
                'time_to_first_token': metrics.time_to_first_token,
            })
            
            print(f"  -> {metrics.tokens_per_second:.1f} tokens/sec, "
                  f"{metrics.generation_time:.2f}s, "
                  f"TTFT: {metrics.time_to_first_token*1000:.1f}ms")
        
        # Calculate aggregate statistics
        tps_values = [r['tokens_per_second'] for r in results]
        ttft_values = [r['time_to_first_token'] for r in results]
        
        summary = {
            'prompts_tested': len(prompts),
            'max_tokens': max_tokens,
            'average_tokens_per_second': statistics.mean(tps_values),
            'min_tokens_per_second': min(tps_values),
            'max_tokens_per_second': max(tps_values),
            'std_tokens_per_second': statistics.stdev(tps_values) if len(tps_values) > 1 else 0,
            'average_time_to_first_token': statistics.mean(ttft_values),
            'individual_results': results,
        }
        
        return summary
    
    def print_results(self, summary: Dict[str, Any]):
        """Print benchmark results"""
        print("\n" + "="*60)
        print("         Benchmark Results")
        print("="*60)
        
        print(f"\nPrompts Tested: {summary['prompts_tested']}")
        print(f"Max Tokens: {summary['max_tokens']}")
        
        print("\n[Token Generation Speed]")
        print(f"  Average: {summary['average_tokens_per_second']:.1f} tokens/sec")
        print(f"  Min: {summary['min_tokens_per_second']:.1f} tokens/sec")
        print(f"  Max: {summary['max_tokens_per_second']:.1f} tokens/sec")
        print(f"  Std Dev: {summary['std_tokens_per_second']:.1f}")
        
        print("\n[Latency]")
        print(f"  Average TTFT: {summary['average_time_to_first_token']*1000:.1f} ms")
        
        print("="*60 + "\n")
    
    def save_results(self, summary: Dict[str, Any], path: str):
        """Save benchmark results"""
        with open(path, 'w') as f:
            json.dump(summary, f, indent=2)
        print(f"Benchmark results saved to: {path}")


# Main execution for testing
if __name__ == "__main__":
    print("HelixLLM Performance Monitor")
    print("="*60)
    
    # Create monitor
    monitor = PerformanceMonitor()
    
    # Test basic functionality
    print("\nTesting performance monitoring...")
    
    monitor.start_generation()
    
    # Simulate token generation
    for i in range(10):
        time.sleep(0.05)  # Simulate 50ms per token
        monitor.on_token(f"token_{i}")
    
    monitor.end_generation(prompt_tokens=5)
    
    # Print results
    monitor.print_summary()
    
    # Test real-time monitor
    print("\nTesting real-time monitor (5 seconds)...")
    rt_monitor = RealTimeMonitor(update_interval=0.5)
    rt_monitor.start(rt_monitor.print_live)
    
    time.sleep(5)
    
    rt_monitor.stop()
    print("\n\nReal-time monitor test complete.")
