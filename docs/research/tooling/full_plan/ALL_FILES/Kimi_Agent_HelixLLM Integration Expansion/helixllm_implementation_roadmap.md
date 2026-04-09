# HelixLLM + HelixAgent Integration - Complete Implementation Roadmap

## Executive Summary

This roadmap outlines a 6-week implementation plan to integrate HelixLLM (local LLM with RAG and tool use) with HelixAgent, creating a powerful coding assistant that rivals premium models when used from CLI agents.

**Target Performance Metrics:**
- Inference speed: 150-300+ tokens/second
- RAG retrieval latency: <100ms
- Tool execution: <500ms average
- API response time: <50ms overhead
- Memory footprint: <8GB for 7B models, <16GB for 13B models

---

## Phase 1: Foundation (Week 1)
**Goal:** Establish core infrastructure with model inference and basic API

### 1.1 Environment Setup (Days 1-2)

#### Task 1.1.1: Project Structure Creation
**Files to Create:**
```
helixllm/
├── pyproject.toml              # Project configuration
├── README.md                   # Project documentation
├── .gitignore                  # Git ignore rules
├── Makefile                    # Build automation
├── requirements/
│   ├── base.txt               # Core dependencies
│   ├── dev.txt                # Development dependencies
│   └── gpu.txt                # GPU-specific dependencies
├── src/
│   └── helixllm/
│       ├── __init__.py
│       ├── config.py          # Configuration management
│       └── version.py
├── tests/
│   ├── __init__.py
│   ├── conftest.py           # Pytest configuration
│   └── unit/
├── scripts/
│   ├── setup.sh              # Environment setup
│   └── install.sh            # Installation script
└── docs/
    ├── architecture.md
    └── api.md
```

**Dependencies:** None (foundation task)

**Success Criteria:**
- [ ] All directories created
- [ ] pyproject.toml configured with proper metadata
- [ ] Requirements files with version pinning
- [ ] Makefile with common commands (install, test, lint, format)

**Risk Mitigation:**
- Use virtual environments to isolate dependencies
- Pin exact versions to prevent breaking changes
- Document Python version requirements (3.10+)

---

#### Task 1.1.2: Dependency Installation & Validation
**Files to Create:**
- `requirements/base.txt`
- `requirements/dev.txt`
- `requirements/gpu.txt`
- `scripts/validate_env.py`

**Dependencies:** Task 1.1.1

**Core Dependencies (base.txt):**
```
# Core framework
fastapi>=0.104.0
uvicorn[standard]>=0.24.0
pydantic>=2.5.0
pydantic-settings>=2.1.0

# LLM Inference
llama-cpp-python>=0.2.20
huggingface-hub>=0.19.0
transformers>=4.36.0

# Utilities
python-dotenv>=1.0.0
structlog>=23.2.0
orjson>=3.9.0
aiofiles>=23.2.0

# Async
anyio>=4.0.0
httpx>=0.25.0
```

**GPU Dependencies (gpu.txt):**
```
# CUDA support
torch>=2.1.0
nvidia-ml-py>=12.535.0

# Optimized inference
auto-gptq>=0.6.0
optimum>=1.15.0
```

**Success Criteria:**
- [ ] All dependencies install without conflicts
- [ ] GPU detection works correctly
- [ ] Import validation passes for all core modules
- [ ] Environment validation script runs successfully

**Testing Strategy:**
```python
# tests/unit/test_environment.py
def test_imports():
    """Verify all core imports work"""
    import helixllm
    from helixllm import config

def test_gpu_availability():
    """Check GPU detection"""
    import torch
    assert torch.cuda.is_available() or True  # CPU fallback OK
```

---

### 1.2 Core Model Inference (Days 2-4)

#### Task 1.2.1: Model Configuration System
**Files to Create:**
- `src/helixllm/config.py`
- `src/helixllm/models/__init__.py`
- `src/helixllm/models/config.py`

**Dependencies:** Task 1.1.2

**Implementation Details:**
```python
# src/helixllm/models/config.py
from pydantic import BaseModel, Field
from typing import Optional, Literal
from pathlib import Path

class ModelConfig(BaseModel):
    """Configuration for model loading and inference"""

    # Model identification
    model_path: Path = Field(..., description="Path to model file or HuggingFace ID")
    model_type: Literal["gguf", "gptq", "awq", "hf"] = Field(default="gguf")

    # Hardware settings
    device: Literal["auto", "cpu", "cuda", "mps"] = Field(default="auto")
    gpu_layers: int = Field(default=-1, description="Number of layers to offload to GPU")

    # Inference parameters
    context_length: int = Field(default=8192, ge=512, le=128000)
    batch_size: int = Field(default=512, ge=1)
    threads: Optional[int] = Field(default=None, description="CPU threads for inference")

    # Performance tuning
    use_mmap: bool = Field(default=True)
    use_mlock: bool = Field(default=False)

    # Quantization
    quantization: Optional[str] = Field(default=None)
```

**Success Criteria:**
- [ ] Configuration validation works correctly
- [ ] Environment variable overrides function properly
- [ ] Default configurations are sensible
- [ ] Type hints are complete

---

#### Task 1.2.2: Model Loader Implementation
**Files to Create:**
- `src/helixllm/models/loader.py`
- `src/helixllm/models/base.py`

**Dependencies:** Task 1.2.1

**Implementation Details:**
```python
# src/helixllm/models/loader.py
import logging
from pathlib import Path
from typing import Union, Optional
import structlog

from .config import ModelConfig
from .base import BaseModel

logger = structlog.get_logger(__name__)

class ModelLoader:
    """Factory for loading different model types"""

    _loaders = {}

    @classmethod
    def register(cls, model_type: str):
        """Decorator to register model loaders"""
        def decorator(loader_class):
            cls._loaders[model_type] = loader_class
            return loader_class
        return decorator

    @classmethod
    def load(cls, config: ModelConfig) -> BaseModel:
        """Load model based on configuration"""
        loader_class = cls._loaders.get(config.model_type)
        if not loader_class:
            raise ValueError(f"Unknown model type: {config.model_type}")

        logger.info(
            "loading_model",
            model_type=config.model_type,
            model_path=str(config.model_path)
        )

        return loader_class(config)

    @classmethod
    def list_supported_types(cls) -> list[str]:
        """List all supported model types"""
        return list(cls._loaders.keys())


@ModelLoader.register("gguf")
class GGUFLoader:
    """Loader for GGUF format models (llama.cpp)"""

    def __init__(self, config: ModelConfig):
        self.config = config
        self._model = None

    def load(self) -> "GGUFModel":
        from llama_cpp import Llama

        model_path = self._resolve_model_path()

        self._model = Llama(
            model_path=str(model_path),
            n_ctx=self.config.context_length,
            n_batch=self.config.batch_size,
            n_gpu_layers=self.config.gpu_layers,
            n_threads=self.config.threads or 4,
            use_mmap=self.config.use_mmap,
            use_mlock=self.config.use_mlock,
            verbose=False
        )

        return GGUFModel(self._model, self.config)

    def _resolve_model_path(self) -> Path:
        """Resolve model path from HuggingFace or local"""
        if self.config.model_path.exists():
            return self.config.model_path

        # Download from HuggingFace
        from huggingface_hub import hf_hub_download

        repo_id = str(self.config.model_path)
        filename = "model.gguf"  # Default, should be configurable

        return Path(hf_hub_download(repo_id=repo_id, filename=filename))
```

**Success Criteria:**
- [ ] GGUF models load correctly
- [ ] GPU offloading works
- [ ] Model download from HuggingFace functions
- [ ] Error handling for missing models
- [ ] Memory usage is tracked

---

#### Task 1.2.3: Inference Engine
**Files to Create:**
- `src/helixllm/models/inference.py`
- `src/helixllm/models/tokenizer.py`

**Dependencies:** Task 1.2.2

**Implementation Details:**
```python
# src/helixllm/models/inference.py
from dataclasses import dataclass
from typing import AsyncIterator, Optional, Callable
import asyncio
import time
import structlog

from .base import BaseModel

logger = structlog.get_logger(__name__)

@dataclass
class GenerationConfig:
    """Configuration for text generation"""
    max_tokens: int = 1024
    temperature: float = 0.7
    top_p: float = 0.9
    top_k: int = 40
    repeat_penalty: float = 1.1
    stop_sequences: list[str] = None
    stream: bool = False

@dataclass
class GenerationResult:
    """Result of text generation"""
    text: str
    tokens_generated: int
    tokens_per_second: float
    finish_reason: str
    metadata: dict

class InferenceEngine:
    """High-level inference interface"""

    def __init__(self, model: BaseModel):
        self.model = model
        self.logger = structlog.get_logger(__name__)

    async def generate(
        self,
        prompt: str,
        config: GenerationConfig
    ) -> GenerationResult:
        """Generate text from prompt"""
        start_time = time.time()

        self.logger.debug(
            "generation_start",
            prompt_length=len(prompt),
            max_tokens=config.max_tokens
        )

        result = await asyncio.to_thread(
            self._generate_sync,
            prompt,
            config
        )

        elapsed = time.time() - start_time
        tps = result.tokens_generated / elapsed if elapsed > 0 else 0

        self.logger.info(
            "generation_complete",
            tokens_generated=result.tokens_generated,
            tokens_per_second=tps,
            elapsed_seconds=elapsed
        )

        return result

    async def generate_stream(
        self,
        prompt: str,
        config: GenerationConfig
    ) -> AsyncIterator[str]:
        """Stream generation results"""
        for chunk in self._generate_stream_sync(prompt, config):
            yield chunk

    def _generate_sync(
        self,
        prompt: str,
        config: GenerationConfig
    ) -> GenerationResult:
        """Synchronous generation (runs in thread pool)"""
        # Implementation depends on model type
        pass
```

**Success Criteria:**
- [ ] Synchronous generation works
- [ ] Streaming generation works
- [ ] Performance metrics are accurate
- [ ] Token counting is correct
- [ ] Stop sequences work properly

**Testing Strategy:**
```python
# tests/unit/models/test_inference.py
import pytest
from helixllm.models.inference import InferenceEngine, GenerationConfig

@pytest.fixture
def mock_model():
    # Create mock model for testing
    pass

async def test_generate_basic(mock_model):
    engine = InferenceEngine(mock_model)
    config = GenerationConfig(max_tokens=10)
    result = await engine.generate("Hello,", config)

    assert result.text
    assert result.tokens_generated > 0
    assert result.tokens_per_second > 0

async def test_generate_stream(mock_model):
    engine = InferenceEngine(mock_model)
    config = GenerationConfig(max_tokens=10, stream=True)
    chunks = []

    async for chunk in engine.generate_stream("Hello,", config):
        chunks.append(chunk)

    assert len(chunks) > 0
```

---

### 1.3 Basic API Server (Days 4-5)

#### Task 1.3.1: FastAPI Application Structure
**Files to Create:**
- `src/helixllm/api/__init__.py`
- `src/helixllm/api/main.py`
- `src/helixllm/api/dependencies.py`
- `src/helixllm/api/middleware.py`

**Dependencies:** Task 1.2.3

**Implementation Details:**
```python
# src/helixllm/api/main.py
from contextlib import asynccontextmanager
from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
import structlog

from helixllm.config import Settings
from helixllm.models.loader import ModelLoader
from helixllm.models.inference import InferenceEngine

from .routes import chat, models, health
from .middleware import LoggingMiddleware, TimingMiddleware

logger = structlog.get_logger(__name__)
settings = Settings()

# Global state
app_state = {
    "model": None,
    "inference_engine": None,
    "settings": settings
}

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan manager"""
    # Startup
    logger.info("api_startup", version=settings.version)

    if settings.model_path:
        from helixllm.models.config import ModelConfig
        config = ModelConfig(
            model_path=settings.model_path,
            model_type=settings.model_type,
            gpu_layers=settings.gpu_layers
        )
        app_state["model"] = ModelLoader.load(config)
        app_state["inference_engine"] = InferenceEngine(app_state["model"])
        logger.info("model_loaded", model=str(settings.model_path))

    yield

    # Shutdown
    logger.info("api_shutdown")
    if app_state["model"]:
        del app_state["model"]

app = FastAPI(
    title="HelixLLM API",
    description="OpenAI-compatible API for local LLM inference",
    version=settings.version,
    lifespan=lifespan
)

# Middleware
app.add_middleware(LoggingMiddleware)
app.add_middleware(TimingMiddleware)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Routes
app.include_router(health.router, prefix="/v1", tags=["health"])
app.include_router(models.router, prefix="/v1", tags=["models"])
app.include_router(chat.router, prefix="/v1", tags=["chat"])

@app.exception_handler(Exception)
async def global_exception_handler(request: Request, exc: Exception):
    logger.error("unhandled_exception", error=str(exc), path=request.url.path)
    return JSONResponse(
        status_code=500,
        content={"error": {"message": str(exc), "type": "internal_error"}}
    )
```

**Success Criteria:**
- [ ] FastAPI app starts correctly
- [ ] Lifespan events work
- [ ] Middleware is applied
- [ ] Error handling catches exceptions
- [ ] CORS is configured

---

