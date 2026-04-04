# Phase 2: RAG System with Enterprise Knowledge Base - Complete Implementation Guide

## Table of Contents
1. [Overview](#overview)
2. [Hardware Configuration](#hardware-configuration)
3. [Project Structure](#project-structure)
4. [Embedding Model Setup](#embedding-model-setup)
5. [Vector Database Setup](#vector-database-setup)
6. [Document Processing Pipeline](#document-processing-pipeline)
7. [Enterprise Knowledge Base Structure](#enterprise-knowledge-base-structure)
8. [RAG Pipeline Implementation](#rag-pipeline-implementation)
9. [Search Optimization](#search-optimization)
10. [FastAPI Wrapper](#fastapi-wrapper)
11. [Configuration Files](#configuration-files)
12. [Running the System](#running-the-system)

---

## Overview

This guide provides a complete implementation of a production-grade RAG (Retrieval-Augmented Generation) system optimized for enterprise software development knowledge bases. The system uses:

- **Embedding Model**: Google EmbeddingGemma-300M (308M parameters, quantized) OR sentence-transformers alternatives
- **Vector Database**: ChromaDB with persistent storage
- **Document Processing**: Multi-format support (PDF, Markdown, Code files)
- **API Layer**: FastAPI for REST endpoints
- **Integration**: llama.cpp for local LLM inference

---

## Hardware Configuration

### RAG Server (Desktop)
- **CPU**: Intel i7 11th Gen
- **RAM**: 64GB DDR4
- **Storage**: SSD recommended for vector database
- **GPU**: Optional (CPU-only embedding inference)

---

## Project Structure

```
rag_system/
├── config/
│   ├── __init__.py
│   ├── settings.py          # Configuration management
│   └── logging_config.py    # Logging setup
├── core/
│   ├── __init__.py
│   ├── embeddings.py        # Embedding model wrapper
│   ├── vector_store.py      # ChromaDB operations
│   └── document_processor.py # Document loading & chunking
├── api/
│   ├── __init__.py
│   ├── main.py              # FastAPI application
│   ├── routes.py            # API endpoints
│   └── models.py            # Pydantic models
├── rag/
│   ├── __init__.py
│   ├── pipeline.py          # Main RAG pipeline
│   ├── retriever.py         # Search & retrieval
│   └── reranker.py          # Re-ranking logic
├── docs/                    # Enterprise knowledge base
│   ├── architecture/
│   ├── backend/
│   ├── frontend/
│   ├── mobile/
│   ├── desktop/
│   ├── devops/
│   ├── testing/
│   └── security/
├── vector_db/               # ChromaDB persistent storage
├── scripts/
│   ├── ingest_documents.py  # Document ingestion script
│   └── setup_kb.py          # Knowledge base setup
├── tests/
├── .env                     # Environment variables
├── requirements.txt         # Python dependencies
└── README.md
```

---

## Embedding Model Setup

### 1. Model Information

**Google EmbeddingGemma-300M**
- Parameters: 308M
- Embedding Dimensions: 3072
- Max Sequence Length: 8192 tokens
- Quantized Size: ~180MB (4-bit)
- RAM Usage: <200MB

**Recommended Alternative: Sentence-Transformers**
| Model | Dimensions | Size | RAM | Best For |
|-------|-----------|------|-----|----------|
| all-MiniLM-L6-v2 | 384 | 80MB | ~200MB | Quick prototyping |
| all-mpnet-base-v2 | 768 | 420MB | ~600MB | Balanced quality |
| BAAI/bge-large-en-v1.5 | 1024 | 1.3GB | ~1.5GB | Best quality |
| WhereIsAI/UAE-Large-V1 | 1024 | 1.3GB | ~1.5GB | Code-optimized |

### 2. Embedding Model Implementation

Create `core/embeddings.py`:

```python
"""Embedding Model Wrapper with multiple backend support."""

import os
import logging
from typing import List, Union, Optional
from pathlib import Path
import torch
from transformers import AutoTokenizer, AutoModel
import numpy as np

logger = logging.getLogger(__name__)


class SentenceTransformerEmbedding:
    """
    Production-ready embedding using sentence-transformers.
    Recommended for stability and ease of deployment.
    """
    
    MODELS = {
        "mini": "sentence-transformers/all-MiniLM-L6-v2",  # 384 dim
        "mpnet": "sentence-transformers/all-mpnet-base-v2",  # 768 dim
        "bge": "BAAI/bge-large-en-v1.5",  # 1024 dim
        "uae": "WhereIsAI/UAE-Large-V1",  # 1024 dim, code-optimized
    }
    
    def __init__(self, model_key: str = "mpnet", device: str = None):
        from sentence_transformers import SentenceTransformer
        
        model_name = self.MODELS.get(model_key, model_key)
        self.device = device or ("cuda" if torch.cuda.is_available() else "cpu")
        self.model = SentenceTransformer(model_name, device=self.device)
        self.embedding_dim = self.model.get_sentence_embedding_dimension()
        self.model_name = model_name
        logger.info(f"Loaded embedding model: {model_name} ({self.embedding_dim}d)")
    
    def embed(self, texts: Union[str, List[str]]) -> np.ndarray:
        if isinstance(texts, str):
            texts = [texts]
        return self.model.encode(texts, convert_to_numpy=True, show_progress_bar=True)
    
    def embed_query(self, text: str) -> np.ndarray:
        return self.embed(text)[0]
    
    def embed_documents(self, documents: List[str]) -> List[List[float]]:
        embeddings = self.embed(documents)
        return embeddings.tolist()


class EmbeddingGemmaModel:
    """Wrapper for Google EmbeddingGemma-300M with quantization support."""
    
    def __init__(
        self,
        model_name: str = "google/embeddinggemma-300m",
        cache_dir: Optional[str] = None,
        device: Optional[str] = None,
        quantization: str = "4bit",
        max_length: int = 8192,
    ):
        self.model_name = model_name
        self.cache_dir = cache_dir or os.path.expanduser("~/.cache/huggingface")
        self.device = device or ("cuda" if torch.cuda.is_available() else "cpu")
        self.quantization = quantization
        self.max_length = max_length
        self.embedding_dim = 3072
        
        self._load_model()
    
    def _load_model(self) -> None:
        logger.info(f"Loading embedding model: {self.model_name}")
        
        self.tokenizer = AutoTokenizer.from_pretrained(
            self.model_name, cache_dir=self.cache_dir, trust_remote_code=True
        )
        
        load_kwargs = {
            "cache_dir": self.cache_dir,
            "trust_remote_code": True,
            "torch_dtype": torch.float32 if self.device == "cpu" else torch.float16,
        }
        
        if self.quantization == "4bit" and self.device == "cuda":
            try:
                from transformers import BitsAndBytesConfig
                load_kwargs["quantization_config"] = BitsAndBytesConfig(
                    load_in_4bit=True,
                    bnb_4bit_compute_dtype=torch.float16,
                    bnb_4bit_quant_type="nf4",
                    bnb_4bit_use_double_quant=True,
                )
                load_kwargs["device_map"] = "auto"
            except ImportError:
                pass
        
        self.model = AutoModel.from_pretrained(self.model_name, **load_kwargs)
        
        if self.device == "cpu" or self.quantization == "none":
            self.model = self.model.to(self.device)
        
        self.model.eval()
        logger.info("Model loaded successfully")
    
    def embed(self, texts: Union[str, List[str]], batch_size: int = 8) -> np.ndarray:
        if isinstance(texts, str):
            texts = [texts]
        
        embeddings = []
        
        with torch.no_grad():
            for i in range(0, len(texts), batch_size):
                batch = texts[i:i + batch_size]
                
                inputs = self.tokenizer(
                    batch, padding=True, truncation=True,
                    max_length=self.max_length, return_tensors="pt"
                )
                
                if self.device == "cpu":
                    inputs = {k: v.to(self.device) for k, v in inputs.items()}
                
                outputs = self.model(**inputs)
                
                # Mean pooling with attention mask
                attention_mask = inputs["attention_mask"]
                mask_expanded = attention_mask.unsqueeze(-1).expand(outputs.last_hidden_state.size()).float()
                sum_embeddings = torch.sum(outputs.last_hidden_state * mask_expanded, 1)
                sum_mask = torch.clamp(mask_expanded.sum(1), min=1e-9)
                batch_embeddings = (sum_embeddings / sum_mask).cpu().numpy()
                
                embeddings.append(batch_embeddings)
        
        return np.vstack(embeddings)
    
    def embed_query(self, text: str) -> np.ndarray:
        return self.embed([text])[0]
    
    def embed_documents(self, documents: List[str]) -> List[List[float]]:
        embeddings = self.embed(documents, batch_size=8)
        return embeddings.tolist()
```

---

## Vector Database Setup

Create `core/vector_store.py`:

```python
"""ChromaDB Vector Store Implementation with persistent storage."""

import os
import logging
from typing import List, Dict, Optional, Any
from pathlib import Path
import chromadb
from chromadb.config import Settings

logger = logging.getLogger(__name__)


class ChromaVectorStore:
    """ChromaDB vector store with persistent storage and optimized indexing."""
    
    def __init__(
        self,
        collection_name: str = "enterprise_kb",
        persist_directory: str = "./vector_db",
        embedding_function=None,
        distance_metric: str = "cosine",
    ):
        self.collection_name = collection_name
        self.persist_directory = Path(persist_directory)
        self.persist_directory.mkdir(parents=True, exist_ok=True)
        self.embedding_function = embedding_function
        self.distance_metric = distance_metric
        
        self.client = chromadb.PersistentClient(
            path=str(self.persist_directory),
            settings=Settings(anonymized_telemetry=False, allow_reset=True)
        )
        
        self.collection = None
        self._get_or_create_collection()
    
    def _get_or_create_collection(self) -> None:
        try:
            self.collection = self.client.get_collection(
                name=self.collection_name,
                embedding_function=self.embedding_function,
            )
            logger.info(f"Loaded collection: {self.collection_name} ({self.collection.count()} docs)")
        except Exception:
            self.collection = self.client.create_collection(
                name=self.collection_name,
                embedding_function=self.embedding_function,
                metadata={
                    "hnsw:space": self.distance_metric,
                    "hnsw:construction_ef": 128,
                    "hnsw:search_ef": 128,
                    "hnsw:M": 16,
                }
            )
            logger.info(f"Created collection: {self.collection_name}")
    
    def add_documents(
        self,
        documents: List[str],
        embeddings: Optional[List[List[float]]] = None,
        metadatas: Optional[List[Dict[str, Any]]] = None,
        ids: Optional[List[str]] = None,
    ) -> List[str]:
        if ids is None:
            import hashlib
            ids = [hashlib.md5(doc.encode()).hexdigest()[:16] for doc in documents]
        
        # Batch processing
        batch_size = 100
        for i in range(0, len(documents), batch_size):
            self.collection.add(
                documents=documents[i:i + batch_size],
                embeddings=embeddings[i:i + batch_size] if embeddings else None,
                metadatas=metadatas[i:i + batch_size] if metadatas else None,
                ids=ids[i:i + batch_size],
            )
        
        logger.info(f"Added {len(documents)} documents")
        return ids
    
    def search(
        self,
        query: str,
        n_results: int = 4,
        filter_dict: Optional[Dict[str, Any]] = None,
    ) -> List[Dict[str, Any]]:
        results = self.collection.query(
            query_texts=[query],
            n_results=n_results,
            where=filter_dict,
            include=["documents", "metadatas", "distances"],
        )
        
        return [
            {
                "id": results["ids"][0][i],
                "document": results["documents"][0][i],
                "metadata": results["metadatas"][0][i] if results["metadatas"] else {},
                "distance": results["distances"][0][i],
            }
            for i in range(len(results["ids"][0]))
        ]
    
    def search_with_embedding(
        self,
        query_embedding: List[float],
        n_results: int = 4,
        filter_dict: Optional[Dict[str, Any]] = None,
    ) -> List[Dict[str, Any]]:
        results = self.collection.query(
            query_embeddings=[query_embedding],
            n_results=n_results,
            where=filter_dict,
            include=["documents", "metadatas", "distances"],
        )
        
        return [
            {
                "id": results["ids"][0][i],
                "document": results["documents"][0][i],
                "metadata": results["metadatas"][0][i] if results["metadatas"] else {},
                "distance": results["distances"][0][i],
            }
            for i in range(len(results["ids"][0]))
        ]
    
    def delete_documents(self, ids: List[str]) -> None:
        self.collection.delete(ids=ids)
    
    def get_all_documents(self, limit: int = 10000, offset: int = 0) -> List[Dict[str, Any]]:
        results = self.collection.get(limit=limit, offset=offset, include=["documents", "metadatas"])
        
        return [
            {
                "id": results["ids"][i],
                "document": results["documents"][i],
                "metadata": results["metadatas"][i] if results["metadatas"] else {},
            }
            for i in range(len(results["ids"]))
        ]
    
    def get_stats(self) -> Dict[str, Any]:
        return {
            "collection_name": self.collection_name,
            "document_count": self.collection.count(),
            "persist_directory": str(self.persist_directory),
        }
    
    def reset_collection(self) -> None:
        try:
            self.client.delete_collection(self.collection_name)
        except Exception:
            pass
        self._get_or_create_collection()
```

---

## Document Processing Pipeline

Create `core/document_processor.py`:

```python
"""Document Processing Pipeline with multi-format support."""

import os
import logging
from typing import List, Dict, Optional, Any, Callable
from pathlib import Path
from dataclasses import dataclass
import re
import hashlib

logger = logging.getLogger(__name__)


@dataclass
class Document:
    """Represents a processed document chunk."""
    content: str
    metadata: Dict[str, Any]
    source: str
    chunk_index: int = 0
    total_chunks: int = 1


class TextChunker:
    """Advanced text chunking with overlap support."""
    
    def __init__(
        self,
        chunk_size: int = 1000,
        chunk_overlap: int = 200,
        separator: str = "\n\n",
    ):
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap
        self.separator = separator
    
    def split_text(self, text: str) -> List[str]:
        splits = text.split(self.separator)
        chunks = []
        current_chunk = []
        current_length = 0
        
        for split in splits:
            split_length = len(split)
            
            if split_length > self.chunk_size:
                if current_chunk:
                    chunks.append(self.separator.join(current_chunk))
                    current_chunk = []
                    current_length = 0
                sub_chunks = self._split_large_chunk(split)
                chunks.extend(sub_chunks)
                continue
            
            if current_length + split_length + len(self.separator) > self.chunk_size:
                if current_chunk:
                    chunks.append(self.separator.join(current_chunk))
                overlap_text = self._get_overlap(current_chunk)
                current_chunk = [overlap_text, split] if overlap_text else [split]
                current_length = len(self.separator.join(current_chunk))
            else:
                current_chunk.append(split)
                current_length += split_length + len(self.separator)
        
        if current_chunk:
            chunks.append(self.separator.join(current_chunk))
        
        return [c.strip() for c in chunks if c.strip()]
    
    def _split_large_chunk(self, text: str) -> List[str]:
        chunks = []
        start = 0
        while start < len(text):
            end = start + self.chunk_size
            chunks.append(text[start:end])
            start = end - self.chunk_overlap
        return chunks
    
    def _get_overlap(self, chunks: List[str]) -> str:
        if not chunks or self.chunk_overlap <= 0:
            return ""
        overlap_text = ""
        for chunk in reversed(chunks):
            overlap_text = chunk + self.separator + overlap_text
            if len(overlap_text) >= self.chunk_overlap:
                break
        return overlap_text[-self.chunk_overlap:].strip() if len(overlap_text) > self.chunk_overlap else overlap_text.strip()
    
    def create_documents(
        self,
        texts: List[str],
        metadatas: Optional[List[Dict[str, Any]]] = None,
    ) -> List[Document]:
        if metadatas is None:
            metadatas = [{} for _ in texts]
        
        documents = []
        for text, metadata in zip(texts, metadatas):
            chunks = self.split_text(text)
            for i, chunk in enumerate(chunks):
                doc = Document(
                    content=chunk,
                    metadata={**metadata, "chunk_index": i, "total_chunks": len(chunks)},
                    source=metadata.get("source", "unknown"),
                    chunk_index=i,
                    total_chunks=len(chunks),
                )
                documents.append(doc)
        
        return documents


class MarkdownChunker(TextChunker):
    """Chunker for Markdown that respects headers."""
    
    HEADER_PATTERN = re.compile(r'^(#{1,6}\s+.+)$', re.MULTILINE)
    
    def __init__(self, chunk_size: int = 1000, chunk_overlap: int = 200):
        super().__init__(chunk_size, chunk_overlap, separator="\n## ")
    
    def split_text(self, text: str) -> List[str]:
        sections = self.HEADER_PATTERN.split(text)
        chunks = []
        current_chunk = ""
        
        for section in sections:
            if section.startswith("#"):
                if current_chunk:
                    chunks.append(current_chunk.strip())
                current_chunk = section
            else:
                current_chunk += section
        
        if current_chunk:
            chunks.append(current_chunk.strip())
        
        final_chunks = []
        for chunk in chunks:
            if len(chunk) > self.chunk_size:
                final_chunks.extend(super().split_text(chunk))
            else:
                final_chunks.append(chunk)
        
        return final_chunks


class DocumentLoader:
    """Universal document loader supporting multiple formats."""
    
    SUPPORTED_EXTENSIONS = {
        ".txt": "text", ".md": "markdown", ".markdown": "markdown", ".pdf": "pdf",
        ".py": "python", ".js": "javascript", ".ts": "typescript", ".jsx": "jsx",
        ".tsx": "tsx", ".java": "java", ".kt": "kotlin", ".cs": "csharp",
        ".go": "go", ".rs": "rust", ".cpp": "cpp", ".c": "c",
        ".rb": "ruby", ".php": "php", ".swift": "swift", ".scala": "scala",
        ".sql": "sql", ".yaml": "yaml", ".yml": "yaml", ".json": "json",
        ".html": "html", ".css": "css", ".sh": "bash", ".dockerfile": "dockerfile",
    }
    
    def __init__(self, chunk_size: int = 1000, chunk_overlap: int = 200):
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap
    
    def load_file(self, file_path: str) -> List[Document]:
        path = Path(file_path)
        if not path.exists():
            raise FileNotFoundError(f"File not found: {file_path}")
        
        extension = path.suffix.lower()
        file_type = self.SUPPORTED_EXTENSIONS.get(extension, "text")
        metadata = self._extract_metadata(path, file_type)
        
        if file_type == "pdf":
            content = self._load_pdf(file_path)
        else:
            content = self._load_text(file_path)
        
        chunker = MarkdownChunker(self.chunk_size, self.chunk_overlap) if file_type == "markdown" else TextChunker(self.chunk_size, self.chunk_overlap)
        documents = chunker.create_documents(texts=[content], metadatas=[metadata])
        
        logger.info(f"Loaded {path.name}: {len(documents)} chunks")
        return documents
    
    def load_directory(
        self,
        directory: str,
        recursive: bool = True,
        file_filter: Optional[Callable[[Path], bool]] = None,
    ) -> List[Document]:
        path = Path(directory)
        if not path.exists():
            raise FileNotFoundError(f"Directory not found: {directory}")
        
        pattern = "**/*" if recursive else "*"
        all_documents = []
        
        for file_path in path.glob(pattern):
            if not file_path.is_file():
                continue
            if file_path.suffix.lower() not in self.SUPPORTED_EXTENSIONS:
                continue
            if file_filter and not file_filter(file_path):
                continue
            
            try:
                documents = self.load_file(str(file_path))
                all_documents.extend(documents)
            except Exception as e:
                logger.error(f"Error loading {file_path}: {e}")
        
        logger.info(f"Loaded {len(all_documents)} total chunks from {directory}")
        return all_documents
    
    def _load_text(self, file_path: str) -> str:
        import chardet
        
        with open(file_path, "rb") as f:
            detected = chardet.detect(f.read())
            encoding = detected.get("encoding", "utf-8")
        
        with open(file_path, "r", encoding=encoding, errors="ignore") as f:
            return f.read()
    
    def _load_pdf(self, file_path: str) -> str:
        try:
            import pdfplumber
            text = ""
            with pdfplumber.open(file_path) as pdf:
                for page in pdf.pages:
                    page_text = page.extract_text()
                    if page_text:
                        text += page_text + "\n\n"
            return text.strip()
        except ImportError:
            raise ImportError("PDF support requires pdfplumber. Install: pip install pdfplumber")
    
    def _extract_metadata(self, path: Path, file_type: str) -> Dict[str, Any]:
        stat = path.stat()
        category = self._determine_category(path)
        
        return {
            "source": str(path),
            "filename": path.name,
            "extension": path.suffix,
            "file_type": file_type,
            "category": category,
            "size_bytes": stat.st_size,
            "file_hash": self._compute_file_hash(path),
        }
    
    def _determine_category(self, path: Path) -> str:
        parent = path.parent.name.lower()
        mapping = {
            "architecture": "architecture", "backend": "backend", "frontend": "frontend",
            "mobile": "mobile", "desktop": "desktop", "devops": "devops",
            "testing": "testing", "security": "security", "docs": "documentation",
        }
        return mapping.get(parent, "general")
    
    def _compute_file_hash(self, path: Path) -> str:
        hash_md5 = hashlib.md5()
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(4096), b""):
                hash_md5.update(chunk)
        return hash_md5.hexdigest()


class DocumentProcessor:
    """High-level document processing orchestrator."""
    
    def __init__(self, chunk_size: int = 1000, chunk_overlap: int = 200):
        self.loader = DocumentLoader(chunk_size, chunk_overlap)
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap
    
    def process_files(self, file_paths: List[str]) -> List[Document]:
        all_documents = []
        for file_path in file_paths:
            try:
                documents = self.loader.load_file(file_path)
                all_documents.extend(documents)
            except Exception as e:
                logger.error(f"Error processing {file_path}: {e}")
        return all_documents
    
    def process_directory(
        self,
        directory: str,
        recursive: bool = True,
        exclude_patterns: Optional[List[str]] = None,
    ) -> List[Document]:
        def file_filter(path: Path) -> bool:
            if exclude_patterns:
                for pattern in exclude_patterns:
                    if pattern in str(path):
                        return False
            return True
        
        return self.loader.load_directory(directory, recursive, file_filter)
```

---

## Enterprise Knowledge Base Structure

Create `scripts/setup_kb.py`:

```python
"""Knowledge Base Setup Script - Creates folder structure for enterprise docs."""

import os
from pathlib import Path
from typing import Dict, List

KB_STRUCTURE = {
    "architecture": {
        "description": "Software architecture patterns and principles",
        "subfolders": ["clean_architecture", "domain_driven_design", "microservices", "event_driven", "cqrs", "hexagonal"],
    },
    "backend": {
        "description": "Backend development frameworks and patterns",
        "subfolders": ["spring_boot", "dotnet_core", "nodejs", "python", "go", "rust", "graphql", "rest_api"],
    },
    "frontend": {
        "description": "Frontend development frameworks and patterns",
        "subfolders": ["react", "angular", "vue", "typescript", "state_management", "css_frameworks"],
    },
    "mobile": {
        "description": "Mobile development frameworks",
        "subfolders": ["flutter", "react_native", "kotlin_multiplatform", "swift", "android", "ios"],
    },
    "desktop": {
        "description": "Desktop application development",
        "subfolders": ["electron", "tauri", "wpf", "qt"],
    },
    "devops": {
        "description": "DevOps practices and tools",
        "subfolders": ["docker", "kubernetes", "cicd", "terraform", "monitoring", "cloud_providers"],
    },
    "testing": {
        "description": "Testing strategies and tools",
        "subfolders": ["unit_testing", "integration_testing", "e2e_testing", "performance_testing"],
    },
    "security": {
        "description": "Security best practices and guidelines",
        "subfolders": ["owasp", "authentication", "authorization", "encryption", "compliance"],
    },
    "database": {
        "description": "Database design and management",
        "subfolders": ["sql", "nosql", "migrations", "optimization"],
    },
}

README_TEMPLATE = """# {category_title}

## Overview

{description}

## Contents

{contents}

## Quick Reference

Add quick reference information here.

## Resources

- [Official Documentation]()
- [Best Practices Guide]()
"""


def create_kb_structure(base_path: str = "./docs") -> None:
    base = Path(base_path)
    base.mkdir(parents=True, exist_ok=True)
    
    print(f"Creating knowledge base at: {base.absolute()}")
    
    for category, config in KB_STRUCTURE.items():
        category_path = base / category
        category_path.mkdir(exist_ok=True)
        print(f"  {category}/")
        
        for subfolder in config.get("subfolders", []):
            (category_path / subfolder).mkdir(exist_ok=True)
        
        readme_path = category_path / "README.md"
        if not readme_path.exists():
            content = README_TEMPLATE.format(
                category_title=category.replace("_", " ").title(),
                description=config.get("description", ""),
                contents="\n".join([f"- {s}" for s in config.get("subfolders", [])]),
            )
            readme_path.write_text(content)
    
    # Main README
    main_readme = base / "README.md"
    if not main_readme.exists():
        sections = []
        for cat, cfg in KB_STRUCTURE.items():
            sections.append(f"### [{cat.replace('_', ' ').title()}](./{cat}/)\n\n{cfg['description']}\n")
        
        main_content = f"""# Enterprise Knowledge Base

## Categories

{chr(10).join(sections)}

## Getting Started

1. Browse category folders
2. Read README.md in each category
3. Follow best practices
"""
        main_readme.write_text(main_content)
    
    print(f"Knowledge base created at: {base.absolute()}")


def get_kb_stats(base_path: str = "./docs") -> Dict:
    base = Path(base_path)
    if not base.exists():
        return {"error": "Not found"}
    
    stats = {"categories": 0, "subfolders": 0, "files": 0, "by_category": {}}
    
    for category in base.iterdir():
        if category.is_dir() and not category.name.startswith("."):
            stats["categories"] += 1
            cat_files = 0
            for item in category.rglob("*"):
                if item.is_file():
                    stats["files"] += 1
                    cat_files += 1
                elif item.is_dir():
                    stats["subfolders"] += 1
            stats["by_category"][category.name] = cat_files
    
    return stats


if __name__ == "__main__":
    import sys
    base_path = sys.argv[1] if len(sys.argv) > 1 else "./docs"
    create_kb_structure(base_path)
    
    stats = get_kb_stats(base_path)
    print(f"\nStats: {stats['categories']} categories, {stats['subfolders']} subfolders, {stats['files']} files")
```

---

## RAG Pipeline Implementation

Create `rag/pipeline.py`:

```python
"""RAG Pipeline - Complete retrieval-augmented generation with llama.cpp."""

import os
import logging
from typing import List, Dict, Optional, Any, Generator
from dataclasses import dataclass, field
from pathlib import Path

from core.embeddings import SentenceTransformerEmbedding
from core.vector_store import ChromaVectorStore
from core.document_processor import DocumentProcessor, Document

logger = logging.getLogger(__name__)


@dataclass
class RAGConfig:
    """Configuration for RAG pipeline."""
    collection_name: str = "enterprise_kb"
    persist_directory: str = "./vector_db"
    distance_metric: str = "cosine"
    embedding_model: str = "mpnet"
    embedding_device: str = "cpu"
    chunk_size: int = 1000
    chunk_overlap: int = 200
    top_k: int = 4
    similarity_threshold: float = 0.7
    llm_model_path: str = "./models/llama-3.2-3b-instruct-q4_k_m.gguf"
    llm_n_ctx: int = 8192
    llm_temperature: float = 0.7
    llm_max_tokens: int = 2048
    system_prompt: str = """You are a helpful technical assistant specializing in software development.
Use the provided context to answer questions accurately and concisely.
If you don't know the answer based on the context, say so clearly."""


@dataclass
class RAGResponse:
    """Response from RAG pipeline."""
    answer: str
    sources: List[Dict[str, Any]]
    context: str
    metadata: Dict[str, Any]


class RAGPipeline:
    """Complete RAG pipeline with document ingestion and query processing."""
    
    def __init__(self, config: Optional[RAGConfig] = None):
        self.config = config or RAGConfig()
        self.embedding_model = None
        self.vector_store = None
        self.document_processor = None
        self.llm = None
        self._initialized = False
    
    def initialize(self) -> None:
        if self._initialized:
            return
        
        logger.info("Initializing RAG pipeline...")
        
        # Embedding model
        logger.info(f"Loading embedding: {self.config.embedding_model}")
        self.embedding_model = SentenceTransformerEmbedding(
            model_key=self.config.embedding_model,
            device=self.config.embedding_device,
        )
        logger.info(f"Embedding dim: {self.embedding_model.embedding_dim}")
        
        # Vector store
        logger.info(f"Connecting to vector store: {self.config.collection_name}")
        self.vector_store = ChromaVectorStore(
            collection_name=self.config.collection_name,
            persist_directory=self.config.persist_directory,
            distance_metric=self.config.distance_metric,
        )
        
        # Document processor
        self.document_processor = DocumentProcessor(
            chunk_size=self.config.chunk_size,
            chunk_overlap=self.config.chunk_overlap,
        )
        
        # Optional LLM
        self._init_llm()
        
        self._initialized = True
        logger.info("RAG pipeline initialized")
    
    def _init_llm(self) -> None:
        try:
            from llama_cpp import Llama
            
            if not os.path.exists(self.config.llm_model_path):
                logger.warning(f"LLM not found: {self.config.llm_model_path}")
                return
            
            logger.info(f"Loading LLM: {self.config.llm_model_path}")
            self.llm = Llama(
                model_path=self.config.llm_model_path,
                n_ctx=self.config.llm_n_ctx,
                verbose=False,
            )
            logger.info("LLM loaded")
        except ImportError:
            logger.warning("llama_cpp not installed")
        except Exception as e:
            logger.error(f"LLM load failed: {e}")
    
    def ingest_documents(
        self,
        source: str,
        recursive: bool = True,
        exclude_patterns: Optional[List[str]] = None,
    ) -> Dict[str, Any]:
        self.initialize()
        
        source_path = Path(source)
        
        if source_path.is_file():
            documents = self.document_processor.process_files([str(source_path)])
        elif source_path.is_dir():
            documents = self.document_processor.process_directory(str(source_path), recursive, exclude_patterns)
        else:
            raise ValueError(f"Source not found: {source}")
        
        if not documents:
            return {"status": "warning", "message": "No documents found"}
        
        logger.info(f"Processing {len(documents)} chunks...")
        
        texts = [doc.content for doc in documents]
        embeddings = self.embedding_model.embed_documents(texts)
        metadatas = [doc.metadata for doc in documents]
        
        ids = self.vector_store.add_documents(
            documents=texts, embeddings=embeddings, metadatas=metadatas
        )
        
        return {
            "status": "success",
            "documents_processed": len(documents),
            "chunks_created": len(ids),
            "source": str(source_path),
        }
    
    def query(
        self,
        question: str,
        top_k: Optional[int] = None,
        filter_dict: Optional[Dict[str, Any]] = None,
        use_llm: bool = True,
    ) -> RAGResponse:
        self.initialize()
        
        top_k = top_k or self.config.top_k
        retrieved_docs = self._retrieve(question, top_k, filter_dict)
        
        if not retrieved_docs:
            return RAGResponse(
                answer="I couldn't find relevant information.",
                sources=[], context="", metadata={"retrieved_count": 0}
            )
        
        context = self._build_context(retrieved_docs)
        
        if use_llm and self.llm:
            answer = self._generate_answer(question, context)
        else:
            answer = self._format_direct_answer(retrieved_docs)
        
        sources = [
            {
                "id": doc["id"],
                "source": doc["metadata"].get("source", "unknown"),
                "category": doc["metadata"].get("category", "general"),
                "distance": doc["distance"],
                "preview": doc["document"][:200] + "..." if len(doc["document"]) > 200 else doc["document"],
            }
            for doc in retrieved_docs
        ]
        
        return RAGResponse(
            answer=answer,
            sources=sources,
            context=context,
            metadata={"retrieved_count": len(retrieved_docs), "query": question},
        )
    
    def _retrieve(
        self,
        query: str,
        top_k: int,
        filter_dict: Optional[Dict[str, Any]] = None,
    ) -> List[Dict[str, Any]]:
        query_embedding = self.embedding_model.embed_query(query)
        
        results = self.vector_store.search_with_embedding(
            query_embedding=query_embedding.tolist(),
            n_results=top_k,
            filter_dict=filter_dict,
        )
        
        filtered = [r for r in results if r["distance"] <= (1 - self.config.similarity_threshold)]
        return filtered or results
    
    def _build_context(self, documents: List[Dict[str, Any]]) -> str:
        parts = []
        for i, doc in enumerate(documents, 1):
            source = doc["metadata"].get("source", "unknown")
            parts.append(f"[Doc {i}] {source}:\n{doc['document']}\n")
        return "\n".join(parts)
    
    def _build_prompt(self, question: str, context: str) -> str:
        return f"""{self.config.system_prompt}

Context:
{context}

Question: {question}

Answer:"""
    
    def _generate_answer(self, question: str, context: str) -> str:
        if not self.llm:
            return self._format_direct_answer([{"document": context}])
        
        prompt = self._build_prompt(question, context)
        response = self.llm(
            prompt,
            max_tokens=self.config.llm_max_tokens,
            temperature=self.config.llm_temperature,
            stop=["</s>", "Human:", "User:"],
        )
        return response["choices"][0]["text"].strip()
    
    def _format_direct_answer(self, documents: List[Dict[str, Any]]) -> str:
        parts = ["Based on retrieved documents:\n"]
        for i, doc in enumerate(documents, 1):
            source = doc.get("metadata", {}).get("source", "unknown")
            content = doc.get("document", "")[:500]
            parts.append(f"\n{i}. {source}:\n{content}")
        return "\n".join(parts)
    
    def get_stats(self) -> Dict[str, Any]:
        stats = {
            "initialized": self._initialized,
            "config": {
                "collection": self.config.collection_name,
                "embedding": self.config.embedding_model,
                "chunk_size": self.config.chunk_size,
                "top_k": self.config.top_k,
            },
        }
        if self.vector_store:
            stats["vector_store"] = self.vector_store.get_stats()
        if self.embedding_model:
            stats["embedding_dim"] = self.embedding_model.embedding_dim
        return stats


def create_pipeline(config: Optional[RAGConfig] = None) -> RAGPipeline:
    pipeline = RAGPipeline(config)
    pipeline.initialize()
    return pipeline
```

---

## Search Optimization

Create `rag/retriever.py`:

```python
"""Advanced Retrieval with Re-ranking and Query Enhancement."""

import logging
from typing import List, Dict, Optional, Any
import numpy as np
from dataclasses import dataclass

logger = logging.getLogger(__name__)


@dataclass
class SearchResult:
    """Search result with scoring."""
    id: str
    document: str
    metadata: Dict[str, Any]
    vector_score: float
    rerank_score: Optional[float] = None
    final_score: Optional[float] = None


class QueryPreprocessor:
    """Preprocess queries for better retrieval."""
    
    EXPANSION_TERMS = {
        "api": ["endpoint", "rest", "graphql"],
        "db": ["database", "sql", "storage"],
        "auth": ["authentication", "authorization", "login"],
        "test": ["testing", "unit test", "integration"],
        "deploy": ["deployment", "cicd", "pipeline"],
    }
    
    ACRONYMS = {
        "api": "API", "db": "database", "ui": "user interface",
        "rest": "REST API", "crud": "create read update delete",
        "orm": "object relational mapping", "ci": "continuous integration",
    }
    
    def preprocess(self, query: str) -> str:
        query = self._expand_acronyms(query)
        query = self._expand_synonyms(query)
        return " ".join(query.split())
    
    def _expand_acronyms(self, query: str) -> str:
        words = query.lower().split()
        expanded = []
        for word in words:
            clean = word.strip(".,!?;:")
            expanded.append(self.ACRONYMS.get(clean, word))
        return " ".join(expanded)
    
    def _expand_synonyms(self, query: str) -> str:
        words = query.lower().split()
        expansions = []
        for word in words:
            expansions.append(word)
            if word in self.EXPANSION_TERMS:
                expansions.extend(self.EXPANSION_TERMS[word][:2])
        return " ".join(expansions)


class ReRanker:
    """Re-rank search results using heuristics or cross-encoder."""
    
    def __init__(self, method: str = "heuristic"):
        self.method = method
        self.cross_encoder = None
        
        if method == "cross-encoder":
            self._init_cross_encoder()
    
    def _init_cross_encoder(self):
        try:
            from sentence_transformers import CrossEncoder
            self.cross_encoder = CrossEncoder("cross-encoder/ms-marco-MiniLM-L-6-v2", max_length=512)
            logger.info("Cross-encoder initialized")
        except Exception as e:
            logger.warning(f"Cross-encoder failed: {e}")
            self.method = "heuristic"
    
    def rerank(self, query: str, results: List[SearchResult]) -> List[SearchResult]:
        if not results:
            return results
        
        if self.method == "cross-encoder" and self.cross_encoder:
            return self._rerank_cross_encoder(query, results)
        return self._rerank_heuristic(query, results)
    
    def _rerank_cross_encoder(self, query: str, results: List[SearchResult]) -> List[SearchResult]:
        pairs = [(query, r.document) for r in results]
        scores = self.cross_encoder.predict(pairs)
        
        for result, score in zip(results, scores):
            result.rerank_score = float(score)
            result.final_score = 0.6 * result.rerank_score + 0.4 * (1 - result.vector_score)
        
        results.sort(key=lambda x: x.final_score or 0, reverse=True)
        return results
    
    def _rerank_heuristic(self, query: str, results: List[SearchResult]) -> List[SearchResult]:
        query_terms = set(query.lower().split())
        
        for result in results:
            doc_lower = result.document.lower()
            metadata = result.metadata
            
            # Term overlap
            doc_terms = set(doc_lower.split())
            term_overlap = len(query_terms & doc_terms) / len(query_terms) if query_terms else 0
            
            # Position score
            position_score = sum(1 / (1 + doc_lower.find(term) / 100) for term in query_terms if doc_lower.find(term) >= 0)
            
            # Category boost
            category_boost = 0.1 if metadata.get("category") in query.lower() else 0
            
            # Code boost
            code_boost = 0.05 if "```" in result.document else 0
            
            result.rerank_score = 0.4 * term_overlap + 0.3 * (position_score / max(len(query_terms), 1)) + category_boost + code_boost
            result.final_score = 0.7 * (1 - result.vector_score) + 0.3 * result.rerank_score
        
        results.sort(key=lambda x: x.final_score or 0, reverse=True)
        return results


class HybridRetriever:
    """Hybrid retriever combining multiple search strategies."""
    
    def __init__(self, vector_store, embedding_model, top_k: int = 4, rerank_method: str = "heuristic"):
        self.vector_store = vector_store
        self.embedding_model = embedding_model
        self.top_k = top_k
        self.query_preprocessor = QueryPreprocessor()
        self.reranker = ReRanker(rerank_method)
    
    def search(
        self,
        query: str,
        filter_dict: Optional[Dict[str, Any]] = None,
        use_preprocessing: bool = True,
        use_reranking: bool = True,
    ) -> List[SearchResult]:
        processed_query = self.query_preprocessor.preprocess(query) if use_preprocessing else query
        query_embedding = self.embedding_model.embed_query(processed_query)
        
        retrieve_k = self.top_k * 3 if use_reranking else self.top_k
        
        raw_results = self.vector_store.search_with_embedding(
            query_embedding=query_embedding.tolist(),
            n_results=retrieve_k,
            filter_dict=filter_dict,
        )
        
        search_results = [
            SearchResult(
                id=r["id"],
                document=r["document"],
                metadata=r["metadata"],
                vector_score=r["distance"],
            )
            for r in raw_results
        ]
        
        if use_reranking:
            search_results = self.reranker.rerank(query, search_results)
        
        return search_results[:self.top_k]


def create_retriever(vector_store, embedding_model, top_k: int = 4, rerank_method: str = "heuristic") -> HybridRetriever:
    return HybridRetriever(vector_store, embedding_model, top_k, rerank_method)
```

---

## FastAPI Wrapper

Create `api/main.py`:

```python
"""FastAPI Application for RAG System."""

import os
import logging
from typing import List, Optional, Dict, Any
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, BackgroundTasks, UploadFile, File, Form
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
import uvicorn

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")
logger = logging.getLogger(__name__)

from rag.pipeline import RAGPipeline, RAGConfig

rag_pipeline: Optional[RAGPipeline] = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global rag_pipeline
    logger.info("Starting RAG API...")
    
    config = RAGConfig(
        collection_name=os.getenv("RAG_COLLECTION", "enterprise_kb"),
        persist_directory=os.getenv("RAG_PERSIST_DIR", "./vector_db"),
        embedding_model=os.getenv("RAG_EMBEDDING_MODEL", "mpnet"),
        chunk_size=int(os.getenv("RAG_CHUNK_SIZE", "1000")),
        chunk_overlap=int(os.getenv("RAG_CHUNK_OVERLAP", "200")),
        top_k=int(os.getenv("RAG_TOP_K", "4")),
        llm_model_path=os.getenv("LLM_MODEL_PATH", "./models/llama-3.2-3b-instruct-q4_k_m.gguf"),
    )
    
    rag_pipeline = RAGPipeline(config)
    rag_pipeline.initialize()
    logger.info("RAG pipeline ready")
    
    yield
    logger.info("Shutting down...")


app = FastAPI(
    title="RAG Knowledge Base API",
    description="Enterprise RAG system for software development",
    version="2.0.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


# Pydantic Models
class QueryRequest(BaseModel):
    question: str = Field(..., min_length=1)
    top_k: Optional[int] = Field(4, ge=1, le=20)
    category: Optional[str] = None
    use_llm: bool = True


class QueryResponse(BaseModel):
    answer: str
    sources: List[Dict[str, Any]]
    metadata: Dict[str, Any]


class IngestRequest(BaseModel):
    source_path: str
    recursive: bool = True


class IngestResponse(BaseModel):
    status: str
    documents_processed: int
    chunks_created: int
    message: str


class HealthResponse(BaseModel):
    status: str
    version: str
    pipeline_initialized: bool
    stats: Dict[str, Any]


# Endpoints
@app.get("/")
async def root():
    return {"message": "RAG Knowledge Base API", "version": "2.0.0", "docs": "/docs"}


@app.get("/health", response_model=HealthResponse)
async def health_check():
    stats = rag_pipeline.get_stats() if rag_pipeline else {}
    return HealthResponse(
        status="healthy",
        version="2.0.0",
        pipeline_initialized=rag_pipeline is not None and rag_pipeline._initialized,
        stats=stats,
    )


@app.post("/query", response_model=QueryResponse)
async def query(request: QueryRequest):
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    
    try:
        filter_dict = {"category": request.category} if request.category else None
        response = rag_pipeline.query(
            question=request.question,
            top_k=request.top_k,
            filter_dict=filter_dict,
            use_llm=request.use_llm,
        )
        return QueryResponse(
            answer=response.answer,
            sources=response.sources,
            metadata=response.metadata,
        )
    except Exception as e:
        logger.error(f"Query error: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/ingest", response_model=IngestResponse)
async def ingest_documents(request: IngestRequest):
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    
    try:
        result = rag_pipeline.ingest_documents(
            source=request.source_path,
            recursive=request.recursive,
        )
        return IngestResponse(
            status=result.get("status", "unknown"),
            documents_processed=result.get("documents_processed", 0),
            chunks_created=result.get("chunks_created", 0),
            message=f"Ingested {result.get('chunks_created', 0)} chunks",
        )
    except Exception as e:
        logger.error(f"Ingestion error: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/ingest/upload", response_model=IngestResponse)
async def ingest_upload(file: UploadFile = File(...), category: Optional[str] = Form(None)):
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    
    try:
        import tempfile
        import shutil
        
        temp_dir = tempfile.mkdtemp()
        temp_path = os.path.join(temp_dir, file.filename)
        
        with open(temp_path, "wb") as f:
            shutil.copyfileobj(file.file, f)
        
        result = rag_pipeline.ingest_documents(source=temp_path)
        shutil.rmtree(temp_dir)
        
        return IngestResponse(
            status=result.get("status", "unknown"),
            documents_processed=result.get("documents_processed", 0),
            chunks_created=result.get("chunks_created", 0),
            message=f"Uploaded {file.filename}",
        )
    except Exception as e:
        logger.error(f"Upload error: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/stats")
async def get_stats():
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    return rag_pipeline.get_stats()


@app.get("/categories")
async def get_categories():
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    
    all_docs = rag_pipeline.vector_store.get_all_documents(limit=10000)
    categories = sorted(set(doc.get("metadata", {}).get("category") for doc in all_docs if doc.get("metadata", {}).get("category")))
    return {"categories": categories}


@app.delete("/documents/{doc_id}")
async def delete_document(doc_id: str):
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    
    try:
        rag_pipeline.vector_store.delete_documents([doc_id])
        return {"status": "success", "message": f"Document {doc_id} deleted"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/reset")
async def reset_collection():
    if not rag_pipeline or not rag_pipeline._initialized:
        raise HTTPException(status_code=503, detail="Pipeline not initialized")
    
    try:
        rag_pipeline.vector_store.reset_collection()
        return {"status": "success", "message": "Collection reset"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


if __name__ == "__main__":
    port = int(os.getenv("RAG_API_PORT", "8000"))
    host = os.getenv("RAG_API_HOST", "0.0.0.0")
    uvicorn.run(app, host=host, port=port)
```

---

## Configuration Files

### .env Configuration

```bash
# RAG System Configuration

# Vector Database
RAG_COLLECTION=enterprise_kb
RAG_PERSIST_DIR=./vector_db
RAG_DISTANCE_METRIC=cosine

# Embedding Model (mini/mpnet/bge/uae)
RAG_EMBEDDING_MODEL=mpnet
RAG_EMBEDDING_DEVICE=cpu

# Document Processing
RAG_CHUNK_SIZE=1000
RAG_CHUNK_OVERLAP=200

# Retrieval
RAG_TOP_K=4
RAG_SIMILARITY_THRESHOLD=0.7
RAG_RERANK_METHOD=heuristic

# LLM (llama.cpp)
LLM_MODEL_PATH=./models/llama-3.2-3b-instruct-q4_k_m.gguf
LLM_N_CTX=8192
LLM_TEMPERATURE=0.7
LLM_MAX_TOKENS=2048

# API Server
RAG_API_HOST=0.0.0.0
RAG_API_PORT=8000

# Knowledge Base
KB_DOCS_PATH=./docs

# Logging
LOG_LEVEL=INFO
```

### requirements.txt

```txt
# Core
fastapi>=0.104.0
uvicorn[standard]>=0.24.0
pydantic>=2.5.0
python-dotenv>=1.0.0

# Vector DB
chromadb>=0.4.18

# Embeddings
sentence-transformers>=2.2.2
transformers>=4.36.0
torch>=2.1.0

# Documents
pypdf2>=3.0.0
pdfplumber>=0.10.0
chardet>=5.2.0

# LLM
llama-cpp-python>=0.2.0

# Utils
numpy>=1.24.0
tqdm>=4.66.0
aiofiles>=23.2.0
```

### config/settings.py

```python
"""Configuration Management."""

import os
from typing import Optional
from dataclasses import dataclass
from dotenv import load_dotenv

load_dotenv()


@dataclass
class Settings:
    RAG_COLLECTION: str = "enterprise_kb"
    RAG_PERSIST_DIR: str = "./vector_db"
    RAG_EMBEDDING_MODEL: str = "mpnet"
    RAG_CHUNK_SIZE: int = 1000
    RAG_CHUNK_OVERLAP: int = 200
    RAG_TOP_K: int = 4
    LLM_MODEL_PATH: str = "./models/llama-3.2-3b-instruct-q4_k_m.gguf"
    RAG_API_HOST: str = "0.0.0.0"
    RAG_API_PORT: int = 8000
    
    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            RAG_COLLECTION=os.getenv("RAG_COLLECTION", "enterprise_kb"),
            RAG_PERSIST_DIR=os.getenv("RAG_PERSIST_DIR", "./vector_db"),
            RAG_EMBEDDING_MODEL=os.getenv("RAG_EMBEDDING_MODEL", "mpnet"),
            RAG_CHUNK_SIZE=int(os.getenv("RAG_CHUNK_SIZE", "1000")),
            RAG_CHUNK_OVERLAP=int(os.getenv("RAG_CHUNK_OVERLAP", "200")),
            RAG_TOP_K=int(os.getenv("RAG_TOP_K", "4")),
            LLM_MODEL_PATH=os.getenv("LLM_MODEL_PATH", "./models/llama-3.2-3b-instruct-q4_k_m.gguf"),
            RAG_API_HOST=os.getenv("RAG_API_HOST", "0.0.0.0"),
            RAG_API_PORT=int(os.getenv("RAG_API_PORT", "8000")),
        )


settings = Settings.from_env()
```

---

## Utility Scripts

### scripts/ingest_documents.py

```python
#!/usr/bin/env python3
"""Document Ingestion Script."""

import os
import sys
import argparse
import logging
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

from rag.pipeline import RAGPipeline, RAGConfig
from config.settings import settings

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def main():
    parser = argparse.ArgumentParser(description="Ingest documents into RAG KB")
    parser.add_argument("source", help="Path to file or directory")
    parser.add_argument("-r", "--recursive", action="store_true", help="Process recursively")
    parser.add_argument("--reset", action="store_true", help="Reset collection first")
    
    args = parser.parse_args()
    
    config = RAGConfig(
        collection_name=settings.RAG_COLLECTION,
        persist_directory=settings.RAG_PERSIST_DIR,
        embedding_model=settings.RAG_EMBEDDING_MODEL,
        chunk_size=settings.RAG_CHUNK_SIZE,
        chunk_overlap=settings.RAG_CHUNK_OVERLAP,
    )
    
    pipeline = RAGPipeline(config)
    pipeline.initialize()
    
    if args.reset:
        logger.warning("Resetting collection...")
        pipeline.vector_store.reset_collection()
    
    logger.info(f"Ingesting: {args.source}")
    result = pipeline.ingest_documents(source=args.source, recursive=args.recursive)
    
    logger.info(f"Complete: {result['documents_processed']} docs, {result['chunks_created']} chunks")


if __name__ == "__main__":
    main()
```

### scripts/query.py

```python
#!/usr/bin/env python3
"""CLI Query Tool."""

import sys
import argparse
import logging
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

from rag.pipeline import RAGPipeline, RAGConfig
from config.settings import settings

logging.basicConfig(level=logging.WARNING)


def main():
    parser = argparse.ArgumentParser(description="Query RAG KB")
    parser.add_argument("question", help="Question to ask")
    parser.add_argument("-k", "--top-k", type=int, default=4)
    parser.add_argument("-c", "--category", help="Filter by category")
    parser.add_argument("--no-llm", action="store_true", help="Show raw results")
    
    args = parser.parse_args()
    
    config = RAGConfig(
        collection_name=settings.RAG_COLLECTION,
        persist_directory=settings.RAG_PERSIST_DIR,
        embedding_model=settings.RAG_EMBEDDING_MODEL,
    )
    
    pipeline = RAGPipeline(config)
    pipeline.initialize()
    
    filter_dict = {"category": args.category} if args.category else None
    
    response = pipeline.query(
        question=args.question,
        top_k=args.top_k,
        filter_dict=filter_dict,
        use_llm=not args.no_llm,
    )
    
    print("\n" + "="*60)
    print("ANSWER:")
    print("="*60)
    print(response.answer)
    
    print("\n" + "="*60)
    print("SOURCES:")
    print("="*60)
    for i, source in enumerate(response.sources, 1):
        print(f"\n[{i}] {source['source']}")
        print(f"    Category: {source['category']}")
        print(f"    Relevance: {1 - source['distance']:.2%}")


if __name__ == "__main__":
    main()
```

### setup.sh

```bash
#!/bin/bash
# RAG System Setup

echo "============================================"
echo "RAG Knowledge Base System - Setup"
echo "============================================"

# Virtual environment
echo "Creating venv..."
python3 -m venv venv
source venv/bin/activate

# Install dependencies
echo "Installing dependencies..."
pip install --upgrade pip
pip install -r requirements.txt

# Directory structure
echo "Creating directories..."
mkdir -p docs vector_db models logs uploads

# Setup KB
echo "Setting up knowledge base..."
python scripts/setup_kb.py

# Create .env if missing
if [ ! -f .env ]; then
    echo "Creating .env..."
    cat > .env << 'EOF'
RAG_COLLECTION=enterprise_kb
RAG_PERSIST_DIR=./vector_db
RAG_EMBEDDING_MODEL=mpnet
RAG_CHUNK_SIZE=1000
RAG_CHUNK_OVERLAP=200
RAG_TOP_K=4
LLM_MODEL_PATH=./models/llama-3.2-3b-instruct-q4_k_m.gguf
RAG_API_PORT=8000
EOF
fi

echo ""
echo "Setup complete!"
echo "1. Edit .env with your settings"
echo "2. Download LLM to ./models/"
echo "3. Add docs to ./docs/"
echo "4. Run: python scripts/ingest_documents.py ./docs -r"
echo "5. Start API: python api/main.py"
```

---

## Quick Start Guide

### 1. Installation

```bash
mkdir rag_system && cd rag_system
# Save all files above
chmod +x setup.sh
./setup.sh
```

### 2. Download LLM Model

```bash
mkdir -p models
cd models
# Download Llama 3.2 3B Q4_K_M (recommended)
wget https://huggingface.co/bartowski/Llama-3.2-3B-Instruct-GGUF/resolve/main/Llama-3.2-3B-Instruct-Q4_K_M.gguf
cd ..
```

### 3. Ingest Documents

```bash
# Setup KB structure
python scripts/setup_kb.py

# Add your documents to docs/ folder, then:
python scripts/ingest_documents.py ./docs --recursive
```

### 4. Start API Server

```bash
python api/main.py
# Or: uvicorn api.main:app --host 0.0.0.0 --port 8000 --reload
```

### 5. Query Examples

**Via API:**
```bash
# Health check
curl http://localhost:8000/health

# Query
curl -X POST http://localhost:8000/query \
  -H "Content-Type: application/json" \
  -d '{"question": "Spring Boot best practices?", "top_k": 4, "category": "backend"}'

# Ingest
curl -X POST http://localhost:8000/ingest \
  -H "Content-Type: application/json" \
  -d '{"source_path": "./docs", "recursive": true}'
```

**Via CLI:**
```bash
python scripts/query.py "How to implement authentication?"
python scripts/query.py "Docker best practices" -c devops -k 6
```

---

## Performance Optimization

### Embedding Model Selection

| Model | Dim | Size | RAM | Use Case |
|-------|-----|------|-----|----------|
| all-MiniLM-L6-v2 | 384 | 80MB | ~200MB | Quick prototyping |
| all-mpnet-base-v2 | 768 | 420MB | ~600MB | Balanced (recommended) |
| BAAI/bge-large-en | 1024 | 1.3GB | ~1.5GB | Best quality |
| UAE-Large-V1 | 1024 | 1.3GB | ~1.5GB | Code-optimized |

### Chunk Size Guidelines

| Document Type | Chunk Size | Overlap |
|--------------|-----------|---------|
| Code files | 800-1200 | 150 |
| Markdown | 1000-1500 | 200 |
| PDF/Text | 1000-2000 | 200 |

### HNSW Index Tuning

```python
metadata = {
    "hnsw:space": "cosine",
    "hnsw:construction_ef": 128,  # Build accuracy
    "hnsw:search_ef": 128,        # Query accuracy
    "hnsw:M": 16,                 # Connections per node
}
```

---

## API Endpoints Reference

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | API info |
| `/health` | GET | Health check |
| `/query` | POST | Query knowledge base |
| `/ingest` | POST | Ingest documents |
| `/ingest/upload` | POST | Upload & ingest file |
| `/stats` | GET | KB statistics |
| `/categories` | GET | List categories |
| `/documents/{id}` | DELETE | Delete document |
| `/reset` | POST | Reset collection |

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Out of Memory | Use `mini` embedding model |
| Slow search | Reduce `search_ef` or `top_k` |
| Poor results | Adjust chunk size/overlap |
| LLM not responding | Check model path |

---

## Summary

This RAG system provides:

- **Flexible embeddings** (sentence-transformers + Gemma support)
- **Persistent ChromaDB** storage
- **Multi-format processing** (PDF, Markdown, 30+ code types)
- **Enterprise KB structure** (9 categories)
- **Advanced retrieval** with re-ranking
- **FastAPI REST API**
- **CLI tools** for ingestion/querying

Optimized for Intel i7 11th Gen, 64GB RAM.
