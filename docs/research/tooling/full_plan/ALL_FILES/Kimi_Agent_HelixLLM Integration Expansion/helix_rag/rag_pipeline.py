"""
HelixLLM RAG Pipeline - Main Integration
========================================
Complete RAG pipeline integrating all components for HelixLLM.
Optimized for coding tasks with 1.5B parameter model.
"""

import os
import time
import json
from pathlib import Path
from dataclasses import dataclass, asdict
from typing import List, Dict, Optional, Any, Callable, Iterator
from contextlib import contextmanager
import logging

# Import all components
from embedding_model import NomicEmbedder, EmbeddingConfig, EmbeddingBenchmark
from document_processor import DocumentProcessor, ChunkConfig, DocumentChunk
from vector_store import ChromaVectorStore, VectorStoreConfig, SearchResult
from retrieval_engine import (
    RetrievalEngine, RetrievalConfig, ReRankerType, 
    RetrievedContext, QueryExpander
)
from context_injector import (
    ContextInjector, PromptTemplateType, InjectedPrompt,
    PromptTemplateLibrary, TokenBudgetManager
)
from knowledge_base import (
    KnowledgeBase, KnowledgeBaseConfig, DocumentVersion,
    DocumentStatus, DocumentIndex
)

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


@dataclass
class HelixRAGConfig:
    """Complete configuration for HelixLLM RAG pipeline."""
    
    # Embedding model
    embedding_model_path: str = "models/nomic-embed-text-v1.5.Q4_K_M.gguf"
    embedding_n_ctx: int = 8192
    embedding_n_batch: int = 512
    embedding_n_gpu_layers: int = -1  # All layers on GPU
    
    # Document processing
    chunk_size: int = 512
    chunk_overlap: int = 128
    preserve_functions: bool = True
    preserve_classes: bool = True
    
    # Vector store
    vector_store_path: str = "./chroma_db"
    collection_name: str = "helix_knowledge"
    hnsw_M: int = 16
    hnsw_construction_ef: int = 128
    hnsw_search_ef: int = 64
    
    # Retrieval
    retrieval_top_k: int = 10
    retrieval_final_k: int = 5
    semantic_weight: float = 0.7
    keyword_weight: float = 0.3
    reranker_type: str = "hybrid"
    min_score_threshold: float = 0.3
    
    # Context injection
    max_context_tokens: int = 2048
    max_response_tokens: int = 1024
    enable_citations: bool = True
    
    # Knowledge base
    knowledge_base_path: str = "./knowledge_base"
    enable_versioning: bool = True
    
    # Performance
    batch_size: int = 32
    use_cache: bool = True
    
    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'HelixRAGConfig':
        return cls(**data)
    
    @classmethod
    def load(cls, path: str) -> 'HelixRAGConfig':
        """Load configuration from JSON file."""
        with open(path, 'r') as f:
            return cls.from_dict(json.load(f))
    
    def save(self, path: str):
        """Save configuration to JSON file."""
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        with open(path, 'w') as f:
            json.dump(self.to_dict(), f, indent=2)