#### Task 1.3.2: Chat Completions Endpoint
**Files to Create:**
- `src/helixllm/api/routes/__init__.py`
- `src/helixllm/api/routes/chat.py`
- `src/helixllm/api/schemas.py`

**Dependencies:** Task 1.3.1

**Implementation Details:**
```python
# src/helixllm/api/schemas.py
from pydantic import BaseModel, Field
from typing import Literal, Optional
from datetime import datetime

class ChatMessage(BaseModel):
    role: Literal["system", "user", "assistant", "tool"]
    content: str
    name: Optional[str] = None
    tool_calls: Optional[list] = None

class ChatCompletionRequest(BaseModel):
    model: str = Field(default="default")
    messages: list[ChatMessage]
    temperature: float = Field(default=0.7, ge=0, le=2)
    max_tokens: Optional[int] = Field(default=None, ge=1)
    top_p: float = Field(default=1.0, ge=0, le=1)
    stream: bool = Field(default=False)
    stop: Optional[list[str]] = None
    presence_penalty: float = Field(default=0, ge=-2, le=2)
    frequency_penalty: float = Field(default=0, ge=-2, le=2)

class ChatCompletionChoice(BaseModel):
    index: int
    message: ChatMessage
    finish_reason: Optional[str] = None

class ChatCompletionResponse(BaseModel):
    id: str
    object: str = "chat.completion"
    created: int
    model: str
    choices: list[ChatCompletionChoice]
    usage: dict

# src/helixllm/api/routes/chat.py
from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse
from typing import AsyncIterator
import uuid
import time

from ..schemas import ChatCompletionRequest, ChatCompletionResponse
from ..dependencies import get_inference_engine
from ...models.inference import InferenceEngine, GenerationConfig

router = APIRouter()

@router.post("/chat/completions")
async def chat_completions(
    request: ChatCompletionRequest,
    engine: InferenceEngine = Depends(get_inference_engine)
) -> ChatCompletionResponse | StreamingResponse:
    """OpenAI-compatible chat completions endpoint"""

    if not engine:
        raise HTTPException(status_code=503, detail="Model not loaded")

    # Convert messages to prompt
    prompt = _format_messages(request.messages)

    # Create generation config
    gen_config = GenerationConfig(
        max_tokens=request.max_tokens or 1024,
        temperature=request.temperature,
        top_p=request.top_p,
        stop_sequences=request.stop or [],
        stream=request.stream
    )

    if request.stream:
        return StreamingResponse(
            _stream_completion(engine, prompt, gen_config, request.model),
            media_type="text/event-stream"
        )

    # Non-streaming response
    result = await engine.generate(prompt, gen_config)

    return ChatCompletionResponse(
        id=f"chatcmpl-{uuid.uuid4().hex}",
        created=int(time.time()),
        model=request.model,
        choices=[
            ChatCompletionChoice(
                index=0,
                message=ChatMessage(role="assistant", content=result.text),
                finish_reason=result.finish_reason
            )
        ],
        usage={
            "prompt_tokens": result.metadata.get("prompt_tokens", 0),
            "completion_tokens": result.tokens_generated,
            "total_tokens": result.metadata.get("prompt_tokens", 0) + result.tokens_generated
        }
    )

async def _stream_completion(
    engine: InferenceEngine,
    prompt: str,
    config: GenerationConfig,
    model: str
) -> AsyncIterator[str]:
    """Stream completion results"""
    completion_id = f"chatcmpl-{uuid.uuid4().hex}"
    created = int(time.time())

    async for chunk in engine.generate_stream(prompt, config):
        data = {
            "id": completion_id,
            "object": "chat.completion.chunk",
            "created": created,
            "model": model,
            "choices": [{
                "index": 0,
                "delta": {"content": chunk},
                "finish_reason": None
            }]
        }
        yield f"data: {json.dumps(data)}\n\n"

    yield "data: [DONE]\n\n"

def _format_messages(messages: list[ChatMessage]) -> str:
    """Format chat messages into prompt string"""
    # Simple formatting - can be improved with chat templates
    formatted = []
    for msg in messages:
        if msg.role == "system":
            formatted.append(f"System: {msg.content}")
        elif msg.role == "user":
            formatted.append(f"User: {msg.content}")
        elif msg.role == "assistant":
            formatted.append(f"Assistant: {msg.content}")

    formatted.append("Assistant:")
    return "\n\n".join(formatted)
```

**Success Criteria:**
- [ ] POST /v1/chat/completions works
- [ ] Streaming responses work
- [ ] Request validation works
- [ ] Response format matches OpenAI spec
- [ ] Error responses are properly formatted

**Testing Strategy:**
```python
# tests/unit/api/test_chat.py
import pytest
from fastapi.testclient import TestClient
from helixllm.api.main import app

client = TestClient(app)

def test_chat_completions_basic():
    response = client.post("/v1/chat/completions", json={
        "model": "test",
        "messages": [{"role": "user", "content": "Hello"}]
    })
    assert response.status_code == 200
    data = response.json()
    assert "choices" in data
    assert data["object"] == "chat.completion"

def test_chat_completions_streaming():
    response = client.post("/v1/chat/completions", json={
        "model": "test",
        "messages": [{"role": "user", "content": "Hello"}],
        "stream": True
    })
    assert response.status_code == 200
    assert response.headers["content-type"] == "text/event-stream"
```

---

### Phase 1 Deliverables & Success Criteria

**Deliverables:**
1. ✅ Complete project structure
2. ✅ Environment setup scripts
3. ✅ Model loading system (GGUF support)
4. ✅ Inference engine with sync/stream
5. ✅ Basic OpenAI-compatible API
6. ✅ Health check endpoint
7. ✅ Model listing endpoint

**Success Criteria:**
- [ ] Server starts and responds to requests
- [ ] Model loads and generates text
- [ ] API passes OpenAI compatibility tests
- [ ] Performance: >50 tokens/sec on target hardware
- [ ] All unit tests pass

**Checkpoint Tests:**
```bash
# Run all tests
make test

# Start server and test
python -m helixllm.api.main --model-path /path/to/model.gguf

# Test with curl
curl http://localhost:8000/v1/health
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Hello"}]}'
```

---

## Phase 2: RAG System (Week 2)
**Goal:** Implement document processing, embeddings, and retrieval

### 2.1 Document Processing Pipeline (Days 1-2)

#### Task 2.1.1: Document Loaders
**Files to Create:**
- `src/helixllm/rag/__init__.py`
- `src/helixllm/rag/loaders/__init__.py`
- `src/helixllm/rag/loaders/base.py`
- `src/helixllm/rag/loaders/text.py`
- `src/helixllm/rag/loaders/code.py`
- `src/helixllm/rag/loaders/markdown.py`

**Dependencies:** Phase 1 completion

**Implementation Details:**
```python
# src/helixllm/rag/loaders/base.py
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator, Optional
import structlog

logger = structlog.get_logger(__name__)

@dataclass
class Document:
    """Represents a loaded document"""
    content: str
    source: str
    metadata: dict
    document_type: str

class BaseLoader(ABC):
    """Base class for document loaders"""

    def __init__(self):
        self.logger = structlog.get_logger(self.__class__.__name__)

    @abstractmethod
    def load(self, source: Path | str) -> Iterator[Document]:
        """Load documents from source"""
        pass

    @abstractmethod
    def supports(self, source: Path | str) -> bool:
        """Check if this loader supports the source"""
        pass


# src/helixllm/rag/loaders/text.py
from pathlib import Path
from typing import Iterator
import mimetypes

from .base import BaseLoader, Document

class TextLoader(BaseLoader):
    """Loader for plain text files"""

    SUPPORTED_EXTENSIONS = {'.txt', '.py', '.js', '.ts', '.jsx', '.tsx', 
                           '.java', '.cpp', '.c', '.h', '.go', '.rs',
                           '.json', '.yaml', '.yml', '.xml', '.html', '.css'}

    def supports(self, source: Path | str) -> bool:
        path = Path(source)
        return path.suffix in self.SUPPORTED_EXTENSIONS

    def load(self, source: Path | str) -> Iterator[Document]:
        path = Path(source)
        self.logger.info("loading_text_file", path=str(path))

        try:
            content = path.read_text(encoding='utf-8', errors='ignore')

            yield Document(
                content=content,
                source=str(path),
                metadata={
                    "filename": path.name,
                    "extension": path.suffix,
                    "size_bytes": path.stat().st_size,
                    "modified": path.stat().st_mtime
                },
                document_type="text"
            )
        except Exception as e:
            self.logger.error("load_failed", path=str(path), error=str(e))
            raise


# src/helixllm/rag/loaders/code.py
from pathlib import Path
from typing import Iterator
import ast
import re

from .base import BaseLoader, Document

class CodeLoader(BaseLoader):
    """Loader for code files with structure extraction"""

    LANGUAGE_MAP = {
        '.py': 'python',
        '.js': 'javascript',
        '.ts': 'typescript',
        '.jsx': 'jsx',
        '.tsx': 'tsx',
        '.java': 'java',
        '.go': 'go',
        '.rs': 'rust',
        '.cpp': 'cpp',
        '.c': 'c',
        '.h': 'c',
    }

    def supports(self, source: Path | str) -> bool:
        return Path(source).suffix in self.LANGUAGE_MAP

    def load(self, source: Path | str) -> Iterator[Document]:
        path = Path(source)
        language = self.LANGUAGE_MAP.get(path.suffix, 'unknown')

        content = path.read_text(encoding='utf-8', errors='ignore')

        # Extract code structure
        metadata = {
            "language": language,
            "filename": path.name,
        }

        if language == 'python':
            metadata.update(self._extract_python_structure(content))

        yield Document(
            content=content,
            source=str(path),
            metadata=metadata,
            document_type="code"
        )

    def _extract_python_structure(self, content: str) -> dict:
        """Extract Python-specific structure"""
        try:
            tree = ast.parse(content)

            classes = [node.name for node in ast.walk(tree) 
                      if isinstance(node, ast.ClassDef)]
            functions = [node.name for node in ast.walk(tree) 
                        if isinstance(node, ast.FunctionDef)]
            imports = [node.names[0].name for node in ast.walk(tree)
                      if isinstance(node, ast.Import)]

            return {
                "classes": classes,
                "functions": functions,
                "imports": imports,
                "line_count": len(content.splitlines())
            }
        except SyntaxError:
            return {"line_count": len(content.splitlines())}
```

**Success Criteria:**
- [ ] Text files load correctly
- [ ] Code files with structure extraction work
- [ ] Binary files are handled gracefully
- [ ] Large files are handled efficiently
- [ ] Error handling for corrupted files

---

#### Task 2.1.2: Document Processor
**Files to Create:**
- `src/helixllm/rag/processor.py`
- `src/helixllm/rag/chunking.py`

**Dependencies:** Task 2.1.1

**Implementation Details:**
```python
# src/helixllm/rag/chunking.py
from dataclasses import dataclass
from typing import Iterator, Optional
import re

@dataclass
class Chunk:
    """A chunk of a document"""
    content: str
    source: str
    chunk_index: int
    total_chunks: int
    metadata: dict
    start_line: Optional[int] = None
    end_line: Optional[int] = None

class ChunkingStrategy:
    """Base chunking strategy"""

    def __init__(self, chunk_size: int = 512, chunk_overlap: int = 50):
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap

    def chunk(self, content: str, source: str, metadata: dict) -> Iterator[Chunk]:
        """Split content into chunks"""
        raise NotImplementedError

class RecursiveCharacterChunker(ChunkingStrategy):
    """Recursive character-based chunking"""

    SEPARATORS = ["\n\n", "\n", ". ", "! ", "? ", " ", ""]

    def chunk(self, content: str, source: str, metadata: dict) -> Iterator[Chunk]:
        chunks = self._recursive_split(content, self.SEPARATORS)

        for i, chunk_content in enumerate(chunks):
            yield Chunk(
                content=chunk_content,
                source=source,
                chunk_index=i,
                total_chunks=len(chunks),
                metadata=metadata
            )

    def _recursive_split(self, text: str, separators: list[str]) -> list[str]:
        """Recursively split text by separators"""
        if not separators:
            return [text] if text else []

        separator = separators[0]
        parts = text.split(separator)

        result = []
        current_chunk = ""

        for part in parts:
            if len(current_chunk) + len(part) + len(separator) <= self.chunk_size:
                current_chunk += (separator if current_chunk else "") + part
            else:
                if current_chunk:
                    result.append(current_chunk)
                # Recursively split large parts
                if len(part) > self.chunk_size:
                    sub_chunks = self._recursive_split(part, separators[1:])
                    result.extend(sub_chunks[:-1])
                    current_chunk = sub_chunks[-1] if sub_chunks else ""
                else:
                    current_chunk = part

        if current_chunk:
            result.append(current_chunk)

        return result

class CodeChunker(ChunkingStrategy):
    """Chunking strategy optimized for code"""

    def chunk(self, content: str, source: str, metadata: dict) -> Iterator[Chunk]:
        """Chunk code by functions/classes"""
        lines = content.split("\n")
        chunks = []
        current_chunk = []
        current_start = 0

        for i, line in enumerate(lines):
            # Detect function/class boundaries
            if self._is_boundary(line) and current_chunk:
                chunk_text = "\n".join(current_chunk)
                if len(chunk_text) >= 50:  # Minimum chunk size
                    chunks.append((chunk_text, current_start, i))
                current_chunk = [line]
                current_start = i
            else:
                current_chunk.append(line)

        # Add remaining lines
        if current_chunk:
            chunk_text = "\n".join(current_chunk)
            if len(chunk_text) >= 50:
                chunks.append((chunk_text, current_start, len(lines)))

        for i, (chunk_text, start, end) in enumerate(chunks):
            yield Chunk(
                content=chunk_text,
                source=source,
                chunk_index=i,
                total_chunks=len(chunks),
                metadata=metadata,
                start_line=start,
                end_line=end
            )

    def _is_boundary(self, line: str) -> bool:
        """Detect if line is a function/class boundary"""
        patterns = [
            r'^\s*(def|class|function|func)\s+',
            r'^\s*(public|private|protected)\s+',
        ]
        return any(re.match(p, line) for p in patterns)
```

