package agents

import (
	"context"
	"fmt"
)

// RegisterMCPTools discovers all tools from each listed MCP server and
// registers them in the given ToolRegistry. Each tool name is prefixed with
// the server's Name field to avoid collisions across servers.
//
// The function initialises one MCP client per server, performs the MCP
// handshake, lists available tools, and wraps each one as an MCPTool.
//
// If any server fails to connect or list tools, an error is returned and no
// tools from that server are registered (already-registered tools from earlier
// servers remain in place).
func RegisterMCPTools(registry *ToolRegistry, servers []MCPServerConfig) error {
	ctx := context.Background()

	for _, srv := range servers {
		if err := registerToolsFromServer(ctx, registry, srv); err != nil {
			return fmt.Errorf("RegisterMCPTools: server %q: %w", srv.Name, err)
		}
	}
	return nil
}

// registerToolsFromServer connects to a single MCP server, lists its tools,
// and registers each one in the registry.
func registerToolsFromServer(
	ctx context.Context,
	registry *ToolRegistry,
	srv MCPServerConfig,
) error {
	client, err := newMCPClient(srv)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	// Perform the MCP initialisation handshake.
	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("list tools: %w", err)
	}

	for _, t := range tools {
		tool := &MCPTool{
			// Prefix with server name to avoid name collisions.
			name:        srv.Name + "_" + t.Name,
			description: t.Description,
			parameters:  t.InputSchema,
			client:      client,
			serverName:  srv.Name,
		}
		if err := registry.Register(tool); err != nil {
			// A duplicate name is non-fatal — skip and continue.
			continue
		}
	}
	return nil
}
