#!/usr/bin/env python3
"""
MCP Client Implementation
Complete client for connecting to MCP servers and executing tools

Usage:
    from mcp_client import MCPClient, ToolResult
    
    async def main():
        client = MCPClient("mcp_config.json")
        await client.connect_all()
        
        result = await client.call_tool("time:get_current_time", {"timezone": "UTC"})
        print(result.get_text())
        
        await client.close_all()
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
    
    def get_json(self) -> Optional[Dict[str, Any]]:
        """Try to parse result as JSON"""
        try:
            text = self.get_text()
            return json.loads(text)
        except (json.JSONDecodeError, ValueError):
            return None


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
    """Server-Sent Events transport for HTTP-based MCP servers"""
    
    def __init__(self, url: str, headers: Optional[Dict[str, str]] = None):
        self.base_url = url.rstrip('/')
        self.headers = headers or {}
        self.session: Optional[aiohttp.ClientSession] = None
        self.message_url: Optional[str] = None
        self._connected = False
        
    async def connect(self) -> bool:
        """Connect to SSE endpoint and discover message URL"""
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
        """Send JSON-RPC message via POST"""
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
    """stdio transport via subprocess for local MCP servers"""
    
    def __init__(self, command: str, args: List[str], env: Optional[Dict[str, str]] = None):
        self.command = command
        self.args = args
        self.env = env
        self.process: Optional[asyncio.subprocess.Process] = None
        self._pending: Dict[str, asyncio.Future] = {}
        self._reader_task: Optional[asyncio.Task] = None
        self._lock = asyncio.Lock()
        
    async def connect(self) -> bool:
        """Start subprocess and establish communication"""
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
        """Read JSON-RPC messages from stdout"""
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
        """Send JSON-RPC message via stdin"""
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
        """Close connection and terminate subprocess"""
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
        """Connect to server and initialize"""
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
        """Send initialize request per MCP protocol"""
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
        """Discover server capabilities (tools, resources)"""
        # List tools
        if self.capabilities.get('tools'):
            await self._list_tools()
            
        # List resources
        if self.capabilities.get('resources'):
            await self._list_resources()
            
    async def _list_tools(self):
        """List available tools from server"""
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
        """List available resources from server"""
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
        """Read a resource from this server"""
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
        """Close connection to this server"""
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
        """Get server configuration by name"""
        return self._config.get('mcpServers', {}).get(name)
        
    async def connect_server(self, name: str, config: Optional[Dict[str, Any]] = None) -> bool:
        """Connect to a specific server"""
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
        """List all available tools from all servers"""
        return list(self._all_tools.values())
        
    def get_server_tools(self, server_name: str) -> List[MCPTool]:
        """Get tools from a specific server"""
        server = self.servers.get(server_name)
        if server:
            return list(server.tools.values())
        return []
        
    async def call_tool(self, tool_name: str, arguments: Dict[str, Any], 
                       server_name: Optional[str] = None) -> ToolResult:
        """Call a tool by name"""
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
        
    async def read_resource(self, uri: str, server_name: Optional[str] = None) -> Optional[str]:
        """Read a resource by URI"""
        if server_name:
            server = self.servers.get(server_name)
            if server:
                return await server.read_resource(uri)
            return None
            
        # Try all servers
        for server in self.servers.values():
            if uri in server.resources:
                return await server.read_resource(uri)
        return None
        
    async def close_all(self):
        """Close all server connections"""
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
    """Decorator for adding retry logic to async functions"""
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


# Example usage
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
    
    # Close all connections
    await client.close_all()


if __name__ == "__main__":
    asyncio.run(main())
