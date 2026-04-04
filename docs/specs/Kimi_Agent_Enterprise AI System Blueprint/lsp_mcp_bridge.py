#!/usr/bin/env python3
"""
LSP-MCP Bridge Implementation

This module provides a bridge between Language Server Protocol (LSP) and 
Model Context Protocol (MCP), enabling AI agents to leverage code intelligence
capabilities through a standardized interface.

Author: AI Agent System
Version: 1.0.0
"""

import asyncio
import json
import logging
import os
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Set, Tuple, Union
from enum import Enum, auto
import uuid

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger('lsp_mcp_bridge')


# ============================================================================
# Data Models
# ============================================================================

class LSPMessageType(Enum):
    """LSP message types."""
    REQUEST = auto()
    RESPONSE = auto()
    NOTIFICATION = auto()


@dataclass
class Position:
    """Position in a text document."""
    line: int
    character: int
    
    def to_dict(self) -> Dict[str, int]:
        return {"line": self.line, "character": self.character}
    
    @classmethod
    def from_dict(cls, data: Dict[str, int]) -> 'Position':
        return cls(line=data["line"], character=data["character"])


@dataclass
class Range:
    """Range in a text document."""
    start: Position
    end: Position
    
    def to_dict(self) -> Dict[str, Dict[str, int]]:
        return {
            "start": self.start.to_dict(),
            "end": self.end.to_dict()
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Dict[str, int]]) -> 'Range':
        return cls(
            start=Position.from_dict(data["start"]),
            end=Position.from_dict(data["end"])
        )


@dataclass
class Location:
    """Location in a workspace."""
    uri: str
    range: Range
    
    def to_dict(self) -> Dict[str, Any]:
        return {"uri": self.uri, "range": self.range.to_dict()}
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'Location':
        return cls(
            uri=data["uri"],
            range=Range.from_dict(data["range"])
        )


@dataclass
class Diagnostic:
    """Diagnostic information."""
    range: Range
    severity: int  # 1=Error, 2=Warning, 3=Information, 4=Hint
    code: Optional[Union[str, int]]
    source: Optional[str]
    message: str
    
    SEVERITY_NAMES = {1: "Error", 2: "Warning", 3: "Info", 4: "Hint"}
    
    def to_dict(self) -> Dict[str, Any]:
        result = {
            "range": self.range.to_dict(),
            "severity": self.severity,
            "message": self.message
        }
        if self.code is not None:
            result["code"] = self.code
        if self.source is not None:
            result["source"] = self.source
        return result
    
    @property
    def severity_name(self) -> str:
        return self.SEVERITY_NAMES.get(self.severity, "Unknown")


@dataclass
class Symbol:
    """Document or workspace symbol."""
    name: str
    kind: int
    location: Location
    containerName: Optional[str] = None
    
    SYMBOL_KINDS = {
        1: "File", 2: "Module", 3: "Namespace", 4: "Package",
        5: "Class", 6: "Method", 7: "Property", 8: "Field",
        9: "Constructor", 10: "Enum", 11: "Interface", 12: "Function",
        13: "Variable", 14: "Constant", 15: "String", 16: "Number",
        17: "Boolean", 18: "Array", 19: "Object", 20: "Key",
        21: "Null", 22: "EnumMember", 23: "Struct", 24: "Event",
        25: "Operator", 26: "TypeParameter"
    }
    
    @property
    def kind_name(self) -> str:
        return self.SYMBOL_KINDS.get(self.kind, "Unknown")


@dataclass
class CompletionItem:
    """Completion item."""
    label: str
    kind: Optional[int] = None
    detail: Optional[str] = None
    documentation: Optional[str] = None
    insertText: Optional[str] = None
    
    COMPLETION_KINDS = {
        1: "Text", 2: "Method", 3: "Function", 4: "Constructor",
        5: "Field", 6: "Variable", 7: "Class", 8: "Interface",
        9: "Module", 10: "Property", 11: "Unit", 12: "Value",
        13: "Enum", 14: "Keyword", 15: "Snippet", 16: "Color",
        17: "File", 18: "Reference", 19: "Folder", 20: "EnumMember",
        21: "Constant", 22: "Struct", 23: "Event", 24: "Operator",
        25: "TypeParameter"
    }
    
    @property
    def kind_name(self) -> str:
        if self.kind is None:
            return "Unknown"
        return self.COMPLETION_KINDS.get(self.kind, "Unknown")


@dataclass
class Hover:
    """Hover information."""
    contents: Union[str, Dict[str, Any], List[Union[str, Dict[str, Any]]]]
    range: Optional[Range] = None
    
    def get_text(self) -> str:
        """Extract plain text from hover contents."""
        if isinstance(self.contents, str):
            return self.contents
        elif isinstance(self.contents, dict):
            return self.contents.get("value", str(self.contents))
        elif isinstance(self.contents, list):
            texts = []
            for item in self.contents:
                if isinstance(item, str):
                    texts.append(item)
                elif isinstance(item, dict):
                    texts.append(item.get("value", str(item)))
            return "\n".join(texts)
        return str(self.contents)


