"""
HelixLLM RAG Pipeline - Performance Benchmarks
==============================================
Comprehensive benchmarking for all pipeline components.
"""

import time
import json
import statistics
from pathlib import Path
from typing import List, Dict, Any, Callable
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from rag_pipeline import HelixRAGPipeline, HelixRAGConfig


class BenchmarkRunner:
    """Run comprehensive benchmarks on the RAG pipeline."""
    
    def __init__(self, pipeline: HelixRAGPipeline):
        self.pipeline = pipeline
        self.results = {}
    
    def benchmark_embedding(self, iterations: int = 10) -> Dict[str, Any]:
        """Benchmark embedding performance."""
        print("\n--- Embedding Benchmark ---")
        
        test_texts = [
            ("Short (100 chars)", "x" * 100),
            ("Medium (500 chars)", "x" * 500),
            ("Long (1000 chars)", "x" * 1000),
            ("Very Long (2000 chars)", "x" * 2000),
        ]
        
        results = {}
        
        for name, text in test_texts:
            times = []
            
            # Warmup
            _ = self.pipeline.embedder.embed(text)
            
            # Benchmark
            for _ in range(iterations):
                start = time.perf_counter()
                _ = self.pipeline.embedder.embed(text)
                elapsed = (time.perf_counter() - start) * 1000
                times.append(elapsed)
            
            results[name] = {
                "mean_ms": statistics.mean(times),
                "median_ms": statistics.median(times),
                "min_ms": min(times),
                "max_ms": max(times),
                "std_ms": statistics.stdev(times) if len(times) > 1 else 0
            }
            
            print(f"  {name}: {results[name]['mean_ms']:.2f}ms "
                  f"(±{results[name]['std_ms']:.2f}ms)")
        
        # Batch benchmark
        print("\n  Batch Processing:")
        batch_sizes = [8, 16, 32, 64]
        
        for batch_size in batch_sizes:
            texts = ["x" * 500] * batch_size
            
            times = []
            for _ in range(5):
                start = time.perf_counter()
                _ = self.pipeline.embedder.embed(texts)
                elapsed = (time.perf_counter() - start) * 1000
                times.append(elapsed)
            
            mean_time = statistics.mean(times)
            throughput = batch_size / (mean_time / 1000)
            
            results[f"batch_{batch_size}"] = {
                "mean_ms": mean_time,
                "throughput": throughput
            }
            
            print(f"    Batch-{batch_size}: {mean_time:.2f}ms "
                  f"({throughput:.1f} docs/sec)")
        
        return results
    
    def benchmark_retrieval(self, queries: List[str], iterations: int = 5) -> Dict[str, Any]:
        """Benchmark retrieval performance."""
        print("\n--- Retrieval Benchmark ---")
        
        results = {
            "queries": {},
            "summary": {}
        }
        
        all_times = []
        
        for query in queries:
            times = []
            
            # Warmup
            _ = self.pipeline.query(query)
            
            # Benchmark
            for _ in range(iterations):
                start = time.perf_counter()
                result = self.pipeline.query(query)
                elapsed = (time.perf_counter() - start) * 1000
                times.append(elapsed)
            
            mean_time = statistics.mean(times)
            all_times.append(mean_time)
            
            results["queries"][query] = {
                "mean_ms": mean_time,
                "median_ms": statistics.median(times),
                "min_ms": min(times),
                "max_ms": max(times),
                "contexts": result['context_count']
            }
            
            print(f"  '{query[:40]}...': {mean_time:.2f}ms "
                  f"({result['context_count']} contexts)")
        
        results["summary"] = {
            "avg_query_time_ms": statistics.mean(all_times),
            "min_query_time_ms": min(all_times),
            "max_query_time_ms": max(all_times)
        }
        
        print(f"\n  Average: {results['summary']['avg_query_time_ms']:.2f}ms")
        
        return results
    
    def benchmark_indexing(self, test_dir: str) -> Dict[str, Any]:
        """Benchmark indexing performance."""
        print("\n--- Indexing Benchmark ---")
        
        start = time.perf_counter()
        results = self.pipeline.index_directory(
            test_dir,
            include_patterns=["*.py", "*.md", "*.txt"]
        )
        elapsed = (time.perf_counter() - start)
        
        benchmark_results = {
            "total_time_s": elapsed,
            "files_processed": results.get('successful', 0),
            "chunks_added": results.get('chunks_added', 0),
            "files_per_second": results.get('successful', 0) / elapsed if elapsed > 0 else 0,
            "chunks_per_second": results.get('chunks_added', 0) / elapsed if elapsed > 0 else 0
        }
        
        print(f"  Total time: {elapsed:.2f}s")
        print(f"  Files processed: {benchmark_results['files_processed']}")
        print(f"  Chunks added: {benchmark_results['chunks_added']}")
        print(f"  Throughput: {benchmark_results['files_per_second']:.2f} files/sec")
        print(f"              {benchmark_results['chunks_per_second']:.2f} chunks/sec")
        
        return benchmark_results
    
    def benchmark_memory(self) -> Dict[str, Any]:
        """Benchmark memory usage."""
        print("\n--- Memory Benchmark ---")
        
        import psutil
        import os
        
        process = psutil.Process(os.getpid())
        
        # Get memory info
        mem_info = process.memory_info()
        
        results = {
            "rss_mb": mem_info.rss / (1024 * 1024),
            "vms_mb": mem_info.vms / (1024 * 1024),
            "embedder_cache_mb": 0,
            "vector_store_stats": {}
        }
        
        # Get embedder cache stats
        if self.pipeline.embedder:
            cache_stats = self.pipeline.embedder.get_cache_stats()
            results["embedder_cache_mb"] = cache_stats.get('memory_usage_mb', 0)
            print(f"  Embedding cache: {results['embedder_cache_mb']:.2f} MB")
        
        # Get vector store stats
        if self.pipeline.vector_store:
            vs_stats = self.pipeline.vector_store.get_stats()
            results["vector_store_stats"] = vs_stats
            print(f"  Vector store chunks: {vs_stats.get('total_chunks', 0)}")
        
        print(f"  Process RSS: {results['rss_mb']:.2f} MB")
        
        return results
    
    def run_all_benchmarks(self, test_dir: str = None) -> Dict[str, Any]:
        """Run all benchmarks."""
        print("=" * 60)
        print("HelixLLM RAG Pipeline - Performance Benchmarks")
        print("=" * 60)
        
        # Initialize if needed
        if not self.pipeline._initialized:
            print("\nInitializing pipeline...")
            if not self.pipeline.initialize():
                print("Failed to initialize pipeline!")
                return {}
        
        # Run benchmarks
        self.results["embedding"] = self.benchmark_embedding()
        
        if test_dir:
            self.results["indexing"] = self.benchmark_indexing(test_dir)
        
        # Test queries
        test_queries = [
            "binary search implementation",
            "tree data structure",
            "sorting algorithm",
            "class definition python",
            "error handling try except"
        ]
        
        self.results["retrieval"] = self.benchmark_retrieval(test_queries)
        self.results["memory"] = self.benchmark_memory()
        
        # System info
        self.results["system"] = {
            "gpu_layers": self.pipeline.config.embedding_n_gpu_layers,
            "chunk_size": self.pipeline.config.chunk_size,
            "retrieval_top_k": self.pipeline.config.retrieval_top_k
        }
        
        return self.results
    
    def save_results(self, output_file: str):
        """Save benchmark results to JSON."""
        with open(output_file, 'w') as f:
            json.dump(self.results, f, indent=2)
        print(f"\nResults saved to: {output_file}")
    
    def print_summary(self):
        """Print benchmark summary."""
        print("\n" + "=" * 60)
        print("Benchmark Summary")
        print("=" * 60)
        
        if "embedding" in self.results:
            emb = self.results["embedding"]
            print(f"\nEmbedding:")
            print(f"  Short text: {emb.get('Short (100 chars)', {}).get('mean_ms', 0):.2f}ms")
            print(f"  Batch-32: {emb.get('batch_32', {}).get('mean_ms', 0):.2f}ms")
        
        if "retrieval" in self.results:
            ret = self.results["retrieval"]
            print(f"\nRetrieval:")
            print(f"  Average query time: {ret.get('summary', {}).get('avg_query_time_ms', 0):.2f}ms")
        
        if "indexing" in self.results:
            idx = self.results["indexing"]
            print(f"\nIndexing:")
            print(f"  Throughput: {idx.get('chunks_per_second', 0):.2f} chunks/sec")