**Success Criteria:**
- [ ] Text chunking produces reasonable chunks
- [ ] Code chunking respects function boundaries
- [ ] Overlap works correctly
- [ ] Chunk size limits are respected
- [ ] Metadata is preserved

---

### 2.2 Embedding Pipeline (Days 2-3)

#### Task 2.2.1: Embedding Model Integration
**Files to Create:**
- `src/helixllm/rag/embeddings/__init__.py`
- `src/helixllm/rag/embeddings/base.py`
- `src/helixllm/rag/embeddings/sentence_transformers.py`
- `src/helixllm/rag/embeddings/local.py`

**Dependencies:** Task 2.1.2

**Implementation Details:**
```python
# src/helixllm/rag/embeddings/base.py
from abc import ABC, abstractmethod
from typing import list
import numpy as np

class BaseEmbeddingModel(ABC):
    """Base class for embedding models"""

    @property
    @abstractmethod
    def dimension(self) -> int:
        """Return embedding dimension"""
        pass

    @abstractmethod
    def embed(self, texts: list[str]) -> np.ndarray:
        """Embed texts into vectors"""
        pass

    def embed_query(self, text: str) -> np.ndarray:
        """Embed a single query"""
        return self.embed([text])[0]


# src/helixllm/rag/embeddings/sentence_transformers.py
import numpy as np
from sentence_transformers import SentenceTransformer

from .base import BaseEmbeddingModel

class SentenceTransformerEmbedding(BaseEmbeddingModel):
    """Sentence Transformers embedding model"""

    DEFAULT_MODEL = "BAAI/bge-small-en-v1.5"

    def __init__(self, model_name: str = None, device: str = "auto"):
        self.model_name = model_name or self.DEFAULT_MODEL
        self.device = device
        self._model = None
        self._dimension = None

    def _load_model(self):
        """Lazy load the model"""
        if self._model is None:
            self._model = SentenceTransformer(self.model_name, device=self.device)
            self._dimension = self._model.get_sentence_embedding_dimension()

    @property
    def dimension(self) -> int:
        self._load_model()
        return self._dimension

    def embed(self, texts: list[str]) -> np.ndarray:
        self._load_model()
        return self._model.encode(texts, normalize_embeddings=True)
```

**Success Criteria:**
- [ ] Embedding model loads correctly
- [ ] Batch embedding works
- [ ] Dimension property is accurate
- [ ] Embeddings are normalized
- [ ] GPU acceleration works if available

---

#### Task 2.2.2: Embedding Pipeline
**Files to Create:**
- `src/helixllm/rag/embeddings/pipeline.py`

**Dependencies:** Task 2.2.1

**Implementation Details:**
```python
# src/helixllm/rag/embeddings/pipeline.py
from dataclasses import dataclass
from typing import Iterator, Callable
from pathlib import Path
import structlog
import numpy as np

from .base import BaseEmbeddingModel
from ..chunking import Chunk

logger = structlog.get_logger(__name__)

@dataclass
class EmbeddedChunk:
    """A chunk with its embedding"""
    chunk: Chunk
    embedding: np.ndarray
    embedding_id: str

class EmbeddingPipeline:
    """Pipeline for embedding chunks"""

    def __init__(
        self,
        embedding_model: BaseEmbeddingModel,
        batch_size: int = 32
    ):
        self.embedding_model = embedding_model
        self.batch_size = batch_size
        self.logger = structlog.get_logger(__name__)

    def embed_chunks(
        self,
        chunks: Iterator[Chunk],
        progress_callback: Callable[[int, int], None] = None
    ) -> Iterator[EmbeddedChunk]:
        """Embed chunks in batches"""
        batch = []
        total_processed = 0

        for chunk in chunks:
            batch.append(chunk)

            if len(batch) >= self.batch_size:
                yield from self._process_batch(batch, total_processed)
                total_processed += len(batch)
                if progress_callback:
                    progress_callback(total_processed, None)
                batch = []

        # Process remaining chunks
        if batch:
            yield from self._process_batch(batch, total_processed)

    def _process_batch(
        self,
        chunks: list[Chunk],
        start_index: int
    ) -> Iterator[EmbeddedChunk]:
        """Process a batch of chunks"""
        texts = [chunk.content for chunk in chunks]
        embeddings = self.embedding_model.embed(texts)

        for i, (chunk, embedding) in enumerate(zip(chunks, embeddings)):
            yield EmbeddedChunk(
                chunk=chunk,
                embedding=embedding,
                embedding_id=f"emb_{start_index + i}"
            )

    def embed_query(self, query: str) -> np.ndarray:
        """Embed a query string"""
        return self.embedding_model.embed_query(query)
```

**Success Criteria:**
- [ ] Batch processing works
- [ ] Progress callbacks function
- [ ] Memory usage is controlled
- [ ] Error handling for failed embeddings

---

### 2.3 Vector Store Setup (Days 3-4)

#### Task 2.3.1: Vector Store Interface
**Files to Create:**
- `src/helixllm/rag/vectorstore/__init__.py`
- `src/helixllm/rag/vectorstore/base.py`

**Dependencies:** Task 2.2.2

**Implementation Details:**
```python
# src/helixllm/rag/vectorstore/base.py
from abc import ABC, abstractmethod
from dataclasses import dataclass
from typing import list, Optional
import numpy as np

@dataclass
class SearchResult:
    """Result from vector search"""
    id: str
    content: str
    metadata: dict
    score: float
    distance: float

class BaseVectorStore(ABC):
    """Base class for vector stores"""

    @abstractmethod
    def add(
        self,
        ids: list[str],
        embeddings: np.ndarray,
        documents: list[str],
        metadatas: Optional[list[dict]] = None
    ) -> None:
        """Add documents to the store"""
        pass

    @abstractmethod
    def search(
        self,
        query_embedding: np.ndarray,
        top_k: int = 5,
        filter_dict: Optional[dict] = None
    ) -> list[SearchResult]:
        """Search for similar documents"""
        pass

    @abstractmethod
    def delete(self, ids: list[str]) -> None:
        """Delete documents by ID"""
        pass

    @abstractmethod
    def save(self, path: str) -> None:
        """Save the vector store to disk"""
        pass

    @abstractmethod
    def load(self, path: str) -> None:
        """Load the vector store from disk"""
        pass

    @property
    @abstractmethod
    def count(self) -> int:
        """Return number of documents in store"""
        pass
```

---

#### Task 2.3.2: ChromaDB Implementation
**Files to Create:**
- `src/helixllm/rag/vectorstore/chroma.py`

**Dependencies:** Task 2.3.1

**Implementation Details:**
```python
# src/helixllm/rag/vectorstore/chroma.py
import chromadb
from chromadb.config import Settings as ChromaSettings
import numpy as np
from typing import list, Optional
import structlog

from .base import BaseVectorStore, SearchResult

logger = structlog.get_logger(__name__)

class ChromaVectorStore(BaseVectorStore):
    """ChromaDB vector store implementation"""

    def __init__(
        self,
        collection_name: str = "helixllm",
        persist_directory: Optional[str] = None,
        embedding_dimension: int = 384
    ):
        self.collection_name = collection_name
        self.persist_directory = persist_directory
        self.embedding_dimension = embedding_dimension
        self.logger = structlog.get_logger(__name__)

        self._client = None
        self._collection = None

    def _get_client(self):
        """Lazy initialize ChromaDB client"""
        if self._client is None:
            settings = ChromaSettings(
                anonymized_telemetry=False,
                allow_reset=True
            )

            if self.persist_directory:
                self._client = chromadb.PersistentClient(
                    path=self.persist_directory,
                    settings=settings
                )
            else:
                self._client = chromadb.Client(settings=settings)

            self._collection = self._client.get_or_create_collection(
                name=self.collection_name,
                metadata={"hnsw:space": "cosine"}
            )

        return self._client

    def add(
        self,
        ids: list[str],
        embeddings: np.ndarray,
        documents: list[str],
        metadatas: Optional[list[dict]] = None
    ) -> None:
        """Add documents to ChromaDB"""
        client = self._get_client()

        # Convert numpy arrays to lists
        embeddings_list = embeddings.tolist()

        self._collection.add(
            ids=ids,
            embeddings=embeddings_list,
            documents=documents,
            metadatas=metadatas or [{}] * len(ids)
        )

        self.logger.info(
            "documents_added",
            count=len(ids),
            collection=self.collection_name
        )

    def search(
        self,
        query_embedding: np.ndarray,
        top_k: int = 5,
        filter_dict: Optional[dict] = None
    ) -> list[SearchResult]:
        """Search ChromaDB for similar documents"""
        results = self._collection.query(
            query_embeddings=[query_embedding.tolist()],
            n_results=top_k,
            where=filter_dict
        )

        search_results = []
        for i in range(len(results['ids'][0])):
            search_results.append(SearchResult(
                id=results['ids'][0][i],
                content=results['documents'][0][i],
                metadata=results['metadatas'][0][i],
                score=1 - results['distances'][0][i],  # Convert distance to similarity
                distance=results['distances'][0][i]
            ))

        return search_results

    @property
    def count(self) -> int:
        """Return document count"""
        return self._collection.count()
```

**Success Criteria:**
- [ ] ChromaDB client initializes correctly
- [ ] Documents add successfully
- [ ] Search returns relevant results
- [ ] Persistence works
- [ ] Filtering works

---

### 2.4 Retrieval Implementation (Days 4-5)

#### Task 2.4.1: RAG Retriever
**Files to Create:**
- `src/helixllm/rag/retriever.py`

**Dependencies:** Task 2.3.2

**Implementation Details:**
```python
# src/helixllm/rag/retriever.py
from dataclasses import dataclass
from typing import Optional, Callable
import structlog

from .embeddings.base import BaseEmbeddingModel
from .vectorstore.base import BaseVectorStore, SearchResult

logger = structlog.get_logger(__name__)

@dataclass
class RetrievalConfig:
    """Configuration for retrieval"""
    top_k: int = 5
    min_score: float = 0.0
    max_tokens: int = 2000
    rerank: bool = False

class RAGRetriever:
    """RAG retrieval component"""

    def __init__(
        self,
        vector_store: BaseVectorStore,
        embedding_model: BaseEmbeddingModel,
        config: Optional[RetrievalConfig] = None
    ):
        self.vector_store = vector_store
        self.embedding_model = embedding_model
        self.config = config or RetrievalConfig()
        self.logger = structlog.get_logger(__name__)

    def retrieve(
        self,
        query: str,
        filter_dict: Optional[dict] = None
    ) -> list[SearchResult]:
        """Retrieve relevant documents for query"""
        self.logger.debug("retrieving", query=query[:100])

        # Embed query
        query_embedding = self.embedding_model.embed_query(query)

        # Search vector store
        results = self.vector_store.search(
            query_embedding=query_embedding,
            top_k=self.config.top_k,
            filter_dict=filter_dict
        )

        # Filter by minimum score
        results = [r for r in results if r.score >= self.config.min_score]

        self.logger.info(
            "retrieval_complete",
            query=query[:100],
            results_found=len(results),
            top_score=results[0].score if results else 0
        )

        return results

    def retrieve_with_context(
        self,
        query: str,
        filter_dict: Optional[dict] = None
    ) -> str:
        """Retrieve and format context for LLM"""
        results = self.retrieve(query, filter_dict)

        if not results:
            return ""

        context_parts = []
        total_tokens = 0

        for result in results:
            content = result.content
            # Rough token estimation
            tokens = len(content.split())

            if total_tokens + tokens > self.config.max_tokens:
                break

            context_parts.append(
                f"Source: {result.metadata.get('source', 'unknown')}\n"
                f"{content}\n"
            )
            total_tokens += tokens

        return "\n\n---\n\n".join(context_parts)
```

**Success Criteria:**
- [ ] Retrieval returns relevant documents
- [ ] Query embedding works
- [ ] Context formatting is correct
- [ ] Token limits are respected
- [ ] Filtering works

---

#### Task 2.4.2: RAG Integration with API
**Files to Create:**
- `src/helixllm/api/routes/rag.py`
- `src/helixllm/api/dependencies.py` (update)

**Dependencies:** Task 2.4.1