# ============================================================================
# JSON-RPC Handler
# ============================================================================

class JSONRPCHandler:
    """Handles JSON-RPC message serialization and deserialization."""
    
    CONTENT_LENGTH_PATTERN = b"Content-Length:"
    
    def __init__(self):
        self._message_buffer = b""
        self._next_id = 1
        self._pending_requests: Dict[int, asyncio.Future] = {}
    
    def get_next_id(self) -> int:
        """Get next request ID."""
        request_id = self._next_id
        self._next_id += 1
        return request_id
    
    def create_request(self, method: str, params: Optional[Dict] = None) -> Tuple[int, bytes]:
        """Create a JSON-RPC request message."""
        request_id = self.get_next_id()
        message = {
            "jsonrpc": "2.0",
            "id": request_id,
            "method": method
        }
        if params is not None:
            message["params"] = params
        
        return request_id, self._encode_message(message)
    
    def create_notification(self, method: str, params: Optional[Dict] = None) -> bytes:
        """Create a JSON-RPC notification message."""
        message = {
            "jsonrpc": "2.0",
            "method": method
        }
        if params is not None:
            message["params"] = params
        
        return self._encode_message(message)
    
    def create_response(self, request_id: int, result: Any) -> bytes:
        """Create a JSON-RPC response message."""
        message = {
            "jsonrpc": "2.0",
            "id": request_id,
            "result": result
        }
        return self._encode_message(message)
    
    def create_error_response(self, request_id: int, code: int, message: str) -> bytes:
        """Create a JSON-RPC error response."""
        error_message = {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {
                "code": code,
                "message": message
            }
        }
        return self._encode_message(error_message)
    
    def _encode_message(self, message: Dict) -> bytes:
        """Encode a message with Content-Length header."""
        content = json.dumps(message, separators=(',', ':')).encode('utf-8')
        header = f"Content-Length: {len(content)}\r\n\r\n".encode('utf-8')
        return header + content
    
    def parse_messages(self, data: bytes) -> List[Dict]:
        """Parse JSON-RPC messages from received data."""
        self._message_buffer += data
        messages = []
        
        while True:
            # Find Content-Length header
            header_end = self._message_buffer.find(b"\r\n\r\n")
            if header_end == -1:
                break
            
            # Parse Content-Length
            header = self._message_buffer[:header_end].decode('utf-8')
            content_length = None
            for line in header.split("\r\n"):
                if line.startswith("Content-Length:"):
                    try:
                        content_length = int(line.split(":", 1)[1].strip())
                    except ValueError:
                        pass
                    break
            
            if content_length is None:
                # Malformed header, skip
                self._message_buffer = self._message_buffer[header_end + 4:]
                continue
            
            # Check if we have complete message
            message_start = header_end + 4
            message_end = message_start + content_length
            
            if len(self._message_buffer) < message_end:
                break  # Wait for more data
            
            # Extract and parse message
            content = self._message_buffer[message_start:message_end]
            self._message_buffer = self._message_buffer[message_end:]
            
            try:
                message = json.loads(content.decode('utf-8'))
                messages.append(message)
            except json.JSONDecodeError as e:
                logger.error(f"Failed to parse JSON-RPC message: {e}")
        
        return messages


# ============================================================================
# LSP Client
# ============================================================================

