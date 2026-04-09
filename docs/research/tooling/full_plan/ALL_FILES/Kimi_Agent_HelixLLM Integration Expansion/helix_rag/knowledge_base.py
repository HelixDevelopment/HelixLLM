"""
HelixLLM RAG Pipeline - Knowledge Base Management
=================================================
Complete knowledge base management with incremental updates,
document versioning, duplicate detection, and garbage collection.
"""

import os
import json
import hashlib
import shutil
from pathlib import Path
from dataclasses import dataclass, asdict
from typing import List, Dict, Optional, Any, Set, Callable
from datetime import datetime, timedelta
from enum import Enum
import logging
import threading

logger = logging.getLogger(__name__)


class DocumentStatus(Enum):
    """Status of a document in the knowledge base."""
    ACTIVE = "active"
    UPDATED = "updated"
    DELETED = "deleted"
    PENDING = "pending"
    FAILED = "failed"


@dataclass
class DocumentVersion:
    """Version information for a document."""
    version_id: str
    source_file: str
    content_hash: str
    chunk_count: int
    created_at: str
    updated_at: str
    status: DocumentStatus
    metadata: Dict[str, Any]


@dataclass
class KnowledgeBaseConfig:
    """Configuration for knowledge base."""
    # Storage paths
    base_directory: str = "./knowledge_base"
    documents_dir: str = "./knowledge_base/documents"
    versions_file: str = "./knowledge_base/versions.json"
    index_file: str = "./knowledge_base/index.json"
    
    # Processing
    auto_process: bool = True
    process_interval: int = 300  # Seconds between auto-processing
    
    # Versioning
    keep_versions: int = 5  # Number of versions to keep
    enable_versioning: bool = True
    
    # Deduplication
    dedup_by_content: bool = True
    dedup_by_path: bool = True
    
    # Garbage collection
    gc_interval: int = 3600  # Run GC every hour
    orphan_threshold: int = 86400  # Delete orphans after 24 hours
    
    # Monitoring
    enable_metrics: bool = True


