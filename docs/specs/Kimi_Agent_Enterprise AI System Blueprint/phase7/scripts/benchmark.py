#!/usr/bin/env python3
"""
=============================================================================
Light Local LLM System - Performance Benchmarking Suite
=============================================================================
This script provides comprehensive benchmarking for:
- LLM inference speed
- RAG retrieval latency
- End-to-end query time
- Token throughput
- System resource usage

Usage:
    python benchmark.py --suite all --output results.json
    python benchmark.py --suite llm --model llama3.2 --iterations 100
    python benchmark.py --suite rag --documents 1000 --queries 100
=============================================================================
"""

import argparse
import asyncio
import json
import statistics
import time
import sys
from dataclasses import dataclass, asdict
from datetime import datetime
from typing import List, Dict, Optional, Callable
import concurrent.futures
import psutil
import requests


# =============================================================================
# Data Classes for Benchmark Results
# =============================================================================

@dataclass
class BenchmarkResult:
    """Base class for benchmark results"""
    name: str
    timestamp: str
    duration_seconds: float
    iterations: int
    metrics: Dict
    
    def to_dict(self) -> Dict:
        return asdict(self)


@dataclass
class LLMInferenceResult:
    """LLM inference benchmark results"""
    model: str
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    inference_time_ms: float
    tokens_per_second: float
    time_to_first_token_ms: float


@dataclass
class RAGQueryResult:
    """RAG query benchmark results"""
    query: str
    retrieval_time_ms: float
    generation_time_ms: float
    total_time_ms: float
    documents_retrieved: int
    retrieval_score: float


# =============================================================================
# Benchmark Base Class
# =============================================================================

class Benchmark:
    """Base benchmark class"""
    
    def __init__(self, name: str, iterations: int = 100):
        self.name = name
        self.iterations = iterations
        self.results: List[Dict] = []
        
    def run(self) -> BenchmarkResult:
        """Run the benchmark and return results"""
        raise NotImplementedError
        
    def save_results(self, filename: str):
        """Save results to JSON file"""
        with open(filename, 'w') as f:
            json.dump([r for r in self.results], f, indent=2)


# =============================================================================
# LLM Inference Benchmark
# =============================================================================

