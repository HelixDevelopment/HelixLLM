#!/usr/bin/env python3
"""
HelixLLM Comprehensive Benchmark Suite
Tests token generation speed, embedding performance, and retrieval latency
"""

import os
import sys
import json
import time
import statistics
from typing import Dict, Any, List, Optional
from dataclasses import dataclass, asdict
from datetime import datetime
from pathlib import Path

# Import HelixLLM components
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from 04_model_loader import HelixLLM, ModelConfig
from 07_performance_monitor import PerformanceMonitor, BenchmarkSuite


@dataclass
class BenchmarkConfig:
    """Configuration for benchmark runs"""
    llm_model_path: str = "models/Qwen2.5-1.5B-Instruct-Q4_K_M.gguf"
    embedding_model_path: str = "models/nomic-embed-text-v1.5.Q4_K_M.gguf"
    
    # Test parameters
    warmup_runs: int = 2
    benchmark_runs: int = 5
    max_tokens: int = 256
    
    # Embedding test
    embedding_batch_sizes: List[int] = None
    embedding_test_documents: int = 100
    
    def __post_init__(self):
        if self.embedding_batch_sizes is None:
            self.embedding_batch_sizes = [1, 8, 16, 32, 64]


@dataclass
class BenchmarkResults:
    """Complete benchmark results"""
    timestamp: str
    system_info: Dict[str, Any]
    llm_results: Dict[str, Any]
    embedding_results: Dict[str, Any]
    retrieval_results: Dict[str, Any]
    overall_score: float