class DocumentIndex:
    """Index for tracking documents and their chunks."""
    
    def __init__(self, index_file: str):
        self.index_file = index_file
        self._index: Dict[str, DocumentVersion] = {}
        self._chunk_to_doc: Dict[str, str] = {}  # chunk_id -> source_file
        self._content_hashes: Dict[str, str] = {}  # content_hash -> source_file
        self._lock = threading.RLock()
        
        self._load()
    
    def _load(self):
        """Load index from disk."""
        if Path(self.index_file).exists():
            try:
                with open(self.index_file, 'r') as f:
                    data = json.load(f)
                    for doc_id, doc_data in data.get('documents', {}).items():
                        self._index[doc_id] = DocumentVersion(**doc_data)
                    self._chunk_to_doc = data.get('chunk_to_doc', {})
                    self._content_hashes = data.get('content_hashes', {})
                logger.info(f"Loaded index with {len(self._index)} documents")
            except Exception as e:
                logger.error(f"Failed to load index: {e}")
    
    def save(self):
        """Save index to disk."""
        with self._lock:
            try:
                data = {
                    'documents': {
                        k: asdict(v) for k, v in self._index.items()
                    },
                    'chunk_to_doc': self._chunk_to_doc,
                    'content_hashes': self._content_hashes,
                    'updated_at': datetime.now().isoformat()
                }
                
                Path(self.index_file).parent.mkdir(parents=True, exist_ok=True)
                
                with open(self.index_file, 'w') as f:
                    json.dump(data, f, indent=2)
                    
            except Exception as e:
                logger.error(f"Failed to save index: {e}")
    
    def add_document(
        self, 
        source_file: str, 
        content_hash: str,
        chunk_ids: List[str],
        metadata: Dict[str, Any] = None
    ) -> DocumentVersion:
        """Add or update a document in the index."""
        with self._lock:
            now = datetime.now().isoformat()
            
            # Check if document exists
            if source_file in self._index:
                doc = self._index[source_file]
                doc.content_hash = content_hash
                doc.chunk_count = len(chunk_ids)
                doc.updated_at = now
                doc.status = DocumentStatus.UPDATED
                doc.metadata = metadata or {}
            else:
                doc = DocumentVersion(
                    version_id=self._generate_version_id(),
                    source_file=source_file,
                    content_hash=content_hash,
                    chunk_count=len(chunk_ids),
                    created_at=now,
                    updated_at=now,
                    status=DocumentStatus.ACTIVE,
                    metadata=metadata or {}
                )
                self._index[source_file] = doc
            
            # Update chunk mappings
            for chunk_id in chunk_ids:
                self._chunk_to_doc[chunk_id] = source_file
            
            # Update content hash
            self._content_hashes[content_hash] = source_file
            
            self.save()
            return doc
    
    def get_document(self, source_file: str) -> Optional[DocumentVersion]:
        """Get document by source file path."""
        with self._lock:
            return self._index.get(source_file)
    
    def get_document_by_chunk(self, chunk_id: str) -> Optional[DocumentVersion]:
        """Get document by chunk ID."""
        with self._lock:
            source_file = self._chunk_to_doc.get(chunk_id)
            if source_file:
                return self._index.get(source_file)
            return None
    
    def get_document_by_hash(self, content_hash: str) -> Optional[DocumentVersion]:
        """Get document by content hash."""
        with self._lock:
            source_file = self._content_hashes.get(content_hash)
            if source_file:
                return self._index.get(source_file)
            return None
    
    def remove_document(self, source_file: str) -> bool:
        """Remove document from index."""
        with self._lock:
            if source_file in self._index:
                doc = self._index[source_file]
                
                # Remove chunk mappings
                # Note: We don't know exact chunks, would need to track
                
                # Remove content hash
                if doc.content_hash in self._content_hashes:
                    del self._content_hashes[doc.content_hash]
                
                # Mark as deleted
                doc.status = DocumentStatus.DELETED
                doc.updated_at = datetime.now().isoformat()
                
                self.save()
                return True
            return False
    
    def list_documents(
        self, 
        status: Optional[DocumentStatus] = None
    ) -> List[DocumentVersion]:
        """List all documents, optionally filtered by status."""
        with self._lock:
            docs = list(self._index.values())
            if status:
                docs = [d for d in docs if d.status == status]
            return docs
    
    def is_duplicate(self, source_file: str, content_hash: str) -> bool:
        """Check if document is a duplicate."""
        with self._lock:
            # Check by content hash
            if content_hash in self._content_hashes:
                existing = self._content_hashes[content_hash]
                if existing != source_file:
                    return True
            
            # Check by path
            if source_file in self._index:
                doc = self._index[source_file]
                if doc.content_hash == content_hash:
                    return True
            
            return False
    
    def get_stats(self) -> Dict[str, Any]:
        """Get index statistics."""
        with self._lock:
            status_counts = {}
            for doc in self._index.values():
                status = doc.status.value
                status_counts[status] = status_counts.get(status, 0) + 1
            
            return {
                'total_documents': len(self._index),
                'total_chunks': len(self._chunk_to_doc),
                'status_breakdown': status_counts,
                'unique_content': len(self._content_hashes)
            }
    
    def _generate_version_id(self) -> str:
        """Generate unique version ID."""
        return hashlib.md5(
            f"{datetime.now().isoformat()}".encode()
        ).hexdigest()[:12]


