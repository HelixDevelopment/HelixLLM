# Lesson 4: MCP Integration

**Duration:** 25 minutes
**Prerequisites:** Lesson 3 (Custom Tools)
**Learning Objectives:**
- Understand the Model Context Protocol (MCP) and its role in agent tool ecosystems
- Configure HelixLLM to connect to external MCP tool servers
- Register MCP tools alongside built-in tools in the ToolRegistry
- Understand how the LSP tool registry manages multi-language server connections

---

## Scene 1: What is MCP? (5 min)

**Narration:** "The Model Context Protocol, or MCP, is an open standard for connecting AI agents to external tool servers. Instead of building every tool into the HelixLLM binary, MCP lets you connect to external servers that provide tools, resources, and prompts. This creates an ecosystem where tools can be developed, shared, and deployed independently."

**Screen:** Show the MCP architecture diagram.

```
HelixLLM Agent
    |
    |--- Built-in Tools (echo, time, knowledge_query)
    |
    |--- MCP Client
           |
           |--- MCP Server A (file system tools)
           |--- MCP Server B (database tools)
           |--- MCP Server C (API integration tools)
```

**Narration:** "HelixLLM acts as an MCP client. It connects to one or more MCP servers, discovers their tools, and registers them in the ToolRegistry alongside built-in tools. The agent sees all tools -- built-in and MCP -- as a unified set."

**Key points:**
- MCP is an open protocol for AI tool interoperability
- HelixLLM is an MCP client that connects to external MCP servers
- MCP servers expose tools, resources, and prompts
- Tools from MCP servers appear alongside built-in tools
- The agent does not distinguish between built-in and MCP tools

---

## Scene 2: MCP Server Configuration (6 min)

**Narration:** "To connect to an MCP server, you define an MCPServerConfig with the server's connection details. Let me show you how to configure and register MCP tools."

**Screen:** Show the configuration structure.

```go
// MCP server configuration
type MCPServerConfig struct {
    Name      string   // Human-readable server name
    Command   string   // Server executable path
    Args      []string // Command-line arguments
    Env       []string // Environment variables
    Transport string   // "stdio" or "http"
    URL       string   // For HTTP transport
}
```

**Narration:** "MCP servers can communicate over two transports: stdio, where the server is a child process communicating over standard input and output, and HTTP, where the server runs independently and communicates over the network."

**Demo steps:**

```go
// Example: connecting to an MCP file system server via stdio
config := MCPServerConfig{
    Name:      "filesystem",
    Command:   "npx",
    Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp/workspace"},
    Transport: "stdio",
}

// Example: connecting to an MCP server via HTTP
config := MCPServerConfig{
    Name:      "database-tools",
    URL:       "http://localhost:3100/mcp",
    Transport: "http",
}
```

**Key points:**
- **stdio transport** -- server runs as a child process, communication via stdin/stdout
- **HTTP transport** -- server runs independently, communication via HTTP/SSE
- The `Command` and `Args` fields are for stdio transport
- The `URL` field is for HTTP transport
- Environment variables can be passed to stdio servers via `Env`

---

## Scene 3: Registering MCP Tools (6 min)

**Narration:** "Once configured, MCP tools are registered in the ToolRegistry using RegisterMCPTools. This discovers the available tools from the server and wraps them as standard Tool interface implementations."

**Screen:** Show the registration code.

```go
// In cmd/helixllm/main.go
toolReg := agents.NewToolRegistry()

// Register built-in tools
toolReg.Register(&tools.EchoTool{})
toolReg.Register(&tools.TimeTool{})
toolReg.Register(tools.NewKnowledgeQueryTool(pipeline, "default"))

// Register MCP tools from an external server
mcpConfig := agents.MCPServerConfig{
    Name:      "filesystem",
    Command:   "npx",
    Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp/workspace"},
    Transport: "stdio",
}

err := agents.RegisterMCPTools(toolReg, mcpConfig)
if err != nil {
    log.Warnf("MCP server %s unavailable: %v", mcpConfig.Name, err)
    // Graceful degradation -- agent works without MCP tools
}
```