class HelixBenchmark:
    """Comprehensive HelixLLM benchmark"""
    
    # Test prompts of varying complexity
    TEST_PROMPTS = [
        {
            "name": "Short Response",
            "prompt": "What is 2+2?",
            "expected_tokens": 10
        },
        {
            "name": "Medium Explanation",
            "prompt": "Explain what a neural network is in simple terms.",
            "expected_tokens": 100
        },
        {
            "name": "Long Generation",
            "prompt": "Write a short story about a robot learning to paint.",
            "expected_tokens": 256
        },
        {
            "name": "Code Generation",
            "prompt": "Write a Python function to calculate fibonacci numbers.",
            "expected_tokens": 150
        },
        {
            "name": "Reasoning",
            "prompt": "What are the main differences between Python and JavaScript?",
            "expected_tokens": 200
        }
    ]
    
    # Test documents for embedding
    TEST_DOCUMENTS = [
        "Machine learning is a subset of artificial intelligence.",
        "Python is a popular programming language for data science.",
        "Neural networks are inspired by biological neural systems.",
        "Deep learning has revolutionized computer vision.",
        "Natural language processing enables machines to understand text.",
        "Reinforcement learning trains agents through rewards and penalties.",
        "Transfer learning allows models to leverage pre-trained knowledge.",
        "Computer vision enables machines to interpret visual information.",
        "Speech recognition converts audio to text.",
        "Recommendation systems suggest items based on user preferences.",
    ] * 10  # 100 documents
    
    def __init__(self, config: BenchmarkConfig = None):
        self.config = config or BenchmarkConfig()
        self.helix = HelixLLM()
        self.monitor = PerformanceMonitor()
        self.results = {}
        
    def run_full_benchmark(self) -> BenchmarkResults:
        """Run complete benchmark suite"""
        print("\n" + "="*70)
        print("         HelixLLM Comprehensive Benchmark Suite")
        print("="*70)
        
        # Initialize models
        self._initialize_models()
        
        # Collect system info
        system_info = self._collect_system_info()
        
        # Run benchmarks
        llm_results = self._benchmark_llm()
        embedding_results = self._benchmark_embeddings()
        retrieval_results = self._benchmark_retrieval()
        
        # Calculate overall score
        overall_score = self._calculate_overall_score(
            llm_results, embedding_results, retrieval_results
        )
        
        # Compile results
        results = BenchmarkResults(
            timestamp=datetime.now().isoformat(),
            system_info=system_info,
            llm_results=llm_results,
            embedding_results=embedding_results,
            retrieval_results=retrieval_results,
            overall_score=overall_score
        )
        
        # Print summary
        self._print_summary(results)
        
        # Cleanup
        self.helix.shutdown()
        
        return results
    
    def _initialize_models(self):
        """Initialize HelixLLM with models"""
        print("\n[1/4] Initializing models...")
        
        # Check if models exist
        if not os.path.exists(self.config.llm_model_path):
            print(f"  Warning: LLM model not found at {self.config.llm_model_path}")
            print("  Please download the model or update the path")
            self.config.llm_model_path = None
        
        if not os.path.exists(self.config.embedding_model_path):
            print(f"  Warning: Embedding model not found at {self.config.embedding_model_path}")
            self.config.embedding_model_path = None
        
        if self.config.llm_model_path or self.config.embedding_model_path:
            self.helix.initialize(
                llm_path=self.config.llm_model_path,
                embedding_path=self.config.embedding_model_path
            )
    
    def _collect_system_info(self) -> Dict[str, Any]:
        """Collect system information"""
        import psutil
        
        info = {
            'cpu': {
                'physical_cores': psutil.cpu_count(logical=False),
                'logical_cores': psutil.cpu_count(logical=True),
                'frequency_mhz': psutil.cpu_freq().max if psutil.cpu_freq() else 0,
            },
            'ram_gb': psutil.virtual_memory().total / (1024**3),
        }
        
        # GPU info
        try:
            import subprocess
            result = subprocess.run(
                ['nvidia-smi', '--query-gpu=name,memory.total,compute_cap,driver_version',
                 '--format=csv,noheader'],
                capture_output=True, text=True
            )
            if result.returncode == 0:
                parts = [p.strip() for p in result.stdout.strip().split(',')]
                info['gpu'] = {
                    'name': parts[0],
                    'memory_gb': int(float(parts[1].split()[0])) / 1024,
                    'compute_cap': parts[2],
                    'driver': parts[3],
                }
        except:
            info['gpu'] = None
        
        return info
    
    def _benchmark_llm(self) -> Dict[str, Any]:
        """Benchmark LLM token generation"""
        if not self.helix.llm:
            return {"error": "LLM not loaded"}
        
        print("\n[2/4] Benchmarking LLM token generation...")
        
        results = {
            'tests': [],
            'summary': {}
        }
        
        # Warmup
        print("  Warming up...")
        for _ in range(self.config.warmup_runs):
            self.helix.generate("Hello", max_tokens=20)
        
        # Run tests
        for test in self.TEST_PROMPTS:
            print(f"  Testing: {test['name']}...")
            
            test_results = []
            
            for run in range(self.config.benchmark_runs):
                start_time = time.time()
                
                output = self.helix.generate(
                    test['prompt'],
                    max_tokens=test['expected_tokens']
                )
                
                test_results.append({
                    'tokens_generated': output['tokens_generated'],
                    'generation_time': output['generation_time'],
                    'tokens_per_second': output['tokens_per_second'],
                })
            
            # Calculate statistics
            tps_values = [r['tokens_per_second'] for r in test_results]
            
            results['tests'].append({
                'name': test['name'],
                'prompt': test['prompt'],
                'avg_tokens_per_second': statistics.mean(tps_values),
                'min_tokens_per_second': min(tps_values),
                'max_tokens_per_second': max(tps_values),
                'std_tokens_per_second': statistics.stdev(tps_values) if len(tps_values) > 1 else 0,
                'runs': test_results,
            })
        
        # Overall summary
        all_tps = [t['avg_tokens_per_second'] for t in results['tests']]
        results['summary'] = {
            'overall_avg_tps': statistics.mean(all_tps),
            'overall_min_tps': min(all_tps),
            'overall_max_tps': max(all_tps),
            'target_met': statistics.mean(all_tps) >= 150,
        }
        
        return results
    
    def _benchmark_embeddings(self) -> Dict[str, Any]:
        """Benchmark embedding generation"""
        if not self.helix.embedder:
            return {"error": "Embedding model not loaded"}
        
        print("\n[3/4] Benchmarking embedding generation...")
        
        results = {
            'batch_tests': [],
            'summary': {}
        }
        
        # Test different batch sizes
        for batch_size in self.config.embedding_batch_sizes:
            print(f"  Testing batch size: {batch_size}...")
            
            # Take subset for testing
            test_docs = self.TEST_DOCUMENTS[:batch_size]
            
            start_time = time.time()
            embeddings = self.helix.embed(test_docs, batch_size=batch_size)
            elapsed = time.time() - start_time
            
            docs_per_sec = len(test_docs) / elapsed
            
            results['batch_tests'].append({
                'batch_size': batch_size,
                'documents': len(test_docs),
                'time_seconds': elapsed,
                'docs_per_second': docs_per_sec,
            })
        
        # Summary
        best_batch = max(results['batch_tests'], key=lambda x: x['docs_per_second'])
        results['summary'] = {
            'best_batch_size': best_batch['batch_size'],
            'best_docs_per_second': best_batch['docs_per_second'],
            'target_met': best_batch['docs_per_second'] >= 10,
        }
        
        return results
    
    def _benchmark_retrieval(self) -> Dict[str, Any]:
        """Benchmark retrieval latency"""
        if not self.helix.embedder:
            return {"error": "Embedding model not loaded"}
        
        print("\n[4/4] Benchmarking retrieval latency...")
        
        # Generate embeddings for documents
        doc_embeddings = self.helix.embed(self.TEST_DOCUMENTS[:50])
        
        # Test queries
        queries = [
            "What is machine learning?",
            "Tell me about Python",
            "How do neural networks work?",
            "Explain computer vision",
            "What is NLP?",
        ]
        
        latencies = []
        
        for query in queries:
            start_time = time.time()
            query_embedding = self.helix.embed([query])
            
            # Simple similarity search (cosine similarity)
            import numpy as np
            query_vec = np.array(query_embedding[0])
            doc_vecs = np.array(doc_embeddings)
            
            # Calculate similarities
            similarities = np.dot(doc_vecs, query_vec) / (
                np.linalg.norm(doc_vecs, axis=1) * np.linalg.norm(query_vec)
            )
            
            # Get top 5
            top_indices = np.argsort(similarities)[-5:][::-1]
            
            elapsed = (time.time() - start_time) * 1000  # Convert to ms
            latencies.append(elapsed)
        
        results = {
            'queries_tested': len(queries),
            'latencies_ms': latencies,
            'avg_latency_ms': statistics.mean(latencies),
            'min_latency_ms': min(latencies),
            'max_latency_ms': max(latencies),
            'target_met': statistics.mean(latencies) <= 50,
        }
        
        return results
    
    def _calculate_overall_score(self, llm, embedding, retrieval) -> float:
        """Calculate overall performance score"""
        scores = []
        
        # LLM score (0-40 points)
        if 'summary' in llm and 'overall_avg_tps' in llm['summary']:
            tps = llm['summary']['overall_avg_tps']
            llm_score = min(40, (tps / 300) * 40)  # 300 TPS = 40 points
            scores.append(llm_score)
        
        # Embedding score (0-30 points)
        if 'summary' in embedding and 'best_docs_per_second' in embedding['summary']:
            dps = embedding['summary']['best_docs_per_second']
            emb_score = min(30, (dps / 20) * 30)  # 20 docs/sec = 30 points
            scores.append(emb_score)
        
        # Retrieval score (0-30 points)
        if 'avg_latency_ms' in retrieval:
            latency = retrieval['avg_latency_ms']
            ret_score = max(0, 30 - (latency / 50) * 10)  # <50ms = 30 points
            scores.append(ret_score)
        
        return sum(scores) if scores else 0
    
    def _print_summary(self, results: BenchmarkResults):
        """Print benchmark summary"""
        print("\n" + "="*70)
        print("         Benchmark Results Summary")
        print("="*70)
        
        # System info
        print("\n[System Information]")
        print(f"  CPU: {results.system_info['cpu']['physical_cores']} cores")
        print(f"  RAM: {results.system_info['ram_gb']:.1f} GB")
        if results.system_info.get('gpu'):
            gpu = results.system_info['gpu']
            print(f"  GPU: {gpu['name']} ({gpu['memory_gb']:.1f} GB)")
        
        # LLM results
        print("\n[LLM Token Generation]")
        if 'summary' in results.llm_results:
            summary = results.llm_results['summary']
            print(f"  Average Speed: {summary['overall_avg_tps']:.1f} tokens/sec")
            print(f"  Range: {summary['overall_min_tps']:.1f} - {summary['overall_max_tps']:.1f} tokens/sec")
            print(f"  Target (150+ TPS): {'✓ Met' if summary['target_met'] else '✗ Not Met'}")
        
        # Embedding results
        print("\n[Embedding Generation]")
        if 'summary' in results.embedding_results:
            summary = results.embedding_results['summary']
            print(f"  Best Speed: {summary['best_docs_per_second']:.1f} docs/sec")
            print(f"  Optimal Batch Size: {summary['best_batch_size']}")
            print(f"  Target (10+ docs/sec): {'✓ Met' if summary['target_met'] else '✗ Not Met'}")
        
        # Retrieval results
        print("\n[Retrieval Latency]")
        if 'avg_latency_ms' in results.retrieval_results:
            print(f"  Average: {results.retrieval_results['avg_latency_ms']:.1f} ms")
            print(f"  Range: {results.retrieval_results['min_latency_ms']:.1f} - {results.retrieval_results['max_latency_ms']:.1f} ms")
            print(f"  Target (<50ms): {'✓ Met' if results.retrieval_results['target_met'] else '✗ Not Met'}")
        
        # Overall score
        print("\n[Overall Performance Score]")
        print(f"  Score: {results.overall_score:.1f}/100")
        if results.overall_score >= 80:
            print("  Rating: Excellent")
        elif results.overall_score >= 60:
            print("  Rating: Good")
        elif results.overall_score >= 40:
            print("  Rating: Fair")
        else:
            print("  Rating: Needs Improvement")
        
        print("="*70 + "\n")
    
    def save_results(self, results: BenchmarkResults, path: str):
        """Save benchmark results to file"""
        with open(path, 'w') as f:
            json.dump(asdict(results), f, indent=2)
        print(f"Results saved to: {path}")


# Main execution
if __name__ == "__main__":
    print("HelixLLM Benchmark Suite")
    print("="*70)
    
    # Create benchmark configuration
    config = BenchmarkConfig()
    
    # Allow command-line override of model paths
    if len(sys.argv) > 1:
        config.llm_model_path = sys.argv[1]
    if len(sys.argv) > 2:
        config.embedding_model_path = sys.argv[2]
    
    # Run benchmark
    benchmark = HelixBenchmark(config)
    results = benchmark.run_full_benchmark()
    
    # Save results
    output_dir = Path.home() / ".config" / "helixllm" / "benchmarks"
    output_dir.mkdir(parents=True, exist_ok=True)
    
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    output_path = output_dir / f"benchmark_{timestamp}.json"
    
    benchmark.save_results(results, str(output_path))