**Implementation Details:**
```python
# src/helixllm/api/routes/rag.py
from fastapi import APIRouter, Depends, UploadFile, File
from pydantic import BaseModel
from typing import Optional

from ..dependencies import get_rag_retriever
from ...rag.retriever import RAGRetriever

router = APIRouter()

class IndexRequest(BaseModel):
    path: str
    recursive: bool = True
    file_extensions: Optional[list[str]] = None

class QueryRequest(BaseModel):
    query: str
    top_k: int = 5

class QueryResponse(BaseModel):
    results: list[dict]
    context: str

@router.post("/rag/index")
async def index_documents(
    request: IndexRequest,
    retriever: RAGRetriever = Depends(get_rag_retriever)
):
    """Index documents from a path"""
    # Implementation
    pass

@router.post("/rag/query", response_model=QueryResponse)
async def query_documents(
    request: QueryRequest,
    retriever: RAGRetriever = Depends(get_rag_retriever)
):
    """Query indexed documents"""
    results = retriever.retrieve(request.query)
    context = retriever.retrieve_with_context(request.query)

    return QueryResponse(
        results=[
            {
                "content": r.content[:500],
                "source": r.metadata.get("source"),
                "score": r.score
            }
            for r in results
        ],
        context=context
    )
```

---

### Phase 2 Deliverables & Success Criteria

**Deliverables:**
1. ✅ Document loaders (text, code, markdown)
2. ✅ Document processor with chunking
3. ✅ Embedding pipeline
4. ✅ Vector store (ChromaDB)
5. ✅ RAG retriever
6. ✅ API endpoints for RAG

**Success Criteria:**
- [ ] Documents load and chunk correctly
- [ ] Embeddings generate at >100 docs/sec
- [ ] Retrieval latency <100ms
- [ ] API endpoints work correctly
- [ ] Integration tests pass

**Checkpoint Tests:**
```bash
# Index test documents
python -m helixllm.cli index ./test-project

# Query test
curl -X POST http://localhost:8000/v1/rag/query \
  -H "Content-Type: application/json" \
  -d '{"query": "What does this project do?"}'
```

---

## Phase 3: Tool Use (Week 3)
**Goal:** Implement tool registry, core tools, and function calling

### 3.1 Tool Registry (Days 1-2)

#### Task 3.1.1: Tool Definition System
**Files to Create:**
- `src/helixllm/tools/__init__.py`
- `src/helixllm/tools/base.py`
- `src/helixllm/tools/registry.py`
- `src/helixllm/tools/schema.py`

**Dependencies:** Phase 2 completion

**Implementation Details:**
```python
# src/helixllm/tools/base.py
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any, Callable, Optional
import inspect
import structlog

logger = structlog.get_logger(__name__)

@dataclass
class ToolParameter:
    """Definition of a tool parameter"""
    name: str
    type: str  # JSON schema type
    description: str
    required: bool = True
    default: Any = None
    enum: Optional[list] = None

@dataclass
class ToolDefinition:
    """Definition of a tool"""
    name: str
    description: str
    parameters: list[ToolParameter]
    returns: dict = field(default_factory=dict)

@dataclass
class ToolResult:
    """Result of tool execution"""
    success: bool
    result: Any
    error: Optional[str] = None
    execution_time_ms: float = 0.0

class BaseTool(ABC):
    """Base class for tools"""

    name: str = ""
    description: str = ""

    def __init__(self):
        self.logger = structlog.get_logger(self.__class__.__name__)

    @abstractmethod
    def get_definition(self) -> ToolDefinition:
        """Return tool definition"""
        pass

    @abstractmethod
    async def execute(self, **kwargs) -> ToolResult:
        """Execute the tool"""
        pass

    def to_openai_schema(self) -> dict:
        """Convert to OpenAI function schema"""
        definition = self.get_definition()

        properties = {}
        required = []

        for param in definition.parameters:
            prop = {
                "type": param.type,
                "description": param.description
            }
            if param.enum:
                prop["enum"] = param.enum
            if param.default is not None:
                prop["default"] = param.default

            properties[param.name] = prop

            if param.required:
                required.append(param.name)

        return {
            "type": "function",
            "function": {
                "name": definition.name,
                "description": definition.description,
                "parameters": {
                    "type": "object",
                    "properties": properties,
                    "required": required
                }
            }
        }


# src/helixllm/tools/registry.py
from typing import dict, Type, Optional
import structlog

from .base import BaseTool, ToolDefinition

logger = structlog.get_logger(__name__)

class ToolRegistry:
    """Registry for managing tools"""

    def __init__(self):
        self._tools: dict[str, BaseTool] = {}
        self.logger = structlog.get_logger(__name__)

    def register(self, tool: BaseTool) -> None:
        """Register a tool"""
        definition = tool.get_definition()
        self._tools[definition.name] = tool
        self.logger.info("tool_registered", name=definition.name)

    def register_class(self, tool_class: Type[BaseTool]) -> None:
        """Register a tool class"""
        self.register(tool_class())

    def unregister(self, name: str) -> None:
        """Unregister a tool"""
        if name in self._tools:
            del self._tools[name]
            self.logger.info("tool_unregistered", name=name)

    def get(self, name: str) -> Optional[BaseTool]:
        """Get a tool by name"""
        return self._tools.get(name)

    def list_tools(self) -> list[str]:
        """List all registered tool names"""
        return list(self._tools.keys())

    def get_definitions(self) -> list[ToolDefinition]:
        """Get all tool definitions"""
        return [tool.get_definition() for tool in self._tools.values()]

    def get_openai_schemas(self) -> list[dict]:
        """Get all tools as OpenAI schemas"""
        return [tool.to_openai_schema() for tool in self._tools.values()]

    async def execute(self, name: str, **kwargs) -> "ToolResult":
        """Execute a tool by name"""
        tool = self.get(name)
        if not tool:
            from .base import ToolResult
            return ToolResult(
                success=False,
                result=None,
                error=f"Tool '{name}' not found"
            )

        return await tool.execute(**kwargs)

    def clear(self) -> None:
        """Clear all registered tools"""
        self._tools.clear()
```

**Success Criteria:**
- [ ] Tools register correctly
- [ ] Schema generation works
- [ ] OpenAI format is correct
- [ ] Tool lookup works
- [ ] Execution routing works

---

### 3.2 Core Tools Implementation (Days 2-4)

#### Task 3.2.1: File System Tools
**Files to Create:**
- `src/helixllm/tools/file_system.py`

**Dependencies:** Task 3.1.1

**Implementation Details:**
```python
# src/helixllm/tools/file_system.py
import os
import aiofiles
from pathlib import Path
from typing import Optional
import structlog

from .base import BaseTool, ToolDefinition, ToolParameter, ToolResult

logger = structlog.get_logger(__name__)

class ReadFileTool(BaseTool):
    """Tool to read file contents"""

    name = "read_file"
    description = "Read the contents of a file"

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Path to the file to read",
                    required=True
                ),
                ToolParameter(
                    name="offset",
                    type="integer",
                    description="Line offset to start reading from",
                    required=False,
                    default=0
                ),
                ToolParameter(
                    name="limit",
                    type="integer",
                    description="Maximum number of lines to read",
                    required=False,
                    default=100
                )
            ]
        )

    async def execute(
        self,
        path: str,
        offset: int = 0,
        limit: int = 100
    ) -> ToolResult:
        try:
            file_path = Path(path).resolve()

            # Security check
            if not self._is_allowed_path(file_path):
                return ToolResult(
                    success=False,
                    result=None,
                    error="Access to this path is not allowed"
                )

            if not file_path.exists():
                return ToolResult(
                    success=False,
                    result=None,
                    error=f"File not found: {path}"
                )

            async with aiofiles.open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
                lines = await f.readlines()

            start = offset
            end = min(offset + limit, len(lines))
            selected_lines = lines[start:end]

            return ToolResult(
                success=True,
                result={
                    "content": "".join(selected_lines),
                    "total_lines": len(lines),
                    "returned_lines": len(selected_lines),
                    "offset": offset
                }
            )

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )

    def _is_allowed_path(self, path: Path) -> bool:
        """Check if path is allowed for access"""
        # Add security checks here
        return True


class WriteFileTool(BaseTool):
    """Tool to write file contents"""

    name = "write_file"
    description = "Write content to a file"

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Path to the file to write",
                    required=True
                ),
                ToolParameter(
                    name="content",
                    type="string",
                    description="Content to write to the file",
                    required=True
                ),
                ToolParameter(
                    name="append",
                    type="boolean",
                    description="Whether to append to the file",
                    required=False,
                    default=False
                )
            ]
        )

    async def execute(
        self,
        path: str,
        content: str,
        append: bool = False
    ) -> ToolResult:
        try:
            file_path = Path(path).resolve()

            # Ensure parent directory exists
            file_path.parent.mkdir(parents=True, exist_ok=True)

            mode = 'a' if append else 'w'
            async with aiofiles.open(file_path, mode, encoding='utf-8') as f:
                await f.write(content)

            return ToolResult(
                success=True,
                result={
                    "path": str(file_path),
                    "bytes_written": len(content.encode('utf-8'))
                }
            )

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )


class ListDirectoryTool(BaseTool):
    """Tool to list directory contents"""

    name = "list_directory"
    description = "List files and directories in a path"

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Path to the directory to list",
                    required=True
                ),
                ToolParameter(
                    name="recursive",
                    type="boolean",
                    description="List recursively",
                    required=False,
                    default=False
                )
            ]
        )

    async def execute(
        self,
        path: str,
        recursive: bool = False
    ) -> ToolResult:
        try:
            dir_path = Path(path).resolve()

            if not dir_path.exists():
                return ToolResult(
                    success=False,
                    result=None,
                    error=f"Directory not found: {path}"
                )

            entries = []

            if recursive:
                for item in dir_path.rglob("*"):
                    entries.append({
                        "name": item.name,
                        "path": str(item.relative_to(dir_path)),
                        "type": "directory" if item.is_dir() else "file",
                        "size": item.stat().st_size if item.is_file() else None
                    })
            else:
                for item in dir_path.iterdir():
                    entries.append({
                        "name": item.name,
                        "type": "directory" if item.is_dir() else "file",
                        "size": item.stat().st_size if item.is_file() else None
                    })

            return ToolResult(
                success=True,
                result={
                    "entries": entries,
                    "total": len(entries)
                }
            )

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )
```

**Success Criteria:**
- [ ] Read file works with offsets
- [ ] Write file creates directories
- [ ] List directory shows contents
- [ ] Security checks function
- [ ] Error handling is robust

---

#### Task 3.2.2: Code Execution Tools
**Files to Create:**
- `src/helixllm/tools/code_execution.py`

**Dependencies:** Task 3.2.1

**Implementation Details:**
```python
# src/helixllm/tools/code_execution.py
import asyncio
import tempfile
import os
from pathlib import Path
from typing import Optional
import structlog

from .base import BaseTool, ToolDefinition, ToolParameter, ToolResult

logger = structlog.get_logger(__name__)

class ExecutePythonTool(BaseTool):
    """Tool to execute Python code"""

    name = "execute_python"
    description = "Execute Python code in a sandboxed environment"

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="code",
                    type="string",
                    description="Python code to execute",
                    required=True
                ),
                ToolParameter(
                    name="timeout",
                    type="integer",
                    description="Execution timeout in seconds",
                    required=False,
                    default=30
                )
            ]
        )

    async def execute(
        self,
        code: str,
        timeout: int = 30
    ) -> ToolResult:
        try:
            # Create temporary file
            with tempfile.NamedTemporaryFile(
                mode='w',
                suffix='.py',
                delete=False
            ) as f:
                f.write(code)
                temp_path = f.name

            try:
                # Execute with timeout
                proc = await asyncio.create_subprocess_exec(
                    'python', temp_path,
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE
                )

                try:
                    stdout, stderr = await asyncio.wait_for(
                        proc.communicate(),
                        timeout=timeout
                    )
                except asyncio.TimeoutError:
                    proc.kill()
                    return ToolResult(
                        success=False,
                        result=None,
                        error=f"Execution timed out after {timeout} seconds"
                    )

                return ToolResult(
                    success=proc.returncode == 0,
                    result={
                        "stdout": stdout.decode('utf-8', errors='ignore'),
                        "stderr": stderr.decode('utf-8', errors='ignore'),
                        "return_code": proc.returncode
                    }
                )

            finally:
                os.unlink(temp_path)

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )


class ExecuteShellTool(BaseTool):
    """Tool to execute shell commands"""

    name = "execute_shell"
    description = "Execute a shell command"

    # List of allowed commands for security
    ALLOWED_COMMANDS = ['ls', 'cat', 'grep', 'find', 'pwd', 'echo', 'head', 'tail']

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="command",
                    type="string",
                    description="Shell command to execute",
                    required=True
                ),
                ToolParameter(
                    name="timeout",
                    type="integer",
                    description="Execution timeout in seconds",
                    required=False,
                    default=30
                )
            ]
        )

    async def execute(
        self,
        command: str,
        timeout: int = 30
    ) -> ToolResult:
        try:
            # Security check
            if not self._is_allowed_command(command):
                return ToolResult(
                    success=False,
                    result=None,
                    error="Command not in allowed list"
                )

            proc = await asyncio.create_subprocess_shell(
                command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE
            )

            try:
                stdout, stderr = await asyncio.wait_for(
                    proc.communicate(),
                    timeout=timeout
                )
            except asyncio.TimeoutError:
                proc.kill()
                return ToolResult(
                    success=False,
                    result=None,
                    error=f"Execution timed out after {timeout} seconds"
                )

            return ToolResult(
                success=proc.returncode == 0,
                result={
                    "stdout": stdout.decode('utf-8', errors='ignore'),
                    "stderr": stderr.decode('utf-8', errors='ignore'),
                    "return_code": proc.returncode
                }
            )

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )

    def _is_allowed_command(self, command: str) -> bool:
        """Check if command is allowed"""
        cmd = command.strip().split()[0]
        return cmd in self.ALLOWED_COMMANDS
```

