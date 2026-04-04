package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/control"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/internal/mode"
	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func main() {
	modeFlag := flag.String("mode", "", "Operating mode (overrides HELIX_MODE env)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// CLI flag overrides env
	if *modeFlag != "" {
		cfg.Mode = *modeFlag
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	m, err := mode.Parse(cfg.Mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	bus := events.NewBus()
	defer bus.Close()

	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm",
		Environment: "production",
		Exporter:    cfg.Log.OTELExporter,
	})
	if err != nil {
		log.Error(fmt.Sprintf("observability init failed: %v", err))
		os.Exit(1)
	}
	defer obs.Shutdown()

	checker := health.NewChecker()

	log.WithField("mode", m.String()).Info("starting HelixLLM")

	srv := server.New(server.Options{
		Host:    cfg.Server.Host,
		Port:    cfg.Server.Port,
		TLSCert: cfg.Server.TLSCert,
		TLSKey:  cfg.Server.TLSKey,
		Checker: checker,
	})

	// Create Brain — registers whichever providers are configured.
	brainSvc := brain.New(brain.Config{
		LlamaCppURL:     fmt.Sprintf("http://localhost:%d", cfg.LLM.LocalRPCPort),
		LlamaCppModels:  []string{cfg.LLM.LocalModel},
		OpenAIKey:       cfg.LLM.OpenAIKey,
		AnthropicKey:    cfg.LLM.AnthropicKey,
		DefaultProvider: cfg.LLM.DefaultProvider,
	})

	// Register gateway routes (OpenAI + Anthropic compatible endpoints)
	gateway.RegisterRoutes(srv.Router(), gateway.RouterOptions{
		APIKeys:   cfg.Auth.APIKeys,
		RateLimit: 0, // TODO: add to config
		Brain:     brainSvc,
	})

	// Create knowledge pipeline with in-memory components.
	embedder := knowledge.NewHashEmbedder(768)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(cfg.Knowledge.RAGChunkSize, cfg.Knowledge.RAGChunkOverlap)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       cfg.Knowledge.RAGTopK,
	})
	knowledge.RegisterKnowledgeRoutes(srv.Router(), pipeline)

	// Create tool registry with built-in tools.
	toolReg := agents.NewToolRegistry()
	toolReg.Register(&tools.EchoTool{})
	toolReg.Register(&tools.TimeTool{})
	toolReg.Register(tools.NewKnowledgeQueryTool(pipeline, "default"))

	// Create agent with Brain, tools, and RAG hook.
	agentSvc := agents.NewAgent(agents.AgentConfig{
		Brain:    brainSvc,
		Tools:    toolReg,
		RAGHook:  knowledge.RAGHook(pipeline, "default"),
		MaxTurns: 10,
	})

	// Create conversation context for multi-turn sessions.
	convCtx := agents.NewConversationContext(100)

	// Register agent routes.
	agents.RegisterAgentRoutes(srv.Router(), agentSvc, convCtx)

	// Create Control Plane for cluster management.
	cp := control.NewControlPlane(control.ControlPlaneOptions{
		Hosts:    cfg.HostList(),
		SSHUser:  cfg.SSHUser,
		SSHKey:   cfg.SSHKey,
		Strategy: cfg.ScheduleStrategy,
	})
	control.RegisterRoutes(srv.Router(), cp)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("shutting down...")
		bus.Publish(events.TopicServerStopped, "main", nil)
		cancel()
	}()

	bus.Publish(events.TopicServerStarted, "main", m.String())
	log.WithField("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).
		Info("server listening")

	if err := srv.ListenAndServe(ctx); err != nil {
		log.WithError(err).Error("server error")
		os.Exit(1)
	}
}
