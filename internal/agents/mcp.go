package agents

import (
	"context"
	"fmt"
	"strings"

	mcpclient "digital.vasic.mcp/pkg/client"
	mcpprotocol "digital.vasic.mcp/pkg/protocol"
)

// MCPServerConfig holds the connection parameters for a single MCP server.
type MCPServerConfig struct {
	// Name is a human-readable label used as a prefix for registered tools.
	Name string
	// URL is the base URL of the MCP server (required for HTTP transport).
	URL string
	// Transport selects the transport type: "http" or "stdio".
	Transport mcpclient.TransportType
	// ServerCommand is the command to start the server (stdio transport only).
	ServerCommand string
	// ServerArgs are additional arguments for the server command (stdio only).
	ServerArgs []string
}

// MCPTool wraps a single MCP server tool and implements the Tool interface so
// it can be registered in a ToolRegistry alongside built-in tools.
type MCPTool struct {
	name        string
	description string
	parameters  map[string]interface{}
	client      mcpclient.Client
	serverName  string
}

// Name returns the tool's unique identifier (prefixed with the server name).
func (t *MCPTool) Name() string { return t.name }

// Description returns a human-readable description of the tool.
func (t *MCPTool) Description() string { return t.description }

// Parameters returns the JSON-Schema-like map describing the tool's arguments.
func (t *MCPTool) Parameters() map[string]interface{} { return t.parameters }

// Execute calls the tool on the remote MCP server and returns the text result.
func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("mcp tool %q: no client configured", t.name)
	}

	// The raw tool name on the server is the part after "<serverName>_".
	rawName := strings.TrimPrefix(t.name, t.serverName+"_")

	result, err := t.client.CallTool(ctx, rawName, args)
	if err != nil {
		return "", fmt.Errorf("mcp tool %q: call failed: %w", t.name, err)
	}

	if result.IsError {
		return "", fmt.Errorf("mcp tool %q: server returned error", t.name)
	}

	return contentBlocksToText(result.Content), nil
}

// contentBlocksToText joins all text content blocks into a single string.
func contentBlocksToText(blocks []mcpprotocol.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// NewMCPToolForTest constructs an MCPTool with no client, intended for unit
// tests that exercise Name/Description/Parameters and nil-client error paths.
func NewMCPToolForTest(name, description string, parameters map[string]interface{}) *MCPTool {
	return &MCPTool{
		name:        name,
		description: description,
		parameters:  parameters,
	}
}

// newMCPClient creates an initialised MCP client for the given server config.
func newMCPClient(cfg MCPServerConfig) (mcpclient.Client, error) {
	clientCfg := mcpclient.DefaultConfig()
	clientCfg.Transport = cfg.Transport
	clientCfg.ServerURL = cfg.URL
	clientCfg.ServerCommand = cfg.ServerCommand
	clientCfg.ServerArgs = cfg.ServerArgs
	clientCfg.ClientName = "helixllm"
	clientCfg.ClientVersion = "1.0.0"

	switch cfg.Transport {
	case mcpclient.TransportHTTP:
		c, err := mcpclient.NewHTTPClient(clientCfg)
		if err != nil {
			return nil, fmt.Errorf("create HTTP MCP client for %q: %w", cfg.Name, err)
		}
		// Connect establishes the long-lived SSE stream. Pass a background
		// context so the stream is not cancelled when this function returns.
		if err := c.Connect(context.Background()); err != nil {
			return nil, fmt.Errorf("connect to MCP server %q: %w", cfg.Name, err)
		}
		return c, nil
	case mcpclient.TransportStdio:
		c, err := mcpclient.NewStdioClient(clientCfg)
		if err != nil {
			return nil, fmt.Errorf("create stdio MCP client for %q: %w", cfg.Name, err)
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q for server %q",
			cfg.Transport, cfg.Name)
	}
}