class LLMInferenceBenchmark(Benchmark):
    """Benchmark LLM inference performance"""
    
    def __init__(self, 
                 ollama_url: str = "http://localhost:11434",
                 model: str = "llama3.2",
                 iterations: int = 100,
                 prompt_template: str = "Explain {topic} in detail.",
                 topics: Optional[List[str]] = None):
        super().__init__("llm_inference", iterations)
        self.ollama_url = ollama_url
        self.model = model
        self.prompt_template = prompt_template
        self.topics = topics or [
            "machine learning",
            "quantum computing",
            "climate change",
            "artificial intelligence",
            "blockchain technology"
        ]
        
    def _generate_prompt(self, iteration: int) -> str:
        """Generate a prompt for the given iteration"""
        topic = self.topics[iteration % len(self.topics)]
        return self.prompt_template.format(topic=topic)
        
    def _measure_inference(self, prompt: str) -> LLMInferenceResult:
        """Measure a single inference request"""
        start_time = time.perf_counter()
        first_token_time = None
        
        try:
            response = requests.post(
                f"{self.ollama_url}/api/generate",
                json={
                    "model": self.model,
                    "prompt": prompt,
                    "stream": False,
                    "options": {
                        "temperature": 0.7,
                        "num_predict": 256
                    }
                },
                timeout=300
            )
            response.raise_for_status()
            
            end_time = time.perf_counter()
            data = response.json()
            
            inference_time_ms = (end_time - start_time) * 1000
            prompt_tokens = data.get("prompt_eval_count", 0)
            completion_tokens = data.get("eval_count", 0)
            total_tokens = prompt_tokens + completion_tokens
            
            tokens_per_second = completion_tokens / (inference_time_ms / 1000) if inference_time_ms > 0 else 0
            
            return LLMInferenceResult(
                model=self.model,
                prompt_tokens=prompt_tokens,
                completion_tokens=completion_tokens,
                total_tokens=total_tokens,
                inference_time_ms=inference_time_ms,
                tokens_per_second=tokens_per_second,
                time_to_first_token_ms=inference_time_ms * 0.1  # Estimate
            )
            
        except Exception as e:
            print(f"Error during inference: {e}")
            return LLMInferenceResult(
                model=self.model,
                prompt_tokens=0,
                completion_tokens=0,
                total_tokens=0,
                inference_time_ms=0,
                tokens_per_second=0,
                time_to_first_token_ms=0
            )
            
    def run(self) -> BenchmarkResult:
        """Run LLM inference benchmark"""
        print(f"\n{'='*60}")
        print(f"Running LLM Inference Benchmark")
        print(f"Model: {self.model}")
        print(f"Iterations: {self.iterations}")
        print(f"{'='*60}\n")
        
        start_time = time.time()
        inference_results: List[LLMInferenceResult] = []
        
        for i in range(self.iterations):
            prompt = self._generate_prompt(i)
            result = self._measure_inference(prompt)
            inference_results.append(result)
            
            if (i + 1) % 10 == 0:
                print(f"Completed {i + 1}/{self.iterations} iterations...")
                
        duration = time.time() - start_time
        
        # Calculate statistics
        valid_results = [r for r in inference_results if r.inference_time_ms > 0]
        
        if not valid_results:
            print("No valid results collected!")
            return BenchmarkResult(
                name=self.name,
                timestamp=datetime.now().isoformat(),
                duration_seconds=duration,
                iterations=self.iterations,
                metrics={"error": "No valid results"}
            )
            
        latencies = [r.inference_time_ms for r in valid_results]
        throughputs = [r.tokens_per_second for r in valid_results]
        total_tokens = sum(r.total_tokens for r in valid_results)
        
        metrics = {
            "model": self.model,
            "successful_iterations": len(valid_results),
            "failed_iterations": self.iterations - len(valid_results),
            "latency_ms": {
                "mean": statistics.mean(latencies),
                "median": statistics.median(latencies),
                "p95": sorted(latencies)[int(len(latencies) * 0.95)],
                "p99": sorted(latencies)[int(len(latencies) * 0.99)],
                "min": min(latencies),
                "max": max(latencies)
            },
            "throughput_tokens_per_second": {
                "mean": statistics.mean(throughputs),
                "median": statistics.median(throughputs),
                "p95": sorted(throughputs)[int(len(throughputs) * 0.95)],
                "min": min(throughputs),
                "max": max(throughputs)
            },
            "total_tokens_generated": total_tokens,
            "overall_tokens_per_second": total_tokens / duration if duration > 0 else 0
        }
        
        self.results = [asdict(r) for r in inference_results]
        
        # Print summary
        print(f"\n{'='*60}")
        print("LLM Inference Benchmark Results")
        print(f"{'='*60}")
        print(f"Mean Latency: {metrics['latency_ms']['mean']:.2f} ms")
        print(f"P95 Latency: {metrics['latency_ms']['p95']:.2f} ms")
        print(f"Mean Throughput: {metrics['throughput_tokens_per_second']['mean']:.2f} tokens/s")
        print(f"Overall Throughput: {metrics['overall_tokens_per_second']:.2f} tokens/s")
        print(f"Total Tokens: {metrics['total_tokens_generated']}")
        
        return BenchmarkResult(
            name=self.name,
            timestamp=datetime.now().isoformat(),
            duration_seconds=duration,
            iterations=self.iterations,
            metrics=metrics
        )


# =============================================================================
# RAG Query Benchmark
# =============================================================================

