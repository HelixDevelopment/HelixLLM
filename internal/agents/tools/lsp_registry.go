package tools

import (
	"context"
	"fmt"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

// LSPServerConfig holds the configuration needed to start a single language
// server and connect an LSPClient to it.
type LSPServerConfig struct {
	// Language is a human-readable identifier such as "go", "python", or
	// "typescript". It is used only for error messages and logging.
	Language string

	// Command is the executable to run, e.g. "gopls", "pyright", or
	// "typescript-language-server".
	Command string

	// Args are the command-line arguments passed to Command.
	Args []string

	// RootURI is the workspace root expressed as a file:// URI,
	// e.g. "file:///home/user/project".
	RootURI string
}

// RegisterLSPTools starts an LSP server for each entry in configs, initialises
// the connection, and registers the four LSP tool wrappers
// (goto_definition, find_references, hover_info, diagnostics) into registry.
//
// When multiple configs are provided the tool names are disambiguated with a
// language suffix, e.g. "goto_definition_go", "goto_definition_python".
//
// The returned slice of LSPClients must be closed by the caller (e.g. via
// Close on each element) when the registry is no longer needed.
func RegisterLSPTools(registry *agents.ToolRegistry, configs []LSPServerConfig) ([]*LSPClient, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	disambiguate := len(configs) > 1

	var clients []*LSPClient

	for _, cfg := range configs {
		if cfg.Command == "" {
			return clients, fmt.Errorf("lsp registry: config for language %q has empty command", cfg.Language)
		}

		client, err := NewLSPClient(cfg.Command, cfg.Args...)
		if err != nil {
			return clients, fmt.Errorf("lsp registry: start server for %q: %w", cfg.Language, err)
		}
		clients = append(clients, client)

		if cfg.RootURI != "" {
			if err := client.Initialize(context.Background(), cfg.RootURI); err != nil {
				_ = client.Close()
				return clients, fmt.Errorf("lsp registry: initialize server for %q: %w", cfg.Language, err)
			}
		}

		suffix := ""
		if disambiguate && cfg.Language != "" {
			suffix = "_" + cfg.Language
		}

		toolset := []agents.Tool{
			newNamedGotoDefinitionTool(client, suffix),
			newNamedFindReferencesTool(client, suffix),
			newNamedHoverInfoTool(client, suffix),
			newNamedDiagnosticsTool(client, suffix),
		}

		for _, t := range toolset {
			if err := registry.Register(t); err != nil {
				return clients, fmt.Errorf("lsp registry: register %q: %w", t.Name(), err)
			}
		}
	}

	return clients, nil
}

// --- named tool variants used when suffix != "" ------------------------------

type namedGotoDefinitionTool struct {
	GotoDefinitionTool
	name string
}

func newNamedGotoDefinitionTool(client LSPProvider, suffix string) agents.Tool {
	if suffix == "" {
		return NewGotoDefinitionTool(client)
	}
	return &namedGotoDefinitionTool{
		GotoDefinitionTool: GotoDefinitionTool{client: client},
		name:               "goto_definition" + suffix,
	}
}

func (t *namedGotoDefinitionTool) Name() string { return t.name }

type namedFindReferencesTool struct {
	FindReferencesTool
	name string
}

func newNamedFindReferencesTool(client LSPProvider, suffix string) agents.Tool {
	if suffix == "" {
		return NewFindReferencesTool(client)
	}
	return &namedFindReferencesTool{
		FindReferencesTool: FindReferencesTool{client: client},
		name:               "find_references" + suffix,
	}
}

func (t *namedFindReferencesTool) Name() string { return t.name }

type namedHoverInfoTool struct {
	HoverInfoTool
	name string
}

func newNamedHoverInfoTool(client LSPProvider, suffix string) agents.Tool {
	if suffix == "" {
		return NewHoverInfoTool(client)
	}
	return &namedHoverInfoTool{
		HoverInfoTool: HoverInfoTool{client: client},
		name:          "hover_info" + suffix,
	}
}

func (t *namedHoverInfoTool) Name() string { return t.name }

type namedDiagnosticsTool struct {
	DiagnosticsTool
	name string
}

func newNamedDiagnosticsTool(client LSPProvider, suffix string) agents.Tool {
	if suffix == "" {
		return NewDiagnosticsTool(client)
	}
	return &namedDiagnosticsTool{
		DiagnosticsTool: DiagnosticsTool{client: client},
		name:            "diagnostics" + suffix,
	}
}

func (t *namedDiagnosticsTool) Name() string { return t.name }