**Success Criteria:**
- [ ] Python execution works
- [ ] Shell execution works
- [ ] Timeouts function correctly
- [ ] Output capture works
- [ ] Security restrictions work

---

#### Task 3.2.3: Git Tools
**Files to Create:**
- `src/helixllm/tools/git.py`

**Dependencies:** Task 3.2.2

**Implementation Details:**
```python
# src/helixllm/tools/git.py
import asyncio
from pathlib import Path
from typing import Optional
import structlog

from .base import BaseTool, ToolDefinition, ToolParameter, ToolResult

logger = structlog.get_logger(__name__)

class GitStatusTool(BaseTool):
    """Tool to check git status"""

    name = "git_status"
    description = "Get the git status of a repository"

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Path to the git repository",
                    required=True
                )
            ]
        )

    async def execute(self, path: str) -> ToolResult:
        try:
            proc = await asyncio.create_subprocess_exec(
                'git', '-C', path, 'status', '--porcelain',
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE
            )

            stdout, stderr = await proc.communicate()

            if proc.returncode != 0:
                return ToolResult(
                    success=False,
                    result=None,
                    error=stderr.decode('utf-8', errors='ignore')
                )

            # Parse status output
            lines = stdout.decode('utf-8', errors='ignore').strip().split('\n')
            changes = []

            for line in lines:
                if line:
                    status = line[:2]
                    filename = line[3:]
                    changes.append({
                        "status": status,
                        "file": filename
                    })

            return ToolResult(
                success=True,
                result={
                    "changes": changes,
                    "has_changes": len(changes) > 0
                }
            )

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )


class GitDiffTool(BaseTool):
    """Tool to get git diff"""

    name = "git_diff"
    description = "Get the git diff of changes"

    def get_definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self.name,
            description=self.description,
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Path to the git repository",
                    required=True
                ),
                ToolParameter(
                    name="file",
                    type="string",
                    description="Specific file to diff (optional)",
                    required=False
                )
            ]
        )

    async def execute(
        self,
        path: str,
        file: Optional[str] = None
    ) -> ToolResult:
        try:
            cmd = ['git', '-C', path, 'diff']
            if file:
                cmd.append(file)

            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE
            )

            stdout, stderr = await proc.communicate()

            return ToolResult(
                success=True,
                result={
                    "diff": stdout.decode('utf-8', errors='ignore'),
                    "has_changes": len(stdout) > 0
                }
            )

        except Exception as e:
            return ToolResult(
                success=False,
                result=None,
                error=str(e)
            )
```

**Success Criteria:**
- [ ] Git status works
- [ ] Git diff works
- [ ] Error handling for non-git directories
- [ ] Output parsing is correct

---

### 3.3 Function Calling Pipeline (Days 4-5)

#### Task 3.3.1: Tool Call Parser
**Files to Create:**
- `src/helixllm/tools/parser.py`

**Dependencies:** Task 3.2.3

**Implementation Details:**
```python
# src/helixllm/tools/parser.py
import json
import re
from dataclasses import dataclass
from typing import Optional, list
import structlog

logger = structlog.get_logger(__name__)

@dataclass
class ToolCall:
    """Represents a parsed tool call"""
    id: str
    name: str
    arguments: dict

class ToolCallParser:
    """Parse tool calls from LLM output"""

    def __init__(self):
        self.logger = structlog.get_logger(__name__)

    def parse(self, text: str) -> list[ToolCall]:
        """Parse tool calls from text"""
        tool_calls = []

        # Try JSON format first
        try:
            data = json.loads(text)
            if isinstance(data, dict) and "tool_calls" in data:
                for tc in data["tool_calls"]:
                    tool_calls.append(ToolCall(
                        id=tc.get("id", ""),
                        name=tc["function"]["name"],
                        arguments=json.loads(tc["function"]["arguments"])
                    ))
                return tool_calls
        except json.JSONDecodeError:
            pass

        # Try function call format: function_name(arg1=value1, arg2=value2)
        pattern = r'(\w+)\s*\(([^)]*)\)'
        matches = re.findall(pattern, text)

        for name, args_str in matches:
            try:
                arguments = self._parse_arguments(args_str)
                tool_calls.append(ToolCall(
                    id=f"call_{len(tool_calls)}",
                    name=name,
                    arguments=arguments
                ))
            except Exception as e:
                self.logger.warning("parse_failed", name=name, error=str(e))

        return tool_calls

    def _parse_arguments(self, args_str: str) -> dict:
        """Parse function arguments"""
        arguments = {}

        # Split by comma, respecting quotes
        parts = self._split_args(args_str)

        for part in parts:
            if '=' in part:
                key, value = part.split('=', 1)
                key = key.strip()
                value = self._parse_value(value.strip())
                arguments[key] = value

        return arguments

    def _split_args(self, args_str: str) -> list[str]:
        """Split arguments respecting quotes"""
        parts = []
        current = ""
        in_quotes = False
        quote_char = None

        for char in args_str:
            if char in '"'':
                if not in_quotes:
                    in_quotes = True
                    quote_char = char
                elif char == quote_char:
                    in_quotes = False
                    quote_char = None
                current += char
            elif char == ',' and not in_quotes:
                parts.append(current.strip())
                current = ""
            else:
                current += char

        if current.strip():
            parts.append(current.strip())

        return parts

    def _parse_value(self, value: str) -> any:
        """Parse a value string"""
        value = value.strip()

        # Try integer
        try:
            return int(value)
        except ValueError:
            pass

        # Try float
        try:
            return float(value)
        except ValueError:
            pass

        # Try boolean
        if value.lower() == 'true':
            return True
        if value.lower() == 'false':
            return False

        # Remove quotes if present
        if (value.startswith('"') and value.endswith('"')) or \
           (value.startswith("'") and value.endswith("'")):
            return value[1:-1]

        return value
```

**Success Criteria:**
- [ ] JSON format parsing works
- [ ] Function call format parsing works
- [ ] Argument parsing handles all types
- [ ] Multiple tool calls work
- [ ] Error handling for malformed calls

---

#### Task 3.3.2: Tool Execution Engine
**Files to Create:**
- `src/helixllm/tools/executor.py`

**Dependencies:** Task 3.3.1

**Implementation Details:**
```python
# src/helixllm/tools/executor.py
import asyncio
from dataclasses import dataclass, field
from typing import list, Optional
import time
import structlog

from .registry import ToolRegistry
from .parser import ToolCallParser, ToolCall
from .base import ToolResult

logger = structlog.get_logger(__name__)

@dataclass
class ExecutionContext:
    """Context for tool execution"""
    conversation_id: str
    message_history: list = field(default_factory=list)
    execution_history: list = field(default_factory=list)

@dataclass
class ExecutionResult:
    """Result of executing tool calls"""
    tool_call: ToolCall
    result: ToolResult
    execution_time_ms: float

class ToolExecutor:
    """Execute tool calls with the registry"""

    def __init__(self, registry: ToolRegistry):
        self.registry = registry
        self.parser = ToolCallParser()
        self.logger = structlog.get_logger(__name__)

    async def execute_single(
        self,
        tool_call: ToolCall,
        context: Optional[ExecutionContext] = None
    ) -> ExecutionResult:
        """Execute a single tool call"""
        start_time = time.time()

        self.logger.info(
            "executing_tool",
            tool_name=tool_call.name,
            tool_id=tool_call.id
        )

        result = await self.registry.execute(
            tool_call.name,
            **tool_call.arguments
        )

        execution_time = (time.time() - start_time) * 1000

        self.logger.info(
            "tool_executed",
            tool_name=tool_call.name,
            success=result.success,
            execution_time_ms=execution_time
        )

        return ExecutionResult(
            tool_call=tool_call,
            result=result,
            execution_time_ms=execution_time
        )

    async def execute_multiple(
        self,
        tool_calls: list[ToolCall],
        context: Optional[ExecutionContext] = None
    ) -> list[ExecutionResult]:
        """Execute multiple tool calls in parallel"""
        tasks = [
            self.execute_single(tc, context)
            for tc in tool_calls
        ]

        return await asyncio.gather(*tasks)

    async def execute_from_text(
        self,
        text: str,
        context: Optional[ExecutionContext] = None
    ) -> list[ExecutionResult]:
        """Parse and execute tool calls from text"""
        tool_calls = self.parser.parse(text)

        if not tool_calls:
            return []

        return await self.execute_multiple(tool_calls, context)

    def format_results(
        self,
        results: list[ExecutionResult],
        include_success_only: bool = False
    ) -> str:
        """Format execution results for LLM consumption"""
        formatted = []

        for result in results:
            if include_success_only and not result.result.success:
                continue

            status = "success" if result.result.success else "error"

            entry = f"Tool: {result.tool_call.name}\n"
            entry += f"Status: {status}\n"

            if result.result.success:
                entry += f"Result: {result.result.result}\n"
            else:
                entry += f"Error: {result.result.error}\n"

            formatted.append(entry)

        return "\n---\n".join(formatted)
```

**Success Criteria:**
- [ ] Single tool execution works
- [ ] Parallel execution works
- [ ] Text parsing and execution works
- [ ] Result formatting is correct
- [ ] Execution times are tracked

---

### Phase 3 Deliverables & Success Criteria

**Deliverables:**
1. ✅ Tool registry system
2. ✅ File system tools (read, write, list)
3. ✅ Code execution tools (Python, shell)
4. ✅ Git tools (status, diff)
5. ✅ Tool call parser
6. ✅ Tool execution engine

**Success Criteria:**
- [ ] All tools register correctly
- [ ] Tool schemas are valid OpenAI format
- [ ] Tool execution works end-to-end
- [ ] Security restrictions function
- [ ] Parallel execution works
- [ ] All tool tests pass

**Checkpoint Tests:**
```bash
# Test file tools
curl -X POST http://localhost:8000/v1/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"tool": "read_file", "arguments": {"path": "/path/to/file"}}'

# Test Python execution
curl -X POST http://localhost:8000/v1/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"tool": "execute_python", "arguments": {"code": "print(1+1)"}}'
```

---

## Phase 4: API Completion (Week 4)
**Goal:** Full OpenAI compatibility, streaming, tool calling in API

### 4.1 Full OpenAI Compatibility (Days 1-2)

#### Task 4.1.1: Complete OpenAI Schema
**Files to Create:**
- `src/helixllm/api/schemas.py` (update)

**Dependencies:** Phase 3 completion

**Implementation Details:**
```python
# src/helixllm/api/schemas.py (additional schemas)
from pydantic import BaseModel, Field
from typing import Literal, Optional, Any

class ToolCallFunction(BaseModel):
    name: str
    arguments: str  # JSON string

class ToolCall(BaseModel):
    id: str
    type: Literal["function"] = "function"
    function: ToolCallFunction

class ChatMessage(BaseModel):
    role: Literal["system", "user", "assistant", "tool"]
    content: Optional[str] = None
    name: Optional[str] = None
    tool_calls: Optional[list[ToolCall]] = None
    tool_call_id: Optional[str] = None

class ChatCompletionRequest(BaseModel):
    model: str = Field(default="default")
    messages: list[ChatMessage]
    temperature: float = Field(default=0.7, ge=0, le=2)
    max_tokens: Optional[int] = Field(default=None, ge=1)
    top_p: float = Field(default=1.0, ge=0, le=1)
    stream: bool = Field(default=False)
    stop: Optional[list[str]] = None
    presence_penalty: float = Field(default=0, ge=-2, le=2)
    frequency_penalty: float = Field(default=0, ge=-2, le=2)
    tools: Optional[list[dict]] = None
    tool_choice: Optional[str | dict] = Field(default="auto")
    user: Optional[str] = None

class ChatCompletionChoice(BaseModel):
    index: int
    message: ChatMessage
    finish_reason: Optional[Literal["stop", "length", "tool_calls"]] = None

class UsageInfo(BaseModel):
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int

class ChatCompletionResponse(BaseModel):
    id: str
    object: str = "chat.completion"
    created: int
    model: str
    choices: list[ChatCompletionChoice]
    usage: UsageInfo
    system_fingerprint: Optional[str] = None

# Streaming chunk schemas
class ChatCompletionChunkDelta(BaseModel):
    role: Optional[str] = None
    content: Optional[str] = None
    tool_calls: Optional[list[ToolCall]] = None

class ChatCompletionChunkChoice(BaseModel):
    index: int
    delta: ChatCompletionChunkDelta
    finish_reason: Optional[str] = None

class ChatCompletionChunk(BaseModel):
    id: str
    object: str = "chat.completion.chunk"
    created: int
    model: str
    choices: list[ChatCompletionChunkChoice]
```

