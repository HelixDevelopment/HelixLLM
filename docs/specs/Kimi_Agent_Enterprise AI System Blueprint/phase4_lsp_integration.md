# Phase 4: LSP Integration for Code Intelligence

## Complete Implementation Guide for Language Server Protocol Integration

---

## Table of Contents

1. [LSP Architecture Overview](#1-lsp-architecture-overview)
2. [LSP Server Setup](#2-lsp-server-setup)
3. [LSP-MCP Bridge Implementation](#3-lsp-mcp-bridge-implementation)
4. [Code Intelligence Capabilities](#4-code-intelligence-capabilities)
5. [Integration with Orchestrator](#5-integration-with-orchestrator)
6. [Configuration Files](#6-configuration-files)
7. [Usage Examples](#7-usage-examples)
8. [Installation and Setup](#8-installation-and-setup)

---

## 1. LSP Architecture Overview

### 1.1 Protocol Explanation

The Language Server Protocol (LSP) is a JSON-RPC based protocol that enables communication between development tools (clients) and language servers. It standardizes how editors and IDEs provide rich language features like auto-completion, go-to-definition, and diagnostics.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         LSP Architecture                                 │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────┐      JSON-RPC      ┌──────────────────┐               │
│  │   Client     │◄──────────────────►│  Language Server │               │
│  │  (Editor/    │    Messages        │                  │               │
│  │  IDE/Agent)  │                    │  ┌────────────┐  │               │
│  └──────────────┘                    │  │  Parser    │  │               │
│         │                            │  │  Compiler  │  │               │
│         │                            │  │  Analyzer  │  │               │
│         ▼                            │  └────────────┘  │               │
│  ┌──────────────┐                    └──────────────────┘               │
│  │   User       │                                                        │
│  │   Interface  │                                                        │
│  └──────────────┘                                                        │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Client-Server Communication

LSP uses a bidirectional communication model over stdin/stdout or TCP/WebSocket:

**Message Format:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "textDocument/definition",
  "params": {
    "textDocument": {"uri": "file:///path/to/file.ts"},
    "position": {"line": 10, "character": 15}
  }
}
```

**Response Format:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "uri": "file:///path/to/definition.ts",
    "range": {
      "start": {"line": 5, "character": 0},
      "end": {"line": 5, "character": 20}
    }
  }
}
```

### 1.3 Message Types

| Message Type | Direction | Description | Example |
|-------------|-----------|-------------|---------|
| **Request** | Client → Server | Client asks for information | `textDocument/definition` |
| **Response** | Server → Client | Server replies to request | Definition location |
| **Notification** | Bidirectional | Fire-and-forget messages | `textDocument/didChange` |

### 1.4 LSP Lifecycle

```
┌─────────────────────────────────────────────────────────────────┐
│                      LSP Lifecycle                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Initialize                                                   │
│     ├── Client sends initialize request with capabilities       │
│     ├── Server responds with server capabilities                │
│     └── Client sends initialized notification                   │
│                                                                  │
│  2. Operational                                                  │
│     ├── Text document synchronization                            │
│     ├── Language features (completion, hover, etc.)             │
│     └── Workspace features (symbols, references, etc.)          │
│                                                                  │
│  3. Shutdown                                                     │
│     ├── Client sends shutdown request                           │
│     ├── Server acknowledges                                      │
│     └── Client sends exit notification                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. LSP Server Setup

### 2.1 Project Structure

```
lsp_integration/
├── lsp_mcp_bridge.py          # Main bridge implementation
├── lsp_client.py              # LSP client wrapper
├── language_servers/          # Language server configurations
│   ├── typescript.json
│   ├── python.json
│   ├── rust.json
│   ├── go.json
│   └── java.json
├── config/
│   └── lsp_config.json        # Global LSP configuration
└── utils/
    ├── json_rpc.py            # JSON-RPC message handling
    └── process_manager.py     # Process lifecycle management
```

### 2.2 Multi-Language Server Setup

#### TypeScript/JavaScript: typescript-language-server

**Installation:**
```bash
# Via npm
npm install -g typescript-language-server typescript

# Via yarn
yarn global add typescript-language-server typescript
```

**Configuration:**
```json
{
  "name": "typescript",
  "command": "typescript-language-server",
  "args": ["--stdio"],
  "filetypes": ["typescript", "javascript", "typescriptreact", "javascriptreact"],
  "rootPatterns": ["package.json", "tsconfig.json", ".git"],
  "initializationOptions": {
    "preferences": {
      "includeCompletionsForModuleExports": true,
      "includeCompletionsWithInsertText": true
    }
  },
  "settings": {
    "typescript": {
      "inlayHints": {
        "parameterNames": {"enabled": "all"},
        "parameterTypes": {"enabled": true},
        "variableTypes": {"enabled": true},
        "functionLikeReturnTypes": {"enabled": true}
      }
    }
  }
}
```

#### Python: pyright

**Installation:**
```bash
# Via npm (recommended)
npm install -g pyright

# Via pip (pylsp alternative)
pip install python-lsp-server
```

**Configuration:**
```json
{
  "name": "python",
  "command": "pyright-langserver",
  "args": ["--stdio"],
  "filetypes": ["python"],
  "rootPatterns": ["pyproject.toml", "setup.py", "setup.cfg", "requirements.txt", ".git"],
  "settings": {
    "python": {
      "analysis": {
        "typeCheckingMode": "basic",
        "autoImportCompletions": true,
        "inlayHints": {
          "variableTypes": true,
          "functionReturnTypes": true
        }
      }
    }
  }
}
```

#### Rust: rust-analyzer

**Installation:**
```bash
# Via rustup
rustup component add rust-analyzer

# Or download latest release
curl -L https://github.com/rust-lang/rust-analyzer/releases/latest/download/rust-analyzer-x86_64-unknown-linux-gnu.gz | gunzip -c - > ~/.local/bin/rust-analyzer
chmod +x ~/.local/bin/rust-analyzer
```

**Configuration:**
```json
{
  "name": "rust",
  "command": "rust-analyzer",
  "filetypes": ["rust"],
  "rootPatterns": ["Cargo.toml", ".git"],
  "initializationOptions": {
    "cargo": {
      "buildScripts": {"enable": true},
      "features": "all"
    },
    "checkOnSave": {
      "command": "clippy",
      "extraArgs": ["--tests"]
    },
    "procMacro": {"enable": true},
    "inlayHints": {
      "bindingModeHints": {"enable": true},
      "closureReturnTypeHints": {"enable": true},
      "lifetimeElisionHints": {"enable": true}
    }
  }
}
```

#### Go: gopls

**Installation:**
```bash
# Install gopls
go install golang.org/x/tools/gopls@latest

# Ensure $GOPATH/bin is in PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

**Configuration:**
```json
{
  "name": "go",
  "command": "gopls",
  "args": ["serve", "-rpc.trace", "--debug=localhost:6060"],
  "filetypes": ["go", "gomod", "gowork", "gotmpl"],
  "rootPatterns": ["go.work", "go.mod", ".git"],
  "settings": {
    "gopls": {
      "ui.diagnostic.annotations": {
        "bounds": true,
        "escape": true,
        "inline": true,
        "nil": true
      },
      "ui.diagnostic.vulncheck": "Imports",
      "formatting.gofumpt": true,
      "ui.diagnostic.staticcheck": true,
      "hints": {
        "assignVariableTypes": true,
        "compositeLiteralFields": true,
        "compositeLiteralTypes": true,
        "constantValues": true,
        "functionTypeParameters": true,
        "parameterNames": true,
        "rangeVariableTypes": true
      }
    }
  }
}
```

#### Java: jdtls (Eclipse JDT Language Server)

**Installation:**
```bash
# Download and extract
curl -L https://download.eclipse.org/jdtls/snapshots/jdt-language-server-latest.tar.gz | tar -xz -C ~/.local/share/

# Create wrapper script
mkdir -p ~/.local/bin
cat > ~/.local/bin/jdtls << 'EOF'
#!/bin/bash
JAR="$HOME/.local/share/jdt-language-server/plugins/org.eclipse.equinox.launcher_*.jar"
GRADLE_HOME=$HOME/.gradle java \
  -Declipse.application=org.eclipse.jdt.ls.core.id1 \
  -Dosgi.bundles.defaultStartLevel=4 \
  -Declipse.product=org.eclipse.jdt.ls.core.product \
  -Dlog.protocol=true \
  -Dlog.level=ALL \
  -Xmx1G \
  --add-modules=ALL-SYSTEM \
  --add-opens java.base/java.util=ALL-UNNAMED \
  --add-opens java.base/java.lang=ALL-UNNAMED \
  -jar "$(ls $JAR | head -n1)" \
  -configuration "$HOME/.local/share/jdt-language-server/config_linux" \
  -data "$1"
EOF
chmod +x ~/.local/bin/jdtls
```

---

## 3. LSP-MCP Bridge Implementation

### 3.1 Complete Bridge Code

The complete LSP-MCP bridge implementation is provided in **`lsp_mcp_bridge.py`**.

### 3.2 Key Components

#### Data Models

- **Position**: Line and character position in a document
- **Range**: Start and end positions
- **Location**: URI + Range for symbol locations
- **Diagnostic**: Error/warning information with severity
- **Symbol**: Document/workspace symbol with kind
- **CompletionItem**: Code completion suggestion
- **Hover**: Hover information with documentation

#### JSONRPCHandler

Handles message serialization/deserialization:
- `create_request()`: Create JSON-RPC requests
- `create_notification()`: Create notifications
- `parse_messages()`: Parse incoming messages
- Content-Length header handling

#### LSPClient

Main client for LSP communication:
- `start()`: Start language server process
- `stop()`: Shutdown gracefully
- `goto_definition()`: Navigate to symbol definition
- `find_references()`: Find all symbol references
- `get_hover()`: Get hover information
- `get_completion()`: Get code completions
- `get_document_symbols()`: Get file outline
- `get_workspace_symbols()`: Search across workspace
- `get_code_actions()`: Get available code actions
- `get_diagnostics()`: Get error/warning list

#### MCPServerInterface

MCP-compatible interface exposing LSP capabilities:
- `goto_definition`: Go to symbol definition
- `find_references`: Find all references
- `get_hover`: Get hover information
- `get_completion`: Get code completions
- `get_document_symbols`: Get document symbols
- `get_workspace_symbols`: Search workspace symbols
- `get_diagnostics`: Get diagnostics
- `get_code_actions`: Get code actions
- `open_document`: Open document in LSP
- `analyze_symbol`: Comprehensive symbol analysis
- `find_symbol_in_workspace`: Find symbol across workspace

#### LSPMCPBridge

Main bridge class managing multiple servers:
- Multi-language server support
- File type to server mapping
- Unified tool execution
- Lifecycle management

#### LSPBridgeFactory

Singleton factory for bridge instances:
- `get_bridge()`: Get or create bridge
- `shutdown()`: Cleanup all resources

---

## 4. Code Intelligence Capabilities

### 4.1 Feature Implementation Matrix

| Feature | Method | Description | Use Case |
|---------|--------|-------------|----------|
| **Go to Definition** | `textDocument/definition` | Navigate to symbol definition | Understanding code structure |
| **Find References** | `textDocument/references` | Find all usages of a symbol | Refactoring, impact analysis |
| **Hover Information** | `textDocument/hover` | Get type/docs at cursor | Quick documentation lookup |
| **Code Completion** | `textDocument/completion` | Auto-complete suggestions | Code writing assistance |
| **Diagnostics** | `textDocument/publishDiagnostics` | Errors and warnings | Code quality checking |
| **Code Actions** | `textDocument/codeAction` | Quick fixes and refactorings | Automated code improvements |
| **Document Symbols** | `textDocument/documentSymbol` | Outline of current file | Navigation within file |
| **Workspace Symbols** | `workspace/symbol` | Search symbols across project | Global symbol search |

---

## 5. Integration with Orchestrator

### 5.1 Orchestrator Integration Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Orchestrator Layer                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐     │
│  │   Task Planner  │───►│  Context Builder│───►│  LLM Interface  │     │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘     │
│           │                      │                      │               │
│           │                      │                      │               │
│           ▼                      ▼                      ▼               │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                    LSP-MCP Bridge                                │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │   │
│  │  │ TypeScript  │  │   Python    │  │    Rust     │  ...        │   │
│  │  │   Server    │  │   Server    │  │   Server    │             │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘             │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Key Integration Points

1. **Task Context Analysis**: Analyze files relevant to a task
2. **Symbol Lookup**: Find symbols for code understanding
3. **Reference Analysis**: Understand code dependencies
4. **Diagnostics**: Check code quality before suggestions
5. **Context Formatting**: Format LSP results for LLM consumption

---

## 6. Configuration Files

### 6.1 Main LSP Configuration: lsp_config.json

See **`lsp_config.json`** for complete configuration including:
- TypeScript/JavaScript server settings
- Python (pyright) configuration
- Rust (rust-analyzer) configuration
- Go (gopls) configuration
- Java (jdtls) configuration
- LLM completion integration

---

## 7. Usage Examples

### 7.1 Example 1: Find and Analyze UserService Class

```python
import asyncio
from lsp_mcp_bridge import LSPBridgeFactory

async def analyze_userservice():
    # Initialize the bridge
    bridge = await LSPBridgeFactory.get_bridge("config/lsp_config.json")
    
    # Get MCP interface for TypeScript file
    mcp = bridge.get_mcp_interface_for_file("/workspace/src/services/UserService.ts")
    
    # Open the file
    await mcp.open_document({
        "uri": "file:///workspace/src/services/UserService.ts",
        "language_id": "typescript",
        "text": open("/workspace/src/services/UserService.ts").read()
    })
    
    # Get document symbols
    result = await mcp.get_document_symbols({
        "uri": "file:///workspace/src/services/UserService.ts"
    })
    
    # Find UserService
    for sym in result.get("symbols", []):
        if "UserService" in sym["name"]:
            print(f"Found: {sym['name']} ({sym['kind']})")
            
            # Analyze the symbol
            analysis = await mcp.analyze_symbol({
                "uri": "file:///workspace/src/services/UserService.ts",
                "line": sym["location"]["range"]["start"]["line"],
                "character": sym["location"]["range"]["start"]["character"]
            })
            
            print(f"References: {analysis['references']['count']}")
            print(f"Definition locations: {analysis['definition']['count']}")
    
    # Cleanup
    await LSPBridgeFactory.shutdown()

asyncio.run(analyze_userservice())
```

### 7.2 Example 2: Get All References to calculate_total Function

```python
import asyncio
from lsp_mcp_bridge import LSPBridgeFactory

async def find_calculate_total_refs():
    bridge = await LSPBridgeFactory.get_bridge("config/lsp_config.json")
    mcp = bridge.get_mcp_interface_for_file("/workspace/src/utils/calculations.py")
    
    # Open file
    await mcp.open_document({
        "uri": "file:///workspace/src/utils/calculations.py",
        "language_id": "python",
        "text": open("/workspace/src/utils/calculations.py").read()
    })
    
    # Get symbols and find calculate_total
    symbols = await mcp.get_document_symbols({
        "uri": "file:///workspace/src/utils/calculations.py"
    })
    
    for sym in symbols.get("symbols", []):
        if sym["name"] == "calculate_total":
            # Find references
            refs = await mcp.find_references({
                "uri": "file:///workspace/src/utils/calculations.py",
                "line": sym["location"]["range"]["start"]["line"],
                "character": sym["location"]["range"]["start"]["character"],
                "include_declaration": True
            })
            
            print(f"Found {refs['count']} references:")
            for loc in refs["locations"]:
                print(f"  {loc['uri']}:{loc['range']['start']['line'] + 1}")
    
    await LSPBridgeFactory.shutdown()

asyncio.run(find_calculate_total_refs())
```

### 7.3 Example 3: Show Diagnostics for Current File

```python
import asyncio
from lsp_mcp_bridge import LSPBridgeFactory

async def show_diagnostics():
    bridge = await LSPBridgeFactory.get_bridge("config/lsp_config.json")
    mcp = bridge.get_mcp_interface_for_file("/workspace/src/components/Button.tsx")
    
    # Open file
    await mcp.open_document({
        "uri": "file:///workspace/src/components/Button.tsx",
        "language_id": "typescriptreact",
        "text": open("/workspace/src/components/Button.tsx").read()
    })
    
    # Wait for diagnostics to be published
    await asyncio.sleep(1)
    
    # Get diagnostics
    result = await mcp.get_diagnostics({
        "uri": "file:///workspace/src/components/Button.tsx"
    })
    
    print(f"Found {result['count']} issues:")
    for diag in result.get("diagnostics", []):
        print(f"[{diag['severity']}] Line {diag['range']['start']['line'] + 1}: {diag['message']}")
    
    await LSPBridgeFactory.shutdown()

asyncio.run(show_diagnostics())
```

### 7.4 Example 4: Workspace Symbol Search

```python
import asyncio
from lsp_mcp_bridge import LSPBridgeFactory

async def search_workspace():
    bridge = await LSPBridgeFactory.get_bridge("config/lsp_config.json")
    
    # Use any file to get MCP interface
    mcp = bridge.get_mcp_interface_for_file("/workspace/src/main.ts")
    
    # Search for all "Service" classes
    result = await mcp.get_workspace_symbols({"query": "Service"})
    
    print(f"Found {result['count']} symbols matching 'Service':")
    for sym in result.get("symbols", []):
        print(f"  {sym['name']} ({sym['kind']}) in {sym['location']['uri']}")
    
    await LSPBridgeFactory.shutdown()

asyncio.run(search_workspace())
```

---

## 8. Installation and Setup

### 8.1 Prerequisites

```bash
# Python 3.8+
python --version

# Node.js (for TypeScript/JavaScript servers)
node --version

# Rust (for rust-analyzer)
rustc --version

# Go (for gopls)
go version

# Java (for jdtls)
java -version
```

### 8.2 Install Language Servers

```bash
#!/bin/bash
# install_lsp_servers.sh

echo "Installing Language Servers..."

# TypeScript/JavaScript
npm install -g typescript-language-server typescript

# Python
echo "Installing Python language servers..."
npm install -g pyright

# Rust
echo "Installing Rust analyzer..."
rustup component add rust-analyzer

# Go
echo "Installing Go language server..."
go install golang.org/x/tools/gopls@latest

# Java
echo "Installing Java language server..."
mkdir -p ~/.local/share
curl -L https://download.eclipse.org/jdtls/snapshots/jdt-language-server-latest.tar.gz | tar -xz -C ~/.local/share/

# Create jdtls wrapper
mkdir -p ~/.local/bin
cat > ~/.local/bin/jdtls << 'EOF'
#!/bin/bash
JAR="$HOME/.local/share/jdt-language-server/plugins/org.eclipse.equinox.launcher_*.jar"
java \
  -Declipse.application=org.eclipse.jdt.ls.core.id1 \
  -Dosgi.bundles.defaultStartLevel=4 \
  -Declipse.product=org.eclipse.jdt.ls.core.product \
  -Xmx1G \
  --add-modules=ALL-SYSTEM \
  --add-opens java.base/java.util=ALL-UNNAMED \
  --add-opens java.base/java.lang=ALL-UNNAMED \
  -jar "$(ls $JAR | head -n1)" \
  -configuration "$HOME/.local/share/jdt-language-server/config_linux" \
  -data "$1"
EOF
chmod +x ~/.local/bin/jdtls

echo "Installation complete!"
echo "Make sure ~/.local/bin is in your PATH"
```

### 8.3 Setup Python Environment

```bash
# Create virtual environment
python -m venv .venv
source .venv/bin/activate

# No external dependencies needed for core LSP client!
```

### 8.4 Configuration

```bash
# Create config directory
mkdir -p config

# Copy configuration files
cp lsp_config.json config/

# Update paths in config for your workspace
sed -i "s|/workspace|$(pwd)|g" config/lsp_config.json
```

### 8.5 Running the Bridge

```bash
# Start the LSP-MCP bridge
python lsp_mcp_bridge.py /path/to/test/file.ts

# Or use in your Python code
python -c "
import asyncio
from lsp_mcp_bridge import LSPBridgeFactory

async def main():
    bridge = await LSPBridgeFactory.get_bridge('config/lsp_config.json')
    print(f'Active servers: {list(bridge.clients.keys())}')
    await LSPBridgeFactory.shutdown()

asyncio.run(main())
"
```

---

## 9. Troubleshooting

### 9.1 Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| LSP server not found | PATH issue | Add server binary location to PATH |
| Connection timeout | Server not starting | Check server logs, verify installation |
| No completions | File not opened | Call `text_document_did_open` first |
| Wrong language server | Filetype mapping | Check `filetypes` in config |
| High memory usage | Too many servers | Disable unused servers in config |

### 9.2 Debug Logging

```python
import logging

# Enable debug logging
logging.getLogger('lsp_mcp_bridge').setLevel(logging.DEBUG)

# Log to file
logging.basicConfig(
    level=logging.DEBUG,
    filename='lsp_debug.log',
    filemode='w'
)
```

### 9.3 Testing Server Connection

```bash
# Test TypeScript server
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | typescript-language-server --stdio

# Test Python server
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | pyright-langserver --stdio
```

---

## Summary

This implementation provides a complete LSP-MCP bridge for code intelligence:

### Key Components:

1. **lsp_mcp_bridge.py** - Core bridge implementation with:
   - JSON-RPC message handling
   - LSP client lifecycle management
   - MCP server interface
   - Multi-language server support

2. **lsp_config.json** - Ready-to-use configuration for:
   - TypeScript/JavaScript
   - Python
   - Rust
   - Go
   - Java

3. **Complete API** exposing all major LSP features:
   - Go to definition
   - Find references
   - Hover information
   - Code completion
   - Diagnostics
   - Code actions
   - Document symbols
   - Workspace symbols

### Next Steps:

1. Install language servers using the provided script
2. Configure workspace paths in `lsp_config.json`
3. Run integration tests to verify setup
4. Integrate with orchestrator layer
5. Customize for specific project needs

---

## Generated Files

The following files have been generated:

| File | Description |
|------|-------------|
| `/mnt/okcomputer/output/phase4_lsp_integration.md` | This documentation |
| `/mnt/okcomputer/output/lsp_mcp_bridge.py` | Complete LSP-MCP bridge implementation |
| `/mnt/okcomputer/output/lsp_config.json` | Multi-language LSP configuration |

---

*Generated for Phase 4: LSP Integration for Code Intelligence*