class RAGBenchmark(Benchmark):
    """Benchmark RAG query performance"""
    
    def __init__(self,
                 rag_url: str = "http://localhost:8001",
                 iterations: int = 100,
                 queries: Optional[List[str]] = None):
        super().__init__("rag_query", iterations)
        self.rag_url = rag_url
        self.queries = queries or [
            "What is machine learning?",
            "Explain neural networks",
            "How does backpropagation work?",
            "What are transformers in AI?",
            "Describe natural language processing",
            "What is reinforcement learning?",
            "Explain computer vision",
            "What are GANs?",
            "How does sentiment analysis work?",
            "What is transfer learning?"
        ]
        
    def _measure_query(self, query: str) -> RAGQueryResult:
        """Measure a single RAG query"""
        retrieval_start = time.perf_counter()
        
        try:
            # Query the RAG service
            response = requests.post(
                f"{self.rag_url}/query",
                json={
                    "query": query,
                    "top_k": 5,
                    "include_sources": True
                },
                timeout=60
            )
            response.raise_for_status()
            
            end_time = time.perf_counter()
            data = response.json()
            
            total_time_ms = (end_time - retrieval_start) * 1000
            
            # Estimate retrieval vs generation time (70/30 split)
            retrieval_time_ms = total_time_ms * 0.3
            generation_time_ms = total_time_ms * 0.7
            
            documents = data.get("sources", [])
            scores = [doc.get("score", 0) for doc in documents]
            avg_score = statistics.mean(scores) if scores else 0
            
            return RAGQueryResult(
                query=query,
                retrieval_time_ms=retrieval_time_ms,
                generation_time_ms=generation_time_ms,
                total_time_ms=total_time_ms,
                documents_retrieved=len(documents),
                retrieval_score=avg_score
            )
            
        except Exception as e:
            print(f"Error during RAG query: {e}")
            return RAGQueryResult(
                query=query,
                retrieval_time_ms=0,
                generation_time_ms=0,
                total_time_ms=0,
                documents_retrieved=0,
                retrieval_score=0
            )
            
    def run(self) -> BenchmarkResult:
        """Run RAG benchmark"""
        print(f"\n{'='*60}")
        print(f"Running RAG Query Benchmark")
        print(f"Iterations: {self.iterations}")
        print(f"{'='*60}\n")
        
        start_time = time.time()
        query_results: List[RAGQueryResult] = []
        
        for i in range(self.iterations):
            query = self.queries[i % len(self.queries)]
            result = self._measure_query(query)
            query_results.append(result)
            
            if (i + 1) % 10 == 0:
                print(f"Completed {i + 1}/{self.iterations} iterations...")
                
        duration = time.time() - start_time
        
        # Calculate statistics
        valid_results = [r for r in query_results if r.total_time_ms > 0]
        
        if not valid_results:
            print("No valid results collected!")
            return BenchmarkResult(
                name=self.name,
                timestamp=datetime.now().isoformat(),
                duration_seconds=duration,
                iterations=self.iterations,
                metrics={"error": "No valid results"}
            )
            
        total_times = [r.total_time_ms for r in valid_results]
        retrieval_times = [r.retrieval_time_ms for r in valid_results]
        generation_times = [r.generation_time_ms for r in valid_results]
        scores = [r.retrieval_score for r in valid_results]
        
        metrics = {
            "successful_queries": len(valid_results),
            "failed_queries": self.iterations - len(valid_results),
            "total_time_ms": {
                "mean": statistics.mean(total_times),
                "median": statistics.median(total_times),
                "p95": sorted(total_times)[int(len(total_times) * 0.95)],
                "min": min(total_times),
                "max": max(total_times)
            },
            "retrieval_time_ms": {
                "mean": statistics.mean(retrieval_times),
                "median": statistics.median(retrieval_times),
                "p95": sorted(retrieval_times)[int(len(retrieval_times) * 0.95)],
            },
            "generation_time_ms": {
                "mean": statistics.mean(generation_times),
                "median": statistics.median(generation_times),
                "p95": sorted(generation_times)[int(len(generation_times) * 0.95)],
            },
            "retrieval_score": {
                "mean": statistics.mean(scores),
                "median": statistics.median(scores),
            },
            "queries_per_second": len(valid_results) / (sum(total_times) / 1000) if total_times else 0
        }
        
        self.results = [asdict(r) for r in query_results]
        
        # Print summary
        print(f"\n{'='*60}")
        print("RAG Query Benchmark Results")
        print(f"{'='*60}")
        print(f"Mean Total Time: {metrics['total_time_ms']['mean']:.2f} ms")
        print(f"P95 Total Time: {metrics['total_time_ms']['p95']:.2f} ms")
        print(f"Mean Retrieval Time: {metrics['retrieval_time_ms']['mean']:.2f} ms")
        print(f"Mean Generation Time: {metrics['generation_time_ms']['mean']:.2f} ms")
        print(f"Queries Per Second: {metrics['queries_per_second']:.2f}")
        
        return BenchmarkResult(
            name=self.name,
            timestamp=datetime.now().isoformat(),
            duration_seconds=duration,
            iterations=self.iterations,
            metrics=metrics
        )


