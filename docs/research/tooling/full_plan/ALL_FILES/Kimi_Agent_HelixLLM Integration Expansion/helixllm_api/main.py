"""
HelixLLM OpenAI-Compatible API Server
======================================
A complete FastAPI implementation providing OpenAI API compatibility
for CLI agents like OpenCode, Crush, Gemini CLI, and Claude Code.

Features:
- Full OpenAI API compatibility
- Streaming responses (SSE)
- Tool/function calling
- Authentication support
- Rate limiting
- Comprehensive error handling
"""

import os
import time
import json
import uuid
import asyncio
from typing import Optional, List, Dict, Any, AsyncGenerator, Union, Literal
from datetime import datetime
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Depends, Request, status
from fastapi.responses import StreamingResponse, JSONResponse
from fastapi.middleware.cors import CORSMiddleware
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from pydantic import BaseModel, Field, validator
import uvicorn

# =============================================================================
# CONFIGURATION
# =============================================================================

class Config:
    """Application configuration"""
    API_HOST = os.getenv("HELIXLLM_HOST", "0.0.0.0")
    API_PORT = int(os.getenv("HELIXLLM_PORT", "8000"))
    API_KEY = os.getenv("HELIXLLM_API_KEY", "")  # Empty = no auth required
    MODEL_NAME = os.getenv("HELIXLLM_MODEL", "helix-llm")
    MODEL_VERSION = os.getenv("HELIXLLM_VERSION", "1.0.0")
    MAX_TOKENS = int(os.getenv("HELIXLLM_MAX_TOKENS", "4096"))
    TEMPERATURE = float(os.getenv("HELIXLLM_TEMPERATURE", "0.7"))
    ENABLE_STREAMING = os.getenv("HELIXLLM_ENABLE_STREAMING", "true").lower() == "true"
    LOG_LEVEL = os.getenv("HELIXLLM_LOG_LEVEL", "INFO")
    CORS_ORIGINS = os.getenv("HELIXLLM_CORS_ORIGINS", "*").split(",")
    RATE_LIMIT_ENABLED = os.getenv("HELIXLLM_RATE_LIMIT", "false").lower() == "true"
    RATE_LIMIT_REQUESTS = int(os.getenv("HELIXLLM_RATE_LIMIT_REQUESTS", "100"))
    RATE_LIMIT_WINDOW = int(os.getenv("HELIXLLM_RATE_LIMIT_WINDOW", "60"))


# =============================================================================
# PYDANTIC SCHEMAS - REQUEST/RESPONSE MODELS
# =============================================================================

# -----------------------------------------------------------------------------
# Common Models
# -----------------------------------------------------------------------------

class Usage(BaseModel):
    """Token usage information"""
    prompt_tokens: int = Field(default=0, description="Number of tokens in the prompt")
    completion_tokens: int = Field(default=0, description="Number of tokens in the completion")
    total_tokens: int = Field(default=0, description="Total number of tokens used")


class ErrorDetail(BaseModel):
    """Error detail structure"""
    message: str
    type: str
    param: Optional[str] = None
    code: Optional[str] = None


class APIError(BaseModel):
    """OpenAI-compatible error response"""
    error: ErrorDetail


# -----------------------------------------------------------------------------
# Chat Completion Models
# -----------------------------------------------------------------------------

class ChatMessage(BaseModel):
    """Chat message structure"""
    role: Literal["system", "user", "assistant", "tool"] = Field(
        ..., description="Role of the message sender"
    )
    content: Optional[str] = Field(
        default=None, description="Content of the message"
    )
    name: Optional[str] = Field(
        default=None, description="Name of the sender (for tool messages)"
    )
    tool_calls: Optional[List[Dict[str, Any]]] = Field(
        default=None, description="Tool calls made by the assistant"
    )
    tool_call_id: Optional[str] = Field(
        default=None, description="ID of the tool call (for tool messages)"
    )


class ToolFunction(BaseModel):
    """Function definition for a tool"""
    name: str = Field(..., description="Name of the function")
    description: Optional[str] = Field(
        default=None, description="Description of the function"
    )
    parameters: Dict[str, Any] = Field(
        default_factory=dict, description="JSON Schema parameters"
    )


