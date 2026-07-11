// Command a2a-server is a standalone Google A2A (Agent2Agent) server that
// fronts the live HelixLLM coder fleet, per the approved design
// docs/research/07.2026/00_master/ACP_A2A_PROVIDER.md (operator decision
// 03_open_clarifications.md C3).
//
// It NEVER starts, stops, or restarts the coder it fronts (§11.4.119 /
// §11.4.122 — the coder is treated as a read-only downstream dependency).
// All host/port/model/credential values are config-injected via environment
// variables (CONST-045/046) — nothing is hardcoded in this file.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/a2a"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	listenAddr := getenv("HELIX_A2A_LISTEN_ADDR", ":18441")
	publicURL := getenv("HELIX_A2A_PUBLIC_URL", "http://localhost:18441")
	basePath := getenv("HELIX_A2A_BASE_PATH", "/a2a")
	downstreamBase := getenv("HELIX_A2A_DOWNSTREAM_URL", "http://localhost:18434")
	bearerKeys := os.Getenv("HELIX_A2A_BEARER_TOKEN") // empty => open-access
	maxTokensStr := getenv("HELIX_A2A_MAX_TOKENS", "512")
	maxTokens := 512
	if v, err := parsePositiveInt(maxTokensStr); err == nil {
		maxTokens = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	downstream := a2a.NewDownstream(downstreamBase, "", 5*time.Minute)
	modelID, err := downstream.DiscoverModelID(ctx)
	if err != nil {
		log.Fatalf("FATAL: could not discover downstream model from %s/v1/models: %v", downstreamBase, err)
	}
	downstream.ModelID = modelID
	log.Printf("discovered downstream model id=%q from %s", modelID, downstreamBase)

	card := a2a.BuildAgentCard(a2a.CardConfig{
		PublicURL:         publicURL,
		BasePath:          basePath,
		DownstreamModelID: modelID,
		BearerConfigured:  bearerKeys != "",
	})

	srv := a2a.NewServer(card, downstream, maxTokens)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	a2a.RegisterRoutes(r, srv, a2a.RouterOptions{BasePath: basePath, BearerKeys: bearerKeys})

	httpServer := &http.Server{Addr: listenAddr, Handler: r}

	go func() {
		log.Printf("A2A server listening on %s (base-path=%s public-url=%s downstream=%s)",
			listenAddr, basePath, publicURL, downstreamBase)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: ListenAndServe: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("shutting down (coder at %s left untouched)...", downstreamBase)
	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	_ = httpServer.Shutdown(shCtx)
}

func parsePositiveInt(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, strconv.ErrRange
	}
	return v, nil
}