class LSPClient:
    """Client for communicating with a Language Server."""
    
    def __init__(
        self,
        command: List[str],
        root_uri: str,
        workspace_folders: Optional[List[Dict[str, str]]] = None,
        initialization_options: Optional[Dict] = None,
        client_capabilities: Optional[Dict] = None
    ):
        self.command = command
        self.root_uri = root_uri
        self.workspace_folders = workspace_folders or [{"uri": root_uri, "name": "workspace"}]
        self.initialization_options = initialization_options or {}
        self.client_capabilities = client_capabilities or self._default_capabilities()
        
        self._process: Optional[subprocess.Popen] = None
        self._json_rpc = JSONRPCHandler()
        self._initialized = False
        self._server_capabilities: Dict[str, Any] = {}
        self._pending_requests: Dict[int, asyncio.Future] = {}
        self._read_task: Optional[asyncio.Task] = None
        self._write_queue: asyncio.Queue = asyncio.Queue()
        self._shutdown_event = asyncio.Event()
        
        # Callbacks for notifications
        self._notification_handlers: Dict[str, Callable] = {}
        self._diagnostics: Dict[str, List[Diagnostic]] = {}
    
    def _default_capabilities(self) -> Dict[str, Any]:
        """Default client capabilities."""
        return {
            "workspace": {
                "applyEdit": True,
                "workspaceEdit": {"documentChanges": True},
                "didChangeConfiguration": {"dynamicRegistration": True},
                "didChangeWatchedFiles": {"dynamicRegistration": True},
                "symbol": {
                    "dynamicRegistration": True,
                    "symbolKind": {"valueSet": list(range(1, 27))}
                },
                "executeCommand": {"dynamicRegistration": True},
                "configuration": True,
                "workspaceFolders": True
            },
            "textDocument": {
                "synchronization": {
                    "dynamicRegistration": True,
                    "willSave": True,
                    "willSaveWaitUntil": True,
                    "didSave": True
                },
                "completion": {
                    "dynamicRegistration": True,
                    "completionItem": {
                        "snippetSupport": True,
                        "commitCharactersSupport": True,
                        "documentationFormat": ["markdown", "plaintext"],
                        "deprecatedSupport": True,
                        "preselectSupport": True
                    }
                },
                "hover": {
                    "dynamicRegistration": True,
                    "contentFormat": ["markdown", "plaintext"]
                },
                "signatureHelp": {
                    "dynamicRegistration": True,
                    "signatureInformation": {
                        "documentationFormat": ["markdown", "plaintext"]
                    }
                },
                "definition": {"dynamicRegistration": True, "linkSupport": True},
                "references": {"dynamicRegistration": True},
                "documentHighlight": {"dynamicRegistration": True},
                "documentSymbol": {
                    "dynamicRegistration": True,
                    "symbolKind": {"valueSet": list(range(1, 27))},
                    "hierarchicalDocumentSymbolSupport": True
                },
                "codeAction": {
                    "dynamicRegistration": True,
                    "codeActionLiteralSupport": {
                        "codeActionKind": {"valueSet": ["", "quickfix", "refactor", "source"]}
                    }
                },
                "formatting": {"dynamicRegistration": True},
                "rename": {"dynamicRegistration": True},
                "publishDiagnostics": {
                    "relatedInformation": True,
                    "versionSupport": True,
                    "tagSupport": {"valueSet": [1, 2]}
                }
            }
        }
    
    async def start(self) -> bool:
        """Start the language server process."""
        try:
            logger.info(f"Starting LSP server: {' '.join(self.command)}")
            
            self._process = subprocess.Popen(
                self.command,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                bufsize=0
            )
            
            # Start read and write tasks
            self._read_task = asyncio.create_task(self._read_loop())
            asyncio.create_task(self._write_loop())
            
            # Initialize
            return await self._initialize()
            
        except Exception as e:
            logger.error(f"Failed to start LSP server: {e}")
            return False
    
    async def stop(self):
        """Stop the language server."""
        if not self._initialized:
            return
        
        try:
            # Shutdown
            await self._send_request("shutdown")
            
            # Exit notification
            await self._send_notification("exit")
            
            self._shutdown_event.set()
            
            if self._read_task:
                self._read_task.cancel()
            
            if self._process:
                self._process.terminate()
                try:
                    self._process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self._process.kill()
                    self._process.wait()
            
            logger.info("LSP server stopped")
            
        except Exception as e:
            logger.error(f"Error stopping LSP server: {e}")
    
    async def _initialize(self) -> bool:
        """Initialize the language server."""
        params = {
            "processId": os.getpid(),
            "rootUri": self.root_uri,
            "workspaceFolders": self.workspace_folders,
            "capabilities": self.client_capabilities,
            "initializationOptions": self.initialization_options
        }
        
        try:
            result = await self._send_request("initialize", params)
            if result:
                self._server_capabilities = result.get("capabilities", {})
                self._initialized = True
                
                # Send initialized notification
                await self._send_notification("initialized", {})
                
                logger.info("LSP server initialized successfully")
                return True
        except Exception as e:
            logger.error(f"Failed to initialize LSP server: {e}")
        
        return False
    
    async def _read_loop(self):
        """Read messages from the language server."""
        try:
            while not self._shutdown_event.is_set():
                if self._process and self._process.stdout:
                    data = self._process.stdout.read(4096)
                    if not data:
                        await asyncio.sleep(0.01)
                        continue
                    
                    messages = self._json_rpc.parse_messages(data)
                    for message in messages:
                        await self._handle_message(message)
                else:
                    await asyncio.sleep(0.1)
        except asyncio.CancelledError:
            pass
        except Exception as e:
            logger.error(f"Error in read loop: {e}")
    
    async def _write_loop(self):
        """Write messages to the language server."""
        try:
            while not self._shutdown_event.is_set():
                try:
                    message = await asyncio.wait_for(
                        self._write_queue.get(),
                        timeout=0.1
                    )
                    if self._process and self._process.stdin:
                        self._process.stdin.write(message)
                        self._process.stdin.flush()
                except asyncio.TimeoutError:
                    continue
        except asyncio.CancelledError:
            pass
        except Exception as e:
            logger.error(f"Error in write loop: {e}")
    
    async def _handle_message(self, message: Dict):
        """Handle incoming message from language server."""
        if "id" in message:
            if "result" in message or "error" in message:
                # Response to a request
                request_id = message["id"]
                if request_id in self._pending_requests:
                    future = self._pending_requests.pop(request_id)
                    if "error" in message:
                        future.set_exception(
                            Exception(f"LSP Error: {message['error']}")
                        )
                    else:
                        future.set_result(message.get("result"))
        else:
            # Notification from server
            method = message.get("method", "")
            params = message.get("params", {})
            
            if method == "textDocument/publishDiagnostics":
                await self._handle_diagnostics(params)
            elif method in self._notification_handlers:
                await self._notification_handlers[method](params)
    
    async def _handle_diagnostics(self, params: Dict):
        """Handle diagnostics notification."""
        uri = params.get("uri", "")
        diagnostics_data = params.get("diagnostics", [])
        
        diagnostics = []
        for d in diagnostics_data:
            diagnostic = Diagnostic(
                range=Range.from_dict(d["range"]),
                severity=d.get("severity", 1),
                code=d.get("code"),
                source=d.get("source"),
                message=d["message"]
            )
            diagnostics.append(diagnostic)
        
        self._diagnostics[uri] = diagnostics
        logger.debug(f"Received {len(diagnostics)} diagnostics for {uri}")
    
    async def _send_request(self, method: str, params: Optional[Dict] = None) -> Any:
        """Send a request and wait for response."""
        request_id, message = self._json_rpc.create_request(method, params)
        
        future = asyncio.get_event_loop().create_future()
        self._pending_requests[request_id] = future
        
        await self._write_queue.put(message)
        
        try:
            return await asyncio.wait_for(future, timeout=30)
        except asyncio.TimeoutError:
            self._pending_requests.pop(request_id, None)
            raise Exception(f"Request timeout: {method}")
    
    async def _send_notification(self, method: str, params: Optional[Dict] = None):
        """Send a notification (fire and forget)."""
        message = self._json_rpc.create_notification(method, params)
        await self._write_queue.put(message)
    
    # ===================================================================
    # LSP Feature Methods
    # ===================================================================
    
    async def text_document_did_open(self, uri: str, language_id: str, version: int, text: str):
        """Notify server that document was opened."""
        params = {
            "textDocument": {
                "uri": uri,
                "languageId": language_id,
                "version": version,
                "text": text
            }
        }
        await self._send_notification("textDocument/didOpen", params)
    
    async def text_document_did_change(self, uri: str, version: int, changes: List[Dict]):
        """Notify server that document was changed."""
        params = {
            "textDocument": {"uri": uri, "version": version},
            "contentChanges": changes
        }
        await self._send_notification("textDocument/didChange", params)
    
    async def text_document_did_save(self, uri: str, text: Optional[str] = None):
        """Notify server that document was saved."""
        params = {"textDocument": {"uri": uri}}
        if text is not None:
            params["text"] = text
        await self._send_notification("textDocument/didSave", params)
    
    async def text_document_did_close(self, uri: str):
        """Notify server that document was closed."""
        params = {"textDocument": {"uri": uri}}
        await self._send_notification("textDocument/didClose", params)
    
    async def goto_definition(self, uri: str, line: int, character: int) -> List[Location]:
        """Go to definition."""
        params = {
            "textDocument": {"uri": uri},
            "position": {"line": line, "character": character}
        }
        
        result = await self._send_request("textDocument/definition", params)
        
        if result is None:
            return []
        
        # Result can be single location or array
        if isinstance(result, list):
            return [Location.from_dict(r) for r in result]
        else:
            return [Location.from_dict(result)]
    
    async def find_references(
        self,
        uri: str,
        line: int,
        character: int,
        include_declaration: bool = True
    ) -> List[Location]:
        """Find references."""
        params = {
            "textDocument": {"uri": uri},
            "position": {"line": line, "character": character},
            "context": {"includeDeclaration": include_declaration}
        }
        
        result = await self._send_request("textDocument/references", params)
        
        if result is None:
            return []
        
        return [Location.from_dict(r) for r in result]
    
    async def get_hover(self, uri: str, line: int, character: int) -> Optional[Hover]:
        """Get hover information."""
        params = {
            "textDocument": {"uri": uri},
            "position": {"line": line, "character": character}
        }
        
        result = await self._send_request("textDocument/hover", params)
        
        if result is None:
            return None
        
        hover_range = None
        if "range" in result:
            hover_range = Range.from_dict(result["range"])
        
        return Hover(contents=result["contents"], range=hover_range)
    
    async def get_completion(
        self,
        uri: str,
        line: int,
        character: int,
        trigger_kind: Optional[int] = None,
        trigger_character: Optional[str] = None
    ) -> List[CompletionItem]:
        """Get completion items."""
        params = {
            "textDocument": {"uri": uri},
            "position": {"line": line, "character": character}
        }
        
        if trigger_kind is not None:
            params["context"] = {"triggerKind": trigger_kind}
            if trigger_character is not None:
                params["context"]["triggerCharacter"] = trigger_character
        
        result = await self._send_request("textDocument/completion", params)
        
        if result is None:
            return []
        
        # Result can be CompletionList or CompletionItem[]
        if isinstance(result, list):
            items = result
        else:
            items = result.get("items", [])
        
        completions = []
        for item in items:
            completion = CompletionItem(
                label=item["label"],
                kind=item.get("kind"),
                detail=item.get("detail"),
                documentation=item.get("documentation"),
                insertText=item.get("insertText")
            )
            completions.append(completion)
        
        return completions
    
    async def get_document_symbols(self, uri: str) -> List[Symbol]:
        """Get document symbols."""
        params = {"textDocument": {"uri": uri}}
        
        result = await self._send_request("textDocument/documentSymbol", params)
        
        if result is None:
            return []
        
        symbols = []
        for item in result:
            # Can be SymbolInformation or DocumentSymbol
            if "location" in item:
                # SymbolInformation
                symbol = Symbol(
                    name=item["name"],
                    kind=item["kind"],
                    location=Location.from_dict(item["location"]),
                    containerName=item.get("containerName")
                )
            else:
                # DocumentSymbol
                symbol = Symbol(
                    name=item["name"],
                    kind=item["kind"],
                    location=Location(
                        uri=uri,
                        range=Range.from_dict(item["range"])
                    ),
                    containerName=item.get("detail")
                )
            symbols.append(symbol)
        
        return symbols
    
    async def get_workspace_symbols(self, query: str) -> List[Symbol]:
        """Get workspace symbols."""
        params = {"query": query}
        
        result = await self._send_request("workspace/symbol", params)
        
        if result is None:
            return []
        
        symbols = []
        for item in result:
            symbol = Symbol(
                name=item["name"],
                kind=item["kind"],
                location=Location.from_dict(item["location"]),
                containerName=item.get("containerName")
            )
            symbols.append(symbol)
        
        return symbols
    
    async def get_code_actions(
        self,
        uri: str,
        range_obj: Range,
        diagnostics: Optional[List[Diagnostic]] = None
    ) -> List[Dict]:
        """Get code actions."""
        params = {
            "textDocument": {"uri": uri},
            "range": range_obj.to_dict(),
            "context": {"diagnostics": [d.to_dict() for d in (diagnostics or [])]}
        }
        
        result = await self._send_request("textDocument/codeAction", params)
        
        return result or []
    
    def get_diagnostics(self, uri: str) -> List[Diagnostic]:
        """Get cached diagnostics for a document."""
        return self._diagnostics.get(uri, [])
    
    @property
    def server_capabilities(self) -> Dict[str, Any]:
        """Get server capabilities."""
        return self._server_capabilities
    
    @property
    def is_initialized(self) -> bool:
        """Check if client is initialized."""
        return self._initialized


