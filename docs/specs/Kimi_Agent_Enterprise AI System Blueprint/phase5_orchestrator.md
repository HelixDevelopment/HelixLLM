# Phase 5: Orchestrator Layer - Complete Implementation Guide

## Table of Contents
1. [Architecture Overview](#architecture-overview)
2. [Project Structure](#project-structure)
3. [Core Orchestrator Implementation](#core-orchestrator-implementation)
4. [LLM Integration Module](#llm-integration-module)
5. [RAG Integration Module](#rag-integration-module)
6. [MCP Tool Manager](#mcp-tool-manager)
7. [LSP Integration Module](#lsp-integration-module)
8. [Agent Loop Implementation](#agent-loop-implementation)
9. [Prompt Engineering](#prompt-engineering)
10. [State Management](#state-management)
11. [API Endpoints](#api-endpoints)
12. [Configuration & Deployment](#configuration--deployment)

---

## Architecture Overview

The Orchestrator Layer serves as the central "brain" of the Light Local LLM system, coordinating all components through a unified interface.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           ORCHESTRATOR LAYER                                │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         FastAPI Application                          │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐  │   │
│  │  │   /chat     │  │ /tools/exec │  │  /health    │  │  /tools    │  │   │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └─────┬──────┘  │   │
│  └─────────┼────────────────┼────────────────┼───────────────┼─────────┘   │
│            │                │                │               │             │
│  ┌─────────▼────────────────▼────────────────▼───────────────▼─────────┐   │
│  │                      AGENT LOOP CONTROLLER                          │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │   │
│  │  │   Planner    │  │   Executor   │  │      State Manager       │  │   │
│  │  └──────────────┘  └──────────────┘  └──────────────────────────┘  │   │
│  └────────────────────────────────────────────────────────────────────┘   │
│            │                │                │               │             │
│  ┌─────────▼────────┐ ┌─────▼──────┐  ┌──────▼───────┐  ┌────▼─────────┐   │
│  │  LLM Integration │ │ RAG Client │  │ MCP Manager  │  │ LSP Client   │   │
│  │  (llama.cpp)     │ │ (ChromaDB) │  │ (Tools)      │  │ (Code Intel) │   │
│  └────────┬─────────┘ └─────┬──────┘  └──────┬───────┘  └────┬─────────┘   │
│           │                 │                │               │             │
└───────────┼─────────────────┼────────────────┼───────────────┼─────────────┘
            │                 │                │               │
            ▼                 ▼                ▼               ▼
    ┌──────────────┐  ┌──────────────┐  ┌──────────┐  ┌──────────────┐
    │ LLM Server   │  │ RAG Server   │  │ MCP Srvs │  │ LSP Servers  │
    │ localhost:8080│  │ Desktop:8000 │  │ Various  │  │ Various      │
    └──────────────┘  └──────────────┘  └──────────┘  └──────────────┘
```

### Key Design Principles

1. **Event-Driven Architecture**: Async/await throughout for non-blocking I/O
2. **Modular Design**: Each component is independently testable and replaceable
3. **Stateless Core**: Session state externalized for horizontal scaling
4. **Graceful Degradation**: Components can fail without crashing the system
5. **Observability**: Comprehensive logging, metrics, and tracing

---

## Project Structure

```
orchestrator/
├── app/
│   ├── __init__.py
│   ├── main.py                 # FastAPI application entry point
│   ├── config.py               # Configuration management
│   ├── logging_config.py       # Logging setup
│   ├── models.py               # Pydantic models
│   ├── state.py                # State management
│   ├── agent/
│   │   ├── __init__.py
│   │   ├── loop.py             # Main agent loop
│   │   ├── planner.py          # Query planning
│   │   └── executor.py         # Tool execution
│   ├── integrations/
│   │   ├── __init__.py
│   │   ├── llm_client.py       # LLM integration
│   │   ├── rag_client.py       # RAG integration
│   │   ├── mcp_manager.py      # MCP tool management
│   │   └── lsp_client.py       # LSP integration
│   ├── prompts/
│   │   ├── __init__.py
│   │   ├── templates.py        # Prompt templates
│   │   └── system_prompts.py   # System prompt definitions
│   └── api/
│       ├── __init__.py
│       ├── routes.py           # API route handlers
│       └── dependencies.py     # FastAPI dependencies
├── tests/
│   ├── __init__.py
│   ├── test_agent.py
│   ├── test_integrations.py
│   └── test_api.py
├── docker/
│   └── Dockerfile
├── requirements.txt
├── docker-compose.yml
├── .env.example
└── README.md
```

---

## Core Orchestrator Implementation

### File: `app/config.py`

```python
"""
Configuration management for the Orchestrator.
Uses Pydantic Settings for environment-based configuration.
"""

import os
from functools import lru_cache
from typing import List, Optional
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class LLMConfig(BaseSettings):
    """LLM server configuration."""
    model_config = SettingsConfigDict(env_prefix="LLM_")
    
    host: str = Field(default="localhost", description="LLM server host")
    port: int = Field(default=8080, description="LLM server port")
    timeout: int = Field(default=120, description="Request timeout in seconds")
    max_tokens: int = Field(default=4096, description="Maximum tokens per request")
    temperature: float = Field(default=0.7, description="Sampling temperature")
    top_p: float = Field(default=0.9, description="Top-p sampling parameter")
    top_k: int = Field(default=40, description="Top-k sampling parameter")
    context_window: int = Field(default=8192, description="Context window size")
    
    @property
    def base_url(self) -> str:
        return f"http://{self.host}:{self.port}"


class RAGConfig(BaseSettings):
    """RAG server configuration."""
    model_config = SettingsConfigDict(env_prefix="RAG_")
    
    host: str = Field(default="desktop", description="RAG server host")
    port: int = Field(default=8000, description="RAG server port")
    timeout: int = Field(default=30, description="Request timeout in seconds")
    default_collection: str = Field(default="documents", description="Default collection name")
    max_results: int = Field(default=5, description="Maximum retrieval results")
    
    @property
    def base_url(self) -> str:
        return f"http://{self.host}:{self.port}"


class MCPConfig(BaseSettings):
    """MCP server configuration."""
    model_config = SettingsConfigDict(env_prefix="MCP_")
    
    servers: str = Field(default="", description="Comma-separated list of MCP server URLs")
    timeout: int = Field(default=60, description="Tool execution timeout")
    max_concurrent: int = Field(default=5, description="Maximum concurrent tool calls")
    
    @field_validator("servers")
    @classmethod
    def parse_servers(cls, v: str) -> List[str]:
        if not v:
            return []
        return [s.strip() for s in v.split(",") if s.strip()]


class LSPConfig(BaseSettings):
    """LSP server configuration."""
    model_config = SettingsConfigDict(env_prefix="LSP_")
    
    enabled: bool = Field(default=True, description="Enable LSP integration")
    servers: str = Field(default="", description="Comma-separated LSP server configs")
    timeout: int = Field(default=30, description="LSP request timeout")
    
    @field_validator("servers")
    @classmethod
    def parse_servers(cls, v: str) -> List[str]:
        if not v:
            return []
        return [s.strip() for s in v.split(",") if s.strip()]


class StateConfig(BaseSettings):
    """State management configuration."""
    model_config = SettingsConfigDict(env_prefix="STATE_")
    
    backend: str = Field(default="memory", description="State backend: memory, redis, file")
    redis_url: Optional[str] = Field(default=None, description="Redis connection URL")
    file_path: str = Field(default="./data/sessions", description="File storage path")
    session_ttl: int = Field(default=3600, description="Session TTL in seconds")
    max_history: int = Field(default=20, description="Maximum conversation turns")


class LoggingConfig(BaseSettings):
    """Logging configuration."""
    model_config = SettingsConfigDict(env_prefix="LOG_")
    
    level: str = Field(default="INFO", description="Logging level")
    format: str = Field(
        default="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        description="Log format string"
    )
    json: bool = Field(default=False, description="Use JSON logging format")
    file: Optional[str] = Field(default=None, description="Log file path")


class Settings(BaseSettings):
    """Main application settings."""
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore"
    )
    
    # Application
    app_name: str = Field(default="LLM Orchestrator", description="Application name")
    app_version: str = Field(default="1.0.0", description="Application version")
    debug: bool = Field(default=False, description="Debug mode")
    
    # Server
    host: str = Field(default="0.0.0.0", description="Server bind host")
    port: int = Field(default=9000, description="Server bind port")
    workers: int = Field(default=1, description="Number of worker processes")
    
    # Sub-configs
    llm: LLMConfig = Field(default_factory=LLMConfig)
    rag: RAGConfig = Field(default_factory=RAGConfig)
    mcp: MCPConfig = Field(default_factory=MCPConfig)
    lsp: LSPConfig = Field(default_factory=LSPConfig)
    state: StateConfig = Field(default_factory=StateConfig)
    logging: LoggingConfig = Field(default_factory=LoggingConfig)


@lru_cache()
def get_settings() -> Settings:
    """Get cached settings instance."""
    return Settings()


def reload_settings() -> Settings:
    """Force reload settings (useful for testing)."""
    get_settings.cache_clear()
    return get_settings()
```

### File: `app/logging_config.py`

```python
"""
Logging configuration with structured logging support.
"""

import json
import logging
import sys
from datetime import datetime
from typing import Any, Dict

from .config import get_settings


class JSONFormatter(logging.Formatter):
    """JSON log formatter for structured logging."""
    
    def format(self, record: logging.LogRecord) -> str:
        log_data: Dict[str, Any] = {
            "timestamp": datetime.utcnow().isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "message": record.getMessage(),
            "source": {
                "file": record.filename,
                "line": record.lineno,
                "function": record.funcName,
            }
        }
        
        # Add extra fields if present
        if hasattr(record, "extra"):
            log_data.update(record.extra)
        
        # Add exception info if present
        if record.exc_info:
            log_data["exception"] = self.formatException(record.exc_info)
        
        return json.dumps(log_data, default=str)


class ColoredFormatter(logging.Formatter):
    """Colored console formatter for development."""
    
    COLORS = {
        "DEBUG": "\033[36m",      # Cyan
        "INFO": "\033[32m",       # Green
        "WARNING": "\033[33m",    # Yellow
        "ERROR": "\033[31m",      # Red
        "CRITICAL": "\033[35m",   # Magenta
        "RESET": "\033[0m"
    }
    
    def format(self, record: logging.LogRecord) -> str:
        color = self.COLORS.get(record.levelname, self.COLORS["RESET"])
        reset = self.COLORS["RESET"]
        record.levelname = f"{color}{record.levelname}{reset}"
        return super().format(record)


def setup_logging() -> logging.Logger:
    """Configure application logging."""
    settings = get_settings()
    log_config = settings.logging
    
    # Get root logger
    logger = logging.getLogger()
    logger.setLevel(getattr(logging, log_config.level.upper()))
    
    # Remove existing handlers
    logger.handlers = []
    
    # Console handler
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(getattr(logging, log_config.level.upper()))
    
    if log_config.json:
        console_formatter = JSONFormatter()
    else:
        console_formatter = ColoredFormatter(log_config.format)
    
    console_handler.setFormatter(console_formatter)
    logger.addHandler(console_handler)
    
    # File handler if configured
    if log_config.file:
        file_handler = logging.FileHandler(log_config.file)
        file_handler.setLevel(getattr(logging, log_config.level.upper()))
        file_formatter = JSONFormatter() if log_config.json else logging.Formatter(log_config.format)
        file_handler.setFormatter(file_formatter)
        logger.addHandler(file_handler)
    
    # Create orchestrator logger
    orchestrator_logger = logging.getLogger("orchestrator")
    orchestrator_logger.info(
        "Logging configured",
        extra={"config": {"level": log_config.level, "json": log_config.json}}
    )
    
    return orchestrator_logger


def get_logger(name: str) -> logging.Logger:
    """Get a logger with the orchestrator prefix."""
    return logging.getLogger(f"orchestrator.{name}")
```

### File: `app/models.py`

```python
"""
Pydantic models for API requests and responses.
"""

from datetime import datetime
from enum import Enum
from typing import Any, Dict, List, Optional, Union
from uuid import UUID, uuid4

from pydantic import BaseModel, Field, field_validator


class MessageRole(str, Enum):
    """Message roles in conversation."""
    SYSTEM = "system"
    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"


class ToolCall(BaseModel):
    """Represents a tool call from the LLM."""
    id: str = Field(default_factory=lambda: str(uuid4()))
    name: str = Field(description="Tool name")
    arguments: Dict[str, Any] = Field(default_factory=dict, description="Tool arguments")
    
    @field_validator("arguments", mode="before")
    @classmethod
    def parse_arguments(cls, v):
        if isinstance(v, str):
            import json
            return json.loads(v)
        return v


class ToolResult(BaseModel):
    """Result of a tool execution."""
    call_id: str = Field(description="ID of the corresponding tool call")
    name: str = Field(description="Tool name")
    success: bool = Field(description="Whether execution succeeded")
    result: Any = Field(default=None, description="Tool output")
    error: Optional[str] = Field(default=None, description="Error message if failed")
    execution_time_ms: float = Field(default=0.0, description="Execution time in milliseconds")


class Message(BaseModel):
    """A message in the conversation."""
    role: MessageRole = Field(description="Message role")
    content: str = Field(description="Message content")
    tool_calls: Optional[List[ToolCall]] = Field(default=None, description="Tool calls")
    tool_results: Optional[List[ToolResult]] = Field(default=None, description="Tool results")
    timestamp: datetime = Field(default_factory=datetime.utcnow)
    metadata: Dict[str, Any] = Field(default_factory=dict)


class ContextSource(str, Enum):
    """Source of context information."""
    RAG = "rag"
    LSP = "lsp"
    MEMORY = "memory"
    SYSTEM = "system"


class ContextItem(BaseModel):
    """A piece of context information."""
    source: ContextSource = Field(description="Context source")
    content: str = Field(description="Context content")
    metadata: Dict[str, Any] = Field(default_factory=dict)
    relevance_score: Optional[float] = Field(default=None, description="Relevance score if from RAG")


class ConversationState(BaseModel):
    """Complete conversation state."""
    session_id: str = Field(default_factory=lambda: str(uuid4()))
    messages: List[Message] = Field(default_factory=list)
    context: List[ContextItem] = Field(default_factory=list)
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)
    metadata: Dict[str, Any] = Field(default_factory=dict)


class ChatRequest(BaseModel):
    """Request body for chat endpoint."""
    message: str = Field(description="User message")
    session_id: Optional[str] = Field(default=None, description="Session ID for continuation")
    context_filter: Optional[Dict[str, Any]] = Field(default=None, description="RAG context filter")
    use_tools: bool = Field(default=True, description="Enable tool usage")
    use_rag: bool = Field(default=True, description="Enable RAG context")
    stream: bool = Field(default=False, description="Stream response")
    temperature: Optional[float] = Field(default=None, description="Override temperature")
    max_tokens: Optional[int] = Field(default=None, description="Override max tokens")


class ChatResponse(BaseModel):
    """Response from chat endpoint."""
    session_id: str = Field(description="Session ID")
    message: Message = Field(description="Assistant response")
    tool_calls: List[ToolCall] = Field(default_factory=list)
    tool_results: List[ToolResult] = Field(default_factory=list)
    context_used: List[ContextItem] = Field(default_factory=list)
    tokens_used: Optional[int] = Field(default=None)
    processing_time_ms: float = Field(description="Total processing time")


class ToolExecuteRequest(BaseModel):
    """Request to execute a tool directly."""
    tool_name: str = Field(description="Tool to execute")
    arguments: Dict[str, Any] = Field(default_factory=dict, description="Tool arguments")


class ToolExecuteResponse(BaseModel):
    """Response from tool execution."""
    success: bool = Field(description="Execution success")
    result: Any = Field(default=None, description="Tool output")
    error: Optional[str] = Field(default=None, description="Error if failed")
    execution_time_ms: float = Field(default=0.0)


class ToolInfo(BaseModel):
    """Information about an available tool."""
    name: str = Field(description="Tool name")
    description: str = Field(description="Tool description")
    parameters: Dict[str, Any] = Field(description="JSON schema for parameters")
    required: List[str] = Field(default_factory=list, description="Required parameters")


class ToolsListResponse(BaseModel):
    """Response listing available tools."""
    tools: List[ToolInfo] = Field(description="Available tools")
    count: int = Field(description="Number of tools")


class HealthStatus(BaseModel):
    """Health check response."""
    status: str = Field(description="Overall status: healthy, degraded, unhealthy")
    version: str = Field(description="Application version")
    timestamp: datetime = Field(default_factory=datetime.utcnow)
    components: Dict[str, Dict[str, Any]] = Field(default_factory=dict)
    

class StreamChunk(BaseModel):
    """A chunk of streamed response."""
    type: str = Field(description="Chunk type: content, tool_call, tool_result, done, error")
    data: Any = Field(description="Chunk data")
    session_id: Optional[str] = Field(default=None)
```

---

## LLM Integration Module

### File: `app/integrations/llm_client.py`

```python
"""
LLM client for llama.cpp server integration.
Supports both standard and streaming responses with tool call detection.
"""

import json
import re
import time
from typing import Any, AsyncGenerator, Dict, List, Optional, Tuple, Union

import aiohttp
from pydantic import BaseModel

from ..config import get_settings
from ..logging_config import get_logger
from ..models import Message, MessageRole, ToolCall

logger = get_logger("llm_client")


class LLMResponse(BaseModel):
    """Parsed LLM response."""
    content: str
    tool_calls: List[ToolCall]
    tokens_used: Optional[int] = None
    finish_reason: Optional[str] = None


class LLMClient:
    """
    Client for llama.cpp server communication.
    
    Features:
    - Chat completions with OpenAI-compatible API
    - Streaming support
    - Tool call detection and parsing
    - Token usage tracking
    - Connection pooling
    """
    
    # Tool call patterns for different formats
    TOOL_CALL_PATTERNS = [
        # XML-style: <tool>name</tool><args>{...}</args>
        r'<tool>([^<]+)</tool>\s*<args>(\{[^}]*\})</args>',
        # JSON-style: {"tool": "name", "args": {...}}
        r'\{\s*"tool"\s*:\s*"([^"]+)"\s*,\s*"args"\s*:\s*(\{[^}]*\})\s*\}',
        # Function-style: tool_name({...})
        r'(\w+)\s*\((\{[^}]*\})\)',
        # Markdown code block with JSON
        r'```(?:json)?\s*\{\s*"tool"\s*:\s*"([^"]+)"[^}]*"args"\s*:\s*(\{[^}]*\})[^}]*\}\s*```',
    ]
    
    def __init__(self):
        self.settings = get_settings().llm
        self.base_url = self.settings.base_url
        self.session: Optional[aiohttp.ClientSession] = None
        logger.info(f"LLM client initialized: {self.base_url}")
    
    async def _get_session(self) -> aiohttp.ClientSession:
        """Get or create aiohttp session."""
        if self.session is None or self.session.closed:
            timeout = aiohttp.ClientTimeout(total=self.settings.timeout)
            self.session = aiohttp.ClientSession(timeout=timeout)
        return self.session
    
    async def close(self):
        """Close the HTTP session."""
        if self.session and not self.session.closed:
            await self.session.close()
            logger.debug("LLM client session closed")
    
    async def health_check(self) -> Tuple[bool, Optional[str]]:
        """Check LLM server health."""
        try:
            session = await self._get_session()
            async with session.get(f"{self.base_url}/health") as response:
                if response.status == 200:
                    return True, None
                return False, f"HTTP {response.status}"
        except Exception as e:
            return False, str(e)
    
    def _format_messages(self, messages: List[Message]) -> List[Dict[str, str]]:
        """Convert Message objects to API format."""
        formatted = []
        for msg in messages:
            formatted_msg = {"role": msg.role.value, "content": msg.content}
            
            # Add tool calls if present
            if msg.tool_calls:
                formatted_msg["tool_calls"] = [
                    {
                        "id": tc.id,
                        "type": "function",
                        "function": {
                            "name": tc.name,
                            "arguments": json.dumps(tc.arguments)
                        }
                    }
                    for tc in msg.tool_calls
                ]
            
            formatted.append(formatted_msg)
        return formatted
    
    def _detect_tool_calls(self, content: str) -> Tuple[str, List[ToolCall]]:
        """
        Detect and extract tool calls from LLM response.
        
        Returns:
            Tuple of (cleaned content, list of tool calls)
        """
        tool_calls = []
        cleaned_content = content
        
        for pattern in self.TOOL_CALL_PATTERNS:
            matches = list(re.finditer(pattern, content, re.DOTALL | re.IGNORECASE))
            
            for match in matches:
                try:
                    tool_name = match.group(1).strip()
                    args_str = match.group(2).strip()
                    
                    # Parse arguments
                    try:
                        arguments = json.loads(args_str)
                    except json.JSONDecodeError:
                        # Try fixing common JSON issues
                        args_str = args_str.replace("'", '"')
                        arguments = json.loads(args_str)
                    
                    tool_call = ToolCall(name=tool_name, arguments=arguments)
                    tool_calls.append(tool_call)
                    
                    # Remove the tool call from content
                    cleaned_content = cleaned_content.replace(match.group(0), "")
                    
                except Exception as e:
                    logger.warning(f"Failed to parse tool call: {e}")
                    continue
        
        # Clean up extra whitespace
        cleaned_content = re.sub(r'\n{3,}', '\n\n', cleaned_content.strip())
        
        return cleaned_content, tool_calls
    
    async def chat(
        self,
        messages: List[Message],
        tools: Optional[List[Dict[str, Any]]] = None,
        temperature: Optional[float] = None,
        max_tokens: Optional[int] = None,
        stream: bool = False
    ) -> Union[LLMResponse, AsyncGenerator[str, None]]:
        """
        Send chat completion request to LLM.
        
        Args:
            messages: Conversation history
            tools: Available tools for function calling
            temperature: Sampling temperature
            max_tokens: Maximum tokens to generate
            stream: Whether to stream response
            
        Returns:
            LLMResponse or async generator for streaming
        """
        session = await self._get_session()
        
        # Build request payload
        payload = {
            "messages": self._format_messages(messages),
            "temperature": temperature or self.settings.temperature,
            "max_tokens": max_tokens or self.settings.max_tokens,
            "top_p": self.settings.top_p,
            "top_k": self.settings.top_k,
            "stream": stream
        }
        
        # Add tools if provided
        if tools:
            payload["tools"] = tools
            payload["tool_choice"] = "auto"
        
        start_time = time.time()
        
        try:
            if stream:
                return self._stream_chat(payload)
            
            async with session.post(
                f"{self.base_url}/v1/chat/completions",
                json=payload
            ) as response:
                if response.status != 200:
                    error_text = await response.text()
                    raise LLMError(f"LLM request failed: HTTP {response.status} - {error_text}")
                
                data = await response.json()
                
                # Extract response
                choice = data["choices"][0]
                content = choice["message"].get("content", "")
                finish_reason = choice.get("finish_reason")
                
                # Detect tool calls
                cleaned_content, tool_calls = self._detect_tool_calls(content)
                
                # Also check for native tool calls
                if "tool_calls" in choice["message"]:
                    for tc in choice["message"]["tool_calls"]:
                        tool_calls.append(ToolCall(
                            id=tc.get("id", ""),
                            name=tc["function"]["name"],
                            arguments=json.loads(tc["function"]["arguments"])
                        ))
                
                tokens_used = data.get("usage", {}).get("total_tokens")
                
                elapsed = (time.time() - start_time) * 1000
                logger.info(
                    f"LLM chat completed",
                    extra={
                        "tokens": tokens_used,
                        "tool_calls": len(tool_calls),
                        "time_ms": elapsed
                    }
                )
                
                return LLMResponse(
                    content=cleaned_content,
                    tool_calls=tool_calls,
                    tokens_used=tokens_used,
                    finish_reason=finish_reason
                )
                
        except aiohttp.ClientError as e:
            logger.error(f"LLM connection error: {e}")
            raise LLMError(f"Connection failed: {e}")
        except Exception as e:
            logger.error(f"LLM request error: {e}")
            raise LLMError(f"Request failed: {e}")
    
    async def _stream_chat(self, payload: Dict[str, Any]) -> AsyncGenerator[str, None]:
        """Stream chat completion response."""
        session = await self._get_session()
        
        try:
            async with session.post(
                f"{self.base_url}/v1/chat/completions",
                json=payload
            ) as response:
                if response.status != 200:
                    error_text = await response.text()
                    raise LLMError(f"Streaming failed: HTTP {response.status} - {error_text}")
                
                async for line in response.content:
                    line = line.decode("utf-8").strip()
                    
                    if not line or line == "data: [DONE]":
                        continue
                    
                    if line.startswith("data: "):
                        try:
                            data = json.loads(line[6:])
                            delta = data["choices"][0].get("delta", {})
                            
                            if "content" in delta:
                                yield delta["content"]
                            
                            # Handle tool call deltas
                            if "tool_calls" in delta:
                                for tc in delta["tool_calls"]:
                                    yield f"<tool>{tc['function']['name']}</tool>"
                                    
                        except json.JSONDecodeError:
                            continue
                            
        except Exception as e:
            logger.error(f"Streaming error: {e}")
            raise LLMError(f"Stream failed: {e}")
    
    async def get_embeddings(self, texts: List[str]) -> List[List[float]]:
        """Get embeddings for texts."""
        session = await self._get_session()
        
        try:
            async with session.post(
                f"{self.base_url}/v1/embeddings",
                json={"input": texts}
            ) as response:
                if response.status != 200:
                    raise LLMError(f"Embedding failed: HTTP {response.status}")
                
                data = await response.json()
                return [item["embedding"] for item in data["data"]]
                
        except Exception as e:
            logger.error(f"Embedding error: {e}")
            raise LLMError(f"Embedding failed: {e}")
    
    async def get_model_info(self) -> Dict[str, Any]:
        """Get model information."""
        session = await self._get_session()
        
        try:
            async with session.get(f"{self.base_url}/v1/models") as response:
                if response.status == 200:
                    return await response.json()
                return {}
        except Exception as e:
            logger.warning(f"Failed to get model info: {e}")
            return {}


class LLMError(Exception):
    """LLM client error."""
    pass


# Singleton instance
_llm_client: Optional[LLMClient] = None


async def get_llm_client() -> LLMClient:
    """Get or create LLM client singleton."""
    global _llm_client
    if _llm_client is None:
        _llm_client = LLMClient()
    return _llm_client


async def close_llm_client():
    """Close LLM client."""
    global _llm_client
    if _llm_client:
        await _llm_client.close()
        _llm_client = None
```


---

## RAG Integration Module

### File: `app/integrations/rag_client.py`

```python
"""
RAG client for ChromaDB-based retrieval.
Handles context retrieval and query preprocessing.
"""

import time
from typing import Any, Dict, List, Optional, Tuple

import aiohttp
from pydantic import BaseModel

from ..config import get_settings
from ..logging_config import get_logger
from ..models import ContextItem, ContextSource

logger = get_logger("rag_client")


class RAGQueryResult(BaseModel):
    """Result from RAG query."""
    documents: List[str]
    metadatas: List[Dict[str, Any]]
    distances: List[float]
    total_results: int


class RAGClient:
    """
    Client for RAG server (ChromaDB) communication.
    
    Features:
    - Semantic search with embeddings
    - Multiple collection support
    - Query preprocessing
    - Context formatting
    - Relevance scoring
    """
    
    def __init__(self):
        self.settings = get_settings().rag
        self.base_url = self.settings.base_url
        self.session: Optional[aiohttp.ClientSession] = None
        logger.info(f"RAG client initialized: {self.base_url}")
    
    async def _get_session(self) -> aiohttp.ClientSession:
        """Get or create aiohttp session."""
        if self.session is None or self.session.closed:
            timeout = aiohttp.ClientTimeout(total=self.settings.timeout)
            self.session = aiohttp.ClientSession(timeout=timeout)
        return self.session
    
    async def close(self):
        """Close the HTTP session."""
        if self.session and not self.session.closed:
            await self.session.close()
            logger.debug("RAG client session closed")
    
    async def health_check(self) -> Tuple[bool, Optional[str]]:
        """Check RAG server health."""
        try:
            session = await self._get_session()
            async with session.get(f"{self.base_url}/health") as response:
                if response.status == 200:
                    return True, None
                return False, f"HTTP {response.status}"
        except Exception as e:
            return False, str(e)
    
    def _preprocess_query(self, query: str) -> str:
        """
        Preprocess query for better retrieval.
        
        Transformations:
        - Remove unnecessary words
        - Expand contractions
        - Normalize technical terms
        """
        # Remove extra whitespace
        query = " ".join(query.split())
        
        # Expand common contractions
        contractions = {
            "what's": "what is",
            "how's": "how is",
            "it's": "it is",
            "that's": "that is",
            "there's": "there is",
            "where's": "where is",
            "who's": "who is",
            "don't": "do not",
            "doesn't": "does not",
            "didn't": "did not",
            "can't": "cannot",
            "couldn't": "could not",
            "won't": "will not",
            "wouldn't": "would not",
            "shouldn't": "should not",
            "isn't": "is not",
            "aren't": "are not",
            "wasn't": "was not",
            "weren't": "were not",
            "haven't": "have not",
            "hasn't": "has not",
            "hadn't": "had not",
        }
        
        query_lower = query.lower()
        for contraction, expansion in contractions.items():
            query_lower = query_lower.replace(contraction, expansion)
        
        return query_lower.strip()
    
    async def search(
        self,
        query: str,
        collection: Optional[str] = None,
        n_results: Optional[int] = None,
        filter_criteria: Optional[Dict[str, Any]] = None,
        include_metadata: bool = True
    ) -> RAGQueryResult:
        """
        Search for relevant documents.
        
        Args:
            query: Search query
            collection: Collection name (default from config)
            n_results: Number of results (default from config)
            filter_criteria: Metadata filters
            include_metadata: Include document metadata
            
        Returns:
            RAGQueryResult with documents and metadata
        """
        session = await self._get_session()
        
        # Preprocess query
        processed_query = self._preprocess_query(query)
        
        # Build request
        payload = {
            "query": processed_query,
            "collection": collection or self.settings.default_collection,
            "n_results": n_results or self.settings.max_results,
            "include_metadata": include_metadata
        }
        
        if filter_criteria:
            payload["filter"] = filter_criteria
        
        start_time = time.time()
        
        try:
            async with session.post(
                f"{self.base_url}/api/search",
                json=payload
            ) as response:
                if response.status != 200:
                    error_text = await response.text()
                    raise RAGError(f"Search failed: HTTP {response.status} - {error_text}")
                
                data = await response.json()
                
                elapsed = (time.time() - start_time) * 1000
                logger.info(
                    f"RAG search completed",
                    extra={
                        "query": query[:50],
                        "results": len(data.get("documents", [])),
                        "time_ms": elapsed
                    }
                )
                
                return RAGQueryResult(
                    documents=data.get("documents", []),
                    metadatas=data.get("metadatas", []),
                    distances=data.get("distances", []),
                    total_results=len(data.get("documents", []))
                )
                
        except aiohttp.ClientError as e:
            logger.error(f"RAG connection error: {e}")
            raise RAGError(f"Connection failed: {e}")
        except Exception as e:
            logger.error(f"RAG search error: {e}")
            raise RAGError(f"Search failed: {e}")
    
    async def get_context_items(
        self,
        query: str,
        collection: Optional[str] = None,
        n_results: Optional[int] = None,
        filter_criteria: Optional[Dict[str, Any]] = None,
        min_relevance: float = 0.0
    ) -> List[ContextItem]:
        """
        Get context items for injection into LLM prompt.
        
        Args:
            query: Search query
            collection: Collection name
            n_results: Number of results
            filter_criteria: Metadata filters
            min_relevance: Minimum relevance score threshold
            
        Returns:
            List of ContextItem objects
        """
        result = await self.search(
            query=query,
            collection=collection,
            n_results=n_results,
            filter_criteria=filter_criteria
        )
        
        context_items = []
        
        for doc, metadata, distance in zip(
            result.documents,
            result.metadatas,
            result.distances
        ):
            # Convert distance to similarity score (assuming cosine distance)
            relevance = 1.0 - distance
            
            if relevance >= min_relevance:
                context_items.append(ContextItem(
                    source=ContextSource.RAG,
                    content=doc,
                    metadata=metadata,
                    relevance_score=relevance
                ))
        
        # Sort by relevance
        context_items.sort(key=lambda x: x.relevance_score or 0, reverse=True)
        
        logger.info(
            f"Retrieved {len(context_items)} context items",
            extra={"query": query[:50]}
        )
        
        return context_items
    
    async def add_documents(
        self,
        documents: List[str],
        metadatas: Optional[List[Dict[str, Any]]] = None,
        ids: Optional[List[str]] = None,
        collection: Optional[str] = None
    ) -> bool:
        """
        Add documents to RAG collection.
        
        Args:
            documents: List of documents to add
            metadatas: Optional metadata for each document
            ids: Optional IDs for documents
            collection: Target collection
            
        Returns:
            True if successful
        """
        session = await self._get_session()
        
        payload = {
            "documents": documents,
            "collection": collection or self.settings.default_collection
        }
        
        if metadatas:
            payload["metadatas"] = metadatas
        if ids:
            payload["ids"] = ids
        
        try:
            async with session.post(
                f"{self.base_url}/api/documents",
                json=payload
            ) as response:
                if response.status == 200:
                    logger.info(f"Added {len(documents)} documents to RAG")
                    return True
                else:
                    error_text = await response.text()
                    raise RAGError(f"Add failed: {error_text}")
                    
        except Exception as e:
            logger.error(f"Add documents error: {e}")
            raise RAGError(f"Failed to add documents: {e}")
    
    async def list_collections(self) -> List[str]:
        """List available collections."""
        session = await self._get_session()
        
        try:
            async with session.get(f"{self.base_url}/api/collections") as response:
                if response.status == 200:
                    data = await response.json()
                    return data.get("collections", [])
                return []
        except Exception as e:
            logger.warning(f"Failed to list collections: {e}")
            return []
    
    async def get_collection_info(self, collection: Optional[str] = None) -> Dict[str, Any]:
        """Get collection information."""
        session = await self._get_session()
        
        coll = collection or self.settings.default_collection
        
        try:
            async with session.get(
                f"{self.base_url}/api/collections/{coll}"
            ) as response:
                if response.status == 200:
                    return await response.json()
                return {}
        except Exception as e:
            logger.warning(f"Failed to get collection info: {e}")
            return {}


class RAGError(Exception):
    """RAG client error."""
    pass


# Singleton instance
_rag_client: Optional[RAGClient] = None


async def get_rag_client() -> RAGClient:
    """Get or create RAG client singleton."""
    global _rag_client
    if _rag_client is None:
        _rag_client = RAGClient()
    return _rag_client


async def close_rag_client():
    """Close RAG client."""
    global _rag_client
    if _rag_client:
        await _rag_client.close()
        _rag_client = None
```

---

## MCP Tool Manager

### File: `app/integrations/mcp_manager.py`

```python
"""
MCP (Model Context Protocol) Tool Manager.
Handles tool discovery, registration, and execution.
"""

import asyncio
import json
import time
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Callable

import aiohttp
from pydantic import BaseModel

from ..config import get_settings
from ..logging_config import get_logger
from ..models import ToolCall, ToolInfo, ToolResult

logger = get_logger("mcp_manager")


@dataclass
class RegisteredTool:
    """Internal representation of a registered tool."""
    info: ToolInfo
    server_url: str
    handler: Optional[Callable] = None
    last_used: float = field(default_factory=time.time)
    call_count: int = 0
    avg_execution_time: float = 0.0


class MCPManager:
    """
    Manager for MCP (Model Context Protocol) tools.
    
    Features:
    - Dynamic tool discovery from multiple MCP servers
    - Tool registration and caching
    - Concurrent tool execution with rate limiting
    - Execution statistics tracking
    - Health monitoring
    """
    
    def __init__(self):
        self.settings = get_settings().mcp
        self.tools: Dict[str, RegisteredTool] = {}
        self.server_tools: Dict[str, List[str]] = {}  # server_url -> tool names
        self.session: Optional[aiohttp.ClientSession] = None
        self._semaphore = asyncio.Semaphore(self.settings.max_concurrent)
        self._discovery_task: Optional[asyncio.Task] = None
        logger.info(f"MCP Manager initialized with {len(self.settings.servers)} servers")
    
    async def _get_session(self) -> aiohttp.ClientSession:
        """Get or create aiohttp session."""
        if self.session is None or self.session.closed:
            timeout = aiohttp.ClientTimeout(total=self.settings.timeout)
            self.session = aiohttp.ClientSession(timeout=timeout)
        return self.session
    
    async def close(self):
        """Close the HTTP session."""
        if self._discovery_task and not self._discovery_task.done():
            self._discovery_task.cancel()
            
        if self.session and not self.session.closed:
            await self.session.close()
            logger.debug("MCP Manager session closed")
    
    async def discover_tools(self) -> int:
        """
        Discover tools from all configured MCP servers.
        
        Returns:
            Number of tools discovered
        """
        session = await self._get_session()
        discovered_count = 0
        
        for server_url in self.settings.servers:
            try:
                logger.info(f"Discovering tools from {server_url}")
                
                async with session.get(f"{server_url}/tools") as response:
                    if response.status != 200:
                        logger.warning(f"Failed to discover tools from {server_url}: HTTP {response.status}")
                        continue
                    
                    data = await response.json()
                    tools_data = data.get("tools", [])
                    
                    server_tool_names = []
                    for tool_data in tools_data:
                        tool_info = ToolInfo(
                            name=tool_data["name"],
                            description=tool_data.get("description", ""),
                            parameters=tool_data.get("parameters", {}),
                            required=tool_data.get("required", [])
                        )
                        
                        # Register tool
                        self.tools[tool_info.name] = RegisteredTool(
                            info=tool_info,
                            server_url=server_url
                        )
                        server_tool_names.append(tool_info.name)
                        discovered_count += 1
                    
                    self.server_tools[server_url] = server_tool_names
                    logger.info(f"Discovered {len(server_tool_names)} tools from {server_url}")
                    
            except Exception as e:
                logger.error(f"Error discovering tools from {server_url}: {e}")
        
        logger.info(f"Total tools discovered: {discovered_count}")
        return discovered_count
    
    async def start_periodic_discovery(self, interval_seconds: int = 300):
        """Start periodic tool discovery in background."""
        async def _discover_loop():
            while True:
                try:
                    await self.discover_tools()
                    await asyncio.sleep(interval_seconds)
                except asyncio.CancelledError:
                    break
                except Exception as e:
                    logger.error(f"Discovery loop error: {e}")
                    await asyncio.sleep(interval_seconds)
        
        self._discovery_task = asyncio.create_task(_discover_loop())
        logger.info(f"Started periodic discovery every {interval_seconds}s")
    
    def get_tool(self, name: str) -> Optional[ToolInfo]:
        """Get tool information by name."""
        tool = self.tools.get(name)
        return tool.info if tool else None
    
    def list_tools(self) -> List[ToolInfo]:
        """List all available tools."""
        return [tool.info for tool in self.tools.values()]
    
    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        """
        Get tool schemas for LLM function calling.
        
        Returns:
            List of tool definitions in OpenAI format
        """
        schemas = []
        for tool in self.tools.values():
            schema = {
                "type": "function",
                "function": {
                    "name": tool.info.name,
                    "description": tool.info.description,
                    "parameters": {
                        "type": "object",
                        "properties": tool.info.parameters,
                        "required": tool.info.required
                    }
                }
            }
            schemas.append(schema)
        return schemas
    
    async def execute_tool(self, tool_call: ToolCall) -> ToolResult:
        """
        Execute a tool call.
        
        Args:
            tool_call: Tool call to execute
            
        Returns:
            ToolResult with execution outcome
        """
        async with self._semaphore:
            tool = self.tools.get(tool_call.name)
            
            if not tool:
                return ToolResult(
                    call_id=tool_call.id,
                    name=tool_call.name,
                    success=False,
                    error=f"Tool '{tool_call.name}' not found"
                )
            
            start_time = time.time()
            
            try:
                logger.info(f"Executing tool: {tool_call.name}")
                
                # If tool has local handler, use it
                if tool.handler:
                    result = await tool.handler(**tool_call.arguments)
                else:
                    # Call remote MCP server
                    result = await self._call_remote_tool(tool, tool_call.arguments)
                
                execution_time = (time.time() - start_time) * 1000
                
                # Update statistics
                tool.last_used = time.time()
                tool.call_count += 1
                tool.avg_execution_time = (
                    (tool.avg_execution_time * (tool.call_count - 1) + execution_time)
                    / tool.call_count
                )
                
                logger.info(
                    f"Tool executed: {tool_call.name}",
                    extra={"time_ms": execution_time}
                )
                
                return ToolResult(
                    call_id=tool_call.id,
                    name=tool_call.name,
                    success=True,
                    result=result,
                    execution_time_ms=execution_time
                )
                
            except Exception as e:
                execution_time = (time.time() - start_time) * 1000
                logger.error(f"Tool execution failed: {tool_call.name} - {e}")
                
                return ToolResult(
                    call_id=tool_call.id,
                    name=tool_call.name,
                    success=False,
                    error=str(e),
                    execution_time_ms=execution_time
                )
    
    async def execute_tools(self, tool_calls: List[ToolCall]) -> List[ToolResult]:
        """
        Execute multiple tool calls concurrently.
        
        Args:
            tool_calls: List of tool calls to execute
            
        Returns:
            List of tool results
        """
        if not tool_calls:
            return []
        
        logger.info(f"Executing {len(tool_calls)} tools concurrently")
        
        tasks = [self.execute_tool(tc) for tc in tool_calls]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        
        # Convert exceptions to error results
        processed_results = []
        for i, result in enumerate(results):
            if isinstance(result, Exception):
                processed_results.append(ToolResult(
                    call_id=tool_calls[i].id,
                    name=tool_calls[i].name,
                    success=False,
                    error=str(result)
                ))
            else:
                processed_results.append(result)
        
        return processed_results
    
    async def _call_remote_tool(
        self,
        tool: RegisteredTool,
        arguments: Dict[str, Any]
    ) -> Any:
        """Call a tool on a remote MCP server."""
        session = await self._get_session()
        
        payload = {
            "tool": tool.info.name,
            "arguments": arguments
        }
        
        async with session.post(
            f"{tool.server_url}/execute",
            json=payload
        ) as response:
            if response.status != 200:
                error_text = await response.text()
                raise MCPError(f"Tool call failed: HTTP {response.status} - {error_text}")
            
            data = await response.json()
            
            if not data.get("success", False):
                raise MCPError(data.get("error", "Unknown error"))
            
            return data.get("result")
    
    def register_local_tool(
        self,
        name: str,
        description: str,
        parameters: Dict[str, Any],
        handler: Callable,
        required: Optional[List[str]] = None
    ):
        """
        Register a local tool handler.
        
        Args:
            name: Tool name
            description: Tool description
            parameters: JSON schema for parameters
            handler: Async function to handle tool calls
            required: Required parameter names
        """
        tool_info = ToolInfo(
            name=name,
            description=description,
            parameters=parameters,
            required=required or []
        )
        
        self.tools[name] = RegisteredTool(
            info=tool_info,
            server_url="local",
            handler=handler
        )
        
        logger.info(f"Registered local tool: {name}")
    
    def get_statistics(self) -> Dict[str, Any]:
        """Get tool usage statistics."""
        return {
            "total_tools": len(self.tools),
            "total_servers": len(self.server_tools),
            "tools": {
                name: {
                    "call_count": tool.call_count,
                    "avg_execution_time_ms": tool.avg_execution_time,
                    "last_used": tool.last_used
                }
                for name, tool in self.tools.items()
            }
        }
    
    async def health_check(self) -> Dict[str, Any]:
        """Check health of all MCP servers."""
        session = await self._get_session()
        results = {}
        
        for server_url in self.settings.servers:
            try:
                async with session.get(f"{server_url}/health") as response:
                    results[server_url] = {
                        "healthy": response.status == 200,
                        "status": response.status
                    }
            except Exception as e:
                results[server_url] = {
                    "healthy": False,
                    "error": str(e)
                }
        
        return results


class MCPError(Exception):
    """MCP manager error."""
    pass


# Singleton instance
_mcp_manager: Optional[MCPManager] = None


async def get_mcp_manager() -> MCPManager:
    """Get or create MCP manager singleton."""
    global _mcp_manager
    if _mcp_manager is None:
        _mcp_manager = MCPManager()
        # Discover tools on first use
        await _mcp_manager.discover_tools()
        # Start periodic discovery
        await _mcp_manager.start_periodic_discovery()
    return _mcp_manager


async def close_mcp_manager():
    """Close MCP manager."""
    global _mcp_manager
    if _mcp_manager:
        await _mcp_manager.close()
        _mcp_manager = None
```

---

## LSP Integration Module

### File: `app/integrations/lsp_client.py`

```python
"""
LSP (Language Server Protocol) client wrapper.
Provides code intelligence features for the orchestrator.
"""

import asyncio
import json
import subprocess
import time
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

from ..config import get_settings
from ..logging_config import get_logger
from ..models import ContextItem, ContextSource

logger = get_logger("lsp_client")


class LSPClient:
    """
    Client for LSP (Language Server Protocol) integration.
    
    Features:
    - Connect to language servers (pylsp, typescript-language-server, etc.)
    - Extract code context (definitions, references, diagnostics)
    - Code analysis and suggestions
    - Document symbols and workspace symbols
    """
    
    # LSP method names
    METHOD_INITIALIZE = "initialize"
    METHOD_SHUTDOWN = "shutdown"
    METHOD_EXIT = "exit"
    METHOD_HOVER = "textDocument/hover"
    METHOD_DEFINITION = "textDocument/definition"
    METHOD_REFERENCES = "textDocument/references"
    METHOD_DOCUMENT_SYMBOL = "textDocument/documentSymbol"
    METHOD_DIAGNOSTICS = "textDocument/publishDiagnostics"
    METHOD_COMPLETION = "textDocument/completion"
    
    def __init__(self):
        self.settings = get_settings().lsp
        self.processes: Dict[str, subprocess.Popen] = {}
        self.message_id = 0
        self.initialized = False
        self.capabilities: Dict[str, Any] = {}
        
        if not self.settings.enabled:
            logger.info("LSP integration disabled")
            return
            
        logger.info(f"LSP client initialized with {len(self.settings.servers)} servers")
    
    async def start_server(
        self,
        language: str,
        command: List[str],
        workspace_path: Optional[str] = None
    ) -> bool:
        """
        Start a language server process.
        
        Args:
            language: Language identifier (python, typescript, etc.)
            command: Command to start the server
            workspace_path: Optional workspace root path
            
        Returns:
            True if server started successfully
        """
        if not self.settings.enabled:
            return False
        
        try:
            logger.info(f"Starting LSP server for {language}: {' '.join(command)}")
            
            process = subprocess.Popen(
                command,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                bufsize=1
            )
            
            self.processes[language] = process
            
            # Initialize the server
            await self._initialize(language, workspace_path)
            
            logger.info(f"LSP server for {language} started and initialized")
            return True
            
        except Exception as e:
            logger.error(f"Failed to start LSP server for {language}: {e}")
            return False
    
    async def _initialize(self, language: str, workspace_path: Optional[str] = None):
        """Send initialize request to LSP server."""
        root_path = workspace_path or str(Path.home())
        
        params = {
            "processId": None,
            "rootPath": root_path,
            "rootUri": f"file://{root_path}",
            "capabilities": {
                "textDocument": {
                    "synchronization": {"dynamicRegistration": False},
                    "completion": {"dynamicRegistration": False},
                    "hover": {"dynamicRegistration": False},
                    "definition": {"dynamicRegistration": False},
                    "documentSymbol": {"dynamicRegistration": False},
                    "codeAction": {"dynamicRegistration": False},
                    "formatting": {"dynamicRegistration": False}
                },
                "workspace": {
                    "workspaceFolders": False,
                    "configuration": False
                }
            },
            "workspaceFolders": None
        }
        
        response = await self._send_request(language, self.METHOD_INITIALIZE, params)
        
        if response:
            self.capabilities[language] = response.get("capabilities", {})
            self.initialized = True
    
    async def _send_request(
        self,
        language: str,
        method: str,
        params: Dict[str, Any]
    ) -> Optional[Dict[str, Any]]:
        """Send JSON-RPC request to LSP server."""
        process = self.processes.get(language)
        if not process or process.poll() is not None:
            logger.error(f"LSP server for {language} not running")
            return None
        
        self.message_id += 1
        message = {
            "jsonrpc": "2.0",
            "id": self.message_id,
            "method": method,
            "params": params
        }
        
        message_str = json.dumps(message)
        header = f"Content-Length: {len(message_str)}\r\n\r\n"
        
        try:
            if process.stdin:
                process.stdin.write(header + message_str)
                process.stdin.flush()
            
            # Read response
            response = await self._read_response(language)
            return response
            
        except Exception as e:
            logger.error(f"LSP request failed: {e}")
            return None
    
    async def _read_response(self, language: str) -> Optional[Dict[str, Any]]:
        """Read JSON-RPC response from LSP server."""
        process = self.processes.get(language)
        if not process or not process.stdout:
            return None
        
        try:
            # Read header
            header = ""
            while True:
                char = process.stdout.read(1)
                if not char:
                    break
                header += char
                if header.endswith("\r\n\r\n"):
                    break
            
            # Parse Content-Length
            content_length = 0
            for line in header.split("\r\n"):
                if line.startswith("Content-Length:"):
                    content_length = int(line.split(":")[1].strip())
                    break
            
            if content_length == 0:
                return None
            
            # Read content
            content = process.stdout.read(content_length)
            return json.loads(content)
            
        except Exception as e:
            logger.error(f"Failed to read LSP response: {e}")
            return None
    
    async def get_hover_info(
        self,
        language: str,
        file_path: str,
        line: int,
        character: int
    ) -> Optional[str]:
        """
        Get hover information for a position in a file.
        
        Args:
            language: Language identifier
            file_path: Path to the file
            line: Line number (0-indexed)
            character: Character position (0-indexed)
            
        Returns:
            Hover information as markdown string
        """
        if language not in self.processes:
            return None
        
        params = {
            "textDocument": {"uri": f"file://{file_path}"},
            "position": {"line": line, "character": character}
        }
        
        response = await self._send_request(language, self.METHOD_HOVER, params)
        
        if response and "result" in response:
            contents = response["result"].get("contents", "")
            if isinstance(contents, dict):
                return contents.get("value", "")
            return str(contents)
        
        return None
    
    async def get_definition(
        self,
        language: str,
        file_path: str,
        line: int,
        character: int
    ) -> List[Dict[str, Any]]:
        """
        Get definition locations for a symbol.
        
        Returns:
            List of location dictionaries
        """
        if language not in self.processes:
            return []
        
        params = {
            "textDocument": {"uri": f"file://{file_path}"},
            "position": {"line": line, "character": character}
        }
        
        response = await self._send_request(language, self.METHOD_DEFINITION, params)
        
        if response and "result" in response:
            result = response["result"]
            if isinstance(result, list):
                return result
            elif isinstance(result, dict):
                return [result]
        
        return []
    
    async def get_document_symbols(
        self,
        language: str,
        file_path: str
    ) -> List[Dict[str, Any]]:
        """
        Get symbols defined in a document.
        
        Returns:
            List of symbol information
        """
        if language not in self.processes:
            return []
        
        params = {
            "textDocument": {"uri": f"file://{file_path}"}
        }
        
        response = await self._send_request(language, self.METHOD_DOCUMENT_SYMBOL, params)
        
        if response and "result" in response:
            return response["result"]
        
        return []
    
    async def get_code_context(
        self,
        file_path: str,
        language: Optional[str] = None,
        include_symbols: bool = True,
        include_diagnostics: bool = True
    ) -> List[ContextItem]:
        """
        Extract comprehensive code context from a file.
        
        Args:
            file_path: Path to the source file
            language: Language identifier (auto-detected if None)
            include_symbols: Include document symbols
            include_diagnostics: Include diagnostic information
            
        Returns:
            List of context items
        """
        if not self.settings.enabled:
            return []
        
        # Auto-detect language from extension
        if language is None:
            ext = Path(file_path).suffix.lower()
            language_map = {
                ".py": "python",
                ".js": "javascript",
                ".ts": "typescript",
                ".tsx": "typescript",
                ".jsx": "javascript",
                ".java": "java",
                ".go": "go",
                ".rs": "rust",
                ".cpp": "cpp",
                ".c": "c",
                ".h": "c",
                ".hpp": "cpp"
            }
            language = language_map.get(ext)
        
        if not language or language not in self.processes:
            return []
        
        context_items = []
        
        try:
            # Get document symbols
            if include_symbols:
                symbols = await self.get_document_symbols(language, file_path)
                for symbol in symbols:
                    context_items.append(ContextItem(
                        source=ContextSource.LSP,
                        content=f"Symbol: {symbol.get('name', '')} ({symbol.get('kind', 'unknown')})",
                        metadata={
                            "file": file_path,
                            "language": language,
                            "symbol": symbol
                        }
                    ))
            
            # Get file content overview
            try:
                with open(file_path, 'r') as f:
                    lines = f.readlines()
                    
                # Add file overview
                context_items.append(ContextItem(
                    source=ContextSource.LSP,
                    content=f"File: {file_path}\nLines: {len(lines)}\nLanguage: {language}",
                    metadata={
                        "file": file_path,
                        "language": language,
                        "line_count": len(lines)
                    }
                ))
            except Exception as e:
                logger.warning(f"Could not read file {file_path}: {e}")
            
        except Exception as e:
            logger.error(f"Error getting code context: {e}")
        
        return context_items
    
    async def stop_server(self, language: str):
        """Stop a language server."""
        process = self.processes.get(language)
        if process:
            try:
                # Send shutdown request
                await self._send_request(language, self.METHOD_SHUTDOWN, {})
                
                # Send exit notification
                if process.stdin:
                    exit_msg = json.dumps({"jsonrpc": "2.0", "method": self.METHOD_EXIT})
                    header = f"Content-Length: {len(exit_msg)}\r\n\r\n"
                    process.stdin.write(header + exit_msg)
                    process.stdin.flush()
                
                # Wait for process to terminate
                process.wait(timeout=5)
                
            except Exception as e:
                logger.warning(f"Error stopping LSP server: {e}")
                process.terminate()
            
            del self.processes[language]
            logger.info(f"LSP server for {language} stopped")
    
    async def stop_all_servers(self):
        """Stop all language servers."""
        for language in list(self.processes.keys()):
            await self.stop_server(language)
    
    def health_check(self) -> Dict[str, Any]:
        """Check health of all LSP servers."""
        results = {}
        for language, process in self.processes.items():
            is_running = process.poll() is None
            results[language] = {
                "running": is_running,
                "pid": process.pid if is_running else None
            }
        return results


# Singleton instance
_lsp_client: Optional[LSPClient] = None


async def get_lsp_client() -> LSPClient:
    """Get or create LSP client singleton."""
    global _lsp_client
    if _lsp_client is None:
        _lsp_client = LSPClient()
    return _lsp_client


async def close_lsp_client():
    """Close LSP client and stop all servers."""
    global _lsp_client
    if _lsp_client:
        await _lsp_client.stop_all_servers()
        _lsp_client = None
```


---

## Agent Loop Implementation

### File: `app/agent/loop.py`

```python
"""
Main Agent Loop - The core orchestration logic.
Coordinates LLM, RAG, MCP tools, and LSP for intelligent responses.
"""

import time
from typing import Any, AsyncGenerator, Dict, List, Optional, Tuple

from ..config import get_settings
from ..integrations.llm_client import LLMClient, LLMResponse, get_llm_client
from ..integrations.mcp_manager import MCPManager, get_mcp_manager
from ..integrations.rag_client import RAGClient, get_rag_client
from ..logging_config import get_logger
from ..models import (
    ChatRequest, ChatResponse, ContextItem, Message, MessageRole,
    StreamChunk, ToolCall, ToolResult
)
from ..prompts.templates import PromptBuilder
from ..state import StateManager, get_state_manager

logger = get_logger("agent_loop")


class AgentLoop:
    """
    Main agent loop implementing the ReAct (Reasoning + Acting) pattern.
    
    The loop follows this flow:
    1. Receive user query
    2. Retrieve RAG context (if enabled)
    3. Build prompt with context and tools
    4. Call LLM for reasoning
    5. Detect and execute tool calls
    6. Generate final response
    7. Update conversation state
    
    Features:
    - Multi-turn conversation support
    - Tool chaining
    - Streaming responses
    - Error recovery
    - Context window management
    """
    
    def __init__(
        self,
        llm_client: Optional[LLMClient] = None,
        rag_client: Optional[RAGClient] = None,
        mcp_manager: Optional[MCPManager] = None,
        state_manager: Optional[StateManager] = None
    ):
        self.llm = llm_client
        self.rag = rag_client
        self.mcp = mcp_manager
        self.state = state_manager
        self.prompt_builder = PromptBuilder()
        self.settings = get_settings()
        
        logger.info("Agent loop initialized")
    
    async def initialize(self):
        """Initialize all dependencies."""
        if self.llm is None:
            self.llm = await get_llm_client()
        if self.rag is None:
            self.rag = await get_rag_client()
        if self.mcp is None:
            self.mcp = await get_mcp_manager()
        if self.state is None:
            self.state = get_state_manager()
        
        logger.info("Agent loop dependencies initialized")
    
    async def process(
        self,
        request: ChatRequest
    ) -> ChatResponse:
        """
        Process a chat request through the agent loop.
        
        Args:
            request: Chat request with message and options
            
        Returns:
            Chat response with assistant message
        """
        start_time = time.time()
        
        # Ensure initialization
        await self.initialize()
        
        # Get or create session
        session_id = request.session_id or self.state.create_session()
        conversation = self.state.get_conversation(session_id)
        
        logger.info(
            f"Processing chat request",
            extra={
                "session_id": session_id,
                "message_length": len(request.message),
                "use_tools": request.use_tools,
                "use_rag": request.use_rag
            }
        )
        
        try:
            # Step 1: Add user message to conversation
            user_message = Message(
                role=MessageRole.USER,
                content=request.message
            )
            conversation.messages.append(user_message)
            
            # Step 2: Retrieve RAG context (if enabled)
            context_items: List[ContextItem] = []
            if request.use_rag and self.rag:
                try:
                    context_items = await self.rag.get_context_items(
                        query=request.message,
                        filter_criteria=request.context_filter,
                        min_relevance=0.5
                    )
                    conversation.context.extend(context_items)
                except Exception as e:
                    logger.warning(f"RAG retrieval failed: {e}")
            
            # Step 3: Get available tools (if enabled)
            tools = []
            if request.use_tools and self.mcp:
                tools = self.mcp.get_tool_schemas()
            
            # Step 4: Build prompt with context
            messages = self.prompt_builder.build_messages(
                conversation=conversation,
                context_items=context_items,
                include_system=True
            )
            
            # Step 5: Call LLM
            llm_response = await self.llm.chat(
                messages=messages,
                tools=tools if tools else None,
                temperature=request.temperature,
                max_tokens=request.max_tokens,
                stream=False
            )
            
            # Step 6: Execute tool calls if present
            tool_results: List[ToolResult] = []
            if llm_response.tool_calls and self.mcp:
                tool_results = await self.mcp.execute_tools(llm_response.tool_calls)
                
                # If tools were called, make another LLM call with results
                if tool_results:
                    # Add assistant message with tool calls
                    assistant_message = Message(
                        role=MessageRole.ASSISTANT,
                        content=llm_response.content,
                        tool_calls=llm_response.tool_calls
                    )
                    conversation.messages.append(assistant_message)
                    
                    # Add tool results as separate messages
                    for result in tool_results:
                        tool_message = Message(
                            role=MessageRole.TOOL,
                            content=self._format_tool_result(result)
                        )
                        conversation.messages.append(tool_message)
                    
                    # Rebuild prompt with tool results
                    messages = self.prompt_builder.build_messages(
                        conversation=conversation,
                        context_items=[],
                        include_system=True
                    )
                    
                    # Second LLM call for final response
                    llm_response = await self.llm.chat(
                        messages=messages,
                        temperature=request.temperature,
                        max_tokens=request.max_tokens,
                        stream=False
                    )
            
            # Step 7: Create final assistant message
            final_message = Message(
                role=MessageRole.ASSISTANT,
                content=llm_response.content,
                tool_calls=llm_response.tool_calls if not tool_results else None,
                tool_results=tool_results if tool_results else None
            )
            conversation.messages.append(final_message)
            
            # Step 8: Update state
            self.state.update_conversation(session_id, conversation)
            
            # Step 9: Build response
            processing_time = (time.time() - start_time) * 1000
            
            response = ChatResponse(
                session_id=session_id,
                message=final_message,
                tool_calls=llm_response.tool_calls,
                tool_results=tool_results,
                context_used=context_items,
                tokens_used=llm_response.tokens_used,
                processing_time_ms=processing_time
            )
            
            logger.info(
                f"Chat request completed",
                extra={
                    "session_id": session_id,
                    "processing_time_ms": processing_time,
                    "tokens_used": llm_response.tokens_used,
                    "tool_calls": len(llm_response.tool_calls),
                    "context_items": len(context_items)
                }
            )
            
            return response
            
        except Exception as e:
            logger.error(f"Agent loop error: {e}", exc_info=True)
            
            # Return error response
            return ChatResponse(
                session_id=session_id,
                message=Message(
                    role=MessageRole.ASSISTANT,
                    content=f"I encountered an error while processing your request: {str(e)}"
                ),
                processing_time_ms=(time.time() - start_time) * 1000
            )
    
    async def process_stream(
        self,
        request: ChatRequest
    ) -> AsyncGenerator[StreamChunk, None]:
        """
        Process a chat request with streaming response.
        
        Yields:
            StreamChunk objects for each part of the response
        """
        session_id = request.session_id or self.state.create_session()
        
        try:
            await self.initialize()
            
            # Get conversation and context
            conversation = self.state.get_conversation(session_id)
            
            # Add user message
            user_message = Message(
                role=MessageRole.USER,
                content=request.message
            )
            conversation.messages.append(user_message)
            
            # Get RAG context
            context_items: List[ContextItem] = []
            if request.use_rag and self.rag:
                try:
                    context_items = await self.rag.get_context_items(
                        query=request.message,
                        min_relevance=0.5
                    )
                except Exception as e:
                    logger.warning(f"RAG retrieval failed: {e}")
            
            # Get tools
            tools = []
            if request.use_tools and self.mcp:
                tools = self.mcp.get_tool_schemas()
            
            # Build prompt
            messages = self.prompt_builder.build_messages(
                conversation=conversation,
                context_items=context_items,
                include_system=True
            )
            
            # Stream LLM response
            accumulated_content = ""
            async for chunk in self.llm.chat(
                messages=messages,
                tools=tools if tools else None,
                stream=True
            ):
                accumulated_content += chunk
                yield StreamChunk(
                    type="content",
                    data=chunk,
                    session_id=session_id
                )
            
            # Detect tool calls in accumulated content
            # (Simplified - in production, use proper parsing)
            
            # Save final message
            final_message = Message(
                role=MessageRole.ASSISTANT,
                content=accumulated_content
            )
            conversation.messages.append(final_message)
            self.state.update_conversation(session_id, conversation)
            
            # Yield completion
            yield StreamChunk(
                type="done",
                data={"session_id": session_id},
                session_id=session_id
            )
            
        except Exception as e:
            logger.error(f"Streaming error: {e}")
            yield StreamChunk(
                type="error",
                data={"error": str(e)},
                session_id=session_id
            )
    
    def _format_tool_result(self, result: ToolResult) -> str:
        """Format a tool result for inclusion in conversation."""
        if result.success:
            return f"Tool '{result.name}' result:\n```\n{result.result}\n```"
        else:
            return f"Tool '{result.name}' failed: {result.error}"
    
    async def execute_direct_tool(
        self,
        tool_name: str,
        arguments: Dict[str, Any]
    ) -> ToolResult:
        """
        Execute a tool directly without going through the full agent loop.
        
        Args:
            tool_name: Name of the tool to execute
            arguments: Tool arguments
            
        Returns:
            Tool execution result
        """
        await self.initialize()
        
        tool_call = ToolCall(name=tool_name, arguments=arguments)
        results = await self.mcp.execute_tools([tool_call])
        
        return results[0] if results else ToolResult(
            call_id="",
            name=tool_name,
            success=False,
            error="No result returned"
        )


# Singleton instance
_agent_loop: Optional[AgentLoop] = None


async def get_agent_loop() -> AgentLoop:
    """Get or create agent loop singleton."""
    global _agent_loop
    if _agent_loop is None:
        _agent_loop = AgentLoop()
        await _agent_loop.initialize()
    return _agent_loop
```

### File: `app/agent/planner.py`

```python
"""
Query planner for the agent loop.
Analyzes user queries to determine the best execution strategy.
"""

import re
from dataclasses import dataclass
from enum import Enum
from typing import List, Optional

from ..logging_config import get_logger

logger = get_logger("agent_planner")


class QueryType(Enum):
    """Types of user queries."""
    GENERAL = "general"           # General knowledge question
    CODE = "code"                 # Code-related query
    DOCUMENT = "document"         # Document retrieval
    TOOL = "tool"                 # Requires tool execution
    MULTI_STEP = "multi_step"     # Requires multiple steps
    CONVERSATION = "conversation" # Simple conversation


class ExecutionStrategy(Enum):
    """Execution strategies for queries."""
    DIRECT_LLM = "direct_llm"           # Direct LLM response
    RAG_THEN_LLM = "rag_then_llm"       # Retrieve context, then LLM
    TOOL_CHAIN = "tool_chain"           # Execute tool chain
    PLAN_AND_EXECUTE = "plan_and_execute"  # Plan then execute


@dataclass
class QueryPlan:
    """Plan for executing a query."""
    query_type: QueryType
    strategy: ExecutionStrategy
    requires_rag: bool
    requires_tools: bool
    suggested_tools: List[str]
    priority: int  # 1-10, higher = more urgent
    reasoning: str


class QueryPlanner:
    """
    Planner that analyzes queries and determines execution strategy.
    
    Uses heuristics and pattern matching to classify queries
    and select the appropriate execution path.
    """
    
    # Patterns for query classification
    CODE_PATTERNS = [
        r'\b(code|function|class|method|variable|import|error|exception|bug|debug|fix)\b',
        r'\b(python|javascript|typescript|java|go|rust|cpp|c#|sql)\b',
        r'\b(syntax|compile|runtime|type error|undefined|null)\b',
        r'```[\s\S]*?```',  # Code blocks
        r'`[^`]+`',  # Inline code
    ]
    
    TOOL_PATTERNS = [
        r'\b(search|find|look up|get|fetch|retrieve)\b',
        r'\b(calculate|compute|convert|translate)\b',
        r'\b(weather|news|stock|price|time|date)\b',
        r'\b(send|email|message|notify)\b',
    ]
    
    DOCUMENT_PATTERNS = [
        r'\b(document|file|pdf|doc|paper|article|report)\b',
        r'\b(according to|in the|from the|based on)\b',
        r'\b(what does.*say|what is.*about|explain.*document)\b',
    ]
    
    CONVERSATION_PATTERNS = [
        r'\b(hello|hi|hey|good morning|good afternoon)\b',
        r'\b(thank|thanks|please|sorry|excuse me)\b',
        r'\b(how are you|what\'s up|how do you do)\b',
    ]
    
    def __init__(self):
        self.code_regex = re.compile('|'.join(self.CODE_PATTERNS), re.IGNORECASE)
        self.tool_regex = re.compile('|'.join(self.TOOL_PATTERNS), re.IGNORECASE)
        self.document_regex = re.compile('|'.join(self.DOCUMENT_PATTERNS), re.IGNORECASE)
        self.conversation_regex = re.compile('|'.join(self.CONVERSATION_PATTERNS), re.IGNORECASE)
        logger.info("Query planner initialized")
    
    def analyze(self, query: str) -> QueryPlan:
        """
        Analyze a query and create an execution plan.
        
        Args:
            query: User query string
            
        Returns:
            QueryPlan with execution strategy
        """
        query_lower = query.lower()
        
        # Check for conversation patterns first (quick exit)
        if self._is_conversation(query_lower):
            return QueryPlan(
                query_type=QueryType.CONVERSATION,
                strategy=ExecutionStrategy.DIRECT_LLM,
                requires_rag=False,
                requires_tools=False,
                suggested_tools=[],
                priority=1,
                reasoning="Simple conversational query, direct response"
            )
        
        # Check for code patterns
        if self._is_code_related(query_lower):
            return QueryPlan(
                query_type=QueryType.CODE,
                strategy=ExecutionStrategy.RAG_THEN_LLM,
                requires_rag=True,
                requires_tools=False,
                suggested_tools=["code_search", "syntax_check"],
                priority=7,
                reasoning="Code-related query, may need context from codebase"
            )
        
        # Check for document patterns
        if self._is_document_query(query_lower):
            return QueryPlan(
                query_type=QueryType.DOCUMENT,
                strategy=ExecutionStrategy.RAG_THEN_LLM,
                requires_rag=True,
                requires_tools=False,
                suggested_tools=["document_search"],
                priority=6,
                reasoning="Document-related query, needs RAG retrieval"
            )
        
        # Check for tool patterns
        if self._requires_tools(query_lower):
            return QueryPlan(
                query_type=QueryType.TOOL,
                strategy=ExecutionStrategy.TOOL_CHAIN,
                requires_rag=False,
                requires_tools=True,
                suggested_tools=self._suggest_tools(query_lower),
                priority=8,
                reasoning="Query requires external tool execution"
            )
        
        # Default to general query with RAG
        return QueryPlan(
            query_type=QueryType.GENERAL,
            strategy=ExecutionStrategy.RAG_THEN_LLM,
            requires_rag=True,
            requires_tools=False,
            suggested_tools=[],
            priority=5,
            reasoning="General query, retrieve context for better response"
        )
    
    def _is_conversation(self, query: str) -> bool:
        """Check if query is simple conversation."""
        return bool(self.conversation_regex.search(query))
    
    def _is_code_related(self, query: str) -> bool:
        """Check if query is code-related."""
        return bool(self.code_regex.search(query))
    
    def _is_document_query(self, query: str) -> bool:
        """Check if query is about documents."""
        return bool(self.document_regex.search(query))
    
    def _requires_tools(self, query: str) -> bool:
        """Check if query likely requires tools."""
        return bool(self.tool_regex.search(query))
    
    def _suggest_tools(self, query: str) -> List[str]:
        """Suggest tools based on query content."""
        suggestions = []
        
        # Simple keyword-based suggestions
        if any(word in query for word in ['weather', 'temperature', 'forecast']):
            suggestions.append('weather')
        if any(word in query for word in ['search', 'find', 'look up']):
            suggestions.append('web_search')
        if any(word in query for word in ['calculate', 'compute', 'math']):
            suggestions.append('calculator')
        if any(word in query for word in ['time', 'date', 'calendar']):
            suggestions.append('datetime')
        if any(word in query for word in ['convert', 'unit', 'currency']):
            suggestions.append('converter')
        
        return suggestions
```

---

## Prompt Engineering

### File: `app/prompts/system_prompts.py`

```python
"""
System prompt definitions for the orchestrator.
"""

from typing import Dict, List


class SystemPrompts:
    """Collection of system prompts for different scenarios."""
    
    BASE_SYSTEM_PROMPT = """You are a helpful AI assistant powered by a local LLM. You have access to:

1. **Retrieval-Augmented Generation (RAG)**: Relevant context from documents is provided in the conversation.
2. **Tools**: You can use available tools to perform actions or retrieve information.

## Guidelines:

- Be concise and helpful in your responses
- Use the provided context to answer questions accurately
- When using tools, clearly indicate what you're doing
- If you don't know something, say so rather than making up information
- For code-related queries, provide clear explanations and examples
- Always format code blocks with appropriate language tags

## Tool Usage:

When you need to use a tool, format your response like this:

<tool>tool_name</tool>
<args>{"param1": "value1", "param2": "value2"}</args>

After the tool executes, you'll receive the result and can provide a final response."""

    CODE_ASSISTANT_PROMPT = """You are a code assistant with expertise in multiple programming languages.

## Capabilities:
- Explain code and concepts clearly
- Debug and fix errors
- Suggest improvements and best practices
- Generate code examples
- Analyze code structure

## Guidelines:
- Always provide code in properly formatted code blocks
- Explain your reasoning step by step
- Consider edge cases and error handling
- Follow language-specific best practices
- When suggesting fixes, explain why the fix works

## Response Format:
1. Brief explanation of the approach
2. Code solution with comments
3. Explanation of key parts
4. Any additional considerations"""

    TOOL_USE_PROMPT = """You have access to tools that can help answer questions and perform tasks.

## Available Tools:
{tool_descriptions}

## Tool Usage Format:
To use a tool, include in your response:

<tool>tool_name</tool>
<args>{"parameter": "value"}</args>

## Guidelines:
- Only use tools when necessary
- Choose the most appropriate tool for the task
- Provide all required parameters
- After tool execution, you'll receive results to incorporate into your response
- You can use multiple tools in sequence if needed

## Example:
User: "What's the weather in New York?"
Assistant: <tool>get_weather</tool>
<args>{"location": "New York"}</args>

[After receiving tool result]
Assistant: The weather in New York is currently 72°F and sunny."""

    RAG_CONTEXT_PROMPT = """The following context has been retrieved from documents to help answer the user's question:

---
{context}
---

Use this context to inform your response. If the context doesn't contain relevant information, rely on your general knowledge but indicate when you're doing so."""

    MULTI_TURN_PROMPT = """You are in a multi-turn conversation. Maintain context from previous messages and build upon them.

## Conversation History:
The full conversation history is provided. Refer back to previous topics when relevant and maintain continuity in the discussion.

## Guidelines:
- Remember details from earlier in the conversation
- Refer back to previous points when relevant
- If the user asks a follow-up question, use context from previous exchanges
- Acknowledge when you're making assumptions about context"""

    SAFETY_PROMPT = """Safety Guidelines:

1. **Harmful Content**: Do not generate content that promotes violence, illegal activities, or self-harm.

2. **Personal Information**: Do not request, store, or reveal personal information about individuals.

3. **Misinformation**: Avoid generating misleading or false information. If uncertain, indicate this clearly.

4. **Bias**: Strive to be neutral and avoid reinforcing harmful stereotypes.

5. **Privacy**: Respect user privacy and confidentiality.

If a request violates these guidelines, politely decline and explain why."""

    @classmethod
    def get_system_prompt(
        cls,
        mode: str = "default",
        tools: List[Dict] = None,
        include_safety: bool = True
    ) -> str:
        """
        Get a system prompt for a specific mode.
        
        Args:
            mode: Prompt mode (default, code, tool_use, rag)
            tools: Available tools for tool_use mode
            include_safety: Include safety guidelines
            
        Returns:
            Formatted system prompt
        """
        prompts = [cls.BASE_SYSTEM_PROMPT]
        
        if mode == "code":
            prompts.append(cls.CODE_ASSISTANT_PROMPT)
        elif mode == "tool_use" and tools:
            tool_desc = cls._format_tool_descriptions(tools)
            prompts.append(cls.TOOL_USE_PROMPT.format(tool_descriptions=tool_desc))
        
        if include_safety:
            prompts.append(cls.SAFETY_PROMPT)
        
        return "\n\n".join(prompts)
    
    @classmethod
    def _format_tool_descriptions(cls, tools: List[Dict]) -> str:
        """Format tool descriptions for prompt."""
        descriptions = []
        for tool in tools:
            func = tool.get("function", {})
            name = func.get("name", "unknown")
            desc = func.get("description", "No description")
            descriptions.append(f"- {name}: {desc}")
        return "\n".join(descriptions)
    
    @classmethod
    def get_rag_context_prompt(cls, context_items: List[str]) -> str:
        """Get formatted RAG context prompt."""
        context_text = "\n\n".join([
            f"[{i+1}] {item}" for i, item in enumerate(context_items)
        ])
        return cls.RAG_CONTEXT_PROMPT.format(context=context_text)
```

### File: `app/prompts/templates.py`

```python
"""
Prompt templates and builder for constructing LLM prompts.
"""

from typing import List, Optional

from ..config import get_settings
from ..models import ContextItem, ConversationState, Message, MessageRole
from ..logging_config import get_logger
from .system_prompts import SystemPrompts

logger = get_logger("prompt_builder")


class PromptBuilder:
    """
    Builder for constructing prompts for the LLM.
    
    Handles:
    - System prompt construction
    - Context injection
    - Message formatting
    - Token budget management
    """
    
    def __init__(self):
        self.settings = get_settings()
        self.max_context_tokens = self.settings.llm.context_window
        logger.info(f"Prompt builder initialized with {self.max_context_tokens} token budget")
    
    def build_messages(
        self,
        conversation: ConversationState,
        context_items: Optional[List[ContextItem]] = None,
        include_system: bool = True,
        mode: str = "default",
        tools: Optional[List[dict]] = None
    ) -> List[Message]:
        """
        Build messages for LLM prompt.
        
        Args:
            conversation: Current conversation state
            context_items: Retrieved context items
            include_system: Include system message
            mode: Prompt mode (default, code, tool_use)
            tools: Available tools
            
        Returns:
            List of messages ready for LLM
        """
        messages: List[Message] = []
        
        # Add system message
        if include_system:
            system_prompt = self._build_system_prompt(mode, tools)
            messages.append(Message(role=MessageRole.SYSTEM, content=system_prompt))
        
        # Add RAG context if available
        if context_items:
            context_message = self._build_context_message(context_items)
            if context_message:
                messages.append(context_message)
        
        # Add conversation history
        history_messages = self._build_history_messages(conversation)
        messages.extend(history_messages)
        
        # Apply token budget management
        messages = self._apply_token_budget(messages)
        
        return messages
    
    def _build_system_prompt(self, mode: str, tools: Optional[List[dict]]) -> str:
        """Build the system prompt."""
        return SystemPrompts.get_system_prompt(
            mode=mode,
            tools=tools,
            include_safety=True
        )
    
    def _build_context_message(self, context_items: List[ContextItem]) -> Optional[Message]:
        """Build a message with RAG context."""
        if not context_items:
            return None
        
        # Format context items
        context_texts = []
        for i, item in enumerate(context_items[:5]):  # Limit to top 5
            source_info = f"[Source: {item.source.value}"
            if item.relevance_score:
                source_info += f", Relevance: {item.relevance_score:.2f}"
            source_info += "]"
            
            context_texts.append(f"{source_info}\n{item.content}")
        
        context_str = "\n\n---\n\n".join(context_texts)
        
        content = f"""Relevant context retrieved for this query:

{context_str}

Use this information to help answer the user's question."""
        
        return Message(role=MessageRole.SYSTEM, content=content)
    
    def _build_history_messages(self, conversation: ConversationState) -> List[Message]:
        """Build messages from conversation history."""
        # Get recent messages (excluding system messages)
        recent_messages = [
            msg for msg in conversation.messages
            if msg.role != MessageRole.SYSTEM
        ]
        
        # Limit history length
        max_history = self.settings.state.max_history
        if len(recent_messages) > max_history:
            recent_messages = recent_messages[-max_history:]
        
        return recent_messages
    
    def _apply_token_budget(self, messages: List[Message]) -> List[Message]:
        """
        Apply token budget to messages.
        
        Simple implementation - in production, use a proper tokenizer.
        """
        # Rough estimate: 4 chars ≈ 1 token
        CHARS_PER_TOKEN = 4
        budget = self.max_context_tokens * 0.8  # Leave 20% for response
        
        total_chars = sum(len(msg.content) for msg in messages)
        estimated_tokens = total_chars / CHARS_PER_TOKEN
        
        if estimated_tokens <= budget:
            return messages
        
        # Need to trim - prioritize system and recent messages
        logger.warning(f"Token budget exceeded ({estimated_tokens:.0f} > {budget:.0f}), trimming...")
        
        # Keep system messages
        system_messages = [m for m in messages if m.role == MessageRole.SYSTEM]
        other_messages = [m for m in messages if m.role != MessageRole.SYSTEM]
        
        # Trim older messages from history
        trimmed = system_messages[:1]  # Keep first system message
        
        # Add recent messages until budget is reached
        current_chars = sum(len(m.content) for m in trimmed)
        for msg in reversed(other_messages):
            msg_chars = len(msg.content)
            if (current_chars + msg_chars) / CHARS_PER_TOKEN <= budget:
                trimmed.append(msg)
                current_chars += msg_chars
            else:
                break
        
        return list(reversed(trimmed))
    
    def build_tool_result_message(self, tool_name: str, result: any) -> str:
        """Build a message showing tool result."""
        if isinstance(result, dict):
            import json
            result_str = json.dumps(result, indent=2)
        else:
            result_str = str(result)
        
        return f"Tool '{tool_name}' returned:\n```\n{result_str}\n```"


# Few-shot examples for common scenarios
FEW_SHOT_EXAMPLES = {
    "tool_use": """
Example 1 - Weather Query:
User: What's the weather like in Tokyo?
Assistant: I'll check the weather in Tokyo for you.
<tool>get_weather</tool>
<args>{"location": "Tokyo"}</args>

Example 2 - Search Query:
User: Find information about Python async/await
Assistant: I'll search for information about Python async/await.
<tool>web_search</tool>
<args>{"query": "Python async await tutorial"}</args>
""",
    
    "code_explanation": """
Example 1 - Code Explanation:
User: What does this code do?
```python
def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n-1) + fibonacci(n-2)
```
Assistant: This is a recursive implementation of the Fibonacci sequence. Here's how it works:

1. **Base case**: If n is 0 or 1, return n (fib(0)=0, fib(1)=1)
2. **Recursive case**: For n > 1, return the sum of the two preceding numbers

**Time Complexity**: O(2^n) - exponential due to repeated calculations
**Space Complexity**: O(n) - call stack depth

**Note**: This naive implementation is inefficient. Here's an optimized version:

```python
def fibonacci_optimized(n, memo={}):
    if n in memo:
        return memo[n]
    if n <= 1:
        return n
    memo[n] = fibonacci_optimized(n-1, memo) + fibonacci_optimized(n-2, memo)
    return memo[n]
```
"""
}


---

## State Management

### File: `app/state.py`

```python
"""
State management for conversation history and session data.
Supports multiple backends: memory, Redis, and file-based.
"""

import json
import os
import pickle
import time
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path
from threading import Lock
from typing import Dict, List, Optional
from uuid import uuid4

from .config import get_settings
from .logging_config import get_logger
from .models import ConversationState, Message

logger = get_logger("state")


class StateBackend(ABC):
    """Abstract base class for state backends."""
    
    @abstractmethod
    def get(self, session_id: str) -> Optional[ConversationState]:
        """Get conversation state by session ID."""
        pass
    
    @abstractmethod
    def set(self, session_id: str, state: ConversationState) -> bool:
        """Set conversation state."""
        pass
    
    @abstractmethod
    def delete(self, session_id: str) -> bool:
        """Delete conversation state."""
        pass
    
    @abstractmethod
    def list_sessions(self) -> List[str]:
        """List all session IDs."""
        pass
    
    @abstractmethod
    def clear_expired(self, max_age_seconds: int) -> int:
        """Clear expired sessions, returns count cleared."""
        pass


class MemoryStateBackend(StateBackend):
    """In-memory state backend (not persistent)."""
    
    def __init__(self):
        self._store: Dict[str, ConversationState] = {}
        self._lock = Lock()
        logger.info("Memory state backend initialized")
    
    def get(self, session_id: str) -> Optional[ConversationState]:
        with self._lock:
            return self._store.get(session_id)
    
    def set(self, session_id: str, state: ConversationState) -> bool:
        with self._lock:
            self._store[session_id] = state
        return True
    
    def delete(self, session_id: str) -> bool:
        with self._lock:
            if session_id in self._store:
                del self._store[session_id]
                return True
            return False
    
    def list_sessions(self) -> List[str]:
        with self._lock:
            return list(self._store.keys())
    
    def clear_expired(self, max_age_seconds: int) -> int:
        with self._lock:
            now = time.time()
            expired = [
                sid for sid, state in self._store.items()
                if (now - state.updated_at.timestamp()) > max_age_seconds
            ]
            for sid in expired:
                del self._store[sid]
            return len(expired)


class FileStateBackend(StateBackend):
    """File-based persistent state backend."""
    
    def __init__(self, base_path: str):
        self.base_path = Path(base_path)
        self.base_path.mkdir(parents=True, exist_ok=True)
        self._lock = Lock()
        logger.info(f"File state backend initialized: {base_path}")
    
    def _get_file_path(self, session_id: str) -> Path:
        """Get file path for session."""
        # Use first 2 chars as subdirectory for distribution
        subdir = session_id[:2] if len(session_id) >= 2 else "xx"
        dir_path = self.base_path / subdir
        dir_path.mkdir(exist_ok=True)
        return dir_path / f"{session_id}.json"
    
    def get(self, session_id: str) -> Optional[ConversationState]:
        file_path = self._get_file_path(session_id)
        
        if not file_path.exists():
            return None
        
        try:
            with open(file_path, 'r') as f:
                data = json.load(f)
            return ConversationState(**data)
        except Exception as e:
            logger.error(f"Failed to load session {session_id}: {e}")
            return None
    
    def set(self, session_id: str, state: ConversationState) -> bool:
        file_path = self._get_file_path(session_id)
        
        try:
            with self._lock:
                with open(file_path, 'w') as f:
                    json.dump(state.model_dump(), f, default=str)
            return True
        except Exception as e:
            logger.error(f"Failed to save session {session_id}: {e}")
            return False
    
    def delete(self, session_id: str) -> bool:
        file_path = self._get_file_path(session_id)
        
        try:
            if file_path.exists():
                file_path.unlink()
                return True
            return False
        except Exception as e:
            logger.error(f"Failed to delete session {session_id}: {e}")
            return False
    
    def list_sessions(self) -> List[str]:
        sessions = []
        
        try:
            for subdir in self.base_path.iterdir():
                if subdir.is_dir():
                    for file_path in subdir.glob("*.json"):
                        sessions.append(file_path.stem)
        except Exception as e:
            logger.error(f"Failed to list sessions: {e}")
        
        return sessions
    
    def clear_expired(self, max_age_seconds: int) -> int:
        count = 0
        now = time.time()
        
        try:
            for subdir in self.base_path.iterdir():
                if subdir.is_dir():
                    for file_path in subdir.glob("*.json"):
                        try:
                            mtime = file_path.stat().st_mtime
                            if (now - mtime) > max_age_seconds:
                                file_path.unlink()
                                count += 1
                        except Exception:
                            pass
        except Exception as e:
            logger.error(f"Failed to clear expired sessions: {e}")
        
        return count


class RedisStateBackend(StateBackend):
    """Redis-based state backend for distributed deployments."""
    
    def __init__(self, redis_url: str):
        try:
            import redis
            self.redis = redis.from_url(redis_url, decode_responses=True)
            self.redis.ping()
            logger.info("Redis state backend initialized")
        except ImportError:
            raise ImportError("redis package required for Redis backend")
        except Exception as e:
            raise ConnectionError(f"Failed to connect to Redis: {e}")
    
    def _get_key(self, session_id: str) -> str:
        """Get Redis key for session."""
        return f"orchestrator:session:{session_id}"
    
    def get(self, session_id: str) -> Optional[ConversationState]:
        try:
            data = self.redis.get(self._get_key(session_id))
            if data:
                return ConversationState(**json.loads(data))
            return None
        except Exception as e:
            logger.error(f"Failed to get session {session_id}: {e}")
            return None
    
    def set(self, session_id: str, state: ConversationState) -> bool:
        try:
            settings = get_settings()
            ttl = settings.state.session_ttl
            
            self.redis.setex(
                self._get_key(session_id),
                ttl,
                json.dumps(state.model_dump(), default=str)
            )
            return True
        except Exception as e:
            logger.error(f"Failed to set session {session_id}: {e}")
            return False
    
    def delete(self, session_id: str) -> bool:
        try:
            return self.redis.delete(self._get_key(session_id)) > 0
        except Exception as e:
            logger.error(f"Failed to delete session {session_id}: {e}")
            return False
    
    def list_sessions(self) -> List[str]:
        try:
            keys = self.redis.keys("orchestrator:session:*")
            return [k.replace("orchestrator:session:", "") for k in keys]
        except Exception as e:
            logger.error(f"Failed to list sessions: {e}")
            return []
    
    def clear_expired(self, max_age_seconds: int) -> int:
        # Redis handles expiration automatically
        return 0


class StateManager:
    """
    Manager for conversation state.
    
    Provides a unified interface for state operations regardless of backend.
    """
    
    def __init__(self):
        self.settings = get_settings().state
        self.backend = self._create_backend()
        logger.info(f"State manager initialized with {self.settings.backend} backend")
    
    def _create_backend(self) -> StateBackend:
        """Create the appropriate backend."""
        if self.settings.backend == "redis":
            if not self.settings.redis_url:
                raise ValueError("REDIS_URL required for Redis backend")
            return RedisStateBackend(self.settings.redis_url)
        
        elif self.settings.backend == "file":
            return FileStateBackend(self.settings.file_path)
        
        else:  # memory
            return MemoryStateBackend()
    
    def create_session(self) -> str:
        """Create a new session and return its ID."""
        session_id = str(uuid4())
        state = ConversationState(session_id=session_id)
        self.backend.set(session_id, state)
        logger.info(f"Created new session: {session_id}")
        return session_id
    
    def get_conversation(self, session_id: str) -> ConversationState:
        """
        Get conversation state for a session.
        Creates new session if not found.
        """
        state = self.backend.get(session_id)
        
        if state is None:
            logger.warning(f"Session {session_id} not found, creating new")
            state = ConversationState(session_id=session_id)
            self.backend.set(session_id, state)
        
        return state
    
    def update_conversation(self, session_id: str, state: ConversationState) -> bool:
        """Update conversation state."""
        state.updated_at = time.time()
        return self.backend.set(session_id, state)
    
    def delete_session(self, session_id: str) -> bool:
        """Delete a session."""
        return self.backend.delete(session_id)
    
    def list_sessions(self) -> List[str]:
        """List all active sessions."""
        return self.backend.list_sessions()
    
    def clear_expired(self) -> int:
        """Clear expired sessions."""
        return self.backend.clear_expired(self.settings.session_ttl)
    
    def get_session_info(self, session_id: str) -> Optional[dict]:
        """Get information about a session."""
        state = self.backend.get(session_id)
        
        if state is None:
            return None
        
        return {
            "session_id": session_id,
            "message_count": len(state.messages),
            "context_count": len(state.context),
            "created_at": state.created_at,
            "updated_at": state.updated_at
        }


# Singleton instance
_state_manager: Optional[StateManager] = None


def get_state_manager() -> StateManager:
    """Get or create state manager singleton."""
    global _state_manager
    if _state_manager is None:
        _state_manager = StateManager()
    return _state_manager
```

---

## API Endpoints

### File: `app/api/routes.py`

```python
"""
API route handlers for the orchestrator.
"""

import time
from typing import AsyncGenerator

from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from ..agent.loop import AgentLoop, get_agent_loop
from ..config import get_settings
from ..integrations.llm_client import close_llm_client, get_llm_client
from ..integrations.mcp_manager import close_mcp_manager, get_mcp_manager
from ..integrations.rag_client import close_rag_client, get_rag_client
from ..logging_config import get_logger
from ..models import (
    ChatRequest, ChatResponse, HealthStatus, StreamChunk,
    ToolExecuteRequest, ToolExecuteResponse, ToolsListResponse
)
from ..state import get_state_manager

logger = get_logger("api_routes")

# Create router
router = APIRouter()


# Dependencies
async def get_llm():
    """Dependency to get LLM client."""
    return await get_llm_client()


async def get_rag():
    """Dependency to get RAG client."""
    return await get_rag_client()


async def get_mcp():
    """Dependency to get MCP manager."""
    return await get_mcp_manager()


async def get_agent():
    """Dependency to get agent loop."""
    return await get_agent_loop()


# Routes
@router.post("/chat", response_model=ChatResponse)
async def chat(
    request: ChatRequest,
    agent: AgentLoop = Depends(get_agent)
):
    """
    Main chat endpoint.
    
    Processes user messages through the agent loop, handling:
    - RAG context retrieval
    - Tool execution
    - Multi-turn conversation
    """
    try:
        response = await agent.process(request)
        return response
    except Exception as e:
        logger.error(f"Chat endpoint error: {e}", exc_info=True)
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to process chat request: {str(e)}"
        )


@router.post("/chat/stream")
async def chat_stream(
    request: ChatRequest,
    agent: AgentLoop = Depends(get_agent)
):
    """
    Streaming chat endpoint.
    
    Returns a streaming response with chunks of the assistant's reply.
    """
    async def event_generator() -> AsyncGenerator[str, None]:
        async for chunk in agent.process_stream(request):
            yield f"data: {chunk.model_dump_json()}\n\n"
        yield "data: [DONE]\n\n"
    
    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream"
    )


@router.post("/tools/execute", response_model=ToolExecuteResponse)
async def execute_tool(
    request: ToolExecuteRequest,
    agent: AgentLoop = Depends(get_agent)
):
    """
    Direct tool execution endpoint.
    
    Execute a tool directly without going through the full agent loop.
    """
    try:
        result = await agent.execute_direct_tool(
            tool_name=request.tool_name,
            arguments=request.arguments
        )
        
        return ToolExecuteResponse(
            success=result.success,
            result=result.result,
            error=result.error,
            execution_time_ms=result.execution_time_ms
        )
    except Exception as e:
        logger.error(f"Tool execution error: {e}")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Tool execution failed: {str(e)}"
        )


@router.get("/tools", response_model=ToolsListResponse)
async def list_tools(mcp = Depends(get_mcp)):
    """
    List all available tools.
    
    Returns information about all registered tools from MCP servers.
    """
    try:
        tools = mcp.list_tools()
        return ToolsListResponse(
            tools=tools,
            count=len(tools)
        )
    except Exception as e:
        logger.error(f"List tools error: {e}")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to list tools: {str(e)}"
        )


@router.get("/health", response_model=HealthStatus)
async def health_check():
    """
    Health check endpoint.
    
    Returns health status of all connected components.
    """
    settings = get_settings()
    start_time = time.time()
    
    components = {}
    overall_status = "healthy"
    
    # Check LLM
    try:
        llm = await get_llm_client()
        healthy, error = await llm.health_check()
        components["llm"] = {
            "status": "healthy" if healthy else "unhealthy",
            "error": error,
            "url": settings.llm.base_url
        }
        if not healthy:
            overall_status = "degraded"
    except Exception as e:
        components["llm"] = {"status": "unhealthy", "error": str(e)}
        overall_status = "degraded"
    
    # Check RAG
    try:
        rag = await get_rag_client()
        healthy, error = await rag.health_check()
        components["rag"] = {
            "status": "healthy" if healthy else "unhealthy",
            "error": error,
            "url": settings.rag.base_url
        }
        if not healthy:
            overall_status = "degraded"
    except Exception as e:
        components["rag"] = {"status": "unhealthy", "error": str(e)}
        overall_status = "degraded"
    
    # Check MCP
    try:
        mcp = await get_mcp_manager()
        mcp_health = await mcp.health_check()
        healthy_servers = sum(1 for h in mcp_health.values() if h.get("healthy"))
        total_servers = len(mcp_health)
        components["mcp"] = {
            "status": "healthy" if healthy_servers == total_servers else "degraded",
            "servers": mcp_health,
            "tool_count": len(mcp.list_tools())
        }
        if healthy_servers < total_servers:
            overall_status = "degraded"
    except Exception as e:
        components["mcp"] = {"status": "unhealthy", "error": str(e)}
        overall_status = "degraded"
    
    # Check state
    try:
        state = get_state_manager()
        sessions = state.list_sessions()
        components["state"] = {
            "status": "healthy",
            "backend": settings.state.backend,
            "active_sessions": len(sessions)
        }
    except Exception as e:
        components["state"] = {"status": "unhealthy", "error": str(e)}
        overall_status = "degraded"
    
    response_time = (time.time() - start_time) * 1000
    
    return HealthStatus(
        status=overall_status,
        version=settings.app_version,
        components=components
    )


@router.get("/sessions")
async def list_sessions():
    """List all active sessions."""
    try:
        state = get_state_manager()
        sessions = state.list_sessions()
        return {
            "sessions": sessions,
            "count": len(sessions)
        }
    except Exception as e:
        logger.error(f"List sessions error: {e}")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to list sessions: {str(e)}"
        )


@router.get("/sessions/{session_id}")
async def get_session(session_id: str):
    """Get information about a specific session."""
    try:
        state = get_state_manager()
        info = state.get_session_info(session_id)
        
        if info is None:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Session {session_id} not found"
            )
        
        return info
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Get session error: {e}")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get session: {str(e)}"
        )


@router.delete("/sessions/{session_id}")
async def delete_session(session_id: str):
    """Delete a session."""
    try:
        state = get_state_manager()
        success = state.delete_session(session_id)
        
        if not success:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Session {session_id} not found"
            )
        
        return {"message": f"Session {session_id} deleted"}
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Delete session error: {e}")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to delete session: {str(e)}"
        )


@router.get("/")
async def root():
    """Root endpoint with basic info."""
    settings = get_settings()
    return {
        "name": settings.app_name,
        "version": settings.app_version,
        "endpoints": [
            "/chat",
            "/chat/stream",
            "/tools",
            "/tools/execute",
            "/health",
            "/sessions"
        ]
    }
```

### File: `app/main.py`

```python
"""
Main FastAPI application entry point.
"""

import asyncio
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.middleware.gzip import GZipMiddleware

from .api.routes import router
from .config import get_settings
from .integrations.llm_client import close_llm_client, get_llm_client
from .integrations.lsp_client import close_lsp_client
from .integrations.mcp_manager import close_mcp_manager
from .integrations.rag_client import close_rag_client
from .logging_config import setup_logging
from .state import get_state_manager

# Setup logging
logger = setup_logging()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """
    Application lifespan manager.
    Handles startup and shutdown events.
    """
    # Startup
    logger.info("Starting up orchestrator...")
    
    settings = get_settings()
    
    # Initialize clients
    try:
        await get_llm_client()
        logger.info("LLM client initialized")
    except Exception as e:
        logger.error(f"Failed to initialize LLM client: {e}")
    
    try:
        from .integrations.rag_client import get_rag_client
        await get_rag_client()
        logger.info("RAG client initialized")
    except Exception as e:
        logger.warning(f"RAG client not available: {e}")
    
    try:
        from .integrations.mcp_manager import get_mcp_manager
        await get_mcp_manager()
        logger.info("MCP manager initialized")
    except Exception as e:
        logger.warning(f"MCP manager not available: {e}")
    
    try:
        from .agent.loop import get_agent_loop
        await get_agent_loop()
        logger.info("Agent loop initialized")
    except Exception as e:
        logger.warning(f"Agent loop not available: {e}")
    
    logger.info("Orchestrator startup complete")
    
    yield
    
    # Shutdown
    logger.info("Shutting down orchestrator...")
    
    await close_llm_client()
    await close_rag_client()
    await close_mcp_manager()
    await close_lsp_client()
    
    logger.info("Orchestrator shutdown complete")


def create_app() -> FastAPI:
    """Create and configure FastAPI application."""
    settings = get_settings()
    
    app = FastAPI(
        title=settings.app_name,
        version=settings.app_version,
        description="Light Local LLM Orchestrator - Central brain for coordinating LLM, RAG, MCP, and LSP",
        docs_url="/docs" if settings.debug else None,
        redoc_url="/redoc" if settings.debug else None,
        lifespan=lifespan
    )
    
    # Add middleware
    app.add_middleware(GZipMiddleware, minimum_size=1000)
    
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],  # Configure for production
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    
    # Include routes
    app.include_router(router, prefix="/api/v1")
    app.include_router(router, prefix="")  # Also at root for convenience
    
    return app


# Create app instance
app = create_app()


if __name__ == "__main__":
    import uvicorn
    
    settings = get_settings()
    
    uvicorn.run(
        "app.main:app",
        host=settings.host,
        port=settings.port,
        workers=settings.workers,
        reload=settings.debug
    )
```

---

## Configuration & Deployment

### File: `requirements.txt`

```
# FastAPI and server
fastapi>=0.104.0
uvicorn[standard]>=0.24.0
python-multipart>=0.0.6

# HTTP client
aiohttp>=3.9.0

# Configuration
pydantic>=2.5.0
pydantic-settings>=2.1.0
python-dotenv>=1.0.0

# State backends (optional)
redis>=5.0.0

# Utilities
orjson>=3.9.0

# Development
pytest>=7.4.0
pytest-asyncio>=0.21.0
httpx>=0.25.0
```

### File: `.env.example`

```
# Application
APP_NAME=LLM Orchestrator
APP_VERSION=1.0.0
DEBUG=false
HOST=0.0.0.0
PORT=9000
WORKERS=1

# LLM Server (llama.cpp)
LLM_HOST=localhost
LLM_PORT=8080
LLM_TIMEOUT=120
LLM_MAX_TOKENS=4096
LLM_TEMPERATURE=0.7
LLM_TOP_P=0.9
LLM_TOP_K=40
LLM_CONTEXT_WINDOW=8192

# RAG Server (ChromaDB)
RAG_HOST=desktop
RAG_PORT=8000
RAG_TIMEOUT=30
RAG_DEFAULT_COLLECTION=documents
RAG_MAX_RESULTS=5

# MCP Servers (comma-separated URLs)
MCP_SERVERS=http://localhost:3001,http://localhost:3002
MCP_TIMEOUT=60
MCP_MAX_CONCURRENT=5

# LSP Configuration
LSP_ENABLED=true
LSP_SERVERS=
LSP_TIMEOUT=30

# State Management
STATE_BACKEND=memory
STATE_REDIS_URL=redis://localhost:6379/0
STATE_FILE_PATH=./data/sessions
STATE_SESSION_TTL=3600
STATE_MAX_HISTORY=20

# Logging
LOG_LEVEL=INFO
LOG_JSON=false
LOG_FILE=
```

### File: `docker/Dockerfile`

```dockerfile
# Multi-stage build for the Orchestrator

# Build stage
FROM python:3.11-slim as builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir --user -r requirements.txt

# Runtime stage
FROM python:3.11-slim

WORKDIR /app

# Copy installed packages from builder
COPY --from=builder /root/.local /root/.local

# Make sure scripts in .local are usable
ENV PATH=/root/.local/bin:$PATH

# Copy application code
COPY app/ ./app/

# Create data directory for file-based state
RUN mkdir -p /app/data/sessions

# Environment variables
ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1

# Expose port
EXPOSE 9000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:9000/health')" || exit 1

# Run the application
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "9000"]
```

### File: `docker-compose.yml`

```yaml
version: '3.8'

services:
  orchestrator:
    build:
      context: .
      dockerfile: docker/Dockerfile
    container_name: llm-orchestrator
    ports:
      - "9000:9000"
    environment:
      - APP_NAME=LLM Orchestrator
      - APP_VERSION=1.0.0
      - DEBUG=false
      - HOST=0.0.0.0
      - PORT=9000
      - LLM_HOST=llm-server
      - LLM_PORT=8080
      - RAG_HOST=rag-server
      - RAG_PORT=8000
      - STATE_BACKEND=memory
      - LOG_LEVEL=INFO
    volumes:
      - ./data:/app/data
    networks:
      - llm-network
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

  # Optional: Redis for distributed state
  redis:
    image: redis:7-alpine
    container_name: llm-redis
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    networks:
      - llm-network
    restart: unless-stopped
    profiles:
      - redis

networks:
  llm-network:
    driver: bridge

volumes:
  redis-data:
```

### File: `README.md`

```markdown
# LLM Orchestrator

The central "brain" that coordinates LLM, RAG, MCP, and LSP components for a Light Local LLM system.

## Features

- **FastAPI-based**: Modern, async web framework
- **Agent Loop**: ReAct pattern for reasoning and acting
- **LLM Integration**: llama.cpp server compatibility
- **RAG Support**: ChromaDB-based retrieval
- **MCP Tools**: Dynamic tool discovery and execution
- **LSP Integration**: Code intelligence features
- **State Management**: Multiple backends (memory, file, Redis)
- **Streaming**: Real-time response streaming
- **Health Checks**: Comprehensive component monitoring

## Quick Start

### Local Development

1. Install dependencies:
```bash
pip install -r requirements.txt
```

2. Configure environment:
```bash
cp .env.example .env
# Edit .env with your settings
```

3. Run the server:
```bash
python -m app.main
```

### Docker

```bash
docker-compose up -d
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/chat` | POST | Main chat endpoint |
| `/chat/stream` | POST | Streaming chat |
| `/tools` | GET | List available tools |
| `/tools/execute` | POST | Execute tool directly |
| `/health` | GET | Health check |
| `/sessions` | GET | List sessions |

## Architecture

```
User Request → FastAPI → Agent Loop → LLM/RAG/MCP/LSP → Response
```

## Configuration

All configuration is done via environment variables. See `.env.example` for options.

## License

MIT
```

---

## Summary

This completes the Phase 5 Orchestrator Layer implementation. The orchestrator provides:

### Core Components
1. **FastAPI Application** (`main.py`): Web server with lifecycle management
2. **Configuration** (`config.py`): Environment-based settings with Pydantic
3. **Logging** (`logging_config.py`): Structured logging with JSON support
4. **Models** (`models.py`): Pydantic models for all data structures

### Integration Modules
1. **LLM Client** (`llm_client.py`): llama.cpp integration with tool call detection
2. **RAG Client** (`rag_client.py`): ChromaDB retrieval with context injection
3. **MCP Manager** (`mcp_manager.py`): Dynamic tool discovery and execution
4. **LSP Client** (`lsp_client.py`): Code intelligence integration

### Agent Logic
1. **Agent Loop** (`loop.py`): Main orchestration with ReAct pattern
2. **Query Planner** (`planner.py`): Query analysis and strategy selection
3. **Prompt Builder** (`templates.py`): Dynamic prompt construction
4. **System Prompts** (`system_prompts.py`): Prompt templates and guidelines

### State & API
1. **State Manager** (`state.py`): Multi-backend conversation state
2. **API Routes** (`routes.py`): RESTful endpoints

### Deployment
1. **Dockerfile**: Multi-stage container build
2. **docker-compose.yml**: Service orchestration
3. **requirements.txt**: Python dependencies
4. **.env.example**: Configuration template