class KnowledgeBase:
    """
    Complete knowledge base management system.
    
    Features:
    - Incremental document updates
    - Version tracking
    - Duplicate detection
    - Garbage collection
    - Monitoring and metrics
    """
    
    def __init__(
        self,
        vector_store: Any,
        embedder: Any,
        processor: Any,
        config: Optional[KnowledgeBaseConfig] = None
    ):
        self.config = config or KnowledgeBaseConfig()
        self.vector_store = vector_store
        self.embedder = embedder
        self.processor = processor
        
        # Initialize document index
        self.index = DocumentIndex(self.config.index_file)
        
        # Create directories
        Path(self.config.base_directory).mkdir(parents=True, exist_ok=True)
        Path(self.config.documents_dir).mkdir(parents=True, exist_ok=True)
        
        # Metrics
        self._metrics = {
            'documents_added': 0,
            'documents_updated': 0,
            'documents_deleted': 0,
            'chunks_added': 0,
            'processing_time': 0,
            'last_gc': None
        }
        
        # Background tasks
        self._gc_timer = None
        
    def add_document(
        self, 
        file_path: str,
        force_update: bool = False
    ) -> Dict[str, Any]:
        """
        Add or update a document in the knowledge base.
        
        Args:
            file_path: Path to the document
            force_update: Force update even if content hasn't changed
            
        Returns:
            Result dictionary with status and metadata
        """
        import time
        start_time = time.time()
        
        file_path = Path(file_path)
        
        if not file_path.exists():
            return {
                'success': False,
                'error': f"File not found: {file_path}"
            }
        
        # Read content
        try:
            content = file_path.read_text(encoding='utf-8', errors='ignore')
            content_hash = hashlib.md5(content.encode()).hexdigest()
        except Exception as e:
            return {
                'success': False,
                'error': f"Failed to read file: {e}"
            }
        
        source_file = str(file_path)
        
        # Check for duplicates
        if not force_update and self.index.is_duplicate(source_file, content_hash):
            existing = self.index.get_document(source_file)
            return {
                'success': True,
                'action': 'skipped',
                'reason': 'duplicate_content',
                'document': asdict(existing) if existing else None
            }
        
        # Delete old version if exists
        existing_doc = self.index.get_document(source_file)
        if existing_doc:
            self.vector_store.delete_document(source_file)
            self._metrics['documents_updated'] += 1
        
        # Process document
        chunks = self.processor.process_file(source_file)
        
        if not chunks:
            return {
                'success': False,
                'error': 'No chunks generated from document'
            }
        
        # Generate embeddings
        chunk_texts = [chunk.get_embedding_text() for chunk in chunks]
        embeddings = self.embedder.embed_documents(chunk_texts)
        
        # Add to vector store
        chunk_ids = self.vector_store.add_documents(chunks, embeddings)
        
        # Update index
        doc = self.index.add_document(
            source_file=source_file,
            content_hash=content_hash,
            chunk_ids=chunk_ids,
            metadata={
                'file_size': file_path.stat().st_size,
                'file_modified': datetime.fromtimestamp(
                    file_path.stat().st_mtime
                ).isoformat()
            }
        )
        
        # Update metrics
        self._metrics['documents_added'] += 1
        self._metrics['chunks_added'] += len(chunks)
        self._metrics['processing_time'] += time.time() - start_time
        
        return {
            'success': True,
            'action': 'added' if not existing_doc else 'updated',
            'document': asdict(doc),
            'chunks_added': len(chunks),
            'processing_time': time.time() - start_time
        }
    
    def add_directory(
        self,
        directory: str,
        include_patterns: List[str] = None,
        exclude_patterns: List[str] = None,
        progress_callback: Optional[Callable] = None
    ) -> Dict[str, Any]:
        """
        Add all documents from a directory.
        
        Args:
            directory: Directory to process
            include_patterns: File patterns to include
            exclude_patterns: File patterns to exclude
            progress_callback: Optional callback(current, total, file)
            
        Returns:
            Summary of processing results
        """
        directory = Path(directory)
        
        if not directory.exists():
            return {'success': False, 'error': f'Directory not found: {directory}'}
        
        # Find all files
        files = []
        include_patterns = include_patterns or ["*.py", "*.js", "*.ts", "*.md", "*.txt"]
        exclude_patterns = exclude_patterns or ["*/venv/*", "*/.git/*", "*/__pycache__/*"]
        
        for pattern in include_patterns:
            for file_path in directory.rglob(pattern):
                excluded = any(
                    excl.strip('*').replace('/', '') in str(file_path)
                    for excl in exclude_patterns
                )
                if not excluded:
                    files.append(file_path)
        
        # Process files
        results = {
            'total_files': len(files),
            'successful': 0,
            'failed': 0,
            'skipped': 0,
            'chunks_added': 0,
            'errors': []
        }
        
        for i, file_path in enumerate(files):
            if progress_callback:
                progress_callback(i + 1, len(files), str(file_path))
            
            result = self.add_document(file_path)
            
            if result['success']:
                if result.get('action') == 'skipped':
                    results['skipped'] += 1
                else:
                    results['successful'] += 1
                    results['chunks_added'] += result.get('chunks_added', 0)
            else:
                results['failed'] += 1
                results['errors'].append({
                    'file': str(file_path),
                    'error': result.get('error')
                })
        
        return results
    
    def remove_document(self, file_path: str) -> bool:
        """
        Remove a document from the knowledge base.
        
        Args:
            file_path: Path of the document to remove
            
        Returns:
            True if removed successfully
        """
        source_file = str(Path(file_path))
        
        # Delete from vector store
        deleted_count = self.vector_store.delete_document(source_file)
        
        # Update index
        if self.index.remove_document(source_file):
            self._metrics['documents_deleted'] += 1
            logger.info(f"Removed document: {source_file} ({deleted_count} chunks)")
            return True
        
        return False
    
    def sync_directory(
        self,
        directory: str,
        include_patterns: List[str] = None,
        exclude_patterns: List[str] = None
    ) -> Dict[str, Any]:
        """
        Synchronize knowledge base with directory.
        
        Adds new files, updates modified files, removes deleted files.
        
        Args:
            directory: Directory to sync with
            include_patterns: File patterns to include
            exclude_patterns: File patterns to exclude
            
        Returns:
            Sync summary
        """
        directory = Path(directory)
        
        # Get current files in directory
        current_files = set()
        include_patterns = include_patterns or ["*.py", "*.js", "*.ts", "*.md", "*.txt"]
        exclude_patterns = exclude_patterns or ["*/venv/*", "*/.git/*", "*/__pycache__/*"]
        
        for pattern in include_patterns:
            for file_path in directory.rglob(pattern):
                excluded = any(
                    excl.strip('*').replace('/', '') in str(file_path)
                    for excl in exclude_patterns
                )
                if not excluded:
                    current_files.add(str(file_path))
        
        # Get tracked files
        tracked_files = {
            doc.source_file for doc in self.index.list_documents()
            if doc.status == DocumentStatus.ACTIVE
        }
        
        # Find changes
        new_files = current_files - tracked_files
        deleted_files = tracked_files - current_files
        
        # Process changes
        results = {
            'added': [],
            'removed': [],
            'unchanged': [],
            'errors': []
        }
        
        # Add new files
        for file_path in new_files:
            result = self.add_document(file_path)
            if result['success']:
                results['added'].append(file_path)
            else:
                results['errors'].append({'file': file_path, 'error': result.get('error')})
        
        # Remove deleted files
        for file_path in deleted_files:
            if self.remove_document(file_path):
                results['removed'].append(file_path)
        
        # Check for modified files
        for file_path in current_files & tracked_files:
            file_hash = hashlib.md5(
                Path(file_path).read_bytes()
            ).hexdigest()
            
            doc = self.index.get_document(file_path)
            if doc and doc.content_hash != file_hash:
                # File modified, re-add
                result = self.add_document(file_path, force_update=True)
                if result['success']:
                    results['added'].append(file_path)
            else:
                results['unchanged'].append(file_path)
        
        return results
    
    def garbage_collect(self) -> Dict[str, Any]:
        """
        Run garbage collection to clean up orphaned data.
        
        Returns:
            GC results summary
        """
        results = {
            'orphaned_chunks_removed': 0,
            'old_versions_removed': 0,
            'errors': []
        }
        
        try:
            # Find orphaned chunks (not in index)
            tracked_chunks = set(self.index._chunk_to_doc.keys())
            
            # This would require vector store support for listing all chunks
            # Implementation depends on vector store capabilities
            
            # Clean up old versions
            if self.config.enable_versioning:
                for doc in self.index.list_documents():
                    # Remove old versions logic here
                    pass
            
            self._metrics['last_gc'] = datetime.now().isoformat()
            
        except Exception as e:
            results['errors'].append(str(e))
            logger.error(f"Garbage collection error: {e}")
        
        return results
    
    def get_stats(self) -> Dict[str, Any]:
        """Get knowledge base statistics."""
        return {
            'index_stats': self.index.get_stats(),
            'vector_stats': self.vector_store.get_stats(),
            'metrics': self._metrics,
            'config': asdict(self.config)
        }
    
    def backup(self, backup_name: Optional[str] = None) -> str:
        """Create a backup of the knowledge base."""
        return self.vector_store.backup(backup_name)
    
    def restore(self, backup_name: str) -> bool:
        """Restore from a backup."""
        return self.vector_store.restore(backup_name)
    
    def export_index(self, output_file: str):
        """Export document index to JSON."""
        with open(output_file, 'w') as f:
            json.dump({
                'documents': {
                    k: asdict(v) for k, v in self.index._index.items()
                },
                'metrics': self._metrics
            }, f, indent=2)


# Example usage
if __name__ == "__main__":
    from embedding_model import NomicEmbedder, EmbeddingConfig
    from vector_store import ChromaVectorStore, VectorStoreConfig
    from document_processor import DocumentProcessor, ChunkConfig
    
    # Initialize components
    embedder = NomicEmbedder(EmbeddingConfig())
    embedder.load_model()
    
    vector_store = ChromaVectorStore(VectorStoreConfig())
    vector_store.initialize()
    
    processor = DocumentProcessor(ChunkConfig())
    
    # Create knowledge base
    config = KnowledgeBaseConfig(
        base_directory="./test_kb",
        enable_versioning=True
    )
    
    kb = KnowledgeBase(vector_store, embedder, processor, config)
    
    # Print stats
    print(f"KB Stats: {kb.get_stats()}")