# ============================================================================
# MCP Server Interface
# ============================================================================

class MCPServerInterface:
    """MCP Server interface for exposing LSP capabilities."""
    
    def __init__(self, lsp_client: LSPClient):
        self.lsp_client = lsp_client
        self._tools: Dict[str, Callable] = {}
        self._register_tools()
    
    def _register_tools(self):
        """Register MCP tools."""
        self._tools = {
            "lsp_goto_definition": self.goto_definition,
            "lsp_find_references": self.find_references,
            "lsp_get_hover": self.get_hover,
            "lsp_get_completion": self.get_completion,
            "lsp_get_document_symbols": self.get_document_symbols,
            "lsp_get_workspace_symbols": self.get_workspace_symbols,
            "lsp_get_diagnostics": self.get_diagnostics,
            "lsp_get_code_actions": self.get_code_actions,
            "lsp_open_document": self.open_document,
            "lsp_analyze_symbol": self.analyze_symbol,
            "lsp_find_symbol_in_workspace": self.find_symbol_in_workspace
        }
    
    def get_tools(self) -> Dict[str, Callable]:
        """Get available tools."""
        return self._tools
    
    async def goto_definition(self, params: Dict) -> Dict:
        """Go to definition of a symbol."""
        try:
            uri = params["uri"]
            line = params["line"]
            character = params["character"]
            
            locations = await self.lsp_client.goto_definition(uri, line, character)
            
            return {
                "success": True,
                "locations": [loc.to_dict() for loc in locations],
                "count": len(locations)
            }
        except Exception as e:
            logger.error(f"goto_definition error: {e}")
            return {"success": False, "error": str(e)}
    
    async def find_references(self, params: Dict) -> Dict:
        """Find all references to a symbol."""
        try:
            uri = params["uri"]
            line = params["line"]
            character = params["character"]
            include_declaration = params.get("include_declaration", True)
            
            locations = await self.lsp_client.find_references(
                uri, line, character, include_declaration
            )
            
            return {
                "success": True,
                "locations": [loc.to_dict() for loc in locations],
                "count": len(locations)
            }
        except Exception as e:
            logger.error(f"find_references error: {e}")
            return {"success": False, "error": str(e)}
    
    async def get_hover(self, params: Dict) -> Dict:
        """Get hover information for a symbol."""
        try:
            uri = params["uri"]
            line = params["line"]
            character = params["character"]
            
            hover = await self.lsp_client.get_hover(uri, line, character)
            
            if hover is None:
                return {"success": True, "contents": None}
            
            result = {
                "success": True,
                "contents": hover.get_text()
            }
            
            if hover.range:
                result["range"] = hover.range.to_dict()
            
            return result
        except Exception as e:
            logger.error(f"get_hover error: {e}")
            return {"success": False, "error": str(e)}
    
    async def get_completion(self, params: Dict) -> Dict:
        """Get code completions at a position."""
        try:
            uri = params["uri"]
            line = params["line"]
            character = params["character"]
            
            completions = await self.lsp_client.get_completion(uri, line, character)
            
            return {
                "success": True,
                "items": [
                    {
                        "label": c.label,
                        "kind": c.kind_name,
                        "detail": c.detail,
                        "documentation": c.documentation
                    }
                    for c in completions
                ],
                "count": len(completions)
            }
        except Exception as e:
            logger.error(f"get_completion error: {e}")
            return {"success": False, "error": str(e)}
    
    async def get_document_symbols(self, params: Dict) -> Dict:
        """Get all symbols in a document."""
        try:
            uri = params["uri"]
            
            symbols = await self.lsp_client.get_document_symbols(uri)
            
            return {
                "success": True,
                "symbols": [
                    {
                        "name": s.name,
                        "kind": s.kind_name,
                        "location": s.location.to_dict(),
                        "containerName": s.containerName
                    }
                    for s in symbols
                ],
                "count": len(symbols)
            }
        except Exception as e:
            logger.error(f"get_document_symbols error: {e}")
            return {"success": False, "error": str(e)}
    
    async def get_workspace_symbols(self, params: Dict) -> Dict:
        """Search for symbols in the workspace."""
        try:
            query = params["query"]
            
            symbols = await self.lsp_client.get_workspace_symbols(query)
            
            return {
                "success": True,
                "symbols": [
                    {
                        "name": s.name,
                        "kind": s.kind_name,
                        "location": s.location.to_dict(),
                        "containerName": s.containerName
                    }
                    for s in symbols
                ],
                "count": len(symbols)
            }
        except Exception as e:
            logger.error(f"get_workspace_symbols error: {e}")
            return {"success": False, "error": str(e)}
    
    async def get_diagnostics(self, params: Dict) -> Dict:
        """Get diagnostics for a document."""
        try:
            uri = params["uri"]
            
            diagnostics = self.lsp_client.get_diagnostics(uri)
            
            return {
                "success": True,
                "diagnostics": [
                    {
                        "range": d.range.to_dict(),
                        "severity": d.severity_name,
                        "message": d.message,
                        "code": d.code
                    }
                    for d in diagnostics
                ],
                "count": len(diagnostics)
            }
        except Exception as e:
            logger.error(f"get_diagnostics error: {e}")
            return {"success": False, "error": str(e)}
    
    async def get_code_actions(self, params: Dict) -> Dict:
        """Get available code actions for a range."""
        try:
            uri = params["uri"]
            range_data = params["range"]
            range_obj = Range.from_dict(range_data)
            
            actions = await self.lsp_client.get_code_actions(uri, range_obj)
            
            return {
                "success": True,
                "actions": actions,
                "count": len(actions)
            }
        except Exception as e:
            logger.error(f"get_code_actions error: {e}")
            return {"success": False, "error": str(e)}
    
    async def open_document(self, params: Dict) -> Dict:
        """Open a document in the language server."""
        try:
            uri = params["uri"]
            language_id = params["language_id"]
            text = params["text"]
            
            await self.lsp_client.text_document_did_open(
                uri, language_id, 1, text
            )
            
            return {"success": True}
        except Exception as e:
            logger.error(f"open_document error: {e}")
            return {"success": False, "error": str(e)}
    
    async def analyze_symbol(self, params: Dict) -> Dict:
        """Comprehensive symbol analysis combining multiple LSP features."""
        try:
            uri = params["uri"]
            line = params["line"]
            character = params["character"]
            
            # Gather all information concurrently
            definition_task = self.lsp_client.goto_definition(uri, line, character)
            references_task = self.lsp_client.find_references(uri, line, character)
            hover_task = self.lsp_client.get_hover(uri, line, character)
            doc_symbols_task = self.lsp_client.get_document_symbols(uri)
            
            definition, references, hover, doc_symbols = await asyncio.gather(
                definition_task,
                references_task,
                hover_task,
                doc_symbols_task
            )
            
            return {
                "success": True,
                "definition": {
                    "locations": [loc.to_dict() for loc in definition],
                    "count": len(definition)
                },
                "references": {
                    "locations": [loc.to_dict() for loc in references],
                    "count": len(references)
                },
                "hover": {
                    "contents": hover.get_text() if hover else None
                },
                "document_symbols": {
                    "symbols": [
                        {"name": s.name, "kind": s.kind_name}
                        for s in doc_symbols
                    ],
                    "count": len(doc_symbols)
                }
            }
        except Exception as e:
            logger.error(f"analyze_symbol error: {e}")
            return {"success": False, "error": str(e)}
    
    async def find_symbol_in_workspace(self, params: Dict) -> Dict:
        """Find a symbol by name across the entire workspace."""
        try:
            symbol_name = params["symbol_name"]
            include_references = params.get("include_references", False)
            
            # Search workspace symbols
            symbols = await self.lsp_client.get_workspace_symbols(symbol_name)
            
            # Filter by name if needed
            matching_symbols = [
                s for s in symbols
                if symbol_name.lower() in s.name.lower()
            ]
            
            result = {
                "success": True,
                "symbols": [
                    {
                        "name": s.name,
                        "kind": s.kind_name,
                        "location": s.location.to_dict(),
                        "containerName": s.containerName
                    }
                    for s in matching_symbols
                ],
                "count": len(matching_symbols)
            }
            
            # Optionally include references
            if include_references and matching_symbols:
                references_map = {}
                for symbol in matching_symbols[:5]:  # Limit to avoid overload
                    loc = symbol.location
                    refs = await self.lsp_client.find_references(
                        loc.uri,
                        loc.range.start.line,
                        loc.range.start.character
                    )
                    references_map[f"{symbol.name}:{loc.uri}"] = [
                        r.to_dict() for r in refs
                    ]
                result["references_map"] = references_map
            
            return result
        except Exception as e:
            logger.error(f"find_symbol_in_workspace error: {e}")
            return {"success": False, "error": str(e)}


