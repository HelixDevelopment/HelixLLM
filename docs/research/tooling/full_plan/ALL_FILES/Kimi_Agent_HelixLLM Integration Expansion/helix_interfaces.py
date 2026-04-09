"""
HelixLLM + HelixAgent - Complete Interface Definitions
======================================================

This module defines all public interfaces, data structures, and types
for the HelixLLM + HelixAgent integration system.

Usage:
    from helix_interfaces import (
        ILLMEngine, IEmbeddingEngine, IVectorStore,
        ChatMessage, ToolCall, ToolDefinition, ...
    )
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum, auto
from typing import (
    Any, AsyncGenerator, Callable, Dict, List, Optional, 
    Tuple, Union, Protocol
)
import numpy as np

# =============================================================================
# ENUMERATIONS
# =============================================================================

class ChunkingStrategy(Enum):
    """Text chunking strategies"""
    FIXED = "fixed"
    SEMANTIC = "semantic"
    RECURSIVE = "recursive"
    TOKEN = "token"

class EmbeddingTask(Enum):
    """Embedding task types for nomic-embed-text"""
    SEARCH_DOCUMENT = "search_document"
    SEARCH_QUERY = "search_query"
    CLUSTERING = "clustering"
    CLASSIFICATION = "classification"

class FinishReason(Enum):
    """Reason for generation completion"""
    STOP = "stop"
    LENGTH = "length"
    TOOL_CALL = "tool_calls"
    CONTENT_FILTER = "content_filter"

class DistanceMetric(Enum):
    """Vector distance metrics"""
    COSINE = "cosine"
    EUCLIDEAN = "l2"
    INNER_PRODUCT = "ip"

# =============================================================================
# TYPE ALIASES
# =============================================================================

EmbeddingVector = List[float]
TokenCallback = Callable[[str, int], None]
ToolHandler = Callable[[Dict[str, Any], 'ExecutionContext'], Any]
JSONSchema = Dict[str, Any]

# =============================================================================
# DATA CLASSES - Core Messages
# =============================================================================

@dataclass
class ChatMessage:
    """
    A single message in a chat conversation.

    Attributes:
        role: Message role ("system", "user", "assistant", "tool")
        content: Message content text
        name: Optional name for the message sender
        tool_calls: Tool calls made by assistant
        tool_call_id: ID of tool call this message responds to
    """
    role: str
    content: str
    name: Optional[str] = None
    tool_calls: Optional[List['ToolCall']] = None
    tool_call_id: Optional[str] = None

    def to_openai_dict(self) -> Dict[str, Any]:
        """Convert to OpenAI API format"""
        result = {
            "role": self.role,
            "content": self.content
        }
        if self.name:
            result["name"] = self.name
        if self.tool_calls:
            result["tool_calls"] = [
                tc.to_openai_dict() for tc in self.tool_calls
            ]
        if self.tool_call_id:
            result["tool_call_id"] = self.tool_call_id
        return result

@dataclass
class ToolCall:
    """
    A tool/function call from the assistant.

    Attributes:
        id: Unique identifier for this tool call
        type: Type of call (always "function" for now)
        function: Function call details with name and arguments
    """
    id: str
    type: str = "function"
    function: Dict[str, Any] = field(default_factory=dict)

    @property
    def name(self) -> str:
        """Get function name"""
        return self.function.get("name", "")

    @property
    def arguments(self) -> Dict[str, Any]:
        """Get parsed function arguments"""
        import json
        args_str = self.function.get("arguments", "{}")
        return json.loads(args_str)

    def to_openai_dict(self) -> Dict[str, Any]:
        """Convert to OpenAI API format"""
        return {
            "id": self.id,
            "type": self.type,
            "function": self.function
        }

# =============================================================================
# DATA CLASSES - Tool System
# =============================================================================

@dataclass
class ToolDefinition:
    """
    Definition of a tool/function available to the LLM.

    Attributes:
        name: Unique tool name (alphanumeric, underscores, hyphens)
        description: Human-readable description for the LLM
        parameters_schema: JSON Schema for tool parameters
    """
    name: str
    description: str
    parameters_schema: JSONSchema

    def to_openai_schema(self) -> Dict[str, Any]:
        """Convert to OpenAI function calling format"""
        return {
            "type": "function",
            "function": {
                "name": self.name,
                "description": self.description,
                "parameters": self.parameters_schema
            }
        }

@dataclass
class RegisteredTool:
    """
    A registered tool with its handler.

    Attributes:
        definition: Tool definition
        handler: Async function to execute the tool
        category: Tool category for organization
        registered_at: Registration timestamp
    """
    definition: ToolDefinition
    handler: ToolHandler
    category: str = "general"
    registered_at: datetime = field(default_factory=datetime.utcnow)

@dataclass
class ToolResult:
    """
    Result of a tool execution.

    Attributes:
        success: Whether execution succeeded
        data: Output data on success
        error: Error message on failure
        tool_name: Name of executed tool
        execution_time: Execution duration in seconds
        tool_call_id: ID of the tool call
    """
    success: bool
    data: Any = None
    error: Optional[str] = None
    tool_name: Optional[str] = None
    execution_time: Optional[float] = None
    tool_call_id: Optional[str] = None

    def to_tool_message(self) -> str:
        """Convert result to tool message content"""
        import json
        if self.success:
            if isinstance(self.data, (dict, list)):
                return json.dumps(self.data)
            return str(self.data)
        return f"Error: {self.error}"

@dataclass
class ExecutionContext:
    """
    Context for tool execution.

    Attributes:
        session_id: Current session ID
        user_id: Current user ID
        start_time: Execution start timestamp
        metadata: Additional context metadata
    """
    session_id: str
    user_id: Optional[str] = None
    start_time: float = field(default_factory=lambda: __import__('time').time())
    metadata: Dict[str, Any] = field(default_factory=dict)

# =============================================================================
# DATA CLASSES - Generation
# =============================================================================

@dataclass
class GenerationParams:
    """
    Parameters for text generation.

    Attributes:
        temperature: Sampling temperature (0.0 = deterministic, 1.0 = creative)
        top_p: Nucleus sampling threshold
        top_k: Top-k sampling limit
        max_tokens: Maximum tokens to generate
        stop: Stop sequences to end generation
        repetition_penalty: Penalty for repeated tokens
        grammar: Grammar constraint for structured output
    """
    temperature: float = 0.7
    top_p: float = 1.0
    top_k: int = 40
    max_tokens: Optional[int] = None
    stop: Optional[List[str]] = None
    repetition_penalty: float = 1.0
    grammar: Optional[str] = None

    def to_llama_cpp_dict(self) -> Dict[str, Any]:
        """Convert to llama.cpp parameters"""
        return {
            "temperature": self.temperature,
            "top_p": self.top_p,
            "top_k": self.top_k,
            "max_tokens": self.max_tokens,
            "stop": self.stop,
            "repeat_penalty": self.repetition_penalty,
        }

@dataclass
class GenerationResult:
    """
    Result of text generation.

    Attributes:
        text: Generated text
        tokens_generated: Number of tokens generated
        prompt_tokens: Number of tokens in prompt
        finish_reason: Why generation stopped
        tool_calls: Tool calls extracted from generation
        generation_time: Time taken for generation
    """
    text: str
    tokens_generated: int
    prompt_tokens: int
    finish_reason: str
    tool_calls: Optional[List[ToolCall]] = None
    generation_time: Optional[float] = None

@dataclass
class StreamChunk:
    """
    A chunk in a streaming response.

    Attributes:
        delta: Incremental content (text or tool_calls)
        finish_reason: Final chunk reason if complete
    """
    delta: Dict[str, Any]
    finish_reason: Optional[str] = None

    @property
    def content(self) -> str:
        """Get content delta"""
        return self.delta.get("content", "")

    @property
    def has_tool_calls(self) -> bool:
        """Check if chunk contains tool calls"""
        return "tool_calls" in self.delta

# =============================================================================
# DATA CLASSES - Document Processing
# =============================================================================

@dataclass
class DocumentSource:
    """
    Source of a document.

    Attributes:
        type: Source type ("file", "url", "text", "stream")
        path: File path or URL
        content: Raw content if type is "text"
        metadata: Source metadata
    """
    type: str  # "file", "url", "text", "stream"
    path: Optional[str] = None
    content: Optional[str] = None
    metadata: Dict[str, Any] = field(default_factory=dict)

@dataclass
class ProcessedDocument:
    """
    A processed document ready for chunking.

    Attributes:
        id: Unique document ID
        content: Cleaned text content
        metadata: Extracted metadata
        source: Original source
        processed_at: Processing timestamp
    """
    id: str
    content: str
    metadata: Dict[str, Any]
    source: DocumentSource
    processed_at: datetime = field(default_factory=datetime.utcnow)

@dataclass
class DocumentChunk:
    """
    A chunk of a document.

    Attributes:
        id: Unique chunk ID
        document_id: Parent document ID
        content: Chunk text content
        token_count: Number of tokens in chunk
        start_index: Start position in document
        end_index: End position in document
        metadata: Chunk metadata
    """
    id: str
    document_id: str
    content: str
    token_count: int
    start_index: int
    end_index: int
    metadata: Dict[str, Any] = field(default_factory=dict)

# =============================================================================
# DATA CLASSES - Vector Search
# =============================================================================

@dataclass
class SearchResult:
    """
    Result from vector search.

    Attributes:
        id: Document/chunk ID
        content: Retrieved content
        metadata: Associated metadata
        similarity: Similarity score (0-1)
        distance: Raw distance metric
    """
    id: str
    content: str
    metadata: Dict[str, Any]
    similarity: float
    distance: float

@dataclass
class RetrievalResult:
    """
    Result of RAG context retrieval.

    Attributes:
        context: Formatted context string for LLM
        sources: List of source documents
        total_tokens: Total tokens in context
        source_count: Number of sources
    """
    context: str
    sources: List[SearchResult]
    total_tokens: int
    source_count: int

# =============================================================================
# DATA CLASSES - Chat Results
# =============================================================================

@dataclass
class ChatResult:
    """
    Result of a chat completion.

    Attributes:
        content: Assistant's response text
        tool_calls: Tool calls made by assistant
        finish_reason: Why generation stopped
        prompt_tokens: Tokens in prompt
        completion_tokens: Tokens generated
        total_tokens: Total tokens used
        session_id: Session ID
    """
    content: str
    tool_calls: Optional[List[ToolCall]]
    finish_reason: str
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    session_id: str

@dataclass
class ChatCompletionResponse:
    """OpenAI-compatible chat completion response"""
    id: str
    object: str = "chat.completion"
    created: int = field(default_factory=lambda: int(__import__('time').time()))
    model: str = "helix-1.5b"
    choices: List[Dict[str, Any]] = field(default_factory=list)
    usage: Dict[str, int] = field(default_factory=dict)

# =============================================================================
# DATA CLASSES - Configuration
# =============================================================================

@dataclass
class ModelConfig:
    """Configuration for LLM model"""
    path: str
    context_length: int = 4096
    gpu_layers: int = -1  # -1 = all layers
    cpu_threads: int = 8
    batch_size: int = 512
    quantization: str = "Q4_K_M"

@dataclass
class EmbeddingConfig:
    """Configuration for embedding engine"""
    model_name: str = "nomic-ai/nomic-embed-text-v1.5"
    model_path: Optional[str] = None
    device: str = "cuda"
    normalize: bool = True
    cache_size: int = 10000
    cache_ttl: int = 3600

@dataclass
class ChromaConfig:
    """Configuration for ChromaDB"""
    persist_directory: str = "./chroma"
    collection_name: str = "default"
    distance_metric: str = "cosine"
    anonymized_telemetry: bool = False

@dataclass
class ChunkerConfig:
    """Configuration for text chunker"""
    strategy: ChunkingStrategy = ChunkingStrategy.FIXED
    chunk_size: int = 512  # tokens
    chunk_overlap: int = 50  # tokens
    tokenizer: Any = None  # Will be set during init

@dataclass
class RetrieverConfig:
    """Configuration for context retriever"""
    embedding_engine: Any = None  # IEmbeddingEngine
    vector_store: Any = None  # IVectorStore
    reranker: Optional[Any] = None
    max_context_tokens: int = 1500
    tokenizer: Any = None

@dataclass
class ExecutorConfig:
    """Configuration for tool executor"""
    registry: Any = None  # IToolRegistry
    timeout_seconds: float = 30.0
    max_retries: int = 1
    sandbox: Optional[Any] = None

@dataclass
class OrchestratorConfig:
    """Configuration for agent orchestrator"""
    llm_engine: Any = None  # ILLMEngine
    tool_registry: Any = None  # IToolRegistry
    tool_executor: Any = None  # ToolExecutor
    retriever: Optional[Any] = None  # ContextRetriever
    session_manager: Any = None  # SessionManager
    tokenizer: Any = None
    max_tool_iterations: int = 5
    enable_rag: bool = True
    rag_trigger_keywords: List[str] = field(default_factory=list)

@dataclass
class APIConfig:
    """Configuration for REST API"""
    host: str = "0.0.0.0"
    port: int = 8000
    api_keys: List[str] = field(default_factory=list)
    auth_enabled: bool = False
    rate_limit: int = 60
    allowed_origins: List[str] = field(default_factory=lambda: ["*"])
    model_id: str = "helix-1.5b"
    orchestrator: Any = None

@dataclass
class WSConfig:
    """Configuration for WebSocket server"""
    orchestrator: Any = None
    ping_interval: int = 20
    ping_timeout: int = 10

@dataclass
class SessionConfig:
    """Configuration for session manager"""
    store: Any = None
    ttl_seconds: int = 3600
    max_history: int = 100

@dataclass
class BridgeConfig:
    """Configuration for CLI agent bridge"""
    agent_path: str
    protocol: Any = None
    env_vars: Dict[str, str] = field(default_factory=dict)

# =============================================================================
# DATA CLASSES - Results
# =============================================================================

@dataclass
class LoadResult:
    """Result of model loading"""
    success: bool
    error: Optional[str] = None
    load_time: Optional[float] = None
    memory_used: Optional[int] = None  # bytes

@dataclass
class AddResult:
    """Result of adding documents to vector store"""
    ids: List[str]
    count: int
    success: bool
    error: Optional[str] = None

@dataclass
class DeleteResult:
    """Result of deleting documents"""
    success: bool
    deleted_count: Optional[int] = None
    error: Optional[str] = None

@dataclass
class RegistrationResult:
    """Result of tool registration"""
    success: bool
    tool_name: Optional[str] = None
    error: Optional[str] = None

@dataclass
class ValidationResult:
    """Result of validation"""
    valid: bool
    error: Optional[str] = None

@dataclass
class ConnectionResult:
    """Result of connection attempt"""
    success: bool
    already_connected: bool = False
    agent_info: Optional[Dict] = None
    error: Optional[str] = None

@dataclass
class BootResult:
    """Result of application bootstrap"""
    success: bool
    components: Dict[str, Any] = field(default_factory=dict)
    error: Optional[str] = None

# =============================================================================
# ABSTRACT INTERFACES
# =============================================================================

class ILLMEngine(ABC):
    """
    Interface for LLM inference engines.

    Implementations: LlamaCppEngine, VLLMEngine, etc.
    """

    @abstractmethod
    async def load_model(self) -> LoadResult:
        """Load model into memory"""
        pass

    @abstractmethod
    async def generate(
        self,
        prompt: str,
        params: GenerationParams,
        callback: Optional[TokenCallback] = None
    ) -> GenerationResult:
        """Generate text with optional streaming callback"""
        pass

    @abstractmethod
    async def generate_batch(
        self,
        prompts: List[str],
        params: GenerationParams
    ) -> List[GenerationResult]:
        """Batch generation for efficiency"""
        pass

    @abstractmethod
    def tokenize(self, text: str) -> List[int]:
        """Convert text to token IDs"""
        pass

    @abstractmethod
    def detokenize(self, tokens: List[int]) -> str:
        """Convert token IDs to text"""
        pass

    @abstractmethod
    def get_token_count(self, text: str) -> int:
        """Get token count without full tokenization"""
        pass

    @abstractmethod
    async def unload(self) -> None:
        """Unload model from memory"""
        pass

class ITokenizer(ABC):
    """Interface for tokenizers"""

    @abstractmethod
    def encode(self, text: str, add_special_tokens: bool = True) -> List[int]:
        """Encode text to tokens"""
        pass

    @abstractmethod
    def decode(self, tokens: List[int], skip_special_tokens: bool = True) -> str:
        """Decode tokens to text"""
        pass

    @abstractmethod
    def count_tokens(self, text: str) -> int:
        """Count tokens in text"""
        pass

    @abstractmethod
    def apply_chat_template(
        self,
        messages: List[ChatMessage],
        tools: Optional[List[ToolDefinition]] = None
    ) -> str:
        """Apply chat template to messages"""
        pass

class IEmbeddingEngine(ABC):
    """
    Interface for embedding engines.

    Implementations: SentenceTransformersEngine, etc.
    """

    @abstractmethod
    async def embed(
        self,
        texts: Union[str, List[str]],
        task_type: EmbeddingTask = EmbeddingTask.SEARCH_DOCUMENT
    ) -> Union[EmbeddingVector, List[EmbeddingVector]]:
        """Generate embeddings for text(s)"""
        pass

    @abstractmethod
    async def embed_batch(
        self,
        texts: List[str],
        batch_size: int = 32
    ) -> List[EmbeddingVector]:
        """Batch embedding generation"""
        pass

    @property
    @abstractmethod
    def embedding_dim(self) -> int:
        """Get embedding dimension"""
        pass

class IVectorStore(ABC):
    """
    Interface for vector stores.

    Implementations: ChromaDBManager, FAISSManager, etc.
    """

    @abstractmethod
    async def add_documents(
        self,
        documents: List[DocumentChunk],
        embeddings: List[EmbeddingVector]
    ) -> AddResult:
        """Add documents with embeddings"""
        pass

    @abstractmethod
    async def search(
        self,
        query_embedding: EmbeddingVector,
        top_k: int = 5,
        filter_dict: Optional[Dict] = None,
        min_similarity: Optional[float] = None
    ) -> List[SearchResult]:
        """Search for similar documents"""
        pass

    @abstractmethod
    async def delete(
        self,
        ids: Optional[List[str]] = None,
        filter_dict: Optional[Dict] = None
    ) -> DeleteResult:
        """Delete documents by ID or filter"""
        pass

    @abstractmethod
    async def update(
        self,
        ids: List[str],
        documents: List[DocumentChunk],
        embeddings: List[EmbeddingVector]
    ) -> AddResult:
        """Update existing documents"""
        pass

    @abstractmethod
    async def count(self, filter_dict: Optional[Dict] = None) -> int:
        """Count documents in store"""
        pass

class IToolRegistry(ABC):
    """Interface for tool registries"""

    @abstractmethod
    async def register(
        self,
        tool: ToolDefinition,
        handler: ToolHandler,
        category: str = "general"
    ) -> RegistrationResult:
        """Register a new tool"""
        pass

    @abstractmethod
    async def unregister(self, tool_name: str) -> bool:
        """Unregister a tool"""
        pass

    @abstractmethod
    def get_tool(self, tool_name: str) -> Optional[RegisteredTool]:
        """Get a registered tool"""
        pass

    @abstractmethod
    def list_tools(
        self,
        category: Optional[str] = None
    ) -> List[ToolDefinition]:
        """List registered tools"""
        pass

    @abstractmethod
    def get_openai_schema(self) -> List[Dict[str, Any]]:
        """Get tools in OpenAI format"""
        pass

class IAgentOrchestrator(ABC):
    """Interface for agent orchestrators"""

    @abstractmethod
    async def chat(
        self,
        messages: List[ChatMessage],
        tools: Optional[List[Dict]] = None,
        tool_choice: Optional[str] = "auto",
        temperature: float = 0.7,
        max_tokens: Optional[int] = None,
        top_p: float = 1.0,
        stop: Optional[List[str]] = None,
        session_id: Optional[str] = None,
    ) -> ChatResult:
        """Process chat request"""
        pass

    @abstractmethod
    async def chat_stream(
        self,
        messages: List[ChatMessage],
        tools: Optional[List[Dict]] = None,
        **kwargs
    ) -> AsyncGenerator[StreamChunk, None]:
        """Streaming chat completion"""
        pass

    @abstractmethod
    async def embed(self, texts: Union[str, List[str]]) -> List[List[float]]:
        """Generate embeddings"""
        pass

class ISessionStore(ABC):
    """Interface for session storage"""

    @abstractmethod
    async def get(self, session_id: str) -> Optional[Any]:
        """Get session by ID"""
        pass

    @abstractmethod
    async def save(self, session: Any) -> None:
        """Save session"""
        pass

    @abstractmethod
    async def delete(self, session_id: str) -> bool:
        """Delete session"""
        pass

# =============================================================================
# EXCEPTIONS
# =============================================================================

class HelixError(Exception):
    """Base exception for Helix system"""
    pass

class ModelLoadError(HelixError):
    """Error loading model"""
    pass

class GenerationError(HelixError):
    """Error during text generation"""
    pass

class ToolNotFoundError(HelixError):
    """Tool not found in registry"""
    pass

class ToolExecutionError(HelixError):
    """Error executing tool"""
    pass

class SessionNotFoundError(HelixError):
    """Session not found"""
    pass

class ValidationError(HelixError):
    """Validation error"""
    pass

class RateLimitError(HelixError):
    """Rate limit exceeded"""
    pass

# =============================================================================
# UTILITY FUNCTIONS
# =============================================================================

def generate_uuid() -> str:
    """Generate a unique ID"""
    import uuid
    return str(uuid.uuid4())

def compute_hash(text: str) -> str:
    """Compute hash for caching"""
    import hashlib
    return hashlib.sha256(text.encode()).hexdigest()

def format_chat_messages(messages: List[ChatMessage]) -> str:
    """Format chat messages for display"""
    lines = []
    for msg in messages:
        prefix = msg.role.upper()
        if msg.name:
            prefix += f" ({msg.name})"
        lines.append(f"{prefix}: {msg.content}")
    return "\n".join(lines)

# =============================================================================
# EXPORTS
# =============================================================================

__all__ = [
    # Enums
    "ChunkingStrategy",
    "EmbeddingTask", 
    "FinishReason",
    "DistanceMetric",

    # Type aliases
    "EmbeddingVector",
    "TokenCallback",
    "ToolHandler",
    "JSONSchema",

    # Data classes - Messages
    "ChatMessage",
    "ToolCall",

    # Data classes - Tools
    "ToolDefinition",
    "RegisteredTool",
    "ToolResult",
    "ExecutionContext",

    # Data classes - Generation
    "GenerationParams",
    "GenerationResult",
    "StreamChunk",

    # Data classes - Documents
    "DocumentSource",
    "ProcessedDocument",
    "DocumentChunk",

    # Data classes - Search
    "SearchResult",
    "RetrievalResult",

    # Data classes - Chat
    "ChatResult",
    "ChatCompletionResponse",

    # Data classes - Config
    "ModelConfig",
    "EmbeddingConfig",
    "ChromaConfig",
    "ChunkerConfig",
    "RetrieverConfig",
    "ExecutorConfig",
    "OrchestratorConfig",
    "APIConfig",
    "WSConfig",
    "SessionConfig",
    "BridgeConfig",

    # Data classes - Results
    "LoadResult",
    "AddResult",
    "DeleteResult",
    "RegistrationResult",
    "ValidationResult",
    "ConnectionResult",
    "BootResult",

    # Interfaces
    "ILLMEngine",
    "ITokenizer",
    "IEmbeddingEngine",
    "IVectorStore",
    "IToolRegistry",
    "IAgentOrchestrator",
    "ISessionStore",

    # Exceptions
    "HelixError",
    "ModelLoadError",
    "GenerationError",
    "ToolNotFoundError",
    "ToolExecutionError",
    "SessionNotFoundError",
    "ValidationError",
    "RateLimitError",

    # Utilities
    "generate_uuid",
    "compute_hash",
    "format_chat_messages",
]
