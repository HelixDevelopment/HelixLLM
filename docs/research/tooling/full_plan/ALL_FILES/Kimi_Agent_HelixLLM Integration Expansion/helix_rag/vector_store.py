"""
HelixLLM RAG Pipeline - Vector Store
====================================
ChromaDB-based vector store with HNSW indexing for fast similarity search.
Optimized for 32GB RAM with persistence and backup support.
"""

import os
import json
import shutil
import hashlib
from pathlib import Path
from dataclasses import dataclass, asdict
from typing import List, Dict, Optional, Any, Union, Tuple
from datetime import datetime
import logging
import numpy as np

logger = logging.getLogger(__name__)


@dataclass
class VectorStoreConfig:
    """Configuration for vector store."""
    # Storage paths
    persist_directory: str = "./chroma_db"
    backup_directory: str = "./chroma_db_backups"
    
    # Collection settings
    collection_name: str = "helix_knowledge"
    embedding_dim: int = 768  # nomic-embed-text-v1.5
    
    # HNSW index parameters (optimized for 32GB RAM)
    hnsw_space: str = "cosine"  # Distance metric
    hnsw_construction_ef: int = 128  # Build-time accuracy
    hnsw_search_ef: int = 64  # Search-time accuracy
    hnsw_M: int = 16  # Connections per layer
    
    # Performance
    batch_size: int = 100  # Batch insert size
    max_results: int = 100  # Max search results
    
    # Metadata
    track_versions: bool = True
    enable_deduplication: bool = True


@dataclass
class SearchResult:
    """Search result with metadata."""
    chunk_id: str
    content: str
    source_file: str
    score: float
    metadata: Dict[str, Any]
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "chunk_id": self.chunk_id,
            "content": self.content[:200] + "..." if len(self.content) > 200 else self.content,
            "source_file": self.source_file,
            "score": round(self.score, 4),
            "metadata": self.metadata
        }


