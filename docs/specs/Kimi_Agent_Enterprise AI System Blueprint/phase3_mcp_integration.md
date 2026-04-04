# Phase 3: MCP Server Integration & Tool Ecosystem

## Complete Implementation Guide for Light Local LLM System

---

## Table of Contents

1. [MCP Overview & Architecture](#1-mcp-overview--architecture)
2. [Public MCP Servers Integration](#2-public-mcp-servers-integration)
3. [MCP Client Implementation](#3-mcp-client-implementation)
4. [MCP Server Configuration](#4-mcp-server-configuration)
5. [Custom MCP Server Development](#5-custom-mcp-server-development)
6. [Orchestrator Integration](#6-orchestrator-integration)
7. [MCP Tools Catalog](#7-mcp-tools-catalog)
8. [Appendix: Complete Code Files](#8-appendix-complete-code-files)

---

## 1. MCP Overview & Architecture

### 1.1 What is MCP?

The **Model Context Protocol (MCP)** is an open protocol that standardizes how applications provide context to Large Language Models (LLMs). Think of MCP as a "USB-C port for AI applications" - it provides a universal way to connect AI models to different data sources and tools.

### 1.2 Core Concepts

```
┌─────────────────────────────────────────────────────────────────┐
│                    MCP Architecture Overview                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────┐         JSON-RPC          ┌─────────────┐     │
│   │   MCP       │◄─────────────────────────►│   MCP       │     │
│   │   Client    │      over stdio/SSE       │   Server    │     │
│   │  (Host App) │                           │  (Tool/Resource)│   │
│   └─────────────┘                           └─────────────┘     │
│         │                                          │             │
│         │ 1. Initialize                            │             │
│         │ 2. Discover Tools                        │             │
│         │ 3. Call Tools                            │             │
│         │ 4. Exchange Resources                    │             │
│         ▼                                          ▼             │
│   ┌──────────────────────────────────────────────────────┐      │
│   │                    Capabilities                       │      │
│   │  • Tools (functions)  • Resources (data)             │      │
│   │  • Prompts (templates) • Sampling (LLM requests)     │      │
│   └──────────────────────────────────────────────────────┘      │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 1.3 Protocol Layers

```
┌─────────────────────────────────────────┐
│         Application Layer               │
│    (Tool calls, Resource access)        │
├─────────────────────────────────────────┤
│         Protocol Layer (MCP)            │
│    (JSON-RPC 2.0 messages)              │
├─────────────────────────────────────────┤
│         Transport Layer                 │
│    (stdio, HTTP/SSE, WebSocket)         │
└─────────────────────────────────────────┘
```

### 1.4 Message Format (JSON-RPC 2.0)

```json
{
  "jsonrpc": "2.0",
  "id": "unique-request-id",
  "method": "tools/call",
  "params": {
    "name": "tool_name",
    "arguments": {
      "param1": "value1"
    }
  }
}
```

**Response Format:**
```json
{
  "jsonrpc": "2.0",
  "id": "unique-request-id",
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Tool execution result"
      }
    ],
    "isError": false
  }
}
```

### 1.5 Lifecycle

```
┌─────────┐    ┌─────────────┐    ┌─────────────┐    ┌──────────┐
│  Init   │───►│  Initialize │───►│   Initialized│───►│  Running │
│         │    │   Request   │    │   Response   │    │          │
└─────────┘    └─────────────┘    └─────────────┘    └────┬─────┘
                                                          │
    ┌─────────────────────────────────────────────────────┘
    │
    ▼
┌─────────┐    ┌─────────────┐    ┌─────────────┐
│ Shutdown│◄───│  Cancelled  │◄───│   Error     │
│         │    │             │    │             │
└─────────┘    └─────────────┘    └─────────────┘
```

---

## 2. Public MCP Servers Integration

### 2.1 Available Public MCP Servers

| Server | URL | Purpose | Transport |
|--------|-----|---------|-----------|
| Everything | `everything.mcp.inevitable.fyi` | Multi-purpose demo | SSE |
| Time | `time.mcp.inevitable.fyi` | Time functions | SSE |
| Echo | `echo.mcp.inevitable.fyi` | Debugging | SSE |
| Semgrep | `mcp.semgrep.ai/sse` | Security scanning | SSE |
| DuckDuckGo | Various implementations | Web search | stdio/SSE |
| Wikipedia | Various implementations | Knowledge base | stdio/SSE |

### 2.2 Server Connection Configurations

```json
{
  "mcpServers": {
    "everything": {
      "name": "Everything Demo Server",
      "url": "https://everything.mcp.inevitable.fyi/sse",
      "transport": "sse",
      "description": "Multi-purpose demo server with various tools",
      "tools": [
        "echo", "add", "multiply", "get_current_time",
        "fetch_url", "search_documents"
      ]
    },
    "time": {
      "name": "Time Server",
      "url": "https://time.mcp.inevitable.fyi/sse",
      "transport": "sse",
      "description": "Time and date utilities",
      "tools": [
        "get_current_time", "convert_timezone",
        "add_time", "format_time"
      ]
    },
    "echo": {
      "name": "Echo Debug Server",
      "url": "https://echo.mcp.inevitable.fyi/sse",
      "transport": "sse",
      "description": "Echo tool for debugging",
      "tools": ["echo"]
    },
    "semgrep": {
      "name": "Semgrep Security Scanner",
      "url": "https://mcp.semgrep.ai/sse",
      "transport": "sse",
      "description": "Security vulnerability scanning",
      "tools": ["scan_code", "check_rules", "get_findings"]
    },
    "duckduckgo": {
      "name": "DuckDuckGo Search",
      "command": "npx",
      "args": ["-y", "@mcp/duckduckgo"],
      "transport": "stdio",
      "description": "Web search via DuckDuckGo",
      "tools": ["search", "get_instant_answer"]
    },
    "wikipedia": {
      "name": "Wikipedia Search",
      "command": "npx",
      "args": ["-y", "@mcp/wikipedia"],
      "transport": "stdio",
      "description": "Wikipedia article search",
      "tools": ["search", "get_summary", "get_full_article"]
    },
    "filesystem": {
      "name": "Local Filesystem",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/allowed/path"],
      "transport": "stdio",
      "description": "Local file operations",
      "tools": ["read_file", "write_file", "list_directory", "search_files"]
    },
    "github": {
      "name": "GitHub Integration",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "transport": "stdio",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      },
      "description": "GitHub repository operations",
      "tools": ["search_repos", "get_repo", "create_issue", "list_issues"]
    },
    "sqlite": {
      "name": "SQLite Database",
      "command": "uvx",
      "args": ["mcp-server-sqlite", "--db-path", "/path/to/database.db"],
      "transport": "stdio",
      "description": "SQLite database queries",
      "tools": ["query", "execute", "get_schema", "list_tables"]
    },
    "fetch": {
      "name": "HTTP Fetch",
      "command": "uvx",
      "args": ["mcp-server-fetch"],
      "transport": "stdio",
      "description": "HTTP requests and web fetching",
      "tools": ["fetch", "post", "get_headers"]
    },
    "puppeteer": {
      "name": "Browser Automation",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-puppeteer"],
      "transport": "stdio",
      "description": "Web browser automation",
      "tools": ["navigate", "screenshot", "click", "get_content", "evaluate"]
    }
  }
}
```

### 2.3 Connection Examples

#### SSE Connection (HTTP Server-Sent Events)

```python
import asyncio
import aiohttp
import json
from typing import Optional, Dict, Any, Callable

class SSEConnection:
    """SSE transport for MCP servers"""
    
    def __init__(self, url: str, headers: Optional[Dict[str, str]] = None):
        self.url = url
        self.headers = headers or {}
        self.session: Optional[aiohttp.ClientSession] = None
        self.message_endpoint: Optional[str] = None
        self._event_queue = asyncio.Queue()
        
    async def connect(self) -> bool:
        """Establish SSE connection and get message endpoint"""
        try:
            self.session = aiohttp.ClientSession()
            
            # Connect to SSE endpoint
            async with self.session.get(
                self.url,
                headers={
                    "Accept": "text/event-stream",
                    **self.headers
                }
            ) as response:
                if response.status != 200:
                    return False
                    
                # Parse SSE events to find message endpoint
                async for line in response.content:
                    line = line.decode('utf-8').strip()
                    if line.startswith('event: endpoint'):
                        # Next line contains the endpoint URL
                        endpoint_line = await response.content.readline()
                        endpoint = endpoint_line.decode('utf-8').strip()
                        if endpoint.startswith('data: '):
                            self.message_endpoint = endpoint[6:]
                            return True
                            
        except Exception as e:
            print(f"SSE connection error: {e}")
            return False
            
    async def send_message(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Send JSON-RPC message via POST"""
        if not self.session or not self.message_endpoint:
            raise ConnectionError("Not connected")
            
        async with self.session.post(
            self.message_endpoint,
            json=message,
            headers={"Content-Type": "application/json"}
        ) as response:
            return await response.json()
            
    async def close(self):
        """Close connection"""
        if self.session:
            await self.session.close()
```

#### stdio Connection (Subprocess)

```python
import asyncio
import subprocess
import json
from typing import Optional, Dict, Any, List

class StdioConnection:
    """stdio transport for MCP servers via subprocess"""
    
    def __init__(self, command: str, args: List[str], env: Optional[Dict[str, str]] = None):
        self.command = command
        self.args = args
        self.env = env
        self.process: Optional[subprocess.Process] = None
        self._pending_requests: Dict[str, asyncio.Future] = {}
        self._reader_task: Optional[asyncio.Task] = None
        
    async def connect(self) -> bool:
        """Start MCP server subprocess"""
        try:
            # Merge environment variables
            process_env = {**os.environ}
            if self.env:
                process_env.update(self.env)
                
            # Start subprocess
            self.process = await asyncio.create_subprocess_exec(
                self.command,
                *self.args,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=process_env
            )
            
            # Start reader task
            self._reader_task = asyncio.create_task(self._read_messages())
            
            return self.process.returncode is None
            
        except Exception as e:
            print(f"stdio connection error: {e}")
            return False
            
    async def _read_messages(self):
        """Read messages from stdout"""
        while self.process and self.process.stdout:
            try:
                line = await self.process.stdout.readline()
                if not line:
                    break
                    
                message = json.loads(line.decode('utf-8'))
                
                # Handle response
                if 'id' in message and message['id'] in self._pending_requests:
                    future = self._pending_requests.pop(message['id'])
                    future.set_result(message)
                    
            except json.JSONDecodeError:
                continue
            except Exception as e:
                print(f"Error reading message: {e}")
                
    async def send_message(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Send JSON-RPC message via stdin"""
        if not self.process or not self.process.stdin:
            raise ConnectionError("Not connected")
            
        # Create future for response
        future = asyncio.get_event_loop().create_future()
        self._pending_requests[message['id']] = future
        
        # Send message
        message_bytes = json.dumps(message).encode('utf-8') + b'\n'
        self.process.stdin.write(message_bytes)
        await self.process.stdin.drain()
        
        # Wait for response with timeout
        try:
            return await asyncio.wait_for(future, timeout=30.0)
        except asyncio.TimeoutError:
            self._pending_requests.pop(message['id'], None)
            raise
            
    async def close(self):
        """Close connection"""
        if self._reader_task:
            self._reader_task.cancel()
            
        if self.process:
            self.process.terminate()
            try:
                await asyncio.wait_for(self.process.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                self.process.kill()
```

---

## 3. MCP Client Implementation

### 3.1 Complete Python MCP Client Class

```python
#!/usr/bin/env python3
"""
MCP Client Implementation
Complete client for connecting to MCP servers and executing tools
"""

import asyncio
import json
import uuid
import logging
from typing import Dict, List, Optional, Any, Callable, Union
from dataclasses import dataclass, field
from enum import Enum
from abc import ABC, abstractmethod
import aiohttp
import subprocess
import os
from datetime import datetime

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class TransportType(Enum):
    """MCP transport types"""
    STDIO = "stdio"
    SSE = "sse"
    WEBSOCKET = "websocket"


@dataclass
class MCPTool:
    """Represents an MCP tool"""
    name: str
    description: str
    input_schema: Dict[str, Any]
    server_name: str = ""
    
    @property
    def parameters(self) -> Dict[str, Any]:
        """Get tool parameters from schema"""
        return self.input_schema.get('properties', {})
    
    @property
    def required_params(self) -> List[str]:
        """Get required parameters"""
        return self.input_schema.get('required', [])


@dataclass
class MCPResource:
    """Represents an MCP resource"""
    uri: str
    name: str
    description: str
    mime_type: str = "text/plain"


@dataclass
class ToolResult:
    """Result of a tool execution"""
    success: bool
    content: List[Dict[str, Any]]
    error: Optional[str] = None
    duration_ms: float = 0.0
    
    def get_text(self) -> str:
        """Extract text content from result"""
        texts = []
        for item in self.content:
            if item.get('type') == 'text':
                texts.append(item.get('text', ''))
            elif item.get('type') == 'image':
                texts.append(f"[Image: {item.get('mimeType', 'unknown')}]")
        return '\n'.join(texts)


class MCPTransport(ABC):
    """Abstract base class for MCP transports"""
    
    @abstractmethod
    async def connect(self) -> bool:
        """Establish connection"""
        pass
    
    @abstractmethod
    async def send_message(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Send message and return response"""
        pass
    
    @abstractmethod
    async def close(self):
        """Close connection"""
        pass
    
    @property
    @abstractmethod
    def is_connected(self) -> bool:
        """Check if connected"""
        pass


class SSETransport(MCPTransport):
    """Server-Sent Events transport"""
    
    def __init__(self, url: str, headers: Optional[Dict[str, str]] = None):
        self.base_url = url.rstrip('/')
        self.headers = headers or {}
        self.session: Optional[aiohttp.ClientSession] = None
        self.message_url: Optional[str] = None
        self._connected = False
        
    async def connect(self) -> bool:
        """Connect to SSE endpoint"""
        try:
            self.session = aiohttp.ClientSession()
            
            # Connect to SSE endpoint
            async with self.session.get(
                self.base_url,
                headers={
                    "Accept": "text/event-stream",
                    "Cache-Control": "no-cache",
                    **self.headers
                }
            ) as response:
                if response.status != 200:
                    logger.error(f"SSE connection failed: {response.status}")
                    return False
                
                # Parse SSE stream for endpoint
                async for line in response.content:
                    line = line.decode('utf-8').strip()
                    
                    if line.startswith('event: endpoint'):
                        # Read data line
                        data_line = await response.content.readline()
                        data = data_line.decode('utf-8').strip()
                        if data.startswith('data: '):
                            endpoint = data[6:]
                            # Handle relative URLs
                            if endpoint.startswith('/'):
                                parsed = aiohttp.URL(self.base_url)
                                self.message_url = f"{parsed.scheme}://{parsed.host}{endpoint}"
                            elif endpoint.startswith('http'):
                                self.message_url = endpoint
                            else:
                                self.message_url = f"{self.base_url}/{endpoint}"
                            
                            self._connected = True
                            logger.info(f"SSE connected, message URL: {self.message_url}")
                            return True
                            
        except Exception as e:
            logger.error(f"SSE connection error: {e}")
            return False
            
    async def send_message(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Send message via POST"""
        if not self.session or not self.message_url:
            raise ConnectionError("Not connected")
            
        async with self.session.post(
            self.message_url,
            json=message,
            headers={"Content-Type": "application/json", **self.headers}
        ) as response:
            if response.status != 200:
                raise ConnectionError(f"HTTP {response.status}")
            return await response.json()
            
    async def close(self):
        """Close connection"""
        self._connected = False
        if self.session:
            await self.session.close()
            
    @property
    def is_connected(self) -> bool:
        return self._connected and self.session is not None


class StdioTransport(MCPTransport):
    """stdio transport via subprocess"""
    
    def __init__(self, command: str, args: List[str], env: Optional[Dict[str, str]] = None):
        self.command = command
        self.args = args
        self.env = env
        self.process: Optional[asyncio.subprocess.Process] = None
        self._pending: Dict[str, asyncio.Future] = {}
        self._reader_task: Optional[asyncio.Task] = None
        self._lock = asyncio.Lock()
        
    async def connect(self) -> bool:
        """Start subprocess"""
        try:
            # Prepare environment
            process_env = {**os.environ}
            if self.env:
                for key, value in self.env.items():
                    if value.startswith('${') and value.endswith('}'):
                        # Resolve from environment
                        env_key = value[2:-1]
                        process_env[key] = os.environ.get(env_key, '')
                    else:
                        process_env[key] = value
            
            # Start process
            self.process = await asyncio.create_subprocess_exec(
                self.command,
                *self.args,
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=process_env
            )
            
            # Start reader
            self._reader_task = asyncio.create_task(self._read_loop())
            
            logger.info(f"stdio process started: {self.command}")
            return True
            
        except Exception as e:
            logger.error(f"stdio connection error: {e}")
            return False
            
    async def _read_loop(self):
        """Read messages from stdout"""
        while self.process and self.process.stdout:
            try:
                line = await self.process.stdout.readline()
                if not line:
                    break
                    
                message = json.loads(line.decode('utf-8'))
                
                # Handle response
                msg_id = message.get('id')
                if msg_id and msg_id in self._pending:
                    future = self._pending.pop(msg_id)
                    if not future.done():
                        future.set_result(message)
                        
            except json.JSONDecodeError:
                continue
            except Exception as e:
                logger.error(f"Read error: {e}")
                
    async def send_message(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Send message via stdin"""
        if not self.process or not self.process.stdin:
            raise ConnectionError("Not connected")
            
        msg_id = message.get('id')
        if not msg_id:
            raise ValueError("Message must have an id")
            
        # Create future for response
        future = asyncio.get_event_loop().create_future()
        
        async with self._lock:
            self._pending[msg_id] = future
            
        # Send message
        message_line = json.dumps(message) + '\n'
        self.process.stdin.write(message_line.encode('utf-8'))
        await self.process.stdin.drain()
        
        # Wait for response
        try:
            return await asyncio.wait_for(future, timeout=30.0)
        except asyncio.TimeoutError:
            self._pending.pop(msg_id, None)
            raise ConnectionError("Request timeout")
            
    async def close(self):
        """Close connection"""
        if self._reader_task:
            self._reader_task.cancel()
            try:
                await self._reader_task
            except asyncio.CancelledError:
                pass
                
        if self.process:
            self.process.terminate()
            try:
                await asyncio.wait_for(self.process.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                self.process.kill()
                await self.process.wait()
                
    @property
    def is_connected(self) -> bool:
        return self.process is not None and self.process.returncode is None


class MCPServer:
    """Represents a connected MCP server"""
    
    def __init__(self, name: str, config: Dict[str, Any]):
        self.name = name
        self.config = config
        self.transport: Optional[MCPTransport] = None
        self.tools: Dict[str, MCPTool] = {}
        self.resources: Dict[str, MCPResource] = {}
        self.capabilities: Dict[str, Any] = {}
        self._initialized = False
        
    async def connect(self) -> bool:
        """Connect to server"""
        transport_type = self.config.get('transport', 'stdio')
        
        if transport_type == TransportType.SSE.value:
            self.transport = SSETransport(
                self.config['url'],
                self.config.get('headers')
            )
        elif transport_type == TransportType.STDIO.value:
            self.transport = StdioTransport(
                self.config['command'],
                self.config.get('args', []),
                self.config.get('env')
            )
        else:
            raise ValueError(f"Unknown transport: {transport_type}")
            
        if not await self.transport.connect():
            return False
            
        # Initialize
        return await self._initialize()
        
    async def _initialize(self) -> bool:
        """Send initialize request"""
        request = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {
                    "sampling": {},
                    "roots": {
                        "listChanged": True
                    }
                },
                "clientInfo": {
                    "name": "light-llm-client",
                    "version": "1.0.0"
                }
            }
        }
        
        try:
            response = await self.transport.send_message(request)
            
            if 'error' in response:
                logger.error(f"Initialize error: {response['error']}")
                return False
                
            result = response.get('result', {})
            self.capabilities = result.get('capabilities', {})
            
            # Send initialized notification
            await self.transport.send_message({
                "jsonrpc": "2.0",
                "method": "notifications/initialized"
            })
            
            self._initialized = True
            logger.info(f"Server {self.name} initialized")
            
            # Discover tools and resources
            await self._discover_capabilities()
            
            return True
            
        except Exception as e:
            logger.error(f"Initialize failed: {e}")
            return False
            
    async def _discover_capabilities(self):
        """Discover server capabilities"""
        # List tools
        if self.capabilities.get('tools'):
            await self._list_tools()
            
        # List resources
        if self.capabilities.get('resources'):
            await self._list_resources()
            
    async def _list_tools(self):
        """List available tools"""
        request = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "tools/list"
        }
        
        try:
            response = await self.transport.send_message(request)
            result = response.get('result', {})
            
            for tool_data in result.get('tools', []):
                tool = MCPTool(
                    name=tool_data['name'],
                    description=tool_data.get('description', ''),
                    input_schema=tool_data.get('inputSchema', {}),
                    server_name=self.name
                )
                self.tools[tool.name] = tool
                
            logger.info(f"Discovered {len(self.tools)} tools from {self.name}")
            
        except Exception as e:
            logger.error(f"Failed to list tools: {e}")
            
    async def _list_resources(self):
        """List available resources"""
        request = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "resources/list"
        }
        
        try:
            response = await self.transport.send_message(request)
            result = response.get('result', {})
            
            for res_data in result.get('resources', []):
                resource = MCPResource(
                    uri=res_data['uri'],
                    name=res_data.get('name', ''),
                    description=res_data.get('description', ''),
                    mime_type=res_data.get('mimeType', 'text/plain')
                )
                self.resources[resource.uri] = resource
                
            logger.info(f"Discovered {len(self.resources)} resources from {self.name}")
            
        except Exception as e:
            logger.error(f"Failed to list resources: {e}")
            
    async def call_tool(self, tool_name: str, arguments: Dict[str, Any]) -> ToolResult:
        """Call a tool on this server"""
        if not self._initialized:
            return ToolResult(
                success=False,
                content=[],
                error="Server not initialized"
            )
            
        if tool_name not in self.tools:
            return ToolResult(
                success=False,
                content=[],
                error=f"Tool '{tool_name}' not found"
            )
            
        request = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "tools/call",
            "params": {
                "name": tool_name,
                "arguments": arguments
            }
        }
        
        start_time = datetime.now()
        
        try:
            response = await self.transport.send_message(request)
            duration = (datetime.now() - start_time).total_seconds() * 1000
            
            if 'error' in response:
                return ToolResult(
                    success=False,
                    content=[],
                    error=response['error'].get('message', 'Unknown error'),
                    duration_ms=duration
                )
                
            result = response.get('result', {})
            
            return ToolResult(
                success=not result.get('isError', False),
                content=result.get('content', []),
                duration_ms=duration
            )
            
        except Exception as e:
            duration = (datetime.now() - start_time).total_seconds() * 1000
            return ToolResult(
                success=False,
                content=[],
                error=str(e),
                duration_ms=duration
            )
            
    async def read_resource(self, uri: str) -> Optional[str]:
        """Read a resource"""
        request = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "resources/read",
            "params": {"uri": uri}
        }
        
        try:
            response = await self.transport.send_message(request)
            result = response.get('result', {})
            contents = result.get('contents', [])
            
            if contents:
                return contents[0].get('text', '')
            return None
            
        except Exception as e:
            logger.error(f"Failed to read resource: {e}")
            return None
            
    async def close(self):
        """Close connection"""
        if self.transport:
            await self.transport.close()
            self.transport = None
            self._initialized = False


class MCPClient:
    """Main MCP client for managing multiple servers"""
    
    def __init__(self, config_path: Optional[str] = None):
        self.config_path = config_path
        self.servers: Dict[str, MCPServer] = {}
        self._all_tools: Dict[str, MCPTool] = {}
        self._config: Dict[str, Any] = {}
        
        if config_path:
            self.load_config(config_path)
            
    def load_config(self, path: str):
        """Load configuration from JSON file"""
        with open(path, 'r') as f:
            self._config = json.load(f)
            
    def get_server_config(self, name: str) -> Optional[Dict[str, Any]]:
        """Get server configuration"""
        return self._config.get('mcpServers', {}).get(name)
        
    async def connect_server(self, name: str, config: Optional[Dict[str, Any]] = None) -> bool:
        """Connect to a server"""
        if config is None:
            config = self.get_server_config(name)
            
        if not config:
            logger.error(f"No configuration for server: {name}")
            return False
            
        server = MCPServer(name, config)
        
        if await server.connect():
            self.servers[name] = server
            # Update tool registry
            for tool_name, tool in server.tools.items():
                self._all_tools[f"{name}:{tool_name}"] = tool
            return True
        return False
        
    async def connect_all(self) -> Dict[str, bool]:
        """Connect to all configured servers"""
        results = {}
        for name in self._config.get('mcpServers', {}):
            results[name] = await self.connect_server(name)
        return results
        
    def get_tool(self, name: str) -> Optional[MCPTool]:
        """Get tool by name (with or without server prefix)"""
        if ':' in name:
            return self._all_tools.get(name)
        
        # Search without prefix
        for full_name, tool in self._all_tools.items():
            if full_name.split(':')[1] == name:
                return tool
        return None
        
    def list_all_tools(self) -> List[MCPTool]:
        """List all available tools"""
        return list(self._all_tools.values())
        
    async def call_tool(self, tool_name: str, arguments: Dict[str, Any], 
                       server_name: Optional[str] = None) -> ToolResult:
        """Call a tool"""
        # Determine server
        if server_name:
            server = self.servers.get(server_name)
            if not server:
                return ToolResult(
                    success=False,
                    content=[],
                    error=f"Server '{server_name}' not connected"
                )
            return await server.call_tool(tool_name, arguments)
            
        # Find server from tool name
        if ':' in tool_name:
            server_name, tool_name = tool_name.split(':', 1)
            server = self.servers.get(server_name)
            if server:
                return await server.call_tool(tool_name, arguments)
                
        # Search all servers
        for server in self.servers.values():
            if tool_name in server.tools:
                return await server.call_tool(tool_name, arguments)
                
        return ToolResult(
            success=False,
            content=[],
            error=f"Tool '{tool_name}' not found"
        )
        
    async def close_all(self):
        """Close all connections"""
        for server in self.servers.values():
            await server.close()
        self.servers.clear()
        self._all_tools.clear()
        
    async def __aenter__(self):
        return self
        
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        await self.close_all()


# Retry decorator for resilient operations
def with_retry(max_retries: int = 3, delay: float = 1.0):
    """Decorator for retry logic"""
    def decorator(func):
        async def wrapper(*args, **kwargs):
            last_error = None
            for attempt in range(max_retries):
                try:
                    return await func(*args, **kwargs)
                except Exception as e:
                    last_error = e
                    logger.warning(f"Attempt {attempt + 1} failed: {e}")
                    if attempt < max_retries - 1:
                        await asyncio.sleep(delay * (attempt + 1))
            raise last_error
        return wrapper
    return decorator
```

### 3.2 Usage Examples

```python
async def main():
    """Example usage of MCP client"""
    
    # Create client with config
    client = MCPClient("mcp_config.json")
    
    # Connect to all servers
    results = await client.connect_all()
    print(f"Connection results: {results}")
    
    # List all available tools
    tools = client.list_all_tools()
    print(f"\nAvailable tools ({len(tools)}):")
    for tool in tools:
        print(f"  - {tool.server_name}:{tool.name}: {tool.description}")
    
    # Call a tool
    result = await client.call_tool(
        "time:get_current_time",
        {"timezone": "UTC"}
    )
    print(f"\nTime result: {result.get_text()}")
    
    # Call another tool
    result = await client.call_tool(
        "echo:echo",
        {"message": "Hello, MCP!"}
    )
    print(f"Echo result: {result.get_text()}")
    
    # Close all connections
    await client.close_all()


# Run example
if __name__ == "__main__":
    asyncio.run(main())
```

---

## 4. MCP Server Configuration

### 4.1 Complete mcp_config.json

```json
{
  "version": "1.0.0",
  "settings": {
    "defaultTimeout": 30,
    "maxRetries": 3,
    "retryDelay": 1.0,
    "connectionPoolSize": 10
  },
  "mcpServers": {
    "everything": {
      "name": "Everything Demo Server",
      "url": "https://everything.mcp.inevitable.fyi/sse",
      "transport": "sse",
      "description": "Multi-purpose demo server",
      "timeout": 30,
      "tools": {
        "echo": {
          "description": "Echo a message back",
          "parameters": {
            "message": {
              "type": "string",
              "description": "Message to echo",
              "required": true
            }
          }
        },
        "add": {
          "description": "Add two numbers",
          "parameters": {
            "a": {
              "type": "number",
              "description": "First number",
              "required": true
            },
            "b": {
              "type": "number",
              "description": "Second number",
              "required": true
            }
          }
        },
        "get_current_time": {
          "description": "Get current time",
          "parameters": {
            "timezone": {
              "type": "string",
              "description": "Timezone (e.g., UTC, America/New_York)",
              "required": false
            }
          }
        }
      }
    },
    "time": {
      "name": "Time Server",
      "url": "https://time.mcp.inevitable.fyi/sse",
      "transport": "sse",
      "description": "Time and date utilities",
      "timeout": 30,
      "tools": {
        "get_current_time": {
          "description": "Get current time in specified timezone",
          "parameters": {
            "timezone": {
              "type": "string",
              "description": "Timezone identifier",
              "required": false,
              "default": "UTC"
            }
          }
        },
        "convert_timezone": {
          "description": "Convert time between timezones",
          "parameters": {
            "time": {
              "type": "string",
              "description": "Time to convert (ISO format)",
              "required": true
            },
            "from_timezone": {
              "type": "string",
              "description": "Source timezone",
              "required": true
            },
            "to_timezone": {
              "type": "string",
              "description": "Target timezone",
              "required": true
            }
          }
        },
        "add_time": {
          "description": "Add time duration",
          "parameters": {
            "time": {
              "type": "string",
              "description": "Base time",
              "required": true
            },
            "days": {
              "type": "integer",
              "description": "Days to add",
              "required": false,
              "default": 0
            },
            "hours": {
              "type": "integer",
              "description": "Hours to add",
              "required": false,
              "default": 0
            },
            "minutes": {
              "type": "integer",
              "description": "Minutes to add",
              "required": false,
              "default": 0
            }
          }
        }
      }
    },
    "echo": {
      "name": "Echo Debug Server",
      "url": "https://echo.mcp.inevitable.fyi/sse",
      "transport": "sse",
      "description": "Echo tool for debugging MCP connections",
      "timeout": 10,
      "tools": {
        "echo": {
          "description": "Echo a message back for testing",
          "parameters": {
            "message": {
              "type": "string",
              "description": "Message to echo",
              "required": true
            },
            "uppercase": {
              "type": "boolean",
              "description": "Convert to uppercase",
              "required": false,
              "default": false
            }
          }
        }
      }
    },
    "semgrep": {
      "name": "Semgrep Security Scanner",
      "url": "https://mcp.semgrep.ai/sse",
      "transport": "sse",
      "description": "Security vulnerability scanning with Semgrep",
      "timeout": 120,
      "headers": {
        "User-Agent": "light-llm-client/1.0"
      },
      "tools": {
        "scan_code": {
          "description": "Scan code for security vulnerabilities",
          "parameters": {
            "code": {
              "type": "string",
              "description": "Code to scan",
              "required": true
            },
            "language": {
              "type": "string",
              "description": "Programming language",
              "required": true,
              "enum": ["python", "javascript", "typescript", "java", "go", "ruby"]
            },
            "rules": {
              "type": "array",
              "description": "Specific rules to run",
              "required": false
            }
          }
        },
        "check_rules": {
          "description": "Check which rules are available",
          "parameters": {
            "language": {
              "type": "string",
              "description": "Filter by language",
              "required": false
            }
          }
        },
        "get_findings": {
          "description": "Get detailed findings from last scan",
          "parameters": {
            "severity": {
              "type": "string",
              "description": "Filter by severity",
              "required": false,
              "enum": ["ERROR", "WARNING", "INFO"]
            }
          }
        }
      }
    },
    "duckduckgo": {
      "name": "DuckDuckGo Search",
      "command": "npx",
      "args": ["-y", "@mcp/duckduckgo"],
      "transport": "stdio",
      "description": "Web search via DuckDuckGo",
      "timeout": 30,
      "tools": {
        "search": {
          "description": "Search the web using DuckDuckGo",
          "parameters": {
            "query": {
              "type": "string",
              "description": "Search query",
              "required": true
            },
            "max_results": {
              "type": "integer",
              "description": "Maximum results to return",
              "required": false,
              "default": 10
            },
            "safe_search": {
              "type": "boolean",
              "description": "Enable safe search",
              "required": false,
              "default": true
            }
          }
        },
        "get_instant_answer": {
          "description": "Get instant answer for a query",
          "parameters": {
            "query": {
              "type": "string",
              "description": "Query for instant answer",
              "required": true
            }
          }
        }
      }
    },
    "wikipedia": {
      "name": "Wikipedia Search",
      "command": "npx",
      "args": ["-y", "@mcp/wikipedia"],
      "transport": "stdio",
      "description": "Wikipedia article search and retrieval",
      "timeout": 30,
      "tools": {
        "search": {
          "description": "Search Wikipedia articles",
          "parameters": {
            "query": {
              "type": "string",
              "description": "Search query",
              "required": true
            },
            "limit": {
              "type": "integer",
              "description": "Maximum results",
              "required": false,
              "default": 5
            },
            "language": {
              "type": "string",
              "description": "Wikipedia language code",
              "required": false,
              "default": "en"
            }
          }
        },
        "get_summary": {
          "description": "Get article summary",
          "parameters": {
            "title": {
              "type": "string",
              "description": "Article title",
              "required": true
            },
            "language": {
              "type": "string",
              "description": "Wikipedia language code",
              "required": false,
              "default": "en"
            }
          }
        },
        "get_full_article": {
          "description": "Get full article content",
          "parameters": {
            "title": {
              "type": "string",
              "description": "Article title",
              "required": true
            },
            "language": {
              "type": "string",
              "description": "Wikipedia language code",
              "required": false,
              "default": "en"
            }
          }
        }
      }
    },
    "filesystem": {
      "name": "Local Filesystem",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/documents"],
      "transport": "stdio",
      "description": "Local file operations with security restrictions",
      "timeout": 30,
      "tools": {
        "read_file": {
          "description": "Read file contents",
          "parameters": {
            "path": {
              "type": "string",
              "description": "File path (relative to allowed directory)",
              "required": true
            }
          }
        },
        "write_file": {
          "description": "Write content to file",
          "parameters": {
            "path": {
              "type": "string",
              "description": "File path",
              "required": true
            },
            "content": {
              "type": "string",
              "description": "Content to write",
              "required": true
            }
          }
        },
        "list_directory": {
          "description": "List directory contents",
          "parameters": {
            "path": {
              "type": "string",
              "description": "Directory path",
              "required": false,
              "default": "."
            }
          }
        },
        "search_files": {
          "description": "Search for files matching pattern",
          "parameters": {
            "pattern": {
              "type": "string",
              "description": "Search pattern (glob)",
              "required": true
            },
            "path": {
              "type": "string",
              "description": "Starting directory",
              "required": false,
              "default": "."
            }
          }
        },
        "get_file_info": {
          "description": "Get file metadata",
          "parameters": {
            "path": {
              "type": "string",
              "description": "File path",
              "required": true
            }
          }
        }
      }
    },
    "github": {
      "name": "GitHub Integration",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "transport": "stdio",
      "description": "GitHub repository operations",
      "timeout": 60,
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      },
      "tools": {
        "search_repos": {
          "description": "Search GitHub repositories",
          "parameters": {
            "query": {
              "type": "string",
              "description": "Search query",
              "required": true
            },
            "sort": {
              "type": "string",
              "description": "Sort field",
              "required": false,
              "enum": ["stars", "forks", "updated"]
            }
          }
        },
        "get_repo": {
          "description": "Get repository information",
          "parameters": {
            "owner": {
              "type": "string",
              "description": "Repository owner",
              "required": true
            },
            "repo": {
              "type": "string",
              "description": "Repository name",
              "required": true
            }
          }
        },
        "list_issues": {
          "description": "List repository issues",
          "parameters": {
            "owner": {
              "type": "string",
              "description": "Repository owner",
              "required": true
            },
            "repo": {
              "type": "string",
              "description": "Repository name",
              "required": true
            },
            "state": {
              "type": "string",
              "description": "Issue state filter",
              "required": false,
              "enum": ["open", "closed", "all"],
              "default": "open"
            }
          }
        },
        "create_issue": {
          "description": "Create a new issue",
          "parameters": {
            "owner": {
              "type": "string",
              "description": "Repository owner",
              "required": true
            },
            "repo": {
              "type": "string",
              "description": "Repository name",
              "required": true
            },
            "title": {
              "type": "string",
              "description": "Issue title",
              "required": true
            },
            "body": {
              "type": "string",
              "description": "Issue body",
              "required": false
            }
          }
        }
      }
    },
    "sqlite": {
      "name": "SQLite Database",
      "command": "uvx",
      "args": ["mcp-server-sqlite", "--db-path", "/path/to/database.db"],
      "transport": "stdio",
      "description": "SQLite database queries and operations",
      "timeout": 30,
      "tools": {
        "query": {
          "description": "Execute SELECT query",
          "parameters": {
            "sql": {
              "type": "string",
              "description": "SQL SELECT statement",
              "required": true
            },
            "params": {
              "type": "array",
              "description": "Query parameters",
              "required": false
            }
          }
        },
        "execute": {
          "description": "Execute INSERT/UPDATE/DELETE",
          "parameters": {
            "sql": {
              "type": "string",
              "description": "SQL statement",
              "required": true
            },
            "params": {
              "type": "array",
              "description": "Statement parameters",
              "required": false
            }
          }
        },
        "get_schema": {
          "description": "Get database schema",
          "parameters": {
            "table": {
              "type": "string",
              "description": "Specific table (optional)",
              "required": false
            }
          }
        },
        "list_tables": {
          "description": "List all tables",
          "parameters": {}
        }
      }
    },
    "fetch": {
      "name": "HTTP Fetch",
      "command": "uvx",
      "args": ["mcp-server-fetch"],
      "transport": "stdio",
      "description": "HTTP requests and web content fetching",
      "timeout": 60,
      "tools": {
        "fetch": {
          "description": "Fetch content from URL",
          "parameters": {
            "url": {
              "type": "string",
              "description": "URL to fetch",
              "required": true
            },
            "method": {
              "type": "string",
              "description": "HTTP method",
              "required": false,
              "enum": ["GET", "POST", "PUT", "DELETE"],
              "default": "GET"
            },
            "headers": {
              "type": "object",
              "description": "HTTP headers",
              "required": false
            },
            "body": {
              "type": "string",
              "description": "Request body",
              "required": false
            }
          }
        },
        "get_headers": {
          "description": "Get HTTP headers from URL",
          "parameters": {
            "url": {
              "type": "string",
              "description": "URL to check",
              "required": true
            }
          }
        }
      }
    },
    "puppeteer": {
      "name": "Browser Automation",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-puppeteer"],
      "transport": "stdio",
      "description": "Web browser automation with Puppeteer",
      "timeout": 120,
      "tools": {
        "navigate": {
          "description": "Navigate to URL",
          "parameters": {
            "url": {
              "type": "string",
              "description": "URL to navigate to",
              "required": true
            },
            "wait_until": {
              "type": "string",
              "description": "When to consider navigation complete",
              "required": false,
              "enum": ["load", "domcontentloaded", "networkidle0", "networkidle2"],
              "default": "networkidle2"
            }
          }
        },
        "screenshot": {
          "description": "Take page screenshot",
          "parameters": {
            "selector": {
              "type": "string",
              "description": "Element selector to screenshot",
              "required": false
            },
            "full_page": {
              "type": "boolean",
              "description": "Capture full page",
              "required": false,
              "default": false
            }
          }
        },
        "click": {
          "description": "Click on element",
          "parameters": {
            "selector": {
              "type": "string",
              "description": "Element selector",
              "required": true
            }
          }
        },
        "get_content": {
          "description": "Get page content",
          "parameters": {
            "selector": {
              "type": "string",
              "description": "Element selector (default: body)",
              "required": false
            }
          }
        },
        "evaluate": {
          "description": "Execute JavaScript on page",
          "parameters": {
            "script": {
              "type": "string",
              "description": "JavaScript code to execute",
              "required": true
            }
          }
        }
      }
    }
  }
}
```

---

## 5. Custom MCP Server Development

### 5.1 FastMCP Server Template

```python
#!/usr/bin/env python3
"""
Custom MCP Server Template using FastMCP
FastMCP is a high-level Python SDK for building MCP servers
"""

from fastmcp import FastMCP, Context
from typing import Optional, List, Dict, Any
import asyncio
import json

# Create MCP server instance
mcp = FastMCP(
    name="custom-tools-server",
    instructions="""
    This server provides custom tools for the Light Local LLM system.
    Available tools include data processing, calculations, and utility functions.
    """
)


# ============================================
# Tool Registration Examples
# ============================================

@mcp.tool()
def calculate(expression: str) -> str:
    """
    Safely evaluate a mathematical expression.
    
    Args:
        expression: Mathematical expression (e.g., "2 + 2 * 3")
    
    Returns:
        Result of the calculation
    """
    try:
        # Safe evaluation using ast
        import ast
        import operator
        
        allowed_ops = {
            ast.Add: operator.add,
            ast.Sub: operator.sub,
            ast.Mult: operator.mul,
            ast.Div: operator.truediv,
            ast.Pow: operator.pow,
            ast.USub: operator.neg,
        }
        
        def eval_node(node):
            if isinstance(node, ast.Num):
                return node.n
            elif isinstance(node, ast.BinOp):
                return allowed_ops[type(node.op)](eval_node(node.left), eval_node(node.right))
            elif isinstance(node, ast.UnaryOp):
                return allowed_ops[type(node.op)](eval_node(node.operand))
            else:
                raise ValueError(f"Unsupported operation: {type(node)}")
        
        tree = ast.parse(expression, mode='eval')
        result = eval_node(tree.body)
        return f"Result: {result}"
        
    except Exception as e:
        return f"Error: {str(e)}"


@mcp.tool()
def format_data(data: str, format_type: str = "json") -> str:
    """
    Format data into specified format.
    
    Args:
        data: Input data string
        format_type: Output format (json, yaml, csv)
    
    Returns:
        Formatted data string
    """
    try:
        # Try to parse as JSON first
        parsed = json.loads(data)
        
        if format_type == "json":
            return json.dumps(parsed, indent=2)
        elif format_type == "yaml":
            import yaml
            return yaml.dump(parsed, default_flow_style=False)
        elif format_type == "csv":
            if isinstance(parsed, list) and len(parsed) > 0:
                import csv
                import io
                output = io.StringIO()
                if isinstance(parsed[0], dict):
                    writer = csv.DictWriter(output, fieldnames=parsed[0].keys())
                    writer.writeheader()
                    writer.writerows(parsed)
                else:
                    writer = csv.writer(output)
                    writer.writerows(parsed)
                return output.getvalue()
        else:
            return f"Unsupported format: {format_type}"
            
    except json.JSONDecodeError:
        return f"Error: Input is not valid JSON"
    except Exception as e:
        return f"Error: {str(e)}"


@mcp.tool()
async def process_text(text: str, operation: str, ctx: Context) -> str:
    """
    Process text with various operations.
    
    Args:
        text: Input text to process
        operation: Operation to perform (uppercase, lowercase, reverse, count_words, summarize)
        ctx: MCP context for progress reporting
    
    Returns:
        Processed text result
    """
    await ctx.info(f"Processing text with operation: {operation}")
    await ctx.report_progress(0, 100)
    
    try:
        if operation == "uppercase":
            result = text.upper()
        elif operation == "lowercase":
            result = text.lower()
        elif operation == "reverse":
            result = text[::-1]
        elif operation == "count_words":
            words = text.split()
            result = f"Word count: {len(words)}"
        elif operation == "summarize":
            # Simple summarization
            sentences = text.split('.')
            result = f"Summary ({len(sentences)} sentences): {sentences[0]}."
        else:
            result = f"Unknown operation: {operation}"
        
        await ctx.report_progress(100, 100)
        return result
        
    except Exception as e:
        await ctx.error(f"Error processing text: {e}")
        return f"Error: {str(e)}"


@mcp.tool()
def validate_email(email: str) -> str:
    """
    Validate email address format.
    
    Args:
        email: Email address to validate
    
    Returns:
        Validation result
    """
    import re
    
    pattern = r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$'
    
    if re.match(pattern, email):
        return f"'{email}' is a valid email address"
    else:
        return f"'{email}' is NOT a valid email address"


@mcp.tool()
def generate_id(prefix: str = "id", length: int = 8) -> str:
    """
    Generate a unique identifier.
    
    Args:
        prefix: ID prefix
        length: Random part length
    
    Returns:
        Generated unique ID
    """
    import random
    import string
    
    random_part = ''.join(random.choices(
        string.ascii_lowercase + string.digits, 
        k=length
    ))
    return f"{prefix}_{random_part}"


# ============================================
# Resource Registration Examples
# ============================================

@mcp.resource("config://app")
def get_app_config() -> str:
    """Get application configuration"""
    config = {
        "version": "1.0.0",
        "name": "Custom Tools Server",
        "features": ["calculator", "text_processor", "validator"],
        "limits": {
            "max_text_length": 10000,
            "max_calculation_depth": 10
        }
    }
    return json.dumps(config, indent=2)


@mcp.resource("docs://usage")
def get_usage_docs() -> str:
    """Get usage documentation"""
    return """
# Custom Tools Server Usage

## Available Tools

### calculate
Evaluate mathematical expressions safely.
Example: `calculate("2 + 2 * 3")` → "Result: 8"

### format_data
Convert data between formats (json, yaml, csv).
Example: `format_data('{"key": "value"}', "yaml")`

### process_text
Process text with various operations.
Operations: uppercase, lowercase, reverse, count_words, summarize

### validate_email
Validate email address format.
Example: `validate_email("user@example.com")`

### generate_id
Generate unique identifiers.
Example: `generate_id("user", 10)` → "user_a3f7k9m2p1"

## Resources

- `config://app` - Application configuration
- `docs://usage` - This documentation
"""


# ============================================
# Prompt Registration Examples
# ============================================

@mcp.prompt()
def code_review_prompt(code: str, language: str = "python") -> str:
    """
    Generate a code review prompt.
    
    Args:
        code: Code to review
        language: Programming language
    """
    return f"""Please review the following {language} code:

```{language}
{code}
```

Consider:
1. Code quality and readability
2. Potential bugs or issues
3. Performance considerations
4. Best practices for {language}
5. Security implications

Provide specific suggestions for improvement."""


@mcp.prompt()
def debug_prompt(error_message: str, context: str = "") -> str:
    """
    Generate a debugging prompt.
    
    Args:
        error_message: The error message
        context: Additional context about the error
    """
    return f"""Help debug this error:

Error: {error_message}

Context: {context}

Please:
1. Explain what this error means
2. Identify likely causes
3. Suggest specific fixes
4. Provide code examples if applicable"""


# ============================================
# Main Entry Point
# ============================================

if __name__ == "__main__":
    # Run the server with stdio transport (default)
    mcp.run(transport='stdio')
    
    # Or run with SSE transport
    # mcp.run(transport='sse', host='0.0.0.0', port=8000)
```

### 5.2 Low-Level MCP Server (Without FastMCP)

```python
#!/usr/bin/env python3
"""
Low-level MCP Server Implementation
For maximum control and customization
"""

import asyncio
import json
import sys
from typing import Dict, List, Any, Optional, Callable
from dataclasses import dataclass


@dataclass
class Tool:
    """Tool definition"""
    name: str
    description: str
    input_schema: Dict[str, Any]
    handler: Callable


class MCPServer:
    """Low-level MCP server implementation"""
    
    def __init__(self, name: str, version: str = "1.0.0"):
        self.name = name
        self.version = version
        self.tools: Dict[str, Tool] = {}
        self.resources: Dict[str, Any] = {}
        self.capabilities = {
            "tools": {"listChanged": False},
            "resources": {"subscribe": False, "listChanged": False}
        }
        
    def register_tool(self, name: str, description: str, 
                     input_schema: Dict[str, Any],
                     handler: Callable) -> Tool:
        """Register a new tool"""
        tool = Tool(name, description, input_schema, handler)
        self.tools[name] = tool
        return tool
        
    def tool(self, name: Optional[str] = None, description: str = ""):
        """Decorator for registering tools"""
        def decorator(func):
            tool_name = name or func.__name__
            # Extract schema from function signature
            import inspect
            sig = inspect.signature(func)
            schema = {
                "type": "object",
                "properties": {},
                "required": []
            }
            
            for param_name, param in sig.parameters.items():
                if param_name == 'self':
                    continue
                    
                param_info = {"type": "string"}
                if param.default != inspect.Parameter.empty:
                    param_info["default"] = param.default
                else:
                    schema["required"].append(param_name)
                    
                schema["properties"][param_name] = param_info
                
            self.register_tool(tool_name, description or func.__doc__ or "", schema, func)
            return func
        return decorator
        
    async def handle_initialize(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """Handle initialize request"""
        return {
            "protocolVersion": "2024-11-05",
            "capabilities": self.capabilities,
            "serverInfo": {
                "name": self.name,
                "version": self.version
            }
        }
        
    async def handle_tools_list(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """Handle tools/list request"""
        tools = []
        for tool in self.tools.values():
            tools.append({
                "name": tool.name,
                "description": tool.description,
                "inputSchema": tool.input_schema
            })
        return {"tools": tools}
        
    async def handle_tools_call(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """Handle tools/call request"""
        tool_name = params.get("name")
        arguments = params.get("arguments", {})
        
        if tool_name not in self.tools:
            return {
                "content": [{"type": "text", "text": f"Tool '{tool_name}' not found"}],
                "isError": True
            }
            
        tool = self.tools[tool_name]
        
        try:
            if asyncio.iscoroutinefunction(tool.handler):
                result = await tool.handler(**arguments)
            else:
                result = tool.handler(**arguments)
                
            return {
                "content": [{"type": "text", "text": str(result)}],
                "isError": False
            }
            
        except Exception as e:
            return {
                "content": [{"type": "text", "text": f"Error: {str(e)}"}],
                "isError": True
            }
            
    async def handle_request(self, request: Dict[str, Any]) -> Optional[Dict[str, Any]]:
        """Handle incoming request"""
        method = request.get("method")
        params = request.get("params", {})
        
        handlers = {
            "initialize": self.handle_initialize,
            "tools/list": self.handle_tools_list,
            "tools/call": self.handle_tools_call,
        }
        
        if method in handlers:
            result = await handlers[method](params)
            return {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": result
            }
        elif method.startswith("notifications/"):
            # Notifications don't need response
            return None
        else:
            return {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "error": {
                    "code": -32601,
                    "message": f"Method not found: {method}"
                }
            }
            
    async def run_stdio(self):
        """Run server with stdio transport"""
        loop = asyncio.get_event_loop()
        
        while True:
            try:
                # Read line from stdin
                line = await loop.run_in_executor(None, sys.stdin.readline)
                if not line:
                    break
                    
                request = json.loads(line)
                response = await self.handle_request(request)
                
                if response:
                    print(json.dumps(response), flush=True)
                    
            except json.JSONDecodeError:
                continue
            except Exception as e:
                error_response = {
                    "jsonrpc": "2.0",
                    "id": None,
                    "error": {
                        "code": -32603,
                        "message": str(e)
                    }
                }
                print(json.dumps(error_response), flush=True)


# Example usage
server = MCPServer("low-level-server")

@server.tool(description="Add two numbers")
def add(a: float, b: float) -> float:
    """Add two numbers"""
    return a + b

@server.tool(description="Multiply two numbers")
def multiply(a: float, b: float) -> float:
    """Multiply two numbers"""
    return a * b


if __name__ == "__main__":
    asyncio.run(server.run_stdio())
```

---

## 6. Orchestrator Integration

### 6.1 Tool Discovery and Registration

```python
#!/usr/bin/env python3
"""
Orchestrator MCP Integration Module
Handles dynamic tool discovery and registration for the orchestrator
"""

import json
import asyncio
from typing import Dict, List, Any, Optional, Callable
from dataclasses import dataclass
from enum import Enum

from mcp_client import MCPClient, MCPTool, ToolResult


class ToolCategory(Enum):
    """Tool categories for organization"""
    SEARCH = "search"
    DATA = "data"
    WEB = "web"
    FILE = "file"
    CODE = "code"
    UTILITY = "utility"
    DATABASE = "database"
    BROWSER = "browser"


@dataclass
class RegisteredTool:
    """Tool registered with the orchestrator"""
    name: str
    full_name: str  # server:tool format
    description: str
    category: ToolCategory
    parameters: Dict[str, Any]
    required_params: List[str]
    server_name: str
    handler: Callable
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for LLM context"""
        return {
            "name": self.name,
            "description": self.description,
            "parameters": self.parameters,
            "required": self.required_params
        }


class MCPOrchestratorIntegration:
    """
    Integration layer between MCP client and orchestrator
    """
    
    def __init__(self, config_path: str):
        self.config_path = config_path
        self.mcp_client = MCPClient(config_path)
        self.registered_tools: Dict[str, RegisteredTool] = {}
        self.tool_categories: Dict[ToolCategory, List[str]] = {
            cat: [] for cat in ToolCategory
        }
        self._initialized = False
        
    async def initialize(self) -> bool:
        """Initialize MCP connections and register tools"""
        try:
            # Connect to all servers
            results = await self.mcp_client.connect_all()
            
            connected_count = sum(1 for r in results.values() if r)
            print(f"Connected to {connected_count}/{len(results)} MCP servers")
            
            # Register all discovered tools
            for server_name, server in self.mcp_client.servers.items():
                for tool_name, tool in server.tools.items():
                    self._register_tool(server_name, tool)
                    
            self._initialized = True
            return True
            
        except Exception as e:
            print(f"MCP initialization failed: {e}")
            return False
            
    def _register_tool(self, server_name: str, tool: MCPTool):
        """Register a single tool with categorization"""
        full_name = f"{server_name}:{tool.name}"
        
        # Determine category
        category = self._categorize_tool(tool.name, tool.description)
        
        # Create handler
        async def handler(**kwargs) -> ToolResult:
            return await self.mcp_client.call_tool(full_name, kwargs)
            
        registered = RegisteredTool(
            name=tool.name,
            full_name=full_name,
            description=tool.description,
            category=category,
            parameters=tool.parameters,
            required_params=tool.required_params,
            server_name=server_name,
            handler=handler
        )
        
        self.registered_tools[full_name] = registered
        self.registered_tools[tool.name] = registered  # Also register by short name
        self.tool_categories[category].append(full_name)
        
    def _categorize_tool(self, name: str, description: str) -> ToolCategory:
        """Categorize a tool based on name and description"""
        text = f"{name} {description}".lower()
        
        if any(kw in text for kw in ['search', 'find', 'lookup', 'query']):
            if 'web' in text or 'url' in text or 'http' in text:
                return ToolCategory.WEB
            return ToolCategory.SEARCH
            
        if any(kw in text for kw in ['file', 'read', 'write', 'directory', 'path']):
            return ToolCategory.FILE
            
        if any(kw in text for kw in ['code', 'scan', 'lint', 'format', 'execute']):
            return ToolCategory.CODE
            
        if any(kw in text for kw in ['database', 'sql', 'table', 'query db']):
            return ToolCategory.DATABASE
            
        if any(kw in text for kw in ['browser', 'navigate', 'click', 'screenshot', 'puppeteer']):
            return ToolCategory.BROWSER
            
        if any(kw in text for kw in ['data', 'json', 'csv', 'format', 'convert']):
            return ToolCategory.DATA
            
        return ToolCategory.UTILITY
        
    def get_tools_for_task(self, task_description: str) -> List[RegisteredTool]:
        """
        Get relevant tools for a task description.
        Uses simple keyword matching - can be enhanced with embeddings.
        """
        task_lower = task_description.lower()
        scores: Dict[str, float] = {}
        
        for full_name, tool in self.registered_tools.items():
            if ':' not in full_name:  # Skip short names
                continue
                
            score = 0.0
            text = f"{tool.name} {tool.description}".lower()
            
            # Check for keyword matches
            task_words = set(task_lower.split())
            tool_words = set(text.split())
            common = task_words & tool_words
            score += len(common) * 0.5
            
            # Boost for category match
            if tool.category == ToolCategory.SEARCH and 'search' in task_lower:
                score += 2.0
            if tool.category == ToolCategory.FILE and 'file' in task_lower:
                score += 2.0
            if tool.category == ToolCategory.CODE and 'code' in task_lower:
                score += 2.0
                
            if score > 0:
                scores[full_name] = score
                
        # Sort by score and return top tools
        sorted_tools = sorted(scores.items(), key=lambda x: x[1], reverse=True)
        return [self.registered_tools[name] for name, _ in sorted_tools[:5]]
        
    def get_tools_by_category(self, category: ToolCategory) -> List[RegisteredTool]:
        """Get all tools in a category"""
        return [
            self.registered_tools[name] 
            for name in self.tool_categories[category]
        ]
        
    def get_all_tools_context(self) -> str:
        """Generate context string with all available tools for LLM"""
        lines = ["# Available Tools\n"]
        
        for category in ToolCategory:
            tools = self.get_tools_by_category(category)
            if tools:
                lines.append(f"\n## {category.value.title()} Tools\n")
                for tool in tools:
                    lines.append(f"### {tool.full_name}")
                    lines.append(f"Description: {tool.description}")
                    if tool.parameters:
                        lines.append("Parameters:")
                        for param, info in tool.parameters.items():
                            req = " (required)" if param in tool.required_params else ""
                            lines.append(f"  - {param}: {info.get('type', 'any')}{req}")
                    lines.append("")
                    
        return '\n'.join(lines)
        
    async def execute_tool(self, tool_name: str, arguments: Dict[str, Any]) -> ToolResult:
        """Execute a registered tool"""
        tool = self.registered_tools.get(tool_name)
        
        if not tool:
            return ToolResult(
                success=False,
                content=[],
                error=f"Tool '{tool_name}' not registered"
            )
            
        # Validate required parameters
        missing = [p for p in tool.required_params if p not in arguments]
        if missing:
            return ToolResult(
                success=False,
                content=[],
                error=f"Missing required parameters: {missing}"
            )
            
        return await tool.handler(**arguments)
        
    async def close(self):
        """Close all connections"""
        await self.mcp_client.close_all()
        self._initialized = False


# Tool selection logic for orchestrator
class ToolSelector:
    """Selects appropriate tools based on task requirements"""
    
    def __init__(self, integration: MCPOrchestratorIntegration):
        self.integration = integration
        
    def select_tools(self, task: Dict[str, Any]) -> List[str]:
        """
        Select tools for a task.
        
        Task format:
        {
            "description": "Task description",
            "requires": ["search", "file_access"],
            "output_type": "text"
        }
        """
        description = task.get("description", "")
        requirements = task.get("requires", [])
        
        selected = []
        
        # Match by requirements
        for req in requirements:
            if req == "search":
                tools = self.integration.get_tools_by_category(ToolCategory.SEARCH)
                tools.extend(self.integration.get_tools_by_category(ToolCategory.WEB))
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req == "file_access":
                tools = self.integration.get_tools_by_category(ToolCategory.FILE)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req == "code_analysis":
                tools = self.integration.get_tools_by_category(ToolCategory.CODE)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req == "database":
                tools = self.integration.get_tools_by_category(ToolCategory.DATABASE)
                selected.extend([t.full_name for t in tools[:2]])
                
            elif req == "browser":
                tools = self.integration.get_tools_by_category(ToolCategory.BROWSER)
                selected.extend([t.full_name for t in tools[:2]])
                
        # Add tools based on description
        relevant = self.integration.get_tools_for_task(description)
        for tool in relevant:
            if tool.full_name not in selected:
                selected.append(tool.full_name)
                
        return selected[:5]  # Limit to top 5 tools
```

### 6.2 Integration Example

```python
async def orchestrator_example():
    """Example of orchestrator using MCP integration"""
    
    # Initialize integration
    integration = MCPOrchestratorIntegration("mcp_config.json")
    await integration.initialize()
    
    # Get tools context for LLM
    tools_context = integration.get_all_tools_context()
    print(tools_context)
    
    # Select tools for a task
    selector = ToolSelector(integration)
    
    task = {
        "description": "Search for Python best practices and save to file",
        "requires": ["search", "file_access"]
    }
    
    selected_tools = selector.select_tools(task)
    print(f"\nSelected tools: {selected_tools}")
    
    # Execute a tool
    result = await integration.execute_tool(
        "duckduckgo:search",
        {"query": "Python best practices 2024", "max_results": 5}
    )
    
    if result.success:
        print(f"\nSearch result:\n{result.get_text()[:500]}...")
    else:
        print(f"\nError: {result.error}")
    
    # Cleanup
    await integration.close()


if __name__ == "__main__":
    asyncio.run(orchestrator_example())
```

---

## 7. MCP Tools Catalog

### 7.1 Available MCP Tools Summary

```
┌─────────────────────────────────────────────────────────────────┐
│                    MCP Tools Catalog                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  🔍 SEARCH TOOLS                                                 │
│  ├── duckduckgo:search          Web search                       │
│  ├── duckduckgo:instant_answer  Quick answers                    │
│  ├── wikipedia:search           Wikipedia articles               │
│  ├── wikipedia:get_summary      Article summaries                │
│  └── everything:search_docs     Document search                  │
│                                                                  │
│  📁 FILESYSTEM TOOLS                                             │
│  ├── filesystem:read_file       Read file contents               │
│  ├── filesystem:write_file      Write to file                    │
│  ├── filesystem:list_directory  List directory                   │
│  ├── filesystem:search_files    Search files                     │
│  └── filesystem:get_file_info   File metadata                    │
│                                                                  │
│  🌐 WEB TOOLS                                                    │
│  ├── fetch:fetch                HTTP requests                    │
│  ├── fetch:get_headers          Get HTTP headers                 │
│  ├── puppeteer:navigate         Browser navigation               │
│  ├── puppeteer:screenshot       Take screenshots                 │
│  ├── puppeteer:click            Click elements                   │
│  ├── puppeteer:get_content      Extract page content             │
│  └── puppeteer:evaluate         Execute JavaScript               │
│                                                                  │
│  💾 DATABASE TOOLS                                               │
│  ├── sqlite:query               SELECT queries                   │
│  ├── sqlite:execute             INSERT/UPDATE/DELETE             │
│  ├── sqlite:get_schema          Get table schema                 │
│  └── sqlite:list_tables         List all tables                  │
│                                                                  │
│  🔒 SECURITY TOOLS                                               │
│  ├── semgrep:scan_code          Security scanning                │
│  ├── semgrep:check_rules        List available rules             │
│  └── semgrep:get_findings       Get scan results                 │
│                                                                  │
│  🔧 GITHUB TOOLS                                                 │
│  ├── github:search_repos        Search repositories              │
│  ├── github:get_repo            Get repo info                    │
│  ├── github:list_issues         List issues                      │
│  └── github:create_issue        Create issue                     │
│                                                                  │
│  ⏰ UTILITY TOOLS                                                │
│  ├── time:get_current_time      Current time                     │
│  ├── time:convert_timezone      Timezone conversion              │
│  ├── time:add_time              Time arithmetic                  │
│  ├── echo:echo                  Debug echo                       │
│  ├── everything:add             Addition                         │
│  └── everything:multiply        Multiplication                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 7.2 Tool Usage Patterns

```python
"""
Common MCP Tool Usage Patterns
"""

import asyncio
from mcp_client import MCPClient


async def search_and_summarize():
    """Pattern: Search web and summarize results"""
    client = MCPClient("mcp_config.json")
    await client.connect_server("duckduckgo")
    
    # Search
    result = await client.call_tool("duckduckgo:search", {
        "query": "MCP protocol AI tools",
        "max_results": 5
    })
    
    # Process results
    search_results = result.get_text()
    
    # Could feed this to LLM for summarization
    print(f"Found {len(search_results)} characters of results")
    
    await client.close_all()


async def analyze_code_security():
    """Pattern: Security scan code files"""
    client = MCPClient("mcp_config.json")
    await client.connect_server("semgrep")
    
    # Read code file
    code = """
def insecure_function(password):
    # Hardcoded password - security issue
    if password == "admin123":
        return True
    return False
"""
    
    # Scan for security issues
    result = await client.call_tool("semgrep:scan_code", {
        "code": code,
        "language": "python"
    })
    
    print(f"Security scan: {result.get_text()}")
    
    await client.close_all()


async def browser_automation():
    """Pattern: Browser automation workflow"""
    client = MCPClient("mcp_config.json")
    await client.connect_server("puppeteer")
    
    # Navigate to page
    await client.call_tool("puppeteer:navigate", {
        "url": "https://example.com"
    })
    
    # Take screenshot
    result = await client.call_tool("puppeteer:screenshot", {
        "full_page": True
    })
    
    # Get content
    content = await client.call_tool("puppeteer:get_content", {})
    
    print(f"Page content length: {len(content.get_text())}")
    
    await client.close_all()


async def database_workflow():
    """Pattern: Database query and processing"""
    client = MCPClient("mcp_config.json")
    await client.connect_server("sqlite")
    
    # List tables
    tables = await client.call_tool("sqlite:list_tables", {})
    print(f"Tables: {tables.get_text()}")
    
    # Query data
    result = await client.call_tool("sqlite:query", {
        "sql": "SELECT * FROM users LIMIT 10"
    })
    
    # Process results
    data = result.get_text()
    print(f"Retrieved {len(data)} characters of data")
    
    await client.close_all()


async def file_processing_pipeline():
    """Pattern: File read, process, write"""
    client = MCPClient("mcp_config.json")
    await client.connect_server("filesystem")
    
    # Read file
    read_result = await client.call_tool("filesystem:read_file", {
        "path": "input.txt"
    })
    
    content = read_result.get_text()
    
    # Process (example: convert to uppercase)
    processed = content.upper()
    
    # Write result
    await client.call_tool("filesystem:write_file", {
        "path": "output.txt",
        "content": processed
    })
    
    print("File processed and saved")
    
    await client.close_all()


# Run examples
if __name__ == "__main__":
    # asyncio.run(search_and_summarize())
    # asyncio.run(analyze_code_security())
    # asyncio.run(browser_automation())
    # asyncio.run(database_workflow())
    # asyncio.run(file_processing_pipeline())
    pass
```

---

## 8. Appendix: Complete Code Files

### 8.1 File Structure

```
light_llm_mcp/
├── mcp_client.py          # Main MCP client implementation
├── mcp_config.json        # Server configurations
├── custom_server.py       # Custom MCP server example
├── orchestrator_integration.py  # Orchestrator integration
├── tools_catalog.py       # Tool catalog and patterns
└── examples/
    ├── basic_usage.py
    ├── advanced_patterns.py
    └── custom_tools.py
```

### 8.2 Installation Requirements

```txt
# requirements.txt
aiohttp>=3.8.0
fastmcp>=0.4.0
pydantic>=2.0.0
```

### 8.3 Environment Setup

```bash
# Install dependencies
pip install aiohttp fastmcp pydantic

# For stdio servers requiring npx
npm install -g npx

# For Python-based servers
pip install uv  # For uvx command

# Set environment variables
export GITHUB_TOKEN="your_github_token_here"
```

---

## Summary

This implementation guide provides:

1. **Complete MCP client** with SSE and stdio transport support
2. **Public server configurations** for 10+ MCP servers
3. **Custom server templates** using both FastMCP and low-level approaches
4. **Orchestrator integration** with tool discovery and selection
5. **Comprehensive tool catalog** covering all major categories
6. **Production-ready code** with error handling and retries

All code is ready to implement directly in your Light Local LLM system.

---

*Document Version: 1.0*
*Last Updated: 2024*