# ============================================================================
# LSP-MCP Bridge Main Class
# ============================================================================

class LSPMCPBridge:
    """
    Main bridge class connecting LSP clients to MCP interface.
    
    Manages multiple language server connections and exposes
    unified code intelligence capabilities.
    """
    
    def __init__(self, config_path: Optional[str] = None):
        self.config_path = config_path or "config/lsp_config.json"
        self.config: Dict[str, Any] = {}
        self.clients: Dict[str, LSPClient] = {}
        self.mcp_interfaces: Dict[str, MCPServerInterface] = {}
        self._language_map: Dict[str, str] = {}
    
    async def initialize(self):
        """Initialize the bridge with configuration."""
        await self._load_config()
        await self._setup_clients()
    
    async def _load_config(self):
        """Load LSP configuration."""
        try:
            with open(self.config_path, 'r') as f:
                self.config = json.load(f)
            logger.info(f"Loaded LSP config from {self.config_path}")
        except Exception as e:
            logger.error(f"Failed to load config: {e}")
            self.config = self._default_config()
    
    def _default_config(self) -> Dict:
        """Default configuration."""
        return {
            "servers": {
                "typescript": {
                    "command": ["typescript-language-server", "--stdio"],
                    "filetypes": ["typescript", "javascript", "typescriptreact", "javascriptreact"],
                    "rootPatterns": ["package.json", "tsconfig.json"]
                },
                "python": {
                    "command": ["pyright-langserver", "--stdio"],
                    "filetypes": ["python"],
                    "rootPatterns": ["pyproject.toml", "setup.py"]
                },
                "rust": {
                    "command": ["rust-analyzer"],
                    "filetypes": ["rust"],
                    "rootPatterns": ["Cargo.toml"]
                },
                "go": {
                    "command": ["gopls"],
                    "filetypes": ["go"],
                    "rootPatterns": ["go.mod"]
                }
            },
            "workspace": {
                "rootUri": "file:///workspace"
            }
        }
    
    async def _setup_clients(self):
        """Setup LSP clients for configured servers."""
        servers = self.config.get("servers", {})
        root_uri = self.config.get("workspace", {}).get("rootUri", "file:///workspace")
        
        for server_name, server_config in servers.items():
            try:
                # Build filetype to server mapping
                for ft in server_config.get("filetypes", []):
                    self._language_map[ft] = server_name
                
                # Create and start client
                client = LSPClient(
                    command=server_config["command"],
                    root_uri=root_uri,
                    initialization_options=server_config.get("initializationOptions", {})
                )
                
                success = await client.start()
                if success:
                    self.clients[server_name] = client
                    self.mcp_interfaces[server_name] = MCPServerInterface(client)
                    logger.info(f"Started LSP server: {server_name}")
                else:
                    logger.error(f"Failed to start LSP server: {server_name}")
                    
            except Exception as e:
                logger.error(f"Error setting up {server_name}: {e}")
    
    def get_client_for_file(self, file_path: str) -> Optional[LSPClient]:
        """Get appropriate LSP client for a file."""
        # Determine language from file extension
        ext = Path(file_path).suffix.lower()
        language_map = {
            ".ts": "typescript",
            ".tsx": "typescript",
            ".js": "typescript",
            ".jsx": "typescript",
            ".py": "python",
            ".rs": "rust",
            ".go": "go"
        }
        
        language = language_map.get(ext)
        if not language:
            return None
        
        server_name = self._language_map.get(language)
        if not server_name:
            return None
        
        return self.clients.get(server_name)
    
    def get_mcp_interface_for_file(self, file_path: str) -> Optional[MCPServerInterface]:
        """Get MCP interface for a file."""
        ext = Path(file_path).suffix.lower()
        language_map = {
            ".ts": "typescript",
            ".tsx": "typescript",
            ".js": "typescript",
            ".jsx": "typescript",
            ".py": "python",
            ".rs": "rust",
            ".go": "go"
        }
        
        language = language_map.get(ext)
        if not language:
            return None
        
        server_name = self._language_map.get(language)
        if not server_name:
            return None
        
        return self.mcp_interfaces.get(server_name)
    
    async def execute_tool(self, tool_name: str, params: Dict, file_path: str) -> Dict:
        """Execute an LSP tool for a specific file."""
        mcp_interface = self.get_mcp_interface_for_file(file_path)
        
        if not mcp_interface:
            return {
                "success": False,
                "error": f"No LSP server available for file: {file_path}"
            }
        
        tools = mcp_interface.get_tools()
        
        if tool_name not in tools:
            return {
                "success": False,
                "error": f"Unknown tool: {tool_name}"
            }
        
        return await tools[tool_name](params)
    
    async def shutdown(self):
        """Shutdown all LSP clients."""
        for server_name, client in self.clients.items():
            try:
                await client.stop()
                logger.info(f"Stopped LSP server: {server_name}")
            except Exception as e:
                logger.error(f"Error stopping {server_name}: {e}")
        
        self.clients.clear()
        self.mcp_interfaces.clear()


