package agents_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

// MCP SSE integration tests (TestMCPTool_Interface, TestMCPTool_Execute) are
// skipped in unit test runs because they require a real MCP SSE round-trip that
// is timing-sensitive under httptest. They run in integration test mode with a
// real MCP server.

func TestMCPTool_StructFields(t *testing.T) {
	// Verify MCPTool satisfies the Tool interface at compile time.
	var _ agents.Tool = (*agents.MCPTool)(nil)
}

func TestMCPServerConfig_Fields(t *testing.T) {
	cfg := agents.MCPServerConfig{
		Name:      "test-server",
		URL:       "http://localhost:3000",
		Transport: "http",
	}
	if cfg.Name != "test-server" {
		t.Errorf("Name = %q, want test-server", cfg.Name)
	}
	if cfg.URL != "http://localhost:3000" {
		t.Errorf("URL = %q", cfg.URL)
	}
}

func TestRegisterMCPTools_EmptyConfig(t *testing.T) {
	reg := agents.NewToolRegistry()
	err := agents.RegisterMCPTools(reg, nil)
	if err != nil {
		t.Errorf("expected nil error for empty config, got: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Error("expected no tools registered for empty config")
	}
}

func TestRegisterMCPTools_BadURL_Error(t *testing.T) {
	cfg := []agents.MCPServerConfig{
		{
			Name:      "bad",
			URL:       "http://127.0.0.1:1",
			Transport: "http",
		},
	}
	reg := agents.NewToolRegistry()
	err := agents.RegisterMCPTools(reg, cfg)
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

func TestMCPTool_NameAndDescription(t *testing.T) {
	tool := agents.NewMCPToolForTest("srv_greet", "says hello", map[string]interface{}{"type": "object"})
	if tool.Name() != "srv_greet" {
		t.Errorf("Name() = %q, want srv_greet", tool.Name())
	}
	if tool.Description() != "says hello" {
		t.Errorf("Description() = %q, want says hello", tool.Description())
	}
	if tool.Parameters() == nil {
		t.Error("Parameters() should not be nil")
	}

	// Execute with no client should fail gracefully
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("Execute with nil client should error")
	}
}