class HelixRAGPipeline:
    """
    Complete RAG pipeline for HelixLLM.
    
    This class integrates all RAG components:
    - Embedding model (nomic-embed-text-v1.5 Q4_K_M)
    - Document processor (code-aware chunking)
    - Vector store (ChromaDB with HNSW)
    - Retrieval engine (hybrid search + re-ranking)
    - Context injector (prompt templates)
    - Knowledge base (incremental updates)
    
    Usage:
        pipeline = HelixRAGPipeline(config)
        pipeline.initialize()
        
        # Index documents
        pipeline.index_directory("./my_project")
        
        # Query
        result = pipeline.query("How to implement binary search?")
        print(result['prompt'].full_prompt)
    """
    
    def __init__(self, config: Optional[HelixRAGConfig] = None):
        self.config = config or HelixRAGConfig()
        
        # Components (initialized in initialize())
        self.embedder: Optional[NomicEmbedder] = None
        self.processor: Optional[DocumentProcessor] = None
        self.vector_store: Optional[ChromaVectorStore] = None
        self.retrieval_engine: Optional[RetrievalEngine] = None
        self.context_injector: Optional[ContextInjector] = None
        self.knowledge_base: Optional[KnowledgeBase] = None
        
        # State
        self._initialized = False
        self._performance_stats = {
            'queries_processed': 0,
            'avg_query_time': 0,
            'total_chunks_indexed': 0
        }
    
    def initialize(self) -> bool:
        """
        Initialize all RAG components.
        
        Returns:
            True if all components initialized successfully
        """
        logger.info("Initializing HelixLLM RAG Pipeline...")
        start_time = time.time()
        
        try:
            # 1. Initialize embedding model
            logger.info("Loading embedding model...")
            embed_config = EmbeddingConfig(
                model_path=self.config.embedding_model_path,
                n_ctx=self.config.embedding_n_ctx,
                n_batch=self.config.embedding_n_batch,
                n_gpu_layers=self.config.embedding_n_gpu_layers,
                max_batch_size=self.config.batch_size
            )
            self.embedder = NomicEmbedder(embed_config)
            if not self.embedder.load_model():
                logger.error("Failed to load embedding model")
                return False
            
            # 2. Initialize document processor
            logger.info("Initializing document processor...")
            chunk_config = ChunkConfig(
                chunk_size=self.config.chunk_size,
                chunk_overlap=self.config.chunk_overlap,
                preserve_functions=self.config.preserve_functions,
                preserve_classes=self.config.preserve_classes
            )
            self.processor = DocumentProcessor(chunk_config)
            
            # 3. Initialize vector store
            logger.info("Initializing vector store...")
            store_config = VectorStoreConfig(
                persist_directory=self.config.vector_store_path,
                collection_name=self.config.collection_name,
                hnsw_M=self.config.hnsw_M,
                hnsw_construction_ef=self.config.hnsw_construction_ef,
                hnsw_search_ef=self.config.hnsw_search_ef
            )
            self.vector_store = ChromaVectorStore(store_config)
            if not self.vector_store.initialize():
                logger.error("Failed to initialize vector store")
                return False
            
            # 4. Initialize retrieval engine
            logger.info("Initializing retrieval engine...")
            retrieval_config = RetrievalConfig(
                top_k=self.config.retrieval_top_k,
                final_k=self.config.retrieval_final_k,
                semantic_weight=self.config.semantic_weight,
                keyword_weight=self.config.keyword_weight,
                reranker_type=ReRankerType(self.config.reranker_type),
                max_context_tokens=self.config.max_context_tokens
            )
            self.retrieval_engine = RetrievalEngine(
                self.vector_store,
                self.embedder,
                retrieval_config
            )
            
            # 5. Initialize context injector
            logger.info("Initializing context injector...")
            self.context_injector = ContextInjector(
                max_tokens=self.config.max_context_tokens + self.config.max_response_tokens
            )
            
            # 6. Initialize knowledge base
            logger.info("Initializing knowledge base...")
            kb_config = KnowledgeBaseConfig(
                base_directory=self.config.knowledge_base_path,
                enable_versioning=self.config.enable_versioning
            )
            self.knowledge_base = KnowledgeBase(
                self.vector_store,
                self.embedder,
                self.processor,
                kb_config
            )
            
            self._initialized = True
            init_time = time.time() - start_time
            logger.info(f"RAG Pipeline initialized in {init_time:.2f}s")
            
            return True
            
        except Exception as e:
            logger.error(f"Failed to initialize RAG pipeline: {e}")
            return False
    
    def index_file(self, file_path: str, force_update: bool = False) -> Dict[str, Any]:
        """
        Index a single file.
        
        Args:
            file_path: Path to file
            force_update: Force re-index even if unchanged
            
        Returns:
            Result dictionary
        """
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized. Call initialize() first.")
        
        return self.knowledge_base.add_document(file_path, force_update)
    
    def index_directory(
        self,
        directory: str,
        include_patterns: List[str] = None,
        exclude_patterns: List[str] = None,
        progress_callback: Optional[Callable[[int, int, str], None]] = None
    ) -> Dict[str, Any]:
        """
        Index all files in a directory.
        
        Args:
            directory: Directory to index
            include_patterns: File patterns to include
            exclude_patterns: File patterns to exclude
            progress_callback: Callback(current, total, file)
            
        Returns:
            Summary of indexing results
        """
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized. Call initialize() first.")
        
        logger.info(f"Indexing directory: {directory}")
        start_time = time.time()
        
        results = self.knowledge_base.add_directory(
            directory,
            include_patterns,
            exclude_patterns,
            progress_callback
        )
        
        elapsed = time.time() - start_time
        logger.info(f"Indexing completed in {elapsed:.2f}s")
        
        # Update stats
        self._performance_stats['total_chunks_indexed'] += results.get('chunks_added', 0)
        
        return results
    
    def sync_directory(
        self,
        directory: str,
        include_patterns: List[str] = None,
        exclude_patterns: List[str] = None
    ) -> Dict[str, Any]:
        """
        Synchronize knowledge base with directory.
        
        Args:
            directory: Directory to sync
            include_patterns: File patterns to include
            exclude_patterns: File patterns to exclude
            
        Returns:
            Sync summary
        """
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized. Call initialize() first.")
        
        logger.info(f"Syncing directory: {directory}")
        return self.knowledge_base.sync_directory(directory, include_patterns, exclude_patterns)
    
    def query(
        self,
        query: str,
        template_type: Optional[PromptTemplateType] = None,
        filters: Optional[Dict[str, Any]] = None,
        top_k: Optional[int] = None,
        use_expansion: bool = False
    ) -> Dict[str, Any]:
        """
        Query the RAG pipeline.
        
        Args:
            query: User query
            template_type: Type of prompt template
            filters: Metadata filters for retrieval
            top_k: Number of results to retrieve
            use_expansion: Use query expansion
            
        Returns:
            Dictionary with prompt, contexts, and metadata
        """
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized. Call initialize() first.")
        
        start_time = time.time()
        
        # Retrieve contexts
        if use_expansion:
            expansions = QueryExpander.expand(query)
            contexts = self.retrieval_engine.retrieve_with_expansion(
                query, expansions, filters
            )
        else:
            contexts = self.retrieval_engine.retrieve(query, filters, top_k)
        
        retrieval_time = time.time() - start_time
        
        # Inject context into prompt
        injected = self.context_injector.inject(
            query=query,
            contexts=contexts,
            template_type=template_type,
            response_tokens=self.config.max_response_tokens
        )
        
        total_time = time.time() - start_time
        
        # Update stats
        self._performance_stats['queries_processed'] += 1
        n = self._performance_stats['queries_processed']
        old_avg = self._performance_stats['avg_query_time']
        self._performance_stats['avg_query_time'] = (
            (old_avg * (n - 1) + total_time) / n
        )
        
        return {
            'query': query,
            'prompt': injected,
            'contexts': contexts,
            'context_count': len(contexts),
            'token_estimate': injected.token_estimate,
            'timing': {
                'retrieval_ms': retrieval_time * 1000,
                'total_ms': total_time * 1000
            },
            'citations': injected.citations
        }
    
    def stream_query(
        self,
        query: str,
        llm_generate_fn: Callable[[str], Iterator[str]],
        template_type: Optional[PromptTemplateType] = None,
        filters: Optional[Dict[str, Any]] = None
    ) -> Iterator[str]:
        """
        Query with streaming LLM response.
        
        Args:
            query: User query
            llm_generate_fn: Function that takes prompt and yields tokens
            template_type: Type of prompt template
            filters: Metadata filters
            
        Yields:
            Generated tokens
        """
        result = self.query(query, template_type, filters)
        prompt = result['prompt']
        
        # Yield citations first
        yield "\n\n## Sources:\n"
        for cit in result['citations']:
            yield f"[{cit['index']}] {cit['source_file']}\n"
        
        yield "\n## Response:\n"
        
        # Stream LLM response
        for token in llm_generate_fn(prompt.full_prompt):
            yield token
    
    def get_relevant_contexts(
        self,
        query: str,
        top_k: int = 5,
        filters: Optional[Dict[str, Any]] = None
    ) -> List[RetrievedContext]:
        """
        Get relevant contexts without building prompt.
        
        Args:
            query: User query
            top_k: Number of contexts
            filters: Metadata filters
            
        Returns:
            List of retrieved contexts
        """
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized. Call initialize() first.")
        
        return self.retrieval_engine.retrieve(query, filters, top_k)
    
    def delete_document(self, file_path: str) -> bool:
        """
        Delete a document from the knowledge base.
        
        Args:
            file_path: Path to document
            
        Returns:
            True if deleted
        """
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized. Call initialize() first.")
        
        return self.knowledge_base.remove_document(file_path)
    
    def get_stats(self) -> Dict[str, Any]:
        """Get pipeline statistics."""
        stats = {
            'initialized': self._initialized,
            'performance': self._performance_stats,
        }
        
        if self._initialized:
            stats['vector_store'] = self.vector_store.get_stats()
            stats['knowledge_base'] = self.knowledge_base.get_stats()
            stats['embedding_cache'] = self.embedder.get_cache_stats()
        
        return stats
    
    def backup(self, backup_name: Optional[str] = None) -> str:
        """Create a backup of the knowledge base."""
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized")
        
        return self.knowledge_base.backup(backup_name)
    
    def restore(self, backup_name: str) -> bool:
        """Restore from a backup."""
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized")
        
        return self.knowledge_base.restore(backup_name)
    
    def benchmark_embeddings(self) -> List[Dict[str, Any]]:
        """Run embedding performance benchmark."""
        if not self._initialized:
            raise RuntimeError("Pipeline not initialized")
        
        return EmbeddingBenchmark.run_benchmark(self.embedder)
    
    def reset(self):
        """Reset the entire pipeline (delete all data)."""
        if self.vector_store:
            self.vector_store.reset()
        
        self._performance_stats = {
            'queries_processed': 0,
            'avg_query_time': 0,
            'total_chunks_indexed': 0
        }
        
        logger.info("RAG Pipeline reset")
    
    def close(self):
        """Clean up resources."""
        if self.embedder:
            self.embedder.unload_model()
        
        self._initialized = False
        logger.info("RAG Pipeline closed")
    
    def __enter__(self):
        self.initialize()
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()


