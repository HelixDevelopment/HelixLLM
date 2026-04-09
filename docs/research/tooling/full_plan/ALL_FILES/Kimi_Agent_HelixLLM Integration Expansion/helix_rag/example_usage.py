"""
HelixLLM RAG Pipeline - Complete Usage Examples
===============================================
Demonstrates all features of the RAG pipeline with real-world examples.
"""

import os
import sys
import json
import time
from pathlib import Path

# Add the current directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from rag_pipeline import HelixRAGPipeline, HelixRAGConfig, RAGPipelineBuilder
from context_injector import PromptTemplateType


def print_section(title: str):
    """Print a formatted section header."""
    print("\n" + "=" * 70)
    print(f"  {title}")
    print("=" * 70)


def print_subsection(title: str):
    """Print a subsection header."""
    print(f"\n--- {title} ---")


def create_sample_project():
    """Create a sample project for demonstration."""
    project_dir = Path("./sample_project")
    project_dir.mkdir(exist_ok=True)
    
    # Sample Python file
    (project_dir / "algorithms.py").write_text('''
"""Common algorithms implementation."""

def binary_search(arr, target):
    """
    Perform binary search on a sorted array.
    
    Args:
        arr: Sorted list of comparable elements
        target: Element to search for
        
    Returns:
        Index of target if found, -1 otherwise
        
    Time Complexity: O(log n)
    Space Complexity: O(1)
    """
    left, right = 0, len(arr) - 1
    
    while left <= right:
        mid = (left + right) // 2
        
        if arr[mid] == target:
            return mid
        elif arr[mid] < target:
            left = mid + 1
        else:
            right = mid - 1
    
    return -1


def quicksort(arr):
    """
    Sort array using quicksort algorithm.
    
    Args:
        arr: List of comparable elements
        
    Returns:
        New sorted list
        
    Time Complexity: O(n log n) average, O(n^2) worst
    """
    if len(arr) <= 1:
        return arr
    
    pivot = arr[len(arr) // 2]
    left = [x for x in arr if x < pivot]
    middle = [x for x in arr if x == pivot]
    right = [x for x in arr if x > pivot]
    
    return quicksort(left) + middle + quicksort(right)


class TreeNode:
    """Node for binary search tree."""
    
    def __init__(self, value):
        self.value = value
        self.left = None
        self.right = None


class BinarySearchTree:
    """Binary Search Tree implementation."""
    
    def __init__(self):
        self.root = None
    
    def insert(self, value):
        """Insert a value into the BST."""
        if not self.root:
            self.root = TreeNode(value)
        else:
            self._insert_recursive(self.root, value)
    
    def _insert_recursive(self, node, value):
        """Helper for recursive insertion."""
        if value < node.value:
            if node.left is None:
                node.left = TreeNode(value)
            else:
                self._insert_recursive(node.left, value)
        else:
            if node.right is None:
                node.right = TreeNode(value)
            else:
                self._insert_recursive(node.right, value)
    
    def search(self, value):
        """Search for a value in the BST."""
        return self._search_recursive(self.root, value)
    
    def _search_recursive(self, node, value):
        """Helper for recursive search."""
        if node is None or node.value == value:
            return node
        
        if value < node.value:
            return self._search_recursive(node.left, value)
        return self._search_recursive(node.right, value)
''')
    
    # Sample documentation
    (project_dir / "README.md").write_text('''
# Sample Project

This is a sample project for demonstrating the RAG pipeline.

## Features

- Binary search implementation
- Quicksort algorithm
- Binary Search Tree data structure

## Usage

```python
from algorithms import binary_search, quicksort, BinarySearchTree

# Binary search
result = binary_search([1, 2, 3, 4, 5], 3)
print(result)  # Output: 2

# Quicksort
sorted_arr = quicksort([3, 1, 4, 1, 5, 9])
print(sorted_arr)  # Output: [1, 1, 3, 4, 5, 9]

# BST
bst = BinarySearchTree()
bst.insert(5)
bst.insert(3)
bst.insert(7)
```

## Installation

```bash
pip install -r requirements.txt
```

## API Reference

### binary_search(arr, target)

Performs binary search on a sorted array.

**Parameters:**
- `arr`: Sorted list of comparable elements
- `target`: Element to search for

**Returns:**
- Index of target if found, -1 otherwise

**Time Complexity:** O(log n)
''')
    
    # Sample config file
    (project_dir / "config.yaml").write_text('''
# Application Configuration
database:
  host: localhost
  port: 5432
  name: myapp
  
logging:
  level: INFO
  format: "%(asctime)s - %(name)s - %(levelname)s - %(message)s"
  
features:
  enable_caching: true
  cache_ttl: 3600
  max_retries: 3
''')
    
    return str(project_dir)