class ChromaVectorStore:
    """
    ChromaDB vector store with optimized HNSW indexing.
    
    Features:
    - Persistent storage on NVMe SSD
    - HNSW indexing for sub-millisecond search
    - Incremental updates
    - Backup/restore functionality
    - Deduplication
    """
    
    def __init__(self, config: Optional[VectorStoreConfig] = None):
        self.config = config or VectorStoreConfig()
        self.client = None
        self.collection = None
        self._doc_hash_index: Dict[str, str] = {}  # content_hash -> chunk_id
        
    def initialize(self) -> bool:
        """
        Initialize the vector store.
        
        Returns:
            bool: True if initialized successfully
        """
        try:
            import chromadb
            from chromadb.config import Settings
            
            logger.info(f"Initializing ChromaDB at: {self.config.persist_directory}")
            
            # Create directories
            Path(self.config.persist_directory).mkdir(parents=True, exist_ok=True)
            Path(self.config.backup_directory).mkdir(parents=True, exist_ok=True)
            
            # Initialize client with optimized settings
            self.client = chromadb.PersistentClient(
                path=self.config.persist_directory,
                settings=Settings(
                    anonymized_telemetry=False,
                    allow_reset=True,
                    is_persistent=True,
                )
            )
            
            # Get or create collection with HNSW parameters
            self.collection = self.client.get_or_create_collection(
                name=self.config.collection_name,
                metadata={
                    "hnsw:space": self.config.hnsw_space,
                    "hnsw:construction_ef": self.config.hnsw_construction_ef,
                    "hnsw:search_ef": self.config.hnsw_search_ef,
                    "hnsw:M": self.config.hnsw_M,
                    "description": "HelixLLM RAG Knowledge Base",
                    "created": datetime.now().isoformat(),
                }
            )
            
            logger.info(f"Collection '{self.config.collection_name}' ready")
            logger.info(f"Document count: {self.collection.count()}")
            
            # Load existing hash index
            self._load_hash_index()
            
            return True
            
        except ImportError:
            logger.error("chromadb not installed. Install with: pip install chromadb")
            return False
        except Exception as e:
            logger.error(f"Failed to initialize vector store: {e}")
            return False
    
    def add_documents(
        self, 
        chunks: List[Any], 
        embeddings: np.ndarray,
        skip_duplicates: bool = True
    ) -> List[str]:
        """
        Add document chunks to the vector store.
        
        Args:
            chunks: List of DocumentChunk objects
            embeddings: Embedding vectors (shape: [n_chunks, embedding_dim])
            skip_duplicates: Skip if content hash matches existing
            
        Returns:
            List of added chunk IDs
        """
        if self.collection is None:
            if not self.initialize():
                raise RuntimeError("Vector store not initialized")
        
        if len(chunks) != len(embeddings):
            raise ValueError(f"Chunks ({len(chunks)}) and embeddings ({len(embeddings)}) count mismatch")
        
        added_ids = []
        
        # Process in batches
        for i in range(0, len(chunks), self.config.batch_size):
            batch_chunks = chunks[i:i + self.config.batch_size]
            batch_embeddings = embeddings[i:i + self.config.batch_size]
            
            ids = []
            documents = []
            metadatas = []
            batch_embeddings_list = []
            
            for chunk, embedding in zip(batch_chunks, batch_embeddings):
                # Compute content hash for deduplication
                content_hash = hashlib.md5(chunk.content.encode()).hexdigest()
                
                # Skip duplicates
                if skip_duplicates and content_hash in self._doc_hash_index:
                    logger.debug(f"Skipping duplicate: {chunk.chunk_id}")
                    continue
                
                # Prepare data
                ids.append(chunk.chunk_id)
                documents.append(chunk.content)
                
                # Build metadata
                metadata = {
                    "source_file": chunk.source_file,
                    "file_type": chunk.file_type.value if hasattr(chunk.file_type, 'value') else str(chunk.file_type),
                    "start_line": chunk.start_line,
                    "end_line": chunk.end_line,
                    "chunk_index": chunk.chunk_index,
                    "total_chunks": chunk.total_chunks,
                    "content_hash": content_hash,
                    "added_at": datetime.now().isoformat(),
                }
                
                # Add optional metadata
                if chunk.headers:
                    metadata["headers"] = json.dumps(chunk.headers)
                if chunk.parent_function:
                    metadata["parent_function"] = chunk.parent_function
                if chunk.parent_class:
                    metadata["parent_class"] = chunk.parent_class
                if chunk.language:
                    metadata["language"] = chunk.language
                if chunk.imports:
                    metadata["imports"] = json.dumps(chunk.imports[:10])  # Limit imports
                
                metadatas.append(metadata)
                batch_embeddings_list.append(embedding.tolist())
                
                # Track hash
                self._doc_hash_index[content_hash] = chunk.chunk_id
                added_ids.append(chunk.chunk_id)
            
            # Add batch to collection
            if ids:
                try:
                    self.collection.add(
                        ids=ids,
                        documents=documents,
                        metadatas=metadatas,
                        embeddings=batch_embeddings_list
                    )
                    logger.info(f"Added batch of {len(ids)} documents")
                except Exception as e:
                    logger.error(f"Error adding batch: {e}")
        
        # Save hash index
        self._save_hash_index()
        
        return added_ids
    
    def search(
        self, 
        query_embedding: np.ndarray,
        top_k: int = 10,
        filters: Optional[Dict[str, Any]] = None
    ) -> List[SearchResult]:
        """
        Search for similar documents.
        
        Args:
            query_embedding: Query embedding vector
            top_k: Number of results to return
            filters: Optional metadata filters
            
        Returns:
            List of search results
        """
        if self.collection is None:
            if not self.initialize():
                raise RuntimeError("Vector store not initialized")
        
        try:
            # Build where clause if filters provided
            where_clause = self._build_where_clause(filters) if filters else None
            
            # Execute search
            results = self.collection.query(
                query_embeddings=[query_embedding.tolist()],
                n_results=min(top_k, self.config.max_results),
                where=where_clause,
                include=["documents", "metadatas", "distances"]
            )
            
            # Parse results
            search_results = []
            
            if results['ids'] and results['ids'][0]:
                for i, chunk_id in enumerate(results['ids'][0]):
                    # Convert distance to similarity score (cosine distance to similarity)
                    distance = results['distances'][0][i]
                    score = 1.0 - distance  # For cosine distance
                    
                    result = SearchResult(
                        chunk_id=chunk_id,
                        content=results['documents'][0][i],
                        source_file=results['metadatas'][0][i].get('source_file', ''),
                        score=score,
                        metadata=results['metadatas'][0][i]
                    )
                    search_results.append(result)
            
            return search_results
            
        except Exception as e:
            logger.error(f"Search error: {e}")
            return []
    
    def hybrid_search(
        self,
        query_embedding: np.ndarray,
        query_text: str,
        top_k: int = 10,
        semantic_weight: float = 0.7,
        keyword_weight: float = 0.3,
        filters: Optional[Dict[str, Any]] = None
    ) -> List[SearchResult]:
        """
        Hybrid search combining semantic and keyword search.
        
        Args:
            query_embedding: Query embedding vector
            query_text: Original query text for keyword search
            top_k: Number of results
            semantic_weight: Weight for semantic search (0-1)
            keyword_weight: Weight for keyword search (0-1)
            filters: Optional metadata filters
            
        Returns:
            Combined and ranked search results
        """
        # Semantic search
        semantic_results = self.search(query_embedding, top_k=top_k * 2, filters=filters)
        
        # Keyword search
        keyword_results = self.keyword_search(query_text, top_k=top_k * 2, filters=filters)
        
        # Combine scores using Reciprocal Rank Fusion
        combined_scores = {}
        
        # Add semantic scores
        for rank, result in enumerate(semantic_results):
            rrf_score = semantic_weight * (1.0 / (rank + 1 + 60))  # RRF formula
            if result.chunk_id in combined_scores:
                combined_scores[result.chunk_id]['score'] += rrf_score
            else:
                combined_scores[result.chunk_id] = {
                    'score': rrf_score,
                    'result': result
                }
        
        # Add keyword scores
        for rank, result in enumerate(keyword_results):
            rrf_score = keyword_weight * (1.0 / (rank + 1 + 60))
            if result.chunk_id in combined_scores:
                combined_scores[result.chunk_id]['score'] += rrf_score
            else:
                combined_scores[result.chunk_id] = {
                    'score': rrf_score,
                    'result': result
                }
        
        # Sort by combined score
        sorted_results = sorted(
            combined_scores.values(),
            key=lambda x: x['score'],
            reverse=True
        )
        
        # Return top_k
        return [item['result'] for item in sorted_results[:top_k]]
    
    def keyword_search(
        self,
        query_text: str,
        top_k: int = 10,
        filters: Optional[Dict[str, Any]] = None
    ) -> List[SearchResult]:
        """
        Keyword-based search using ChromaDB's full-text search.
        
        Args:
            query_text: Search query
            top_k: Number of results
            filters: Optional metadata filters
            
        Returns:
            List of search results
        """
        if self.collection is None:
            return []
        
        try:
            where_clause = self._build_where_clause(filters) if filters else None
            
            # Use ChromaDB's where_document for keyword search
            results = self.collection.query(
                query_texts=[query_text],
                n_results=min(top_k, self.config.max_results),
                where=where_clause,
                include=["documents", "metadatas", "distances"]
            )
            
            search_results = []
            
            if results['ids'] and results['ids'][0]:
                for i, chunk_id in enumerate(results['ids'][0]):
                    distance = results['distances'][0][i]
                    score = 1.0 - distance
                    
                    result = SearchResult(
                        chunk_id=chunk_id,
                        content=results['documents'][0][i],
                        source_file=results['metadatas'][0][i].get('source_file', ''),
                        score=score,
                        metadata=results['metadatas'][0][i]
                    )
                    search_results.append(result)
            
            return search_results
            
        except Exception as e:
            logger.error(f"Keyword search error: {e}")
            return []
    
    def delete_document(self, source_file: str) -> int:
        """
        Delete all chunks from a source file.
        
        Args:
            source_file: Path of the source file
            
        Returns:
            Number of chunks deleted
        """
        if self.collection is None:
            return 0
        
        try:
            # Find all chunks from this file
            results = self.collection.get(
                where={"source_file": source_file}
            )
            
            if results['ids']:
                self.collection.delete(ids=results['ids'])
                
                # Remove from hash index
                for metadata in results['metadatas']:
                    content_hash = metadata.get('content_hash')
                    if content_hash and content_hash in self._doc_hash_index:
                        del self._doc_hash_index[content_hash]
                
                self._save_hash_index()
                logger.info(f"Deleted {len(results['ids'])} chunks from {source_file}")
                return len(results['ids'])
            
            return 0
            
        except Exception as e:
            logger.error(f"Error deleting document: {e}")
            return 0
    
    def get_stats(self) -> Dict[str, Any]:
        """Get vector store statistics."""
        if self.collection is None:
            return {}
        
        count = self.collection.count()
        
        # Get unique source files
        try:
            all_meta = self.collection.get(include=["metadatas"])
            source_files = set()
            languages = set()
            
            for meta in all_meta.get('metadatas', []):
                if meta:
                    source_files.add(meta.get('source_file', 'unknown'))
                    languages.add(meta.get('language', 'unknown'))
            
            return {
                "total_chunks": count,
                "unique_files": len(source_files),
                "languages": list(languages),
                "storage_path": self.config.persist_directory,
                "collection_name": self.config.collection_name,
                "hnsw_config": {
                    "space": self.config.hnsw_space,
                    "M": self.config.hnsw_M,
                    "construction_ef": self.config.hnsw_construction_ef,
                    "search_ef": self.config.hnsw_search_ef,
                }
            }
        except Exception as e:
            return {
                "total_chunks": count,
                "error": str(e)
            }
    
    def backup(self, backup_name: Optional[str] = None) -> str:
        """
        Create a backup of the vector store.
        
        Args:
            backup_name: Name for the backup (default: timestamp)
            
        Returns:
            Path to backup directory
        """
        if backup_name is None:
            backup_name = datetime.now().strftime("%Y%m%d_%H%M%S")
        
        backup_path = Path(self.config.backup_directory) / backup_name
        
        try:
            if Path(self.config.persist_directory).exists():
                shutil.copytree(
                    self.config.persist_directory,
                    backup_path,
                    dirs_exist_ok=True
                )
                logger.info(f"Backup created: {backup_path}")
                return str(backup_path)
        except Exception as e:
            logger.error(f"Backup failed: {e}")
            return ""
    
    def restore(self, backup_name: str) -> bool:
        """
        Restore from a backup.
        
        Args:
            backup_name: Name of the backup to restore
            
        Returns:
            True if restored successfully
        """
        backup_path = Path(self.config.backup_directory) / backup_name
        
        if not backup_path.exists():
            logger.error(f"Backup not found: {backup_path}")
            return False
        
        try:
            # Remove current data
            if Path(self.config.persist_directory).exists():
                shutil.rmtree(self.config.persist_directory)
            
            # Restore from backup
            shutil.copytree(backup_path, self.config.persist_directory)
            
            # Reinitialize
            self.client = None
            self.collection = None
            self._doc_hash_index = {}
            
            return self.initialize()
            
        except Exception as e:
            logger.error(f"Restore failed: {e}")
            return False
    
    def list_backups(self) -> List[str]:
        """List available backups."""
        backup_dir = Path(self.config.backup_directory)
        if not backup_dir.exists():
            return []
        
        return [d.name for d in backup_dir.iterdir() if d.is_dir()]
    
    def _build_where_clause(self, filters: Dict[str, Any]) -> Dict[str, Any]:
        """Build ChromaDB where clause from filters."""
        if not filters:
            return {}
        
        where_clause = {}
        
        for key, value in filters.items():
            if isinstance(value, list):
                where_clause[key] = {"$in": value}
            elif isinstance(value, dict):
                where_clause[key] = value
            else:
                where_clause[key] = {"$eq": value}
        
        return where_clause
    
    def _load_hash_index(self):
        """Load content hash index from disk."""
        hash_file = Path(self.config.persist_directory) / "content_hashes.json"
        if hash_file.exists():
            try:
                with open(hash_file, 'r') as f:
                    self._doc_hash_index = json.load(f)
                logger.info(f"Loaded {len(self._doc_hash_index)} content hashes")
            except Exception as e:
                logger.warning(f"Failed to load hash index: {e}")
    
    def _save_hash_index(self):
        """Save content hash index to disk."""
        hash_file = Path(self.config.persist_directory) / "content_hashes.json"
        try:
            with open(hash_file, 'w') as f:
                json.dump(self._doc_hash_index, f)
        except Exception as e:
            logger.warning(f"Failed to save hash index: {e}")
    
    def reset(self):
        """Reset the vector store (delete all data)."""
        if self.client:
            try:
                self.client.delete_collection(self.config.collection_name)
                self._doc_hash_index = {}
                self.initialize()
                logger.info("Vector store reset")
            except Exception as e:
                logger.error(f"Reset failed: {e}")


# Example usage
if __name__ == "__main__":
    # Configuration
    config = VectorStoreConfig(
        persist_directory="./test_chroma_db",
        collection_name="test_knowledge",
        hnsw_M=16,
        hnsw_construction_ef=128,
        hnsw_search_ef=64
    )
    
    # Initialize store
    store = ChromaVectorStore(config)
    store.initialize()
    
    # Print stats
    print(f"Store stats: {store.get_stats()}")
    
    # Test backup
    backup_path = store.backup("test_backup")
    print(f"Backup created: {backup_path}")
    print(f"Available backups: {store.list_backups()}")