class Tool(BaseModel):
    """Tool definition"""
    type: Literal["function"] = Field(default="function", description="Type of tool")
    function: ToolFunction = Field(..., description="Function definition")


class ToolChoice(BaseModel):
    """Tool choice specification"""
    type: Literal["function"] = Field(default="function")
    function: Dict[str, str] = Field(..., description="Function to call")


class ChatCompletionRequest(BaseModel):
    """Chat completion request - OpenAI compatible"""
    model: str = Field(..., description="Model to use for completion")
    messages: List[ChatMessage] = Field(
        ..., description="List of messages in the conversation"
    )
    temperature: Optional[float] = Field(
        default=0.7, ge=0, le=2, description="Sampling temperature"
    )
    top_p: Optional[float] = Field(
        default=1.0, ge=0, le=1, description="Nucleus sampling parameter"
    )
    n: Optional[int] = Field(
        default=1, ge=1, le=10, description="Number of completions to generate"
    )
    stream: Optional[bool] = Field(
        default=False, description="Whether to stream the response"
    )
    stop: Optional[Union[str, List[str]]] = Field(
        default=None, description="Stop sequences"
    )
    max_tokens: Optional[int] = Field(
        default=None, description="Maximum number of tokens to generate"
    )
    presence_penalty: Optional[float] = Field(
        default=0, ge=-2, le=2, description="Presence penalty"
    )
    frequency_penalty: Optional[float] = Field(
        default=0, ge=-2, le=2, description="Frequency penalty"
    )
    logit_bias: Optional[Dict[str, float]] = Field(
        default=None, description="Logit bias for token selection"
    )
    user: Optional[str] = Field(
        default=None, description="User identifier"
    )
    tools: Optional[List[Tool]] = Field(
        default=None, description="List of tools available"
    )
    tool_choice: Optional[Union[str, ToolChoice]] = Field(
        default="auto", description="Tool choice strategy"
    )
    parallel_tool_calls: Optional[bool] = Field(
        default=True, description="Whether to allow parallel tool calls"
    )
    response_format: Optional[Dict[str, str]] = Field(
        default=None, description="Response format specification"
    )

    @validator("tool_choice")
    def validate_tool_choice(cls, v):
        if isinstance(v, str) and v not in ["none", "auto", "required"]:
            raise ValueError("tool_choice must be 'none', 'auto', 'required', or a ToolChoice object")
        return v


class ChatCompletionChoice(BaseModel):
    """Chat completion choice"""
    index: int = Field(..., description="Index of the choice")
    message: ChatMessage = Field(..., description="The message generated")
    finish_reason: Optional[Literal["stop", "length", "tool_calls", "content_filter"]] = Field(
        default=None, description="Reason for finishing"
    )
    logprobs: Optional[Dict[str, Any]] = Field(
        default=None, description="Log probabilities"
    )


class ChatCompletionResponse(BaseModel):
    """Chat completion response"""
    id: str = Field(..., description="Unique identifier for the completion")
    object: Literal["chat.completion"] = Field(default="chat.completion")
    created: int = Field(..., description="Unix timestamp of creation")
    model: str = Field(..., description="Model used")
    choices: List[ChatCompletionChoice] = Field(..., description="List of choices")
    usage: Usage = Field(..., description="Token usage")
    system_fingerprint: Optional[str] = Field(
        default=None, description="System fingerprint"
    )


class ChatCompletionStreamChoice(BaseModel):
    """Chat completion stream choice"""
    index: int = Field(..., description="Index of the choice")
    delta: Dict[str, Any] = Field(..., description="Delta content")
    finish_reason: Optional[Literal["stop", "length", "tool_calls", "content_filter"]] = Field(
        default=None, description="Reason for finishing"
    )
    logprobs: Optional[Dict[str, Any]] = Field(
        default=None, description="Log probabilities"
    )


class ChatCompletionStreamResponse(BaseModel):
    """Chat completion stream chunk"""
    id: str = Field(..., description="Unique identifier")
    object: Literal["chat.completion.chunk"] = Field(default="chat.completion.chunk")
    created: int = Field(..., description="Unix timestamp")
    model: str = Field(..., description="Model used")
    choices: List[ChatCompletionStreamChoice] = Field(..., description="List of choices")
    system_fingerprint: Optional[str] = Field(default=None)


