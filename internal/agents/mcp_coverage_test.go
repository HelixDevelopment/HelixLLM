package agents

import (
	"context"
	"testing"

	mcpprotocol "digital.vasic.mcp/pkg/protocol"
)

// ---------------------------------------------------------------------------
// contentBlocksToText
// ---------------------------------------------------------------------------

func TestContentBlocksToText_Empty(t *testing.T) {
	result := contentBlocksToText(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestContentBlocksToText_SingleText(t *testing.T) {
	blocks := []mcpprotocol.ContentBlock{
		{Type: "text", Text: "hello"},
	}
	result := contentBlocksToText(blocks)
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestContentBlocksToText_MultipleTexts(t *testing.T) {
	blocks := []mcpprotocol.ContentBlock{
		{Type: "text", Text: "line one"},
		{Type: "text", Text: "line two"},
	}
	result := contentBlocksToText(blocks)
	if result != "line one\nline two" {
		t.Errorf("expected %q, got %q", "line one\nline two", result)
	}
}

func TestContentBlocksToText_SkipsNonText(t *testing.T) {
	blocks := []mcpprotocol.ContentBlock{
		{Type: "image", Text: "should be skipped"},
		{Type: "text", Text: "actual text"},
	}
	result := contentBlocksToText(blocks)
	if result != "actual text" {
		t.Errorf("expected %q, got %q", "actual text", result)
	}
}

func TestContentBlocksToText_SkipsEmptyText(t *testing.T) {
	blocks := []mcpprotocol.ContentBlock{
		{Type: "text", Text: ""},
		{Type: "text", Text: "not empty"},
	}
	result := contentBlocksToText(blocks)
	if result != "not empty" {
		t.Errorf("expected %q, got %q", "not empty", result)
	}
}

// ---------------------------------------------------------------------------
// MCPTool.Execute error paths
// ---------------------------------------------------------------------------

func TestMCPTool_Execute_NilClient(t *testing.T) {
	tool := &MCPTool{
		name:       "srv_tool",
		serverName: "srv",
	}
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if got := err.Error(); got != `mcp tool "srv_tool": no client configured` {
		t.Errorf("unexpected error: %q", got)
	}
}

// ---------------------------------------------------------------------------
// newMCPClient error paths
// ---------------------------------------------------------------------------

func TestNewMCPClient_UnsupportedTransport(t *testing.T) {
	_, err := newMCPClient(MCPServerConfig{
		Name:      "test",
		Transport: "invalid-transport",
	})
	if err == nil {
		t.Fatal("expected error for unsupported transport")
	}
}