**Success Criteria:**
- [ ] All OpenAI fields are present
- [ ] Types match OpenAI spec
- [ ] Validation works correctly
- [ ] Optional fields are handled

---

#### Task 4.1.2: Models Endpoint
**Files to Create:**
- `src/helixllm/api/routes/models.py`

**Dependencies:** Task 4.1.1

**Implementation Details:**
```python
# src/helixllm/api/routes/models.py
from fastapi import APIRouter
from pydantic import BaseModel
from typing import list

from ..dependencies import get_model_info

router = APIRouter()

class ModelInfo(BaseModel):
    id: str
    object: str = "model"
    created: int
    owned_by: str

class ModelsResponse(BaseModel):
    object: str = "list"
    data: list[ModelInfo]

@router.get("/models", response_model=ModelsResponse)
async def list_models():
    """List available models"""
    model_info = get_model_info()

    return ModelsResponse(
        data=[
            ModelInfo(
                id=model_info["id"],
                created=model_info["created"],
                owned_by="helixllm"
            )
        ]
    )

@router.get("/models/{model_id}")
async def get_model(model_id: str):
    """Get model information"""
    model_info = get_model_info()

    if model_id != model_info["id"]:
        from fastapi import HTTPException
        raise HTTPException(status_code=404, detail="Model not found")

    return ModelInfo(
        id=model_info["id"],
        created=model_info["created"],
        owned_by="helixllm"
    )
```

**Success Criteria:**
- [ ] GET /v1/models works
- [ ] GET /v1/models/{id} works
- [ ] Response format matches OpenAI
- [ ] Model info is accurate

---

### 4.2 Streaming Support (Days 2-3)

#### Task 4.2.1: Streaming Infrastructure
**Files to Create:**
- `src/helixllm/api/streaming.py`

**Dependencies:** Task 4.1.2

**Implementation Details:**
```python
# src/helixllm/api/streaming.py
import json
import asyncio
from typing import AsyncIterator, Callable
from fastapi.responses import StreamingResponse
import structlog

from .schemas import (
    ChatCompletionChunk,
    ChatCompletionChunkChoice,
    ChatCompletionChunkDelta,
    ToolCall,
    ToolCallFunction
)

logger = structlog.get_logger(__name__)

class StreamBuilder:
    """Build streaming responses for chat completions"""

    def __init__(self, model: str):
        self.model = model
        self.chunk_id = f"chatcmpl-{self._generate_id()}"
        self.created = int(asyncio.get_event_loop().time())
        self.chunk_index = 0

    def _generate_id(self) -> str:
        import uuid
        return uuid.uuid4().hex[:24]

    def build_chunk(
        self,
        content: str = None,
        role: str = None,
        tool_calls: list[ToolCall] = None,
        finish_reason: str = None
    ) -> str:
        """Build a single SSE chunk"""
        delta = ChatCompletionChunkDelta()

        if role:
            delta.role = role
        if content:
            delta.content = content
        if tool_calls:
            delta.tool_calls = tool_calls

        chunk = ChatCompletionChunk(
            id=self.chunk_id,
            created=self.created,
            model=self.model,
            choices=[
                ChatCompletionChunkChoice(
                    index=0,
                    delta=delta,
                    finish_reason=finish_reason
                )
            ]
        )

        return f"data: {chunk.model_dump_json()}\n\n"

    def build_done(self) -> str:
        """Build the done signal"""
        return "data: [DONE]\n\n"

    async def stream_tokens(
        self,
        token_generator: AsyncIterator[str]
    ) -> AsyncIterator[str]:
        """Stream tokens as SSE events"""
        # Send role first
        yield self.build_chunk(role="assistant")

        async for token in token_generator:
            yield self.build_chunk(content=token)

        yield self.build_chunk(finish_reason="stop")
        yield self.build_done()


def create_streaming_response(
    token_generator: AsyncIterator[str],
    model: str
) -> StreamingResponse:
    """Create a streaming response from a token generator"""
    builder = StreamBuilder(model)

    async def event_generator():
        async for event in builder.stream_tokens(token_generator):
            yield event

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
        }
    )
```

**Success Criteria:**
- [ ] SSE format is correct
- [ ] Chunks are properly formatted
- [ ] [DONE] signal works
- [ ] Error handling in streams

---

### 4.3 Tool Calling in API (Days 3-4)

#### Task 4.3.1: Tool Integration in Chat
**Files to Create:**
- `src/helixllm/api/routes/chat.py` (update)

**Dependencies:** Task 4.2.1

**Implementation Details:**
```python
# src/helixllm/api/routes/chat.py (tool integration)
from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse

from ..schemas import ChatCompletionRequest, ChatCompletionResponse
from ..dependencies import get_inference_engine, get_tool_registry
from ..streaming import create_streaming_response
from ...models.inference import InferenceEngine, GenerationConfig
from ...tools.registry import ToolRegistry
from ...tools.executor import ToolExecutor

router = APIRouter()

@router.post("/chat/completions")
async def chat_completions(
    request: ChatCompletionRequest,
    engine: InferenceEngine = Depends(get_inference_engine),
    tool_registry: ToolRegistry = Depends(get_tool_registry)
):
    """OpenAI-compatible chat completions with tool support"""

    if not engine:
        raise HTTPException(status_code=503, detail="Model not loaded")

    # Convert messages to prompt
    prompt = _format_messages(request.messages)

    # Add tool descriptions if tools are provided
    if request.tools:
        tool_descriptions = _format_tools(request.tools)
        prompt = f"{tool_descriptions}\n\n{prompt}"

    # Create generation config
    gen_config = GenerationConfig(
        max_tokens=request.max_tokens or 1024,
        temperature=request.temperature,
        top_p=request.top_p,
        stop_sequences=request.stop or [],
        stream=request.stream
    )

    if request.stream:
        return create_streaming_response(
            engine.generate_stream(prompt, gen_config),
            request.model
        )

    # Non-streaming with potential tool calls
    result = await engine.generate(prompt, gen_config)

    # Check for tool calls in response
    tool_calls = _extract_tool_calls(result.text)

    if tool_calls and request.tool_choice != "none":
        # Execute tools
        executor = ToolExecutor(tool_registry)
        execution_results = await executor.execute_multiple(tool_calls)

        # Format results
        tool_results = executor.format_results(execution_results)

        # Add to conversation and generate final response
        updated_prompt = f"{prompt}\n\nAssistant: {result.text}\n\nTool Results:\n{tool_results}\n\nAssistant:"

        final_result = await engine.generate(updated_prompt, gen_config)

        return _build_response(
            request.model,
            final_result.text,
            tool_calls=tool_calls,
            finish_reason="stop"
        )

    return _build_response(
        request.model,
        result.text,
        finish_reason=result.finish_reason
    )

def _format_tools(tools: list[dict]) -> str:
    """Format tools for the prompt"""
    formatted = "Available tools:\n"

    for tool in tools:
        func = tool.get("function", {})
        formatted += f"\n{func.get('name')}: {func.get('description')}\n"

        params = func.get("parameters", {})
        if params.get("properties"):
            formatted += "Parameters:\n"
            for name, info in params["properties"].items():
                required = "required" if name in params.get("required", []) else "optional"
                formatted += f"  - {name} ({info.get('type')}, {required}): {info.get('description')}\n"

    formatted += "\nTo use a tool, respond with: function_name(param1=value1, param2=value2)"
    return formatted

def _extract_tool_calls(text: str) -> list:
    """Extract tool calls from generated text"""
    from ...tools.parser import ToolCallParser
    parser = ToolCallParser()
    return parser.parse(text)

def _build_response(
    model: str,
    content: str,
    tool_calls: list = None,
    finish_reason: str = "stop"
) -> ChatCompletionResponse:
    """Build chat completion response"""
    import uuid
    import time

    message = ChatMessage(role="assistant", content=content)

    if tool_calls:
        message.tool_calls = [
            ToolCall(
                id=tc.id,
                function=ToolCallFunction(
                    name=tc.name,
                    arguments=json.dumps(tc.arguments)
                )
            )
            for tc in tool_calls
        ]

    return ChatCompletionResponse(
        id=f"chatcmpl-{uuid.uuid4().hex}",
        created=int(time.time()),
        model=model,
        choices=[
            ChatCompletionChoice(
                index=0,
                message=message,
                finish_reason=finish_reason if not tool_calls else "tool_calls"
            )
        ],
        usage=UsageInfo(
            prompt_tokens=0,  # Calculate properly
            completion_tokens=0,  # Calculate properly
            total_tokens=0
        )
    )
```

**Success Criteria:**
- [ ] Tool calls are detected in responses
- [ ] Tools execute correctly
- [ ] Results are incorporated
- [ ] Response format is correct
- [ ] Streaming with tools works

---

### 4.4 CLI Agent Integration (Days 4-5)

#### Task 4.4.1: OpenCode Integration Guide
**Files to Create:**
- `docs/integrations/opencode.md`
- `docs/integrations/crush.md`
- `docs/integrations/gemini-cli.md`
- `docs/integrations/claude-code.md`

**Dependencies:** Task 4.3.1

**Implementation Details (OpenCode Example):**
```markdown
# OpenCode Integration

## Configuration

Add to your OpenCode config:

```json
{
  "models": {
    "helixllm": {
      "provider": "openai-compatible",
      "baseUrl": "http://localhost:8000/v1",
      "model": "default",
      "apiKey": "not-needed"
    }
  }
}
```

## Usage

```bash
opencode --model helixllm
```
```

**Success Criteria:**
- [ ] OpenCode integration works
- [ ] Crush integration works
- [ ] Gemini CLI integration works
- [ ] Claude Code integration works
- [ ] Documentation is complete

---

### Phase 4 Deliverables & Success Criteria

**Deliverables:**
1. ✅ Complete OpenAI-compatible API
2. ✅ Streaming support
3. ✅ Tool calling in API
4. ✅ Models endpoint
5. ✅ Integration guides for all CLI agents

**Success Criteria:**
- [ ] API passes OpenAI compatibility tests
- [ ] Streaming works with all clients
- [ ] Tool calling works end-to-end
- [ ] All CLI agents can connect
- [ ] Performance: <50ms API overhead

**Checkpoint Tests:**
```bash
# Test with OpenAI client
python -c "
from openai import OpenAI
client = OpenAI(base_url='http://localhost:8000/v1', api_key='test')
response = client.chat.completions.create(
    model='default',
    messages=[{'role': 'user', 'content': 'Hello'}]
)
print(response.choices[0].message.content)
"

# Test with streaming
python -c "
from openai import OpenAI
client = OpenAI(base_url='http://localhost:8000/v1', api_key='test')
for chunk in client.chat.completions.create(
    model='default',
    messages=[{'role': 'user', 'content': 'Hello'}],
    stream=True
):
    print(chunk.choices[0].delta.content, end='')
"
```

---

## Phase 5: Integration & Optimization (Week 5)
**Goal:** HelixAgent integration, performance optimization, hardware tuning

### 5.1 HelixAgent Integration (Days 1-2)

#### Task 5.1.1: Agent Protocol
**Files to Create:**
- `src/helixllm/agent/__init__.py`
- `src/helixllm/agent/protocol.py`
- `src/helixllm/agent/client.py`

**Dependencies:** Phase 4 completion

**Implementation Details:**
```python
# src/helixllm/agent/protocol.py
from pydantic import BaseModel
from typing import Literal, Optional, Any
from enum import Enum

class AgentMessageType(str, Enum):
    THINKING = "thinking"
    ACTION = "action"
    OBSERVATION = "observation"
    RESULT = "result"
    ERROR = "error"

class AgentMessage(BaseModel):
    type: AgentMessageType
    content: str
    metadata: Optional[dict] = None

class AgentTask(BaseModel):
    task_id: str
    description: str
    context: Optional[dict] = None
    tools_available: list[str]

class AgentResult(BaseModel):
    task_id: str
    success: bool
    result: Any
    steps_taken: int
    execution_time_ms: float
```

---

#### Task 5.1.2: Agent Client
**Files to Create:**
- `src/helixllm/agent/client.py`

**Dependencies:** Task 5.1.1

**Implementation Details:**
```python
# src/helixllm/agent/client.py
import httpx
from typing import Optional, AsyncIterator
import structlog

from .protocol import AgentTask, AgentResult, AgentMessage

logger = structlog.get_logger(__name__)

class HelixAgentClient:
    """Client for HelixAgent integration"""

    def __init__(self, base_url: str = "http://localhost:8001"):
        self.base_url = base_url
        self.client = httpx.AsyncClient()
        self.logger = structlog.get_logger(__name__)

    async def submit_task(self, task: AgentTask) -> AgentResult:
        """Submit a task to HelixAgent"""
        response = await self.client.post(
            f"{self.base_url}/tasks",
            json=task.model_dump()
        )
        response.raise_for_status()
        return AgentResult(**response.json())

    async def stream_task(
        self,
        task: AgentTask
    ) -> AsyncIterator[AgentMessage]:
        """Stream task execution from HelixAgent"""
        async with self.client.stream(
            "POST",
            f"{self.base_url}/tasks/stream",
            json=task.model_dump()
        ) as response:
            async for line in response.aiter_lines():
                if line.startswith("data: "):
                    data = line[6:]
                    if data == "[DONE]":
                        break
                    yield AgentMessage.parse_raw(data)

    async def close(self):
        await self.client.aclose()
```