def create_test_dataset():
    """Create a test dataset for benchmarking."""
    test_dir = Path("./benchmark_test_data")
    test_dir.mkdir(exist_ok=True)
    
    # Create Python files
    for i in range(20):
        (test_dir / f"module_{i}.py").write_text(f'''
"""Module {i} - Sample code for benchmarking."""

class DataProcessor{i}:
    """Process data for module {i}."""
    
    def __init__(self, config):
        self.config = config
        self.data = []
    
    def process(self, items):
        """Process a list of items."""
        results = []
        for item in items:
            results.append(self.transform(item))
        return results
    
    def transform(self, item):
        """Transform a single item."""
        return item * 2
    
    def validate(self, data):
        """Validate the processed data."""
        return all(isinstance(x, (int, float)) for x in data)

def utility_function_{i}(x, y):
    """Utility function for module {i}."""
    return x + y * {i}
''')
    
    # Create markdown files
    for i in range(10):
        (test_dir / f"doc_{i}.md").write_text(f'''
# Documentation {i}

## Overview

This is documentation for module {i}.

## API Reference

### Function {i}

Description of function {i}.

**Parameters:**
- `param1`: First parameter
- `param2`: Second parameter

**Returns:**
- Result value

## Examples

```python
result = function_{i}(1, 2)
print(result)
```
''')
    
    return str(test_dir)


if __name__ == "__main__":
    import os
    
    # Create test dataset
    print("Creating test dataset...")
    test_dir = create_test_dataset()
    print(f"Test dataset created at: {test_dir}")
    
    # Configure pipeline
    config = HelixRAGConfig(
        embedding_model_path="models/nomic-embed-text-v1.5.Q4_K_M.gguf",
        vector_store_path="./benchmark_chroma_db",
        chunk_size=512,
        retrieval_top_k=10
    )
    
    # Create pipeline
    pipeline = HelixRAGPipeline(config)
    
    # Run benchmarks
    runner = BenchmarkRunner(pipeline)
    results = runner.run_all_benchmarks(test_dir)
    
    # Save and print results
    runner.save_results("benchmark_results.json")
    runner.print_summary()
    
    # Cleanup
    pipeline.close()
    
    print("\nBenchmark complete!")