class RAGPipelineBuilder:
    """Builder for creating configured RAG pipelines."""
    
    def __init__(self):
        self.config = HelixRAGConfig()
    
    def with_model(self, model_path: str) -> 'RAGPipelineBuilder':
        """Set embedding model path."""
        self.config.embedding_model_path = model_path
        return self
    
    def with_gpu_layers(self, layers: int) -> 'RAGPipelineBuilder':
        """Set GPU layer offloading."""
        self.config.embedding_n_gpu_layers = layers
        return self
    
    def with_chunk_size(self, size: int) -> 'RAGPipelineBuilder':
        """Set chunk size."""
        self.config.chunk_size = size
        return self
    
    def with_vector_store(self, path: str) -> 'RAGPipelineBuilder':
        """Set vector store path."""
        self.config.vector_store_path = path
        return self
    
    def with_retrieval(self, top_k: int, final_k: int) -> 'RAGPipelineBuilder':
        """Set retrieval parameters."""
        self.config.retrieval_top_k = top_k
        self.config.retrieval_final_k = final_k
        return self
    
    def with_context_window(self, tokens: int) -> 'RAGPipelineBuilder':
        """Set context window size."""
        self.config.max_context_tokens = tokens
        return self
    
    def build(self) -> HelixRAGPipeline:
        """Build the RAG pipeline."""
        return HelixRAGPipeline(self.config)