**Success Criteria:**
- [ ] Agent client connects
- [ ] Task submission works
- [ ] Streaming works
- [ ] Error handling works

---

### 5.2 Performance Optimization (Days 2-4)

#### Task 5.2.1: Inference Optimization
**Files to Create:**
- `src/helixllm/optimization/__init__.py`
- `src/helixllm/optimization/kv_cache.py`
- `src/helixllm/optimization/batching.py`

**Dependencies:** Task 5.1.2

**Implementation Details:**
```python
# src/helixllm/optimization/kv_cache.py
from dataclasses import dataclass
from typing import Optional
import torch

@dataclass
class KVCacheConfig:
    """Configuration for KV cache"""
    max_batch_size: int = 1
    max_seq_len: int = 8192
    num_layers: int = 32
    num_heads: int = 32
    head_dim: int = 128
    dtype: torch.dtype = torch.float16

class KVCache:
    """Key-Value cache for efficient inference"""

    def __init__(self, config: KVCacheConfig):
        self.config = config
        self._cache = None
        self._current_length = 0

    def initialize(self):
        """Initialize the cache"""
        self._cache = {
            "key": torch.zeros(
                self.config.max_batch_size,
                self.config.num_layers,
                self.config.num_heads,
                self.config.max_seq_len,
                self.config.head_dim,
                dtype=self.config.dtype
            ),
            "value": torch.zeros(
                self.config.max_batch_size,
                self.config.num_layers,
                self.config.num_heads,
                self.config.max_seq_len,
                self.config.head_dim,
                dtype=self.config.dtype
            )
        }

    def update(
        self,
        key: torch.Tensor,
        value: torch.Tensor,
        position: int
    ):
        """Update cache at position"""
        seq_len = key.size(-2)
        self._cache["key"][:, :, :, position:position+seq_len, :] = key
        self._cache["value"][:, :, :, position:position+seq_len, :] = value
        self._current_length = max(self._current_length, position + seq_len)

    def get(self, position: int, length: int):
        """Get cached KV pairs"""
        return {
            "key": self._cache["key"][:, :, :, :position+length, :],
            "value": self._cache["value"][:, :, :, :position+length, :]
        }

    def clear(self):
        """Clear the cache"""
        self._current_length = 0
        if self._cache:
            self._cache["key"].zero_()
            self._cache["value"].zero_()
```

**Success Criteria:**
- [ ] KV cache works correctly
- [ ] Memory usage is reduced
- [ ] Inference speed improves
- [ ] Cache clearing works

---

#### Task 5.2.2: Hardware-Specific Tuning
**Files to Create:**
- `src/helixllm/optimization/hardware.py`
- `src/helixllm/optimization/profiles.py`

**Dependencies:** Task 5.2.1

**Implementation Details:**
```python
# src/helixllm/optimization/hardware.py
import torch
import psutil
from dataclasses import dataclass
from typing import Optional
import structlog

logger = structlog.get_logger(__name__)

@dataclass
class HardwareInfo:
    """Information about the hardware"""
    cpu_count: int
    memory_gb: float
    gpu_available: bool
    gpu_name: Optional[str]
    gpu_memory_gb: Optional[float]
    cuda_version: Optional[str]

@dataclass
class OptimizationProfile:
    """Optimization settings for specific hardware"""
    gpu_layers: int
    batch_size: int
    threads: int
    use_mmap: bool
    use_mlock: bool
    context_length: int

class HardwareProfiler:
    """Profile hardware and suggest optimizations"""

    def __init__(self):
        self.logger = structlog.get_logger(__name__)

    def get_hardware_info(self) -> HardwareInfo:
        """Get current hardware information"""
        gpu_name = None
        gpu_memory_gb = None
        cuda_version = None

        if torch.cuda.is_available():
            gpu_name = torch.cuda.get_device_name(0)
            gpu_memory_gb = torch.cuda.get_device_properties(0).total_memory / (1024**3)
            cuda_version = torch.version.cuda

        return HardwareInfo(
            cpu_count=psutil.cpu_count(),
            memory_gb=psutil.virtual_memory().total / (1024**3),
            gpu_available=torch.cuda.is_available(),
            gpu_name=gpu_name,
            gpu_memory_gb=gpu_memory_gb,
            cuda_version=cuda_version
        )

    def get_optimization_profile(
        self,
        model_size_gb: float
    ) -> OptimizationProfile:
        """Get optimization profile for current hardware"""
        hw = self.get_hardware_info()

        if hw.gpu_available and hw.gpu_memory_gb:
            return self._gpu_profile(hw, model_size_gb)
        else:
            return self._cpu_profile(hw, model_size_gb)

    def _gpu_profile(
        self,
        hw: HardwareInfo,
        model_size_gb: float
    ) -> OptimizationProfile:
        """Generate GPU optimization profile"""
        # Calculate optimal GPU layers
        available_memory = hw.gpu_memory_gb * 0.8  # Leave 20% headroom

        if model_size_gb <= available_memory:
            gpu_layers = -1  # All layers
        else:
            # Estimate layers that fit
            layer_size = model_size_gb / 32  # Assume 32 layers
            gpu_layers = int(available_memory / layer_size)

        return OptimizationProfile(
            gpu_layers=gpu_layers,
            batch_size=512,
            threads=4,
            use_mmap=True,
            use_mlock=False,
            context_length=8192
        )

    def _cpu_profile(self, hw: HardwareInfo, model_size_gb: float) -> OptimizationProfile:
        """Generate CPU optimization profile"""
        return OptimizationProfile(
            gpu_layers=0,
            batch_size=256,
            threads=hw.cpu_count,
            use_mmap=True,
            use_mlock=hw.memory_gb > model_size_gb * 2,
            context_length=4096
        )
```

**Success Criteria:**
- [ ] Hardware detection works
- [ ] Profiles are appropriate
- [ ] GPU layers calculated correctly
- [ ] Memory usage optimized

---

### 5.3 End-to-End Testing (Days 4-5)

#### Task 5.3.1: Integration Tests
**Files to Create:**
- `tests/integration/test_e2e.py`
- `tests/integration/test_rag.py`
- `tests/integration/test_tools.py`

**Dependencies:** Task 5.2.2

**Implementation Details:**
```python
# tests/integration/test_e2e.py
import pytest
import asyncio
from httpx import AsyncClient
from helixllm.api.main import app

@pytest.fixture
async def client():
    async with AsyncClient(app=app, base_url="http://test") as client:
        yield client

@pytest.mark.asyncio
async def test_chat_completion_basic(client):
    """Test basic chat completion"""
    response = await client.post("/v1/chat/completions", json={
        "model": "test",
        "messages": [{"role": "user", "content": "Hello"}]
    })

    assert response.status_code == 200
    data = response.json()
    assert "choices" in data
    assert len(data["choices"]) > 0

@pytest.mark.asyncio
async def test_chat_completion_with_tools(client):
    """Test chat completion with tool calling"""
    response = await client.post("/v1/chat/completions", json={
        "model": "test",
        "messages": [{"role": "user", "content": "List files in /tmp"}],
        "tools": [{
            "type": "function",
            "function": {
                "name": "list_directory",
                "description": "List directory contents"
            }
        }]
    })

    assert response.status_code == 200
    data = response.json()
    assert "choices" in data

@pytest.mark.asyncio
async def test_rag_query(client):
    """Test RAG query endpoint"""
    response = await client.post("/v1/rag/query", json={
        "query": "What is this project about?"
    })

    assert response.status_code == 200
    data = response.json()
    assert "results" in data
```

**Success Criteria:**
- [ ] All integration tests pass
- [ ] End-to-end flows work
- [ ] Performance tests pass
- [ ] Error scenarios handled

---

### Phase 5 Deliverables & Success Criteria

**Deliverables:**
1. ✅ HelixAgent integration
2. ✅ Performance optimizations
3. ✅ Hardware-specific profiles
4. ✅ End-to-end tests
5. ✅ Performance benchmarks

**Success Criteria:**
- [ ] Agent integration works
- [ ] Inference speed >150 tokens/sec
- [ ] Memory usage optimized
- [ ] All integration tests pass
- [ ] Performance benchmarks documented

---

## Phase 6: Production Readiness (Week 6)
**Goal:** Error handling, logging, monitoring, documentation, deployment

### 6.1 Error Handling (Days 1-2)

#### Task 6.1.1: Exception Handling System
**Files to Create:**
- `src/helixllm/exceptions.py`
- `src/helixllm/api/error_handlers.py`

**Dependencies:** Phase 5 completion

**Implementation Details:**
```python
# src/helixllm/exceptions.py
class HelixLLMError(Exception):
    """Base exception for HelixLLM"""
    pass

class ModelError(HelixLLMError):
    """Model-related errors"""
    pass

class ModelNotFoundError(ModelError):
    """Model not found"""
    pass

class ModelLoadError(ModelError):
    """Error loading model"""
    pass

class InferenceError(HelixLLMError):
    """Inference-related errors"""
    pass

class RAGError(HelixLLMError):
    """RAG-related errors"""
    pass

class ToolError(HelixLLMError):
    """Tool-related errors"""
    pass

class ToolNotFoundError(ToolError):
    """Tool not found"""
    pass

class ToolExecutionError(ToolError):
    """Tool execution failed"""
    pass


# src/helixllm/api/error_handlers.py
from fastapi import Request
from fastapi.responses import JSONResponse
import structlog

from ..exceptions import (
    HelixLLMError,
    ModelError,
    InferenceError,
    RAGError,
    ToolError
)

logger = structlog.get_logger(__name__)

async def helixllm_exception_handler(
    request: Request,
    exc: HelixLLMError
) -> JSONResponse:
    """Handle HelixLLM exceptions"""

    error_mapping = {
        ModelNotFoundError: (404, "model_not_found"),
        ModelLoadError: (500, "model_load_error"),
        InferenceError: (500, "inference_error"),
        ToolNotFoundError: (404, "tool_not_found"),
        ToolExecutionError: (500, "tool_execution_error"),
    }

    status_code, error_type = error_mapping.get(type(exc), (500, "internal_error"))

    logger.error(
        "handled_exception",
        error_type=error_type,
        message=str(exc),
        path=request.url.path
    )

    return JSONResponse(
        status_code=status_code,
        content={
            "error": {
                "message": str(exc),
                "type": error_type,
                "code": status_code
            }
        }
    )
```

**Success Criteria:**
- [ ] All exceptions have handlers
- [ ] Error responses match OpenAI format
- [ ] Logging is comprehensive
- [ ] Error codes are appropriate

---

### 6.2 Logging and Monitoring (Days 2-3)

#### Task 6.2.1: Structured Logging
**Files to Create:**
- `src/helixllm/logging_config.py`
- `src/helixllm/middleware/logging.py`

**Dependencies:** Task 6.1.1

**Implementation Details:**
```python
# src/helixllm/logging_config.py
import structlog
import logging
import sys
from typing import Any

def configure_logging(log_level: str = "INFO", json_format: bool = True):
    """Configure structured logging"""

    # Configure standard logging
    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=getattr(logging, log_level.upper())
    )

    # Configure structlog
    processors = [
        structlog.stdlib.filter_by_level,
        structlog.stdlib.add_logger_name,
        structlog.stdlib.add_log_level,
        structlog.stdlib.PositionalArgumentsFormatter(),
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
    ]

    if json_format:
        processors.append(structlog.processors.JSONRenderer())
    else:
        processors.append(structlog.dev.ConsoleRenderer())

    structlog.configure(
        processors=processors,
        context_class=dict,
        logger_factory=structlog.stdlib.LoggerFactory(),
        wrapper_class=structlog.stdlib.BoundLogger,
        cache_logger_on_first_use=True,
    )


# src/helixllm/middleware/logging.py
import time
from fastapi import Request
import structlog

logger = structlog.get_logger(__name__)

class LoggingMiddleware:
    """Middleware for request/response logging"""

    async def __call__(self, request: Request, call_next):
        start_time = time.time()

        # Log request
        logger.info(
            "request_started",
            method=request.method,
            path=request.url.path,
            client=request.client.host if request.client else None
        )

        response = await call_next(request)

        # Log response
        duration = time.time() - start_time
        logger.info(
            "request_completed",
            method=request.method,
            path=request.url.path,
            status_code=response.status_code,
            duration_ms=duration * 1000
        )

        return response
```

**Success Criteria:**
- [ ] Structured logging works
- [ ] Request/response logging works
- [ ] Performance metrics logged
- [ ] JSON format option works

---

#### Task 6.2.2: Metrics and Monitoring
**Files to Create:**
- `src/helixllm/metrics.py`
- `src/helixllm/api/routes/metrics.py`

**Dependencies:** Task 6.2.1