# -----------------------------------------------------------------------------
# Completion Models (Legacy)
# -----------------------------------------------------------------------------

class CompletionRequest(BaseModel):
    """Legacy completion request"""
    model: str = Field(..., description="Model to use")
    prompt: Union[str, List[str]] = Field(..., description="Prompt text")
    suffix: Optional[str] = Field(default=None, description="Suffix for completion")
    max_tokens: Optional[int] = Field(default=16, description="Max tokens")
    temperature: Optional[float] = Field(default=1.0, ge=0, le=2)
    top_p: Optional[float] = Field(default=1.0, ge=0, le=1)
    n: Optional[int] = Field(default=1, ge=1, le=10)
    stream: Optional[bool] = Field(default=False)
    logprobs: Optional[int] = Field(default=None, ge=0, le=5)
    echo: Optional[bool] = Field(default=False)
    stop: Optional[Union[str, List[str]]] = Field(default=None)
    presence_penalty: Optional[float] = Field(default=0, ge=-2, le=2)
    frequency_penalty: Optional[float] = Field(default=0, ge=-2, le=2)
    best_of: Optional[int] = Field(default=1, ge=1, le=20)
    logit_bias: Optional[Dict[str, float]] = Field(default=None)
    user: Optional[str] = Field(default=None)


class CompletionChoice(BaseModel):
    """Legacy completion choice"""
    text: str = Field(..., description="Generated text")
    index: int = Field(..., description="Choice index")
    logprobs: Optional[Dict[str, Any]] = Field(default=None)
    finish_reason: Optional[Literal["stop", "length", "content_filter"]] = Field(default=None)


class CompletionResponse(BaseModel):
    """Legacy completion response"""
    id: str = Field(..., description="Unique identifier")
    object: Literal["text_completion"] = Field(default="text_completion")
    created: int = Field(..., description="Unix timestamp")
    model: str = Field(..., description="Model used")
    choices: List[CompletionChoice] = Field(..., description="List of choices")
    usage: Usage = Field(..., description="Token usage")


# -----------------------------------------------------------------------------
# Embedding Models
# -----------------------------------------------------------------------------

class EmbeddingRequest(BaseModel):
    """Embedding request"""
    input: Union[str, List[str], List[int], List[List[int]]] = Field(
        ..., description="Input text to embed"
    )
    model: str = Field(..., description="Model to use")
    encoding_format: Optional[Literal["float", "base64"]] = Field(default="float")
    dimensions: Optional[int] = Field(default=None, description="Embedding dimensions")
    user: Optional[str] = Field(default=None)


class EmbeddingData(BaseModel):
    """Embedding data"""
    object: Literal["embedding"] = Field(default="embedding")
    embedding: List[float] = Field(..., description="Embedding vector")
    index: int = Field(..., description="Index of the embedding")


class EmbeddingResponse(BaseModel):
    """Embedding response"""
    object: Literal["list"] = Field(default="list")
    data: List[EmbeddingData] = Field(..., description="List of embeddings")
    model: str = Field(..., description="Model used")
    usage: Usage = Field(..., description="Token usage")


# -----------------------------------------------------------------------------
# Model Listing Models
# -----------------------------------------------------------------------------

class ModelPermission(BaseModel):
    """Model permission"""
    id: str = Field(default="modelperm-default")
    object: Literal["model_permission"] = Field(default="model_permission")
    created: int = Field(default_factory=lambda: int(time.time()))
    allow_create_engine: bool = Field(default=False)
    allow_sampling: bool = Field(default=True)
    allow_logprobs: bool = Field(default=True)
    allow_search_indices: bool = Field(default=False)
    allow_view: bool = Field(default=True)
    allow_fine_tuning: bool = Field(default=False)
    organization: str = Field(default="*")
    group: Optional[str] = Field(default=None)
    is_blocking: bool = Field(default=False)