**Narration:** "RegisterMCPTools connects to the server, discovers its tools through the MCP protocol, and wraps each one as a Tool implementation. If the server is unavailable, the error is logged and the agent continues with its built-in tools."

**Demo steps:**

```bash
# After registering MCP tools, verify they appear in the listing
curl -sk https://localhost:8443/v1/agents/tools | python3 -m json.tool
```

**Narration:** "MCP tools appear alongside built-in tools. The agent can now use file system operations, database queries, or whatever tools the MCP server provides."

**Key points:**
- `RegisterMCPTools` handles discovery, wrapping, and registration
- MCP tools implement the same Tool interface as built-in tools
- Graceful degradation -- the agent works without MCP if servers are unavailable
- Tool names from MCP servers are prefixed with the server name to avoid conflicts
- Multiple MCP servers can be registered simultaneously

---

## Scene 4: LSP Tool Registry (4 min)

**Narration:** "HelixLLM extends the tool concept beyond MCP with an LSP tool registry. This manages connections to Language Server Protocol servers, enabling language-aware code analysis tools."

**Screen:** Show the LSP registry concept.

```go
// LSP server registration for code analysis tools
type LSPServerConfig struct {
    Language  string   // "go", "python", "typescript", etc.
    Command   string   // LSP server executable
    Args      []string // Server arguments
}

// The LSP registry manages multiple language servers
lspRegistry := agents.NewLSPToolRegistry()
lspRegistry.Register(LSPServerConfig{
    Language: "go",
    Command:  "gopls",
})
lspRegistry.Register(LSPServerConfig{
    Language: "python",
    Command:  "pylsp",
})
```

**Narration:** "The LSP registry provides tools for code navigation, diagnostics, and refactoring. When an agent needs to analyze code, it can use LSP tools to get accurate, language-aware results instead of relying solely on the LLM's training data."

**Key points:**
- LSP servers provide language-aware code analysis
- One server per language (Go, Python, TypeScript, etc.)
- Tools include: find references, go to definition, diagnostics, hover info
- Managed by the LSP tool registry separate from the main ToolRegistry
- Servers are started on demand and share the same lifecycle as the agent

---

## Scene 5: Building an MCP Tool Server (4 min)

**Narration:** "You can also build your own MCP tool server to expose custom functionality to any MCP-compatible agent, not just HelixLLM."

**Screen:** Show a minimal MCP server example.

```python
# Example: Simple MCP server in Python using the mcp package
from mcp.server import Server
from mcp.types import Tool, TextContent

server = Server("my-tools")

@server.tool()
async def lookup_user(user_id: str) -> str:
    """Look up a user by their ID and return their profile information."""
    # Your business logic here
    return f"User {user_id}: Alice, Engineering, active since 2024"

@server.tool()
async def search_tickets(query: str, status: str = "open") -> str:
    """Search support tickets by query text and optional status filter."""
    return f"Found 3 tickets matching '{query}' with status '{status}'"

if __name__ == "__main__":
    server.run()
```

**Narration:** "This Python server exposes two tools via MCP: user lookup and ticket search. Any MCP client, including HelixLLM, can discover and use these tools. The tool descriptions and parameter types are derived from the function signatures and docstrings."

**Key points:**
- MCP servers can be built in any language (Python, Node.js, Go, etc.)
- Tools are discovered automatically by MCP clients
- The protocol handles serialization, transport, and error handling
- Servers can be shared across multiple AI agent platforms
- Build once, use with HelixLLM, Claude, and any MCP-compatible client

---

## Exercises

1. Set up an MCP file system server (using the `@modelcontextprotocol/server-filesystem` npm package) and register it with HelixLLM, then ask the agent to list files in a directory
2. Write a minimal MCP server in Python that exposes a single tool, start it locally, and register it in HelixLLM's configuration
3. Compare the tool listings before and after MCP registration by calling `GET /v1/agents/tools` and noting which tools came from built-in registration versus MCP discovery