# Example usage and demonstration
if __name__ == "__main__":
    print("=" * 70)
    print("HelixLLM RAG Pipeline - Example Usage")
    print("=" * 70)
    
    # Configuration for RTX 6GB VRAM
    config = HelixRAGConfig(
        embedding_model_path="models/nomic-embed-text-v1.5.Q4_K_M.gguf",
        embedding_n_gpu_layers=-1,  # All on GPU
        chunk_size=512,
        chunk_overlap=128,
        vector_store_path="./helix_chroma_db",
        collection_name="helix_code_knowledge",
        retrieval_top_k=10,
        retrieval_final_k=5,
        max_context_tokens=2048,
        max_response_tokens=1024
    )
    
    # Create and initialize pipeline
    with HelixRAGPipeline(config) as pipeline:
        print("\n1. Pipeline Statistics:")
        print(json.dumps(pipeline.get_stats(), indent=2))
        
        # Example: Index a directory (uncomment to use)
        # print("\n2. Indexing directory...")
        # results = pipeline.index_directory(
        #     "./sample_project",
        #     include_patterns=["*.py", "*.md"],
        #     exclude_patterns=["*/venv/*", "*/__pycache__/*"]
        # )
        # print(json.dumps(results, indent=2))
        
        # Example: Query (requires indexed documents)
        # print("\n3. Querying...")
        # result = pipeline.query(
        #     "How do I implement a binary search tree?",
        #     template_type=PromptTemplateType.CODE_GENERATION
        # )
        # print(f"Token estimate: {result['token_estimate']}")
        # print(f"Contexts: {result['context_count']}")
        # print(f"\nPrompt preview:\n{result['prompt'].full_prompt[:500]}...")
        
        print("\n4. Pipeline ready for use!")
        print("   - Use pipeline.index_directory() to index documents")
        print("   - Use pipeline.query() to retrieve and generate")
        print("   - Use pipeline.get_stats() to monitor performance")