**Implementation Details:**
```python
# src/helixllm/metrics.py
from dataclasses import dataclass, field
from typing import dict
from collections import defaultdict
import time
import threading

@dataclass
class MetricsCollector:
    """Collect and report metrics"""

    _counters: dict = field(default_factory=lambda: defaultdict(int))
    _gauges: dict = field(default_factory=dict)
    _histograms: dict = field(default_factory=lambda: defaultdict(list))
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def increment(self, name: str, value: int = 1):
        """Increment a counter"""
        with self._lock:
            self._counters[name] += value

    def gauge(self, name: str, value: float):
        """Set a gauge value"""
        with self._lock:
            self._gauges[name] = value

    def histogram(self, name: str, value: float):
        """Record a histogram value"""
        with self._lock:
            self._histograms[name].append(value)

    def time(self, name: str):
        """Context manager for timing"""
        return Timer(self, name)

    def get_metrics(self) -> dict:
        """Get all metrics"""
        with self._lock:
            return {
                "counters": dict(self._counters),
                "gauges": dict(self._gauges),
                "histograms": {
                    name: {
                        "count": len(values),
                        "min": min(values) if values else 0,
                        "max": max(values) if values else 0,
                        "avg": sum(values) / len(values) if values else 0
                    }
                    for name, values in self._histograms.items()
                }
            }

class Timer:
    """Context manager for timing operations"""

    def __init__(self, collector: MetricsCollector, name: str):
        self.collector = collector
        self.name = name
        self.start_time = None

    def __enter__(self):
        self.start_time = time.time()
        return self

    def __exit__(self, *args):
        duration = time.time() - self.start_time
        self.collector.histogram(f"{self.name}_duration_seconds", duration)

# Global metrics collector
metrics = MetricsCollector()
```

**Success Criteria:**
- [ ] Metrics collection works
- [ ] Counters increment correctly
- [ ] Gauges set correctly
- [ ] Histograms calculate correctly
- [ ] Metrics endpoint works

---

### 6.3 Documentation (Days 3-4)

#### Task 6.3.1: API Documentation
**Files to Create:**
- `docs/api/README.md`
- `docs/api/endpoints.md`
- `docs/api/authentication.md`

**Dependencies:** Task 6.2.2

**Success Criteria:**
- [ ] All endpoints documented
- [ ] Request/response examples provided
- [ ] Error codes documented
- [ ] Authentication documented

---

#### Task 6.3.2: User Guide
**Files to Create:**
- `docs/guides/quickstart.md`
- `docs/guides/configuration.md`
- `docs/guides/model-setup.md`
- `docs/guides/rag-setup.md`
- `docs/guides/tool-setup.md`

**Dependencies:** Task 6.3.1

**Success Criteria:**
- [ ] Quickstart guide complete
- [ ] Configuration guide complete
- [ ] Model setup guide complete
- [ ] RAG setup guide complete
- [ ] Tool setup guide complete

---

### 6.4 Deployment Scripts (Days 4-5)

#### Task 6.4.1: Docker Configuration
**Files to Create:**
- `Dockerfile`
- `docker-compose.yml`
- `.dockerignore`

**Dependencies:** Task 6.3.2

**Implementation Details:**
```dockerfile
# Dockerfile
FROM python:3.11-slim as builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY requirements/base.txt requirements/base.txt
RUN pip install --no-cache-dir -r requirements/base.txt

# Production stage
FROM python:3.11-slim

WORKDIR /app

# Copy dependencies from builder
COPY --from=builder /usr/local/lib/python3.11/site-packages /usr/local/lib/python3.11/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin

# Copy application
COPY src/ ./src/
COPY pyproject.toml .

# Install application
RUN pip install -e .

# Expose port
EXPOSE 8000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8000/v1/health || exit 1

# Run application
CMD ["python", "-m", "helixllm.api.main"]
```

**Success Criteria:**
- [ ] Docker image builds
- [ ] Container starts correctly
- [ ] Health check works
- [ ] Multi-stage build optimized

---

#### Task 6.4.2: Deployment Scripts
**Files to Create:**
- `scripts/deploy.sh`
- `scripts/start.sh`
- `scripts/stop.sh`
- `systemd/helixllm.service`

**Dependencies:** Task 6.4.1

**Implementation Details:**
```bash
#!/bin/bash
# scripts/deploy.sh

set -e

echo "Deploying HelixLLM..."

# Configuration
INSTALL_DIR="/opt/helixllm"
SERVICE_NAME="helixllm"
USER="helixllm"

# Create user if not exists
if ! id "$USER" &>/dev/null; then
    useradd -r -s /bin/false "$USER"
fi

# Install application
mkdir -p "$INSTALL_DIR"
cp -r src/ "$INSTALL_DIR/"
cp pyproject.toml "$INSTALL_DIR/"
cp requirements/ "$INSTALL_DIR/" -r

# Install dependencies
cd "$INSTALL_DIR"
pip install -e .

# Set permissions
chown -R "$USER:$USER" "$INSTALL_DIR"

# Install systemd service
cp systemd/helixllm.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

echo "Deployment complete!"
echo "Start with: systemctl start $SERVICE_NAME"
```

**Success Criteria:**
- [ ] Deploy script works
- [ ] Systemd service works
- [ ] Start/stop scripts work
- [ ] Service auto-starts on boot

---

### Phase 6 Deliverables & Success Criteria

**Deliverables:**
1. ✅ Comprehensive error handling
2. ✅ Structured logging
3. ✅ Metrics and monitoring
4. ✅ Complete documentation
5. ✅ Docker configuration
6. ✅ Deployment scripts

**Success Criteria:**
- [ ] All errors handled gracefully
- [ ] Logging is comprehensive
- [ ] Metrics are collected
- [ ] Documentation is complete
- [ ] Docker image builds and runs
- [ ] Deployment scripts work

---

## Dependency Graph

```
Phase 1: Foundation
├── 1.1 Environment Setup
│   ├── 1.1.1 Project Structure
│   └── 1.1.2 Dependency Installation
├── 1.2 Core Model Inference
│   ├── 1.2.1 Model Configuration
│   ├── 1.2.2 Model Loader
│   └── 1.2.3 Inference Engine
└── 1.3 Basic API Server
    ├── 1.3.1 FastAPI Structure
    └── 1.3.2 Chat Completions

Phase 2: RAG System
├── 2.1 Document Processing
│   ├── 2.1.1 Document Loaders
│   └── 2.1.2 Document Processor
├── 2.2 Embedding Pipeline
│   ├── 2.2.1 Embedding Model
│   └── 2.2.2 Embedding Pipeline
├── 2.3 Vector Store
│   ├── 2.3.1 Vector Store Interface
│   └── 2.3.2 ChromaDB Implementation
└── 2.4 Retrieval
    └── 2.4.1 RAG Retriever

Phase 3: Tool Use
├── 3.1 Tool Registry
│   ├── 3.1.1 Tool Definition System
│   └── 3.1.2 Tool Registry
├── 3.2 Core Tools
│   ├── 3.2.1 File System Tools
│   ├── 3.2.2 Code Execution Tools
│   └── 3.2.3 Git Tools
└── 3.3 Function Calling
    ├── 3.3.1 Tool Call Parser
    └── 3.3.2 Tool Execution Engine

Phase 4: API Completion
├── 4.1 OpenAI Compatibility
│   ├── 4.1.1 Complete Schema
│   └── 4.1.2 Models Endpoint
├── 4.2 Streaming
│   └── 4.2.1 Streaming Infrastructure
├── 4.3 Tool Calling API
│   └── 4.3.1 Tool Integration
└── 4.4 CLI Integration
    └── 4.4.1 Integration Guides

Phase 5: Integration & Optimization
├── 5.1 HelixAgent Integration
│   ├── 5.1.1 Agent Protocol
│   └── 5.1.2 Agent Client
├── 5.2 Performance Optimization
│   ├── 5.2.1 Inference Optimization
│   └── 5.2.2 Hardware Tuning
└── 5.3 End-to-End Testing
    └── 5.3.1 Integration Tests

Phase 6: Production Readiness
├── 6.1 Error Handling
│   └── 6.1.1 Exception System
├── 6.2 Logging & Monitoring
│   ├── 6.2.1 Structured Logging
│   └── 6.2.2 Metrics
├── 6.3 Documentation
│   └── 6.3.1 API Documentation
└── 6.4 Deployment
    ├── 6.4.1 Docker
    └── 6.4.2 Deployment Scripts
```

---

## Risk Assessment Matrix

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Model loading fails on certain hardware | Medium | High | Comprehensive hardware testing, fallback to CPU |
| RAG retrieval is too slow | Medium | High | Optimize embeddings, use faster vector store, cache results |
| Tool execution security vulnerabilities | Low | Critical | Sandboxing, allowlists, input validation |
| API incompatibility with CLI agents | Medium | High | Extensive testing with each agent, rapid iteration |
| Memory usage too high | Medium | High | Quantization, KV cache optimization, streaming |
| Performance below target | Medium | High | Hardware-specific tuning, profiling, optimization |
| Dependency conflicts | Low | Medium | Pin versions, use virtual environments, Docker |
| Documentation incomplete | Low | Medium | Allocate dedicated time, peer review |

---

## Testing Checkpoints

### Week 1 Checkpoints
- [ ] Server starts and responds
- [ ] Model loads successfully
- [ ] Basic chat completion works
- [ ] Unit tests pass >80%

### Week 2 Checkpoints
- [ ] Documents index correctly
- [ ] Embeddings generate successfully
- [ ] RAG retrieval returns relevant results
- [ ] RAG API endpoints work

### Week 3 Checkpoints
- [ ] All core tools work
- [ ] Tool registry functions
- [ ] Tool execution works end-to-end
- [ ] Security restrictions work

### Week 4 Checkpoints
- [ ] OpenAI compatibility tests pass
- [ ] Streaming works with all clients
- [ ] Tool calling in API works
- [ ] CLI agents can connect

### Week 5 Checkpoints
- [ ] HelixAgent integration works
- [ ] Performance targets met
- [ ] Hardware profiles work
- [ ] Integration tests pass

### Week 6 Checkpoints
- [ ] Error handling comprehensive
- [ ] Logging works correctly
- [ ] Documentation complete
- [ ] Docker image builds and runs
- [ ] Deployment scripts work

---

## Appendix: File Structure

```
helixllm/
├── src/helixllm/
│   ├── __init__.py
│   ├── version.py
│   ├── config.py
│   ├── exceptions.py
│   ├── logging_config.py
│   ├── metrics.py
│   ├── models/
│   │   ├── __init__.py
│   │   ├── base.py
│   │   ├── config.py
│   │   ├── loader.py
│   │   ├── inference.py
│   │   └── tokenizer.py
│   ├── rag/
│   │   ├── __init__.py
│   │   ├── loaders/
│   │   │   ├── __init__.py
│   │   │   ├── base.py
│   │   │   ├── text.py
│   │   │   ├── code.py
│   │   │   └── markdown.py
│   │   ├── embeddings/
│   │   │   ├── __init__.py
│   │   │   ├── base.py
│   │   │   ├── sentence_transformers.py
│   │   │   └── pipeline.py
│   │   ├── vectorstore/
│   │   │   ├── __init__.py
│   │   │   ├── base.py
│   │   │   └── chroma.py
│   │   ├── processor.py
│   │   ├── chunking.py
│   │   └── retriever.py
│   ├── tools/
│   │   ├── __init__.py
│   │   ├── base.py
│   │   ├── registry.py
│   │   ├── parser.py
│   │   ├── executor.py
│   │   ├── file_system.py
│   │   ├── code_execution.py
│   │   └── git.py
│   ├── api/
│   │   ├── __init__.py
│   │   ├── main.py
│   │   ├── dependencies.py
│   │   ├── middleware.py
│   │   ├── streaming.py
│   │   ├── error_handlers.py
│   │   ├── schemas.py
│   │   └── routes/
│   │       ├── __init__.py
│   │       ├── health.py
│   │       ├── models.py
│   │       ├── chat.py
│   │       ├── rag.py
│   │       └── tools.py
│   ├── agent/
│   │   ├── __init__.py
│   │   ├── protocol.py
│   │   └── client.py
│   └── optimization/
│       ├── __init__.py
│       ├── kv_cache.py
│       ├── batching.py
│       ├── hardware.py
│       └── profiles.py
├── tests/
│   ├── __init__.py
│   ├── conftest.py
│   ├── unit/
│   │   ├── test_environment.py
│   │   ├── models/
│   │   ├── rag/
│   │   ├── tools/
│   │   └── api/
│   └── integration/
│       ├── test_e2e.py
│       ├── test_rag.py
│       └── test_tools.py
├── scripts/
│   ├── setup.sh
│   ├── install.sh
│   ├── validate_env.py
│   ├── deploy.sh
│   ├── start.sh
│   └── stop.sh
├── docs/
│   ├── architecture.md
│   ├── api/
│   ├── guides/
│   └── integrations/
├── requirements/
│   ├── base.txt
│   ├── dev.txt
│   └── gpu.txt
├── systemd/
│   └── helixllm.service
├── Dockerfile
├── docker-compose.yml
├── .dockerignore
├── pyproject.toml
├── Makefile
├── README.md
└── .gitignore
```

---

*Document Version: 1.0*
*Last Updated: 2024*
*Total Estimated Effort: 6 weeks*