# ============================================================================
# Factory and Helper Functions
# ============================================================================

class LSPBridgeFactory:
    """Factory for creating LSP-MCP bridge instances."""
    
    _instance: Optional[LSPMCPBridge] = None
    
    @classmethod
    async def get_bridge(cls, config_path: Optional[str] = None) -> LSPMCPBridge:
        """Get or create singleton bridge instance."""
        if cls._instance is None:
            cls._instance = LSPMCPBridge(config_path)
            await cls._instance.initialize()
        return cls._instance
    
    @classmethod
    async def shutdown(cls):
        """Shutdown the bridge."""
        if cls._instance:
            await cls._instance.shutdown()
            cls._instance = None


async def analyze_code_symbol(
    file_path: str,
    line: int,
    character: int,
    config_path: Optional[str] = None
) -> Dict:
    """High-level function to analyze a code symbol."""
    bridge = await LSPBridgeFactory.get_bridge(config_path)
    
    # Convert file path to URI
    uri = f"file://{os.path.abspath(file_path)}"
    
    # Read file and open in LSP
    try:
        with open(file_path, 'r') as f:
            text = f.read()
        
        ext = Path(file_path).suffix.lower()
        language_map = {
            ".ts": "typescript",
            ".tsx": "typescriptreact",
            ".js": "javascript",
            ".jsx": "javascriptreact",
            ".py": "python",
            ".rs": "rust",
            ".go": "go"
        }
        language_id = language_map.get(ext, "plaintext")
        
        # Open document
        mcp = bridge.get_mcp_interface_for_file(file_path)
        if mcp:
            await mcp.open_document({
                "uri": uri,
                "language_id": language_id,
                "text": text
            })
    except Exception as e:
        logger.warning(f"Could not open document: {e}")
    
    # Execute analysis
    result = await bridge.execute_tool(
        "lsp_analyze_symbol",
        {"uri": uri, "line": line, "character": character},
        file_path
    )
    
    return result