class ModelInfo(BaseModel):
    """Model information"""
    id: str = Field(..., description="Model identifier")
    object: Literal["model"] = Field(default="model")
    created: int = Field(..., description="Unix timestamp")
    owned_by: str = Field(default="helix-llm")
    permission: List[ModelPermission] = Field(default_factory=list)
    root: Optional[str] = Field(default=None)
    parent: Optional[str] = Field(default=None)


class ModelListResponse(BaseModel):
    """Model list response"""
    object: Literal["list"] = Field(default="list")
    data: List[ModelInfo] = Field(..., description="List of models")


# =============================================================================
# HELIXLLM BACKEND INTEGRATION
# =============================================================================

class HelixLLMBackend:
    """
    Backend integration for HelixLLM.
    This is a placeholder that should be replaced with actual HelixLLM integration.
    """
    
    def __init__(self):
        self.model_name = Config.MODEL_NAME
        self.model_version = Config.MODEL_VERSION
    
    async def chat_completion(
        self,
        messages: List[ChatMessage],
        temperature: float = 0.7,
        max_tokens: Optional[int] = None,
        tools: Optional[List[Tool]] = None,
        tool_choice: Optional[Union[str, ToolChoice]] = "auto",
        stream: bool = False,
        **kwargs
    ) -> Dict[str, Any]:
        """
        Generate chat completion.
        This is a mock implementation - replace with actual HelixLLM integration.
        """
        # TODO: Replace with actual HelixLLM integration
        # This mock simulates a response for testing purposes
        
        # Check if tools should be called
        if tools and tool_choice != "none":
            # Simulate tool call detection
            tool_call = self._should_call_tool(messages, tools)
            if tool_call:
                return {
                    "content": None,
                    "tool_calls": [tool_call],
                    "finish_reason": "tool_calls",
                    "usage": {
                        "prompt_tokens": sum(len(m.content or "") // 4 for m in messages),
                        "completion_tokens": 50,
                        "total_tokens": sum(len(m.content or "") // 4 for m in messages) + 50
                    }
                }
        
        # Generate text response
        last_message = messages[-1].content if messages else ""
        response_content = self._generate_response(messages, temperature)
        
        prompt_tokens = sum(len(m.content or "") // 4 for m in messages)
        completion_tokens = len(response_content) // 4
        
        return {
            "content": response_content,
            "tool_calls": None,
            "finish_reason": "stop",
            "usage": {
                "prompt_tokens": prompt_tokens,
                "completion_tokens": completion_tokens,
                "total_tokens": prompt_tokens + completion_tokens
            }
        }
    
    def _should_call_tool(
        self,
        messages: List[ChatMessage],
        tools: List[Tool]
    ) -> Optional[Dict[str, Any]]:
        """Determine if a tool should be called based on the conversation"""
        # Simple heuristic: if user mentions a tool-related keyword
        last_message = messages[-1].content.lower() if messages and messages[-1].content else ""
        
        tool_keywords = {
            "weather": ["weather", "temperature", "forecast"],
            "search": ["search", "find", "lookup", "google"],
            "calculator": ["calculate", "compute", "math", "sum", "add"],
            "code": ["code", "function", "script", "program"],
        }
        
        for tool in tools:
            tool_name = tool.function.name
            keywords = tool_keywords.get(tool_name, [tool_name])
            
            for keyword in keywords:
                if keyword in last_message:
                    # Generate mock tool call
                    return {
                        "id": f"call_{uuid.uuid4().hex[:24]}",
                        "type": "function",
                        "function": {
                            "name": tool_name,
                            "arguments": json.dumps({"query": messages[-1].content})
                        }
                    }
        
        return None
    
    def _generate_response(
        self,
        messages: List[ChatMessage],
        temperature: float
    ) -> str:
        """Generate a text response"""
        # TODO: Replace with actual HelixLLM generation
        last_message = messages[-1].content if messages else ""
        
        # Simple response patterns for testing
        greetings = ["hello", "hi", "hey"]
        if any(g in last_message.lower() for g in greetings):
            return "Hello! I'm HelixLLM, your AI assistant. How can I help you today?"
        
        if "help" in last_message.lower():
            return "I can help you with a variety of tasks including answering questions, writing code, analyzing data, and more. What would you like assistance with?"
        
        return f"I received your message: '{last_message}'. This is a placeholder response from HelixLLM. Replace this with actual model integration."
    
    async def stream_chat_completion(
        self,
        messages: List[ChatMessage],
        temperature: float = 0.7,
        max_tokens: Optional[int] = None,
        tools: Optional[List[Tool]] = None,
        **kwargs
    ) -> AsyncGenerator[str, None]:
        """
        Stream chat completion chunks.
        Yields content chunks for SSE streaming.
        """
        # TODO: Replace with actual HelixLLM streaming integration
        response = await self.chat_completion(
            messages=messages,
            temperature=temperature,
            max_tokens=max_tokens,
            tools=tools,
            stream=True,
            **kwargs
        )
        
        content = response.get("content", "")
        tool_calls = response.get("tool_calls")
        
        # Stream content word by word
        if content:
            words = content.split()
            for i, word in enumerate(words):
                chunk = word + (" " if i < len(words) - 1 else "")
                yield chunk
                await asyncio.sleep(0.02)  # Simulate streaming delay
        
        # Stream tool calls if present
        if tool_calls:
            for tool_call in tool_calls:
                yield json.dumps({"tool_call": tool_call})
                await asyncio.sleep(0.05)
    
    async def create_embedding(
        self,
        input_text: Union[str, List[str]],
        model: str,
        dimensions: Optional[int] = None
    ) -> List[List[float]]:
        """
        Create embeddings for input text.
        Returns list of embedding vectors.
        """
        # TODO: Replace with actual embedding model integration
        # Mock implementation returning random embeddings
        import random
        
        texts = [input_text] if isinstance(input_text, str) else input_text
        dim = dimensions or 1536
        
        embeddings = []
        for text in texts:
            # Deterministic mock embedding based on text
            seed = sum(ord(c) for c in text)
            random.seed(seed)
            embedding = [random.uniform(-1, 1) for _ in range(dim)]
            # Normalize
            magnitude = sum(x**2 for x in embedding) ** 0.5
            embedding = [x / magnitude for x in embedding]
            embeddings.append(embedding)
        
        return embeddings
    
    def get_model_info(self) -> ModelInfo:
        """Get information about the model"""
        return ModelInfo(
            id=self.model_name,
            created=int(time.time()),
            owned_by="helix-llm",
            permission=[ModelPermission()]
        )


# Global backend instance
helix_backend = HelixLLMBackend()


# =============================================================================
# AUTHENTICATION & SECURITY
# =============================================================================

security = HTTPBearer(auto_error=False)

async def verify_api_key(
    credentials: HTTPAuthorizationCredentials = Depends(security)
) -> Optional[str]:
    """Verify API key if authentication is enabled"""
    if not Config.API_KEY:
        # Authentication is disabled
        return None
    
    if not credentials:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={
                "error": {
                    "message": "Missing API key",
                    "type": "authentication_error",
                    "code": "missing_api_key"
                }
            }
        )
    
    if credentials.credentials != Config.API_KEY:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={
                "error": {
                    "message": "Invalid API key",
                    "type": "authentication_error",
                    "code": "invalid_api_key"
                }
            }
        )
    
    return credentials.credentials


# =============================================================================
# RATE LIMITING (Optional)
# =============================================================================

class RateLimiter:
    """Simple in-memory rate limiter"""
    
    def __init__(self, max_requests: int, window_seconds: int):
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self.requests: Dict[str, List[float]] = {}
    
    def is_allowed(self, key: str) -> bool:
        """Check if request is allowed under rate limit"""
        now = time.time()
        
        if key not in self.requests:
            self.requests[key] = []
        
        # Remove old requests outside the window
        self.requests[key] = [
            req_time for req_time in self.requests[key]
            if now - req_time < self.window_seconds
        ]
        
        # Check if under limit
        if len(self.requests[key]) < self.max_requests:
            self.requests[key].append(now)
            return True
        
        return False
    
    def get_retry_after(self, key: str) -> int:
        """Get seconds until next request is allowed"""
        if key not in self.requests or not self.requests[key]:
            return 0
        
        now = time.time()
        oldest = min(self.requests[key])
        retry = int(self.window_seconds - (now - oldest)) + 1
        return max(0, retry)


rate_limiter = RateLimiter(
    Config.RATE_LIMIT_REQUESTS,
    Config.RATE_LIMIT_WINDOW
) if Config.RATE_LIMIT_ENABLED else None


async def check_rate_limit(request: Request) -> None:
    """Check rate limit for request"""
    if not rate_limiter:
        return
    
    # Use client IP as rate limit key
    client_ip = request.client.host if request.client else "unknown"
    
    if not rate_limiter.is_allowed(client_ip):
        retry_after = rate_limiter.get_retry_after(client_ip)
        raise HTTPException(
            status_code=status.HTTP_429_TOO_MANY_REQUESTS,
            headers={"Retry-After": str(retry_after)},
            detail={
                "error": {
                    "message": f"Rate limit exceeded. Try again in {retry_after} seconds.",
                    "type": "rate_limit_error",
                    "code": "rate_limit_exceeded"
                }
            }
        )


# =============================================================================
# FASTAPI APPLICATION
# =============================================================================

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan handler"""
    # Startup
    print(f"🚀 HelixLLM API Server starting...")
    print(f"   Model: {Config.MODEL_NAME}")
    print(f"   Auth: {'Enabled' if Config.API_KEY else 'Disabled'}")
    print(f"   Streaming: {'Enabled' if Config.ENABLE_STREAMING else 'Disabled'}")
    print(f"   Rate Limit: {'Enabled' if Config.RATE_LIMIT_ENABLED else 'Disabled'}")
    yield
    # Shutdown
    print("🛑 HelixLLM API Server shutting down...")


app = FastAPI(
    title="HelixLLM API",
    description="OpenAI-compatible API for HelixLLM - CLI Agent Integration",
    version=Config.MODEL_VERSION,
    lifespan=lifespan
)

# CORS Middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=Config.CORS_ORIGINS,
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


# =============================================================================
# ERROR HANDLERS
# =============================================================================

@app.exception_handler(HTTPException)
async def http_exception_handler(request: Request, exc: HTTPException):
    """Handle HTTP exceptions with OpenAI-compatible format"""
    if isinstance(exc.detail, dict) and "error" in exc.detail:
        return JSONResponse(
            status_code=exc.status_code,
            content=exc.detail
        )
    
    return JSONResponse(
        status_code=exc.status_code,
        content={
            "error": {
                "message": str(exc.detail),
                "type": "api_error",
                "code": f"http_{exc.status_code}"
            }
        }
    )


@app.exception_handler(Exception)
async def general_exception_handler(request: Request, exc: Exception):
    """Handle general exceptions"""
    return JSONResponse(
        status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
        content={
            "error": {
                "message": "An internal error occurred",
                "type": "internal_server_error",
                "code": "internal_error"
            }
        }
    )


# =============================================================================
# ENDPOINTS
# =============================================================================

@app.get("/")
async def root():
    """Root endpoint"""
    return {
        "name": "HelixLLM API",
        "version": Config.MODEL_VERSION,
        "model": Config.MODEL_NAME,
        "endpoints": [
            "/v1/models",
            "/v1/chat/completions",
            "/v1/completions",
            "/v1/embeddings"
        ],
        "documentation": "/docs"
    }


@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {
        "status": "healthy",
        "model": Config.MODEL_NAME,
        "timestamp": int(time.time())
    }


# -----------------------------------------------------------------------------
# Models Endpoint
# -----------------------------------------------------------------------------

@app.get("/v1/models", response_model=ModelListResponse)
async def list_models(
    api_key: Optional[str] = Depends(verify_api_key)
):
    """
    List available models.
    Compatible with OpenAI's /v1/models endpoint.
    """
    model_info = helix_backend.get_model_info()
    
    return ModelListResponse(
        data=[model_info]
    )


@app.get("/v1/models/{model_id}", response_model=ModelInfo)
async def get_model(
    model_id: str,
    api_key: Optional[str] = Depends(verify_api_key)
):
    """
    Get information about a specific model.
    """
    if model_id != Config.MODEL_NAME:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail={
                "error": {
                    "message": f"Model '{model_id}' not found",
                    "type": "invalid_request_error",
                    "code": "model_not_found"
                }
            }
        )
    
    return helix_backend.get_model_info()


# -----------------------------------------------------------------------------
# Chat Completions Endpoint
# -----------------------------------------------------------------------------

def create_chat_response(
    request: ChatCompletionRequest,
    content: Optional[str],
    tool_calls: Optional[List[Dict[str, Any]]],
    finish_reason: str,
    usage: Dict[str, int]
) -> ChatCompletionResponse:
    """Create a chat completion response"""
    return ChatCompletionResponse(
        id=f"chatcmpl-{uuid.uuid4().hex}",
        created=int(time.time()),
        model=request.model,
        choices=[
            ChatCompletionChoice(
                index=0,
                message=ChatMessage(
                    role="assistant",
                    content=content,
                    tool_calls=tool_calls
                ),
                finish_reason=finish_reason if finish_reason != "tool_calls" else "tool_calls"
            )
        ],
        usage=Usage(**usage),
        system_fingerprint=f"fp_{Config.MODEL_VERSION.replace('.', '_')}"
    )


async def stream_chat_completion_chunks(
    request: ChatCompletionRequest
) -> AsyncGenerator[str, None]:
    """
    Generate SSE chunks for streaming chat completion.
    Format follows OpenAI's SSE specification.
    """
    completion_id = f"chatcmpl-{uuid.uuid4().hex}"
    created = int(time.time())
    
    # First chunk - role
    first_chunk = ChatCompletionStreamResponse(
        id=completion_id,
        created=created,
        model=request.model,
        choices=[
            ChatCompletionStreamChoice(
                index=0,
                delta={"role": "assistant"},
                finish_reason=None
            )
        ]
    )
    yield f"data: {first_chunk.json()}\n\n"
    
    # Stream content chunks
    buffer = ""
    async for chunk in helix_backend.stream_chat_completion(
        messages=request.messages,
        temperature=request.temperature or 0.7,
        max_tokens=request.max_tokens,
        tools=request.tools
    ):
        try:
            # Check if chunk is a tool call
            data = json.loads(chunk)
            if "tool_call" in data:
                # Tool call chunk
                tool_chunk = ChatCompletionStreamResponse(
                    id=completion_id,
                    created=created,
                    model=request.model,
                    choices=[
                        ChatCompletionStreamChoice(
                            index=0,
                            delta={"tool_calls": [data["tool_call"]]},
                            finish_reason=None
                        )
                    ]
                )
                yield f"data: {tool_chunk.json()}\n\n"
                continue
        except json.JSONDecodeError:
            pass
        
        # Regular content chunk
        buffer += chunk
        content_chunk = ChatCompletionStreamResponse(
            id=completion_id,
            created=created,
            model=request.model,
            choices=[
                ChatCompletionStreamChoice(
                    index=0,
                    delta={"content": chunk},
                    finish_reason=None
                )
            ]
        )
        yield f"data: {content_chunk.json()}\n\n"
    
    # Final chunk - stop
    final_chunk = ChatCompletionStreamResponse(
        id=completion_id,
        created=created,
        model=request.model,
        choices=[
            ChatCompletionStreamChoice(
                index=0,
                delta={},
                finish_reason="stop"
            )
        ]
    )
    yield f"data: {final_chunk.json()}\n\n"
    yield "data: [DONE]\n\n"


@app.post("/v1/chat/completions")
async def create_chat_completion(
    request: ChatCompletionRequest,
    api_key: Optional[str] = Depends(verify_api_key),
    rate_limit: None = Depends(check_rate_limit)
):
    """
    Create a chat completion.
    Compatible with OpenAI's /v1/chat/completions endpoint.
    Supports streaming and tool calling.
    """
    # Validate model
    if request.model != Config.MODEL_NAME:
        # Allow any model for compatibility, but log warning
        print(f"Warning: Requested model '{request.model}' but using '{Config.MODEL_NAME}'")
    
    # Handle streaming
    if request.stream:
        if not Config.ENABLE_STREAMING:
            raise HTTPException(
                status_code=status.HTTP_400_BAD_REQUEST,
                detail={
                    "error": {
                        "message": "Streaming is not enabled",
                        "type": "invalid_request_error",
                        "code": "streaming_disabled"
                    }
                }
            )
        
        return StreamingResponse(
            stream_chat_completion_chunks(request),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no"
            }
        )
    
    # Non-streaming response
    result = await helix_backend.chat_completion(
        messages=request.messages,
        temperature=request.temperature,
        max_tokens=request.max_tokens,
        tools=request.tools,
        tool_choice=request.tool_choice
    )
    
    response = create_chat_response(
        request=request,
        content=result.get("content"),
        tool_calls=result.get("tool_calls"),
        finish_reason=result.get("finish_reason", "stop"),
        usage=result.get("usage", {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0})
    )
    
    return response


# -----------------------------------------------------------------------------
# Legacy Completions Endpoint
# -----------------------------------------------------------------------------

@app.post("/v1/completions")
async def create_completion(
    request: CompletionRequest,
    api_key: Optional[str] = Depends(verify_api_key),
    rate_limit: None = Depends(check_rate_limit)
):
    """
    Create a completion (legacy endpoint).
    Compatible with OpenAI's /v1/completions endpoint.
    """
    # Convert completion request to chat format
    prompts = [request.prompt] if isinstance(request.prompt, str) else request.prompt
    
    choices = []
    total_prompt_tokens = 0
    total_completion_tokens = 0
    
    for i, prompt in enumerate(prompts):
        # Convert to chat messages
        messages = [ChatMessage(role="user", content=prompt)]
        
        result = await helix_backend.chat_completion(
            messages=messages,
            temperature=request.temperature,
            max_tokens=request.max_tokens
        )
        
        choices.append(
            CompletionChoice(
                text=result.get("content", ""),
                index=i,
                logprobs=None,
                finish_reason=result.get("finish_reason", "stop")
            )
        )
        
        usage = result.get("usage", {})
        total_prompt_tokens += usage.get("prompt_tokens", 0)
        total_completion_tokens += usage.get("completion_tokens", 0)
    
    return CompletionResponse(
        id=f"cmpl-{uuid.uuid4().hex}",
        created=int(time.time()),
        model=request.model,
        choices=choices,
        usage=Usage(
            prompt_tokens=total_prompt_tokens,
            completion_tokens=total_completion_tokens,
            total_tokens=total_prompt_tokens + total_completion_tokens
        )
    )


# -----------------------------------------------------------------------------
# Embeddings Endpoint
# -----------------------------------------------------------------------------

@app.post("/v1/embeddings")
async def create_embedding(
    request: EmbeddingRequest,
    api_key: Optional[str] = Depends(verify_api_key),
    rate_limit: None = Depends(check_rate_limit)
):
    """
    Create embeddings for input text.
    Compatible with OpenAI's /v1/embeddings endpoint.
    """
    # Handle different input types
    if isinstance(request.input, str):
        texts = [request.input]
    elif isinstance(request.input, list) and len(request.input) > 0:
        if isinstance(request.input[0], int):
            # Token IDs - convert to string (simplified)
            texts = [" ".join(map(str, request.input))]
        elif isinstance(request.input[0], list):
            # List of token ID lists
            texts = [" ".join(map(str, tokens)) for tokens in request.input]
        else:
            # List of strings
            texts = request.input
    else:
        texts = []
    
    # Generate embeddings
    embeddings = await helix_backend.create_embedding(
        input_text=texts,
        model=request.model,
        dimensions=request.dimensions
    )
    
    # Calculate token usage (rough estimate)
    prompt_tokens = sum(len(text.split()) for text in texts)
    
    # Build response
    data = [
        EmbeddingData(
            embedding=embedding,
            index=i
        )
        for i, embedding in enumerate(embeddings)
    ]
    
    return EmbeddingResponse(
        data=data,
        model=request.model,
        usage=Usage(
            prompt_tokens=prompt_tokens,
            completion_tokens=0,
            total_tokens=prompt_tokens
        )
    )


# =============================================================================
# MAIN ENTRY POINT
# =============================================================================

if __name__ == "__main__":
    uvicorn.run(
        "main:app",
        host=Config.API_HOST,
        port=Config.API_PORT,
        log_level=Config.LOG_LEVEL.lower(),
        reload=False
    )