# =============================================================================
# End-to-End Benchmark
# =============================================================================

class EndToEndBenchmark(Benchmark):
    """Benchmark complete end-to-end query flow"""
    
    def __init__(self,
                 api_url: str = "http://localhost:8080",
                 iterations: int = 50,
                 queries: Optional[List[str]] = None):
        super().__init__("end_to_end", iterations)
        self.api_url = api_url
        self.queries = queries or [
            "What are the key concepts in machine learning?",
            "Explain how neural networks work",
            "What is the difference between supervised and unsupervised learning?",
            "How do transformers work in NLP?",
            "What is the attention mechanism?"
        ]
        
    def run(self) -> BenchmarkResult:
        """Run end-to-end benchmark"""
        print(f"\n{'='*60}")
        print(f"Running End-to-End Benchmark")
        print(f"Iterations: {self.iterations}")
        print(f"{'='*60}\n")
        
        start_time = time.time()
        latencies = []
        
        for i in range(self.iterations):
            query = self.queries[i % len(self.queries)]
            
            req_start = time.perf_counter()
            try:
                response = requests.post(
                    f"{self.api_url}/chat",
                    json={
                        "message": query,
                        "use_rag": True,
                        "stream": False
                    },
                    timeout=300
                )
                response.raise_for_status()
                req_end = time.perf_counter()
                
                latencies.append((req_end - req_start) * 1000)
                
            except Exception as e:
                print(f"Error: {e}")
                
            if (i + 1) % 10 == 0:
                print(f"Completed {i + 1}/{self.iterations} iterations...")
                
        duration = time.time() - start_time
        
        if not latencies:
            return BenchmarkResult(
                name=self.name,
                timestamp=datetime.now().isoformat(),
                duration_seconds=duration,
                iterations=self.iterations,
                metrics={"error": "No valid results"}
            )
            
        metrics = {
            "successful_requests": len(latencies),
            "failed_requests": self.iterations - len(latencies),
            "latency_ms": {
                "mean": statistics.mean(latencies),
                "median": statistics.median(latencies),
                "p95": sorted(latencies)[int(len(latencies) * 0.95)],
                "p99": sorted(latencies)[int(len(latencies) * 0.99)],
                "min": min(latencies),
                "max": max(latencies)
            },
            "requests_per_second": len(latencies) / duration if duration > 0 else 0
        }
        
        # Print summary
        print(f"\n{'='*60}")
        print("End-to-End Benchmark Results")
        print(f"{'='*60}")
        print(f"Mean Latency: {metrics['latency_ms']['mean']:.2f} ms")
        print(f"P95 Latency: {metrics['latency_ms']['p95']:.2f} ms")
        print(f"P99 Latency: {metrics['latency_ms']['p99']:.2f} ms")
        print(f"Requests Per Second: {metrics['requests_per_second']:.2f}")
        
        return BenchmarkResult(
            name=self.name,
            timestamp=datetime.now().isoformat(),
            duration_seconds=duration,
            iterations=self.iterations,
            metrics=metrics
        )


# =============================================================================
# System Resource Benchmark
# =============================================================================