async def find_symbol_references(
    file_path: str,
    line: int,
    character: int,
    include_declaration: bool = True,
    config_path: Optional[str] = None
) -> Dict:
    """Find all references to a symbol."""
    bridge = await LSPBridgeFactory.get_bridge(config_path)
    
    uri = f"file://{os.path.abspath(file_path)}"
    
    result = await bridge.execute_tool(
        "lsp_find_references",
        {
            "uri": uri,
            "line": line,
            "character": character,
            "include_declaration": include_declaration
        },
        file_path
    )
    
    return result


# ============================================================================
# Main Entry Point
# ============================================================================

async def main():
    """Main entry point for testing."""
    # Example usage
    bridge = LSPMCPBridge()
    await bridge.initialize()
    
    print(f"Active LSP servers: {list(bridge.clients.keys())}")
    
    # Test with a sample file if provided
    import sys
    if len(sys.argv) > 1:
        test_file = sys.argv[1]
        if os.path.exists(test_file):
            print(f"\nAnalyzing: {test_file}")
            
            # Get document symbols
            uri = f"file://{os.path.abspath(test_file)}"
            mcp = bridge.get_mcp_interface_for_file(test_file)
            
            if mcp:
                # Read and open file
                with open(test_file, 'r') as f:
                    text = f.read()
                
                ext = Path(test_file).suffix.lower()
                language_map = {
                    ".ts": "typescript",
                    ".py": "python",
                    ".rs": "rust",
                    ".go": "go"
                }
                
                await mcp.open_document({
                    "uri": uri,
                    "language_id": language_map.get(ext, "plaintext"),
                    "text": text
                })
                
                # Get symbols
                symbols_result = await mcp.get_document_symbols({"uri": uri})
                print(f"\nDocument Symbols ({symbols_result.get('count', 0)}):")
                for sym in symbols_result.get("symbols", [])[:10]:
                    print(f"  - {sym['name']} ({sym['kind']})")
    
    await bridge.shutdown()


if __name__ == "__main__":
    asyncio.run(main())
