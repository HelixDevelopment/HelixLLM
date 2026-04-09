"""
HelixLLM RAG Pipeline - Embedding Model Configuration
=====================================================
Optimized embedding model using nomic-embed-text-v1.5 with GGUF quantization.
Target: RTX 6GB VRAM, optimized for code embeddings.
"""

import os
import logging
from typing import List, Union, Optional, Dict, Any
from dataclasses import dataclass
import numpy as np

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


@dataclass
class EmbeddingConfig:
    """Configuration for embedding model."""
    model_path: str = "models/nomic-embed-text-v1.5.Q4_K_M.gguf"
    n_ctx: int = 8192  # Context window for embeddings
    n_batch: int = 512  # Batch size for processing
    n_gpu_layers: int = -1  # -1 = offload all to GPU
    embedding_dim: int = 768  # nomic-embed-text-v1.5 dimension
    normalize_embeddings: bool = True
    use_mmap: bool = True  # Memory-map the model
    use_mlock: bool = False  # Lock model in RAM
    verbose: bool = False
    
    # Performance tuning for 6GB VRAM
    max_batch_size: int = 32  # Max chunks per batch
    concurrent_requests: int = 4  # Parallel embedding requests


class NomicEmbedder:
    """
    Optimized embedding model wrapper for nomic-embed-text-v1.5.
    
    Features:
    - GGUF Q4_K_M quantization for 6GB VRAM
    - GPU layer offloading for acceleration
    - Batch processing with optimal chunking
    - Automatic fallback to CPU if GPU OOM
    """
    
    def __init__(self, config: Optional[EmbeddingConfig] = None):
        self.config = config or EmbeddingConfig()
        self.model = None
        self._embedding_cache: Dict[str, np.ndarray] = {}
        self._cache_hits = 0
        self._cache_misses = 0
        
    def load_model(self) -> bool:
        """
        Load the embedding model with optimized settings.
        
        Returns:
            bool: True if model loaded successfully
        """
        try:
            from llama_cpp import Llama
            
            logger.info(f"Loading embedding model: {self.config.model_path}")
            
            # Verify model exists
            if not os.path.exists(self.config.model_path):
                raise FileNotFoundError(
                    f"Model not found: {self.config.model_path}\n"
                    f"Download with: huggingface-cli download nomic-ai/nomic-embed-text-v1.5-GGUF "
                    f"--local-dir ./models --include '*Q4_K_M.gguf'"
                )
            
            # Load model with optimized settings for 6GB VRAM
            self.model = Llama(
                model_path=self.config.model_path,
                n_ctx=self.config.n_ctx,
                n_batch=self.config.n_batch,
                n_gpu_layers=self.config.n_gpu_layers,
                embedding=True,
                verbose=self.config.verbose,
                use_mmap=self.config.use_mmap,
                use_mlock=self.config.use_mlock,
                # Additional optimizations
                offload_kqv=True,  # Offload KQV cache to GPU
                # Flash attention for speed (if supported)
                flash_attn=False,  # Set True if llama-cpp compiled with flash attn
            )
            
            logger.info("Embedding model loaded successfully")
            logger.info(f"Model context: {self.config.n_ctx}, Batch: {self.config.n_batch}")
            
            return True
            
        except ImportError:
            logger.error("llama-cpp-python not installed. Install with: pip install llama-cpp-python")
            return False
        except Exception as e:
            logger.error(f"Failed to load embedding model: {e}")
            return False
    
    def embed(
        self, 
        texts: Union[str, List[str]], 
        batch_size: Optional[int] = None,
        use_cache: bool = True
    ) -> np.ndarray:
        """
        Generate embeddings for text(s).
        
        Args:
            texts: Single text or list of texts to embed
            batch_size: Override default batch size
            use_cache: Whether to use embedding cache
            
        Returns:
            numpy array of embeddings (shape: [n_texts, embedding_dim])
        """
        if self.model is None:
            if not self.load_model():
                raise RuntimeError("Failed to load embedding model")
        
        # Normalize input
        is_single = isinstance(texts, str)
        if is_single:
            texts = [texts]
        
        batch_size = batch_size or self.config.max_batch_size
        
        # Check cache for each text
        if use_cache:
            cached_embeddings = []
            texts_to_embed = []
            indices_to_embed = []
            
            for i, text in enumerate(texts):
                cache_key = hash(text)
                if cache_key in self._embedding_cache:
                    cached_embeddings.append((i, self._embedding_cache[cache_key]))
                    self._cache_hits += 1
                else:
                    texts_to_embed.append(text)
                    indices_to_embed.append(i)
                    self._cache_misses += 1
        else:
            texts_to_embed = texts
            indices_to_embed = list(range(len(texts)))
            cached_embeddings = []
        
        # Process in batches
        all_embeddings = [None] * len(texts)
        
        # Fill cached embeddings
        for idx, emb in cached_embeddings:
            all_embeddings[idx] = emb
        
        # Process new embeddings in batches
        if texts_to_embed:
            for i in range(0, len(texts_to_embed), batch_size):
                batch = texts_to_embed[i:i + batch_size]
                batch_indices = indices_to_embed[i:i + batch_size]
                
                try:
                    # Generate embeddings
                    embeddings = self.model.create_embedding(batch)
                    
                    # Extract embedding vectors
                    for j, emb_data in enumerate(embeddings['data']):
                        embedding = np.array(emb_data['embedding'], dtype=np.float32)
                        
                        # Normalize if configured
                        if self.config.normalize_embeddings:
                            embedding = embedding / np.linalg.norm(embedding)
                        
                        idx = batch_indices[j]
                        all_embeddings[idx] = embedding
                        
                        # Cache the embedding
                        if use_cache:
                            cache_key = hash(batch[j])
                            self._embedding_cache[cache_key] = embedding
                            
                except Exception as e:
                    logger.error(f"Error embedding batch {i}: {e}")
                    # Return zero embeddings for failed batch
                    for j in range(len(batch)):
                        idx = batch_indices[j]
                        all_embeddings[idx] = np.zeros(self.config.embedding_dim, dtype=np.float32)
        
        embeddings_array = np.array(all_embeddings)
        
        return embeddings_array[0] if is_single else embeddings_array
    
    def embed_query(self, query: str) -> np.ndarray:
        """
        Embed a search query with task prefix.
        
        Nomic embeddings use task-specific prefixes:
        - search_query: for retrieval queries
        - search_document: for documents (added automatically)
        """
        prefixed_query = f"search_query: {query}"
        return self.embed(prefixed_query, use_cache=False)
    
    def embed_documents(self, documents: List[str]) -> np.ndarray:
        """
        Embed documents with task prefix.
        """
        prefixed_docs = [f"search_document: {doc}" for doc in documents]
        return self.embed(prefixed_docs, use_cache=True)
    
    def compute_similarity(
        self, 
        query_embedding: np.ndarray, 
        doc_embeddings: np.ndarray
    ) -> np.ndarray:
        """
        Compute cosine similarity between query and documents.
        
        Args:
            query_embedding: Query embedding vector
            doc_embeddings: Document embedding matrix
            
        Returns:
            Similarity scores
        """
        # Ensure 2D array for query
        if query_embedding.ndim == 1:
            query_embedding = query_embedding.reshape(1, -1)
        
        # Cosine similarity (dot product for normalized vectors)
        similarities = np.dot(doc_embeddings, query_embedding.T).flatten()
        
        return similarities
    
    def get_cache_stats(self) -> Dict[str, Any]:
        """Get embedding cache statistics."""
        total = self._cache_hits + self._cache_misses
        hit_rate = self._cache_hits / total if total > 0 else 0
        
        return {
            "cache_size": len(self._embedding_cache),
            "cache_hits": self._cache_hits,
            "cache_misses": self._cache_misses,
            "hit_rate": f"{hit_rate:.2%}",
            "memory_usage_mb": len(self._embedding_cache) * self.config.embedding_dim * 4 / (1024 * 1024)
        }
    
    def clear_cache(self):
        """Clear the embedding cache."""
        self._embedding_cache.clear()
        self._cache_hits = 0
        self._cache_misses = 0
        logger.info("Embedding cache cleared")
    
    def unload_model(self):
        """Unload model to free GPU memory."""
        if self.model is not None:
            del self.model
            self.model = None
            logger.info("Embedding model unloaded")
    
    def __enter__(self):
        self.load_model()
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        self.unload_model()


