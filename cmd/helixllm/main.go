package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	modeFlag       := flag.String("mode", "", "Operating mode (overrides HELIX_MODE env)")
	monitorFlag    := flag.Bool("monitor", false, "Launch the TUI cluster status monitor instead of the server")
	challengesFlag := flag.Bool("challenges", false, "Run challenge banks instead of starting server")
	banksDirFlag   := flag.String("banks-dir", "challenges/banks/", "Directory containing YAML challenge banks")
	baseURLFlag    := flag.String("base-url", "https://localhost:8443", "Base URL for challenge HTTP requests")
	categoryFlag   := flag.String("category", "", "Run only challenges matching this category")
	priorityFlag   := flag.String("priority", "", "Run only challenges matching this priority")
	flag.Parse()

	if *challengesFlag {
		os.Exit(runChallenges(*baseURLFlag, *banksDirFlag, *categoryFlag, *priorityFlag))
	}

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

	// Build the Control Plane — needed by both the server and the monitor.
	cpOpts := control.ControlPlaneOptions{
		Hosts:    cfg.HostList(),
		SSHUser:  cfg.SSHUser,
		SSHKey:   cfg.SSHKey,
		Strategy: cfg.ScheduleStrategy,
	}
	if len(cfg.HostList()) > 0 {
		sshClient, sshErr := control.NewSSHClient(
			cfg.HostList()[0], 22, cfg.SSHUser, cfg.SSHKey,
		)
		if sshErr != nil {
			log.WithError(sshErr).Warn("SSH key unavailable; control plane running in no-op mode")
		} else {
			cpOpts.SSH = sshClient
		}
	}
	cp := control.NewControlPlane(cpOpts)

	// Graceful shutdown context — shared by both code paths.
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

	// --monitor: show the TUI cluster status display and exit.
	if *monitorFlag {
		log.Info("starting cluster monitor")
		if err := control.RunMonitor(ctx, cp, 2*time.Second); err != nil {
			log.WithError(err).Error("monitor error")
			os.Exit(1)
		}
		return
	}

	checker := health.NewChecker()

	log.WithField("mode", m.String()).Info("starting HelixLLM")

	srv := server.New(server.Options{
		Host:    cfg.Server.Host,
		Port:    cfg.Server.Port,
		TLSCert: cfg.Server.TLSCert,
		TLSKey:  cfg.Server.TLSKey,
		Checker: checker,
		Obs:     obs,
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
		RateLimit: cfg.Server.RatePerMinute,
		Brain:     brainSvc,
	})

	// Create knowledge pipeline using configured backends.
	embedder, err := knowledge.NewEmbedder(
		cfg.Knowledge.EmbeddingProvider,
		cfg.LLM.OpenAIKey,
		cfg.Knowledge.EmbeddingModel,
		768,
	)
	if err != nil {
		log.WithError(err).Error("failed to create embedder, falling back to hash embedder")
		embedder = knowledge.NewHashEmbedder(768)
	}
	store, err := knowledge.NewVectorStore(cfg.Knowledge.VectorDB, "localhost", 6333)
	if err != nil {
		log.WithError(err).Error("failed to connect to vector store, falling back to memory store")
		store = knowledge.NewMemoryStore()
	}
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

	// Create three-tier memory manager (working + episodic + semantic via RAG).
	memMgr := agents.NewMemoryManager(convCtx, pipeline)

	// Create multi-agent coordinator (phase-based: investigation → synthesis → implementation → verification).
	coordinator := agents.NewCoordinator(agents.CoordinatorConfig{
		Brain:   brainSvc,
		Tools:   toolReg,
		RAGHook: knowledge.RAGHook(pipeline, "default"),
	})

	// Create task planner (LLM-driven goal decomposition into ordered steps).
	planner := agents.NewPlanner(brainSvc)

	// Register agent routes (chat, tools, coordinate, plan, memory/remember, memory/recall).
	agents.RegisterAgentRoutesWithExtras(srv.Router(), agentSvc, convCtx, coordinator, planner, memMgr)

	control.RegisterRoutes(srv.Router(), cp)

	bus.Publish(events.TopicServerStarted, "main", m.String())
	log.WithField("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).
		Info("server listening")

	if err := srv.ListenAndServe(ctx); err != nil {
		log.WithError(err).Error("server error")
		os.Exit(1)
	}
}