def example_1_basic_initialization():
    """Example 1: Basic pipeline initialization."""
    print_section("Example 1: Basic Pipeline Initialization")
    
    # Method 1: Direct configuration
    config = HelixRAGConfig(
        embedding_model_path="models/nomic-embed-text-v1.5.Q4_K_M.gguf",
        vector_store_path="./example_chroma_db",
        chunk_size=512,
        retrieval_top_k=10
    )
    
    print_subsection("Configuration")
    print(json.dumps(config.to_dict(), indent=2))
    
    # Method 2: Using builder pattern
    print_subsection("Using Builder Pattern")
    pipeline = (RAGPipelineBuilder()
        .with_model("models/nomic-embed-text-v1.5.Q4_K_M.gguf")
        .with_gpu_layers(-1)  # All layers on GPU
        .with_chunk_size(512)
        .with_vector_store("./example_chroma_db")
        .with_retrieval(top_k=10, final_k=5)
        .with_context_window(tokens=2048)
        .build())
    
    print("Pipeline created successfully!")
    return pipeline


def example_2_indexing(pipeline: HelixRAGPipeline):
    """Example 2: Document indexing."""
    print_section("Example 2: Document Indexing")
    
    # Create sample project
    project_dir = create_sample_project()
    print(f"Created sample project at: {project_dir}")
    
    # Initialize pipeline
    if not pipeline.initialize():
        print("Failed to initialize pipeline. Exiting.")
        return False
    
    # Index directory with progress callback
    print_subsection("Indexing Directory")
    
    def progress_callback(current, total, file):
        pct = (current / total) * 100
        print(f"  [{pct:5.1f}%] ({current}/{total}) {file}")
    
    results = pipeline.index_directory(
        directory=project_dir,
        include_patterns=["*.py", "*.md", "*.yaml"],
        exclude_patterns=["*/venv/*", "*/__pycache__/*"],
        progress_callback=progress_callback
    )
    
    print_subsection("Indexing Results")
    print(json.dumps(results, indent=2))
    
    # Show stats
    print_subsection("Pipeline Statistics")
    print(json.dumps(pipeline.get_stats(), indent=2))
    
    return True


def example_3_querying(pipeline: HelixRAGPipeline):
    """Example 3: Querying the knowledge base."""
    print_section("Example 3: Querying the Knowledge Base")
    
    queries = [
        ("How do I implement binary search?", PromptTemplateType.CODE_GENERATION),
        ("Explain the BinarySearchTree class", PromptTemplateType.CODE_ANALYSIS),
        ("What is the time complexity of quicksort?", PromptTemplateType.DOCUMENTATION),
    ]
    
    for query, template_type in queries:
        print_subsection(f"Query: {query}")
        print(f"Template: {template_type.value}")
        
        start = time.time()
        result = pipeline.query(query, template_type=template_type)
        elapsed = (time.time() - start) * 1000
        
        print(f"Retrieved {result['context_count']} contexts in {elapsed:.1f}ms")
        print(f"Token estimate: {result['token_estimate']}")
        
        # Show retrieved contexts
        print("\nRetrieved Contexts:")
        for i, ctx in enumerate(result['contexts'][:3]):
            print(f"  [{i+1}] Score: {ctx.score:.3f} | Source: {ctx.source_file}")
            preview = ctx.content[:100].replace('\\n', ' ')
            print(f"      Preview: {preview}...")
        
        # Show prompt preview
        print("\nPrompt Preview:")
        print(result['prompt'].full_prompt[:400] + "...")
        print()


def example_4_advanced_retrieval(pipeline: HelixRAGPipeline):
    """Example 4: Advanced retrieval with filters."""
    print_section("Example 4: Advanced Retrieval with Filters")
    
    # Query with language filter
    print_subsection("Filter by Language (Python only)")
    result = pipeline.query(
        "binary search implementation",
        filters={"language": "py"}
    )
    print(f"Found {result['context_count']} Python contexts")
    
    # Query with file type filter
    print_subsection("Filter by File Type (Markdown only)")
    result = pipeline.query(
        "API documentation",
        filters={"file_type": "md"}
    )
    print(f"Found {result['context_count']} Markdown contexts")
    
    # Query with expansion
    print_subsection("Query with Expansion")
    result = pipeline.query(
        "How to use BST",
        use_expansion=True
    )
    print(f"Found {result['context_count']} contexts with expansion")


def example_5_context_window_management(pipeline: HelixRAGPipeline):
    """Example 5: Context window management."""
    print_section("Example 5: Context Window Management")
    
    # Get contexts
    contexts = pipeline.get_relevant_contexts(
        "binary search tree implementation",
        top_k=10
    )
    
    print(f"Retrieved {len(contexts)} contexts")
    
    # Build context window with different sizes
    from retrieval_engine import RetrievalEngine
    
    engine = pipeline.retrieval_engine
    
    for max_tokens in [1024, 2048, 3072]:
        context_window = engine.build_context_window(contexts, max_tokens)
        token_estimate = int(len(context_window) * 0.25)
        print(f"\nMax tokens: {max_tokens}")
        print(f"  Context window: {len(context_window)} chars (~{token_estimate} tokens)")
        print(f"  Included contexts: {context_window.count('--- Context')}")


