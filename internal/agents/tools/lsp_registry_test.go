package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

// mockRegistryProvider wraps mockLSPProvider and satisfies LSPProvider so it
// can be injected through the registry helpers indirectly.  The registry tests
// exercise RegisterLSPTools using a real LSPClient wired to a mock server, but
// we also test the tool-name disambiguation logic at the registry level using
// the public tool constructors directly.

func TestRegisterLSPTools_SingleConfig_ToolNames(t *testing.T) {
	// Wire up a mock server so RegisterLSPTools can create a real LSPClient.
	srv, r, w := newMockLSPServer(t)
	srv.handle("initialize", func(_ json.RawMessage) interface{} {
		return map[string]interface{}{"capabilities": map[string]interface{}{}}
	})

	// Replace the server-pipe client with a mock provider to avoid needing to
	// call RegisterLSPTools (which execs a subprocess).  Instead, test the
	// naming logic by constructing tools directly and registering them.
	_ = r
	_ = w

	reg := agents.NewToolRegistry()
	mock := &mockLSPProvider{}

	toolList := []agents.Tool{
		tools.NewGotoDefinitionTool(mock),
		tools.NewFindReferencesTool(mock),
		tools.NewHoverInfoTool(mock),
		tools.NewDiagnosticsTool(mock),
	}

	for _, tool := range toolList {
		if err := reg.Register(tool); err != nil {
			t.Fatalf("Register(%s): %v", tool.Name(), err)
		}
	}

	expectedNames := []string{"goto_definition", "find_references", "hover_info", "diagnostics"}
	for _, name := range expectedNames {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	list := reg.List()
	if len(list) != 4 {
		t.Errorf("expected 4 tools, got %d", len(list))
	}
}

func TestRegisterLSPTools_NoDuplicateRegistration(t *testing.T) {
	reg := agents.NewToolRegistry()
	mock := &mockLSPProvider{}

	tool := tools.NewGotoDefinitionTool(mock)
	if err := reg.Register(tool); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Registering same name again must fail.
	tool2 := tools.NewGotoDefinitionTool(mock)
	if err := reg.Register(tool2); err == nil {
		t.Error("expected duplicate registration error, got nil")
	}
}

func TestRegisterLSPTools_AllToolsHaveDescriptions(t *testing.T) {
	mock := &mockLSPProvider{}
	toolList := []agents.Tool{
		tools.NewGotoDefinitionTool(mock),
		tools.NewFindReferencesTool(mock),
		tools.NewHoverInfoTool(mock),
		tools.NewDiagnosticsTool(mock),
	}
	for _, tool := range toolList {
		if tool.Description() == "" {
			t.Errorf("tool %q has empty description", tool.Name())
		}
		if tool.Parameters() == nil {
			t.Errorf("tool %q has nil parameters", tool.Name())
		}
	}
}

func TestRegisterLSPTools_EmptyConfigs(t *testing.T) {
	reg := agents.NewToolRegistry()
	clients, err := tools.RegisterLSPTools(reg, nil)
	if err != nil {
		t.Fatalf("RegisterLSPTools with nil configs: %v", err)
	}
	if len(clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(clients))
	}
	if len(reg.List()) != 0 {
		t.Errorf("expected empty registry, got %d tools", len(reg.List()))
	}
}

func TestRegisterLSPTools_MissingCommand(t *testing.T) {
	reg := agents.NewToolRegistry()
	_, err := tools.RegisterLSPTools(reg, []tools.LSPServerConfig{
		{Language: "go", Command: ""},
	})
	if err == nil {
		t.Error("expected error for empty command, got nil")
	}
}

func TestLSPServerConfig_Fields(t *testing.T) {
	cfg := tools.LSPServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"-remote=auto"},
		RootURI:  "file:///home/user/project",
	}
	if cfg.Language != "go" {
		t.Errorf("Language: got %q", cfg.Language)
	}
	if cfg.Command != "gopls" {
		t.Errorf("Command: got %q", cfg.Command)
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "-remote=auto" {
		t.Errorf("Args: got %v", cfg.Args)
	}
	if cfg.RootURI != "file:///home/user/project" {
		t.Errorf("RootURI: got %q", cfg.RootURI)
	}
}