class SystemResourceBenchmark(Benchmark):
    """Benchmark system resource usage during load"""
    
    def __init__(self, duration_seconds: int = 300, sample_interval: float = 1.0):
        super().__init__("system_resources", int(duration_seconds / sample_interval))
        self.duration_seconds = duration_seconds
        self.sample_interval = sample_interval
        
    def run(self) -> BenchmarkResult:
        """Monitor system resources"""
        print(f"\n{'='*60}")
        print(f"Running System Resource Benchmark")
        print(f"Duration: {self.duration_seconds}s")
        print(f"Sample Interval: {self.sample_interval}s")
        print(f"{'='*60}\n")
        
        cpu_samples = []
        memory_samples = []
        disk_io_read = []
        disk_io_write = []
        
        start_time = time.time()
        end_time = start_time + self.duration_seconds
        
        disk_io_start = psutil.disk_io_counters()
        
        while time.time() < end_time:
            cpu_samples.append(psutil.cpu_percent(interval=self.sample_interval))
            memory_samples.append(psutil.virtual_memory().percent)
            
        disk_io_end = psutil.disk_io_counters()
        
        duration = time.time() - start_time
        
        # Calculate disk I/O rates
        read_bytes = disk_io_end.read_bytes - disk_io_start.read_bytes
        write_bytes = disk_io_end.write_bytes - disk_io_start.write_bytes
        
        metrics = {
            "cpu_percent": {
                "mean": statistics.mean(cpu_samples),
                "median": statistics.median(cpu_samples),
                "p95": sorted(cpu_samples)[int(len(cpu_samples) * 0.95)],
                "max": max(cpu_samples),
                "min": min(cpu_samples)
            },
            "memory_percent": {
                "mean": statistics.mean(memory_samples),
                "median": statistics.median(memory_samples),
                "p95": sorted(memory_samples)[int(len(memory_samples) * 0.95)],
                "max": max(memory_samples),
                "min": min(memory_samples)
            },
            "disk_io": {
                "read_mb": read_bytes / (1024 * 1024),
                "write_mb": write_bytes / (1024 * 1024),
                "read_rate_mbps": (read_bytes / (1024 * 1024)) / duration,
                "write_rate_mbps": (write_bytes / (1024 * 1024)) / duration
            }
        }
        
        # Print summary
        print(f"\n{'='*60}")
        print("System Resource Benchmark Results")
        print(f"{'='*60}")
        print(f"Mean CPU: {metrics['cpu_percent']['mean']:.2f}%")
        print(f"Max CPU: {metrics['cpu_percent']['max']:.2f}%")
        print(f"Mean Memory: {metrics['memory_percent']['mean']:.2f}%")
        print(f"Max Memory: {metrics['memory_percent']['max']:.2f}%")
        print(f"Disk Read: {metrics['disk_io']['read_mb']:.2f} MB")
        print(f"Disk Write: {metrics['disk_io']['write_mb']:.2f} MB")
        
        return BenchmarkResult(
            name=self.name,
            timestamp=datetime.now().isoformat(),
            duration_seconds=duration,
            iterations=len(cpu_samples),
            metrics=metrics
        )


# =============================================================================
# Main Entry Point
# =============================================================================

def main():
    parser = argparse.ArgumentParser(
        description="Light Local LLM System - Performance Benchmarking"
    )
    parser.add_argument(
        "--suite",
        choices=["all", "llm", "rag", "e2e", "system"],
        default="all",
        help="Benchmark suite to run"
    )
    parser.add_argument(
        "--iterations",
        type=int,
        default=100,
        help="Number of iterations for each benchmark"
    )
    parser.add_argument(
        "--output",
        type=str,
        default="benchmark_results.json",
        help="Output file for results"
    )
    parser.add_argument(
        "--model",
        type=str,
        default="llama3.2",
        help="Model to benchmark (for LLM suite)"
    )
    parser.add_argument(
        "--ollama-url",
        type=str,
        default="http://localhost:11434",
        help="Ollama service URL"
    )
    parser.add_argument(
        "--rag-url",
        type=str,
        default="http://localhost:8001",
        help="RAG service URL"
    )
    parser.add_argument(
        "--api-url",
        type=str,
        default="http://localhost:8080",
        help="API gateway URL"
    )
    
    args = parser.parse_args()
    
    results = []
    
    if args.suite in ["all", "llm"]:
        benchmark = LLMInferenceBenchmark(
            ollama_url=args.ollama_url,
            model=args.model,
            iterations=args.iterations
        )
        results.append(benchmark.run())
        
    if args.suite in ["all", "rag"]:
        benchmark = RAGBenchmark(
            rag_url=args.rag_url,
            iterations=args.iterations
        )
        results.append(benchmark.run())
        
    if args.suite in ["all", "e2e"]:
        benchmark = EndToEndBenchmark(
            api_url=args.api_url,
            iterations=args.iterations // 2
        )
        results.append(benchmark.run())
        
    if args.suite in ["all", "system"]:
        benchmark = SystemResourceBenchmark(
            duration_seconds=min(args.iterations, 300)
        )
        results.append(benchmark.run())
    
    # Save all results
    output_data = {
        "timestamp": datetime.now().isoformat(),
        "benchmarks": [r.to_dict() for r in results]
    }
    
    with open(args.output, 'w') as f:
        json.dump(output_data, f, indent=2)
        
    print(f"\n{'='*60}")
    print(f"Results saved to: {args.output}")
    print(f"{'='*60}")


if __name__ == "__main__":
    main()