def example_6_knowledge_base_management(pipeline: HelixRAGPipeline):
    """Example 6: Knowledge base management."""
    print_section("Example 6: Knowledge Base Management")
    
    # Sync directory
    print_subsection("Sync Directory")
    project_dir = create_sample_project()
    sync_results = pipeline.sync_directory(project_dir)
    print(json.dumps(sync_results, indent=2))
    
    # Create backup
    print_subsection("Create Backup")
    backup_path = pipeline.backup("example_backup")
    print(f"Backup created: {backup_path}")
    
    # Get detailed stats
    print_subsection("Detailed Statistics")
    stats = pipeline.get_stats()
    print(json.dumps(stats, indent=2))


def example_7_performance_benchmarks(pipeline: HelixRAGPipeline):
    """Example 7: Performance benchmarks."""
    print_section("Example 7: Performance Benchmarks")
    
    # Embedding benchmark
    print_subsection("Embedding Performance")
    results = pipeline.benchmark_embeddings()
    
    print("Text Length | Single (ms) | Batch-32 (ms) | Throughput")
    print("-" * 60)
    for r in results:
        print(f"{r['length']:11d} | {r['single_ms']:10.2f} | "
              f"{r['batch_32_ms']:12.2f} | {r['throughput']:8.1f} docs/sec")
    
    # Query benchmark
    print_subsection("Query Performance")
    queries = [
        "binary search",
        "tree implementation",
        "sorting algorithm",
        "BST insert method"
    ]
    
    times = []
    for query in queries:
        start = time.time()
        result = pipeline.query(query)
        elapsed = (time.time() - start) * 1000
        times.append(elapsed)
        print(f"  '{query}': {elapsed:.1f}ms ({result['context_count']} contexts)")
    
    print(f"\nAverage query time: {sum(times)/len(times):.1f}ms")


def example_8_custom_templates():
    """Example 8: Custom prompt templates."""
    print_section("Example 8: Custom Prompt Templates")
    
    custom_template = """You are a specialized code review assistant.

## Task
Review the following code and provide constructive feedback.

## Code Context:
{context}

## Code to Review:
{query}

## Review Feedback:
- Code Quality:
- Potential Issues:
- Suggestions for Improvement:
- Security Considerations:
"""
    
    # This would be used like:
    # result = pipeline.query(
    #     "def my_function():\\n    pass",
    #     custom_template=custom_template
    # )
    
    print("Custom template created!")
    print(custom_template[:500] + "...")


def example_9_error_handling():
    """Example 9: Error handling and recovery."""
    print_section("Example 9: Error Handling and Recovery")
    
    print("""
Common errors and solutions:

1. Model not found:
   Error: FileNotFoundError: Model not found
   Solution: Download the model:
     huggingface-cli download nomic-ai/nomic-embed-text-v1.5-GGUF \\
       --local-dir ./models --include '*Q4_K_M.gguf'

2. GPU out of memory:
   Error: CUDA out of memory
   Solution: Reduce GPU layers:
     config.embedding_n_gpu_layers = 20  # Instead of -1

3. Document processing errors:
   Error: Failed to process document
   Solution: Check file encoding and permissions

4. Vector store corruption:
   Error: Collection not found
   Solution: Restore from backup:
     pipeline.restore("backup_name")
""")


def run_all_examples():
    """Run all examples in sequence."""
    print("\n" + "#" * 70)
    print("#" + " " * 68 + "#")
    print("#" + "  HelixLLM RAG Pipeline - Complete Usage Examples".center(68) + "#")
    print("#" + " " * 68 + "#")
    print("#" * 70)
    
    # Example 1: Initialization
    pipeline = example_1_basic_initialization()
    
    # Example 2: Indexing
    if not example_2_indexing(pipeline):
        print("\nSkipping remaining examples (pipeline not initialized)")
        return
    
    # Example 3: Querying
    example_3_querying(pipeline)
    
    # Example 4: Advanced retrieval
    example_4_advanced_retrieval(pipeline)
    
    # Example 5: Context management
    example_5_context_window_management(pipeline)
    
    # Example 6: KB management
    example_6_knowledge_base_management(pipeline)
    
    # Example 7: Benchmarks
    example_7_performance_benchmarks(pipeline)
    
    # Example 8: Custom templates
    example_8_custom_templates()
    
    # Example 9: Error handling
    example_9_error_handling()
    
    # Cleanup
    pipeline.close()
    
    print("\n" + "#" * 70)
    print("#  All examples completed!")
    print("#" * 70)


if __name__ == "__main__":
    run_all_examples()