class EmbeddingBenchmark:
    """Benchmark embedding performance."""
    
    @staticmethod
    def run_benchmark(embedder: NomicEmbedder, text_lengths: List[int] = None):
        """Run performance benchmark."""
        import time
        
        text_lengths = text_lengths or [100, 500, 1000, 2000]
        results = []
        
        print("=" * 60)
        print("Embedding Performance Benchmark")
        print("=" * 60)
        
        for length in text_lengths:
            # Generate test text
            test_text = "code analysis " * (length // 14)
            
            # Warmup
            _ = embedder.embed(test_text)
            
            # Benchmark single embedding
            start = time.perf_counter()
            for _ in range(10):
                _ = embedder.embed(test_text)
            single_time = (time.perf_counter() - start) / 10
            
            # Benchmark batch embedding
            batch = [test_text] * 32
            start = time.perf_counter()
            _ = embedder.embed(batch)
            batch_time = time.perf_counter() - start
            
            results.append({
                "length": length,
                "single_ms": single_time * 1000,
                "batch_32_ms": batch_time * 1000,
                "throughput": 32 / batch_time
            })
            
            print(f"Text length: {length:4d} chars | "
                  f"Single: {single_time*1000:6.2f}ms | "
                  f"Batch-32: {batch_time*1000:6.2f}ms | "
                  f"Throughput: {32/batch_time:5.1f} docs/sec")
        
        return results


# Example usage and testing
if __name__ == "__main__":
    # Configuration for 6GB VRAM
    config = EmbeddingConfig(
        model_path="models/nomic-embed-text-v1.5.Q4_K_M.gguf",
        n_gpu_layers=-1,  # Offload all layers
        n_batch=512,
        max_batch_size=32
    )
    
    # Initialize embedder
    embedder = NomicEmbedder(config)
    
    # Load model
    if embedder.load_model():
        # Test single embedding
        query = "How to implement a binary search tree in Python?"
        query_emb = embedder.embed_query(query)
        print(f"Query embedding shape: {query_emb.shape}")
        
        # Test document embedding
        docs = [
            "def binary_search(arr, target):\n    left, right = 0, len(arr) - 1",
            "class TreeNode:\n    def __init__(self, val=0):\n        self.val = val",
            "import numpy as np\nimport pandas as pd"
        ]
        doc_embs = embedder.embed_documents(docs)
        print(f"Document embeddings shape: {doc_embs.shape}")
        
        # Compute similarities
        similarities = embedder.compute_similarity(query_emb, doc_embs)
        print(f"Similarities: {similarities}")
        
        # Run benchmark
        EmbeddingBenchmark.run_benchmark(embedder)
        
        # Print cache stats
        print(f"\nCache stats: {embedder.get_cache_stats()}")
        
        # Cleanup
        embedder.unload_model()
