// Command mcp-gateway exposes HelixLLM's local capabilities as MCP
// tools/resources over Streamable-HTTP, per the GO'd design memo at
// docs/research/07.2026/05_mcp_acp_protocols/MCP_OKF_GATEWAY_MEMO.md
// (verdict GO — go-sdk v1.6.1 stable ships StreamableHTTPHandler +
// Stateless + RequireBearerToken).
//
// It NEVER starts, stops, or restarts the live coder it fronts (§11.4.119 /
// §11.4.122 — the coder is a read-only downstream dependency, exactly like
// the sibling cmd/a2a-server). All host/port/token/path values are
// config-injected via environment variables (CONST-045/046) — nothing is
// hardcoded in this file.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/mcpgateway"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	listenAddr := getenv("HELIX_MCP_GATEWAY_LISTEN_ADDR", ":18444")
	coderBase := getenv("HELIX_LLM_LOCAL_OPENAI_ENDPOINT", "http://localhost:18434")
	okfRoot := getenv("HELIX_MCP_GATEWAY_OKF_ROOT", "")
	bearerToken := os.Getenv("HELIX_MCP_GATEWAY_TOKEN")

	if bearerToken == "" {
		log.Fatal("FATAL: HELIX_MCP_GATEWAY_TOKEN must be set (§11.4.10 — no hardcoded credentials); " +
			"the gateway refuses to start with an empty/implicit bearer token")
	}

	coder := mcpgateway.NewCoderClient(coderBase, 120*time.Second)
	okf := mcpgateway.NewOKFStore(okfRoot)

	srv := mcpgateway.NewServer("helixllm-mcp-gateway", "0.1.0", coder, okf)
	handler := mcpgateway.NewHTTPHandler(srv, bearerToken)

	httpServer := &http.Server{Addr: listenAddr, Handler: handler}

	go func() {
		log.Printf("MCP gateway listening on %s (coder=%s okf_root=%q)", listenAddr, coderBase, okfRoot)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: ListenAndServe: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("shutting down (coder at %s left untouched)...", coderBase)
	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	_ = httpServer.Shutdown(shCtx)
}
