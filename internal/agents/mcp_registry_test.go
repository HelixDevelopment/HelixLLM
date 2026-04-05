package agents_test

import (
	"testing"

	mcpclient "digital.vasic.mcp/pkg/client"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

func TestRegisterMCPTools_EmptyServerList(t *testing.T) {
	registry := agents.NewToolRegistry()
	err := agents.RegisterMCPTools(registry, nil)
	if err != nil {
		t.Fatalf("RegisterMCPTools with nil servers: %v", err)
	}
	err = agents.RegisterMCPTools(registry, []agents.MCPServerConfig{})
	if err != nil {
		t.Fatalf("RegisterMCPTools with empty servers: %v", err)
	}
}

func TestRegisterMCPTools_InvalidHTTPServer(t *testing.T) {
	registry := agents.NewToolRegistry()
	servers := []agents.MCPServerConfig{
		{
			Name:      "nonexistent",
			Transport: mcpclient.TransportHTTP,
			URL:       "http://127.0.0.1:1", // unreachable port
		},
	}
	err := agents.RegisterMCPTools(registry, servers)
	if err == nil {
		t.Fatal("RegisterMCPTools with unreachable HTTP server should return error")
	}
}

func TestRegisterMCPTools_UnsupportedTransport(t *testing.T) {
	registry := agents.NewToolRegistry()
	servers := []agents.MCPServerConfig{
		{
			Name:      "bad-transport",
			Transport: mcpclient.TransportType("unknown"),
		},
	}
	err := agents.RegisterMCPTools(registry, servers)
	if err == nil {
		t.Fatal("RegisterMCPTools with unsupported transport should return error")
	}
}
