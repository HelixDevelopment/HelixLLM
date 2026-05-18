package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/internal/control"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/internal/mode"
	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/analytics"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/hardware"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/metrics"
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

	// CONST-046: user-facing CLI strings resolved via i18n Translator.
	// Language selection follows the standard env-precedence fallback
	// (LANG / LC_ALL → "en") used by every CLI in the platform.
	lang := resolveCLILang()
	tr := i18n.New(lang)

	if *challengesFlag {
		os.Exit(runChallenges(tr, lang, *baseURLFlag, *banksDirFlag, *categoryFlag, *priorityFlag))
	}

	cfg, err := config.Load()
	if err != nil {
		msg := tr.T(lang, i18n.KeyHelixllmCLIErrorLoadingConfig, map[string]string{
			"detail": err.Error(),
		})
		fmt.Fprintf(os.Stderr, "%s\n", msg)
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

	analyticsCollector := analytics.NewCollector(cfg.Analytics.ClickHouseAddr, cfg.Analytics.ClickHouseDatabase)
	defer analyticsCollector.Close() //nolint:errcheck

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

	// Register Prometheus metrics collectors.
	metrics.Register()

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

	// Multi-model fleet initialization
	hwProfile := hardware.Detect()
	hardware.UpdateSystemMetrics(hwProfile)
	log.WithFields(map[string]interface{}{
		"gpu":     hwProfile.GPU.Available,
		"vram_mb": hwProfile.GPU.VRAMTotal / (1024 * 1024),
		"preset":  hwProfile.PresetProfile,
	}).Info("Hardware detected")

	catalog := models.DefaultCatalog()
	var available []models.ModelDefinition
	if hwProfile.GPU.Available {
		available = catalog.FilterByVRAM(hwProfile.GPU.VRAMFree)
	} else {
		available = catalog.FilterByVRAM(0)
		// CPU-only: just use the smallest model
		if fast := catalog.ByTier(models.TierFast); len(fast) > 0 {
			available = fast
		}
	}

	registry := models.NewRegistry()
	downloader := brain.NewDownloader(cfg.LLM.ModelsDir)
	for _, def := range available {
		rm := models.RuntimeModel{
			Definition: def,
			Status:     models.StatusUnloaded,
			FilePath:   filepath.Join(cfg.LLM.ModelsDir, def.Filename),
			Downloaded: downloader.ModelExists(def.Filename),
		}
		registry.Add(rm)
	}

	// Download missing models
	if cfg.LLM.ModelsAutoDownload {
		for _, def := range available {
			if !downloader.ModelExists(def.Filename) {
				url := downloader.HuggingFaceURL(def.HuggingFaceRepo, def.Filename)
				log.WithField("model", def.ID).Info("Downloading model from HuggingFace")
				if err := downloader.Download(ctx, brain.DownloadRequest{URL: url, Filename: def.Filename}); err != nil {
					log.WithError(err).WithField("model", def.ID).Warn("Failed to download model, skipping")
				}
			}
		}
	}

	// Generate presets and optionally start embedded llama-server
	var llamaSrv *brain.LlamaServer
	if cfg.LLM.LlamaServerEmbed {
		var downloadedModels []models.ModelDefinition
		for _, def := range available {
			if downloader.ModelExists(def.Filename) {
				downloadedModels = append(downloadedModels, def)
			}
		}
		if len(downloadedModels) > 0 {
			presetsINI, _ := models.GeneratePresets(downloadedModels, hwProfile)
			presetsPath := filepath.Join(os.TempDir(), "helixllm-presets.ini")
			os.WriteFile(presetsPath, []byte(presetsINI), 0644) //nolint:errcheck

			llamaSrv = brain.NewLlamaServer(brain.LlamaServerConfig{
				Port:         cfg.LLM.LlamaServerPort,
				ModelsDir:    cfg.LLM.ModelsDir,
				PresetsPath:  presetsPath,
				MaxModels:    cfg.LLM.ModelsMax,
				Threads:      hwProfile.InferenceThreads(),
				ThreadsBatch: hwProfile.BatchThreads(),
			})
			if err := llamaSrv.Start(ctx); err != nil {
				log.WithError(err).Warn("Failed to start embedded llama-server")
			} else {
				log.Info("Waiting for llama-server to be ready...")
				if err := llamaSrv.WaitReady(ctx, 120*time.Second); err != nil {
					log.WithError(err).Warn("llama-server not ready within timeout")
				} else {
					for _, def := range downloadedModels {
						registry.UpdateStatus(def.ID, models.StatusLoaded)
					}
				}
			}
		}
	}
	if llamaSrv != nil {
		defer llamaSrv.Stop() //nolint:errcheck
		// Override to use embedded server
		cfg.LLM.LocalRPCHost = "127.0.0.1"
		cfg.LLM.LocalRPCPort = cfg.LLM.LlamaServerPort
	}

	// Create KV cache for conversation context persistence.
	var kvCache brain.KVCacher
	if cfg.Cache.RedisHost != "" {
		kvCache = brain.NewKVCache(brain.KVCacheConfig{
			RedisAddr:     fmt.Sprintf("%s:%d", cfg.Cache.RedisHost, cfg.Cache.RedisPort),
			RedisPassword: cfg.Cache.RedisPassword,
		})
		if kvCache.Available() {
			log.Info("KV cache: Redis connected")
		} else {
			log.Warn("KV cache: Redis unreachable, falling back to in-memory")
			kvCache = brain.NewMemoryKVCache(time.Hour)
		}
	} else {
		kvCache = brain.NewMemoryKVCache(time.Hour)
		log.Info("KV cache: using in-memory (no Redis configured)")
	}

	// Create Brain — registers whichever providers are configured.
	brainSvc := brain.New(brain.Config{
		LlamaCppURL:       fmt.Sprintf("http://%s:%d", cfg.LLM.LocalRPCHost, cfg.LLM.LocalRPCPort),
		LlamaCppModels:    []string{cfg.LLM.LocalModel},
		OpenAIKey:         cfg.LLM.OpenAIKey,
		OpenAIBaseURL:     cfg.LLM.OpenAIBaseURL,
		AnthropicKey:      cfg.LLM.AnthropicKey,
		ChutesKey:         cfg.LLM.ChutesKey,
		OpenRouterKey:     cfg.LLM.OpenRouterKey,
		HuggingFaceKey:    cfg.LLM.HuggingFaceKey,
		NvidiaKey:         cfg.LLM.NvidiaKey,
		CerebrasKey:       cfg.LLM.CerebrasKey,
		SambaNovaKey:      cfg.LLM.SambaNovaKey,
		TogetherKey:       cfg.LLM.TogetherKey,
		DefaultProvider:   cfg.LLM.DefaultProvider,
		ComplexityEnabled: cfg.LLM.ComplexityEnabled,
		Registry:          registry,
		KVCache:           kvCache,
	})

	// Create embedder early so the gateway can use it for /v1/embeddings.
	// For the "llama" embedding provider, the second argument is the
	// base URL of the embedding server. Use the dedicated
	// HELIX_EMBEDDING_BASE_URL when set; fall back to OpenAIBaseURL
	// for backward compatibility. For "openai" provider, it's the API key.
	embeddingAPIKeyOrURL := cfg.LLM.OpenAIKey
	if cfg.Knowledge.EmbeddingProvider == "llama" {
		if cfg.Knowledge.EmbeddingBaseURL != "" {
			embeddingAPIKeyOrURL = cfg.Knowledge.EmbeddingBaseURL
		} else if cfg.LLM.OpenAIBaseURL != "" {
			embeddingAPIKeyOrURL = cfg.LLM.OpenAIBaseURL
		}
	}
	embedder, err := knowledge.NewEmbedder(
		cfg.Knowledge.EmbeddingProvider,
		embeddingAPIKeyOrURL,
		cfg.Knowledge.EmbeddingModel,
		768,
	)
	if err != nil {
		log.WithError(err).Error("failed to create embedder, falling back to hash embedder")
		embedder = knowledge.NewHashEmbedder(768)
	}

	// Create knowledge pipeline BEFORE gateway so RAG hook is available.
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

	// Build the FallbackChain — ordered by LLMsVerifier quality scores with
	// llamacpp always placed last as the local safety net.
	scorerBridge := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL:     cfg.LLM.VerifierURL,
		RefreshInterval: parseDuration(cfg.LLM.ScoreRefreshInterval, 5*time.Minute),
	})

	// Discover models from all registered providers (must happen before
	// discoverProviderModels, which reads the cached model lists).
	fetchProviderModels(ctx, brainSvc)

	providerModels := discoverProviderModels(brainSvc)
	scores, _ := scorerBridge.FetchScores(ctx)
	entries := scorerBridge.BuildEntries(scores, providerModels)

	rateLimiter := fallback.NewRateLimitTracker(5, 1000)
	fallbackChain := fallback.NewChain(brainSvc.Providers(), rateLimiter)
	fallbackChain.SetEntries(entries)

	scorerBridge.StartRefreshLoop(ctx, fallbackChain, providerModels)

	memAdapter := fallback.NewMemoryAdapter(fallback.MemoryAdapterConfig{
		HelixMemoryURL: cfg.LLM.MemoryURL,
		SyncInterval:   30 * time.Second,
		Enabled:        cfg.LLM.MemorySyncEnabled,
	})
	memAdapter.Start(ctx)

	for i, e := range entries {
		slog.Info("fallback chain entry",
			"rank", i+1,
			"provider", e.ProviderName,
			"model", e.ModelID,
			"score", e.Score,
			"local", e.IsLocalFallback,
		)
	}

	// Register gateway routes with RAG hook — this injects retrieved codebase
	// context into every /v1/chat/completions request so small models have
	// relevant code pre-loaded instead of exploring via repeated tool calls.
	// The FallbackChain is the primary Completer; brainSvc is kept as
	// ModelBrain so /v1/models can enumerate available models.
	toolMgr := gateway.DefaultToolManager()
	gateway.RegisterRoutes(srv.Router(), gateway.RouterOptions{
		APIKeys:         cfg.Auth.APIKeys,
		RateLimit:       cfg.Server.RatePerMinute,
		Brain:           fallbackChain,
		ModelBrain:      brainSvc,
		Embedder:        embedder,
		ToolManager:     toolMgr,
		RAGHook:         knowledge.RAGHook(pipeline, "codebase"),
		TOONEnabled:     cfg.Features.TOON,
		HardwareProfile: hwProfile,
	})
	knowledge.RegisterKnowledgeRoutes(srv.Router(), pipeline)

	// Auto-ingest codebase if configured.
	if cfg.Knowledge.IngestDir != "" {
		ingestDir := cfg.Knowledge.IngestDir
		go func() {
			log.WithField("dir", ingestDir).Info("starting codebase auto-ingest")
			if err := knowledge.AutoIngest(pipeline, ingestDir, "codebase"); err != nil {
				log.WithError(err).Error("codebase auto-ingest failed")
			}
		}()
	}

	// Create tool registry with built-in tools.
	toolReg := agents.NewToolRegistry()
	toolReg.Register(&tools.EchoTool{})
	toolReg.Register(&tools.TimeTool{})
	toolReg.Register(tools.NewKnowledgeQueryTool(pipeline, "default"))

	// Security sandbox for file/exec/git/analysis tools.
	workDir, _ := os.Getwd()
	sandbox := tools.NewSandbox(tools.SandboxConfig{
		AllowedPaths: []string{"/tmp", workDir},
	})

	// File system tools (5).
	toolReg.Register(tools.NewReadFileTool(sandbox))
	toolReg.Register(tools.NewWriteFileTool(sandbox))
	toolReg.Register(tools.NewListDirectoryTool(sandbox))
	toolReg.Register(tools.NewSearchFilesTool(sandbox))
	toolReg.Register(tools.NewFileInfoTool(sandbox))

	// Code execution tools (2).
	toolReg.Register(tools.NewExecutePythonTool(sandbox))
	toolReg.Register(tools.NewExecuteShellTool(sandbox))

	// Git tools (8).
	toolReg.Register(tools.NewGitStatusTool(sandbox))
	toolReg.Register(tools.NewGitDiffTool(sandbox))
	toolReg.Register(tools.NewGitLogTool(sandbox))
	toolReg.Register(tools.NewGitBranchTool(sandbox))
	toolReg.Register(tools.NewGitCommitTool(sandbox))
	toolReg.Register(tools.NewGitPushTool(sandbox))
	toolReg.Register(tools.NewGitPullTool(sandbox))
	toolReg.Register(tools.NewGitCreateBranchTool(sandbox))

	// Analysis tools (4).
	toolReg.Register(tools.NewAnalyzeCodeTool(sandbox))
	toolReg.Register(tools.NewRunTestsTool(sandbox))
	toolReg.Register(tools.NewGetDependenciesTool(sandbox))
	toolReg.Register(tools.NewCalculateComplexityTool(sandbox))

	// Web tools (2).
	toolReg.Register(tools.NewWebSearchTool())
	toolReg.Register(tools.NewFetchURLTool())

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

	// Wire the MemoryAdapter into the MemoryManager so that high-importance
	// memories (importance >= 0.7) are queued for persistent sync to HelixMemory.
	if cfg.LLM.MemorySyncEnabled {
		memMgr.SetPersistentSyncer(memAdapter)
	}

	// Create multi-agent coordinator (phase-based: investigation → synthesis → implementation → verification).
	coordinator := agents.NewCoordinator(agents.CoordinatorConfig{
		Brain:   brainSvc,
		Tools:   toolReg,
		RAGHook: knowledge.RAGHook(pipeline, "default"),
	})

	// Create task planner (LLM-driven goal decomposition into ordered steps).
	planner := agents.NewPlanner(brainSvc)

	// Register agent routes (chat, tools, coordinate, plan, memory/remember, memory/recall, cache/stats).
	agents.RegisterAgentRoutesWithExtras(srv.Router(), agentSvc, convCtx, coordinator, planner, memMgr, kvCache)

	control.RegisterRoutes(srv.Router(), cp)

	bus.Publish(events.TopicServerStarted, "main", m.String())
	log.WithField("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).
		Info("server listening")

	if err := srv.ListenAndServe(ctx); err != nil {
		log.WithError(err).Error("server error")
		os.Exit(1)
	}
}

// fetchProviderModels calls FetchModels on each provider that supports it,
// populating the cached model list before the fallback chain is built.
func fetchProviderModels(ctx context.Context, b *brain.Brain) {
	type modelFetcher interface {
		FetchModels(ctx context.Context, filterFn func(string) bool) error
	}
	type freeModelFetcher interface {
		DiscoverFreeModels(ctx context.Context) error
	}

	providers := b.Providers()
	slog.Info("fetchProviderModels: starting", "provider_count", len(providers))
	for name, p := range providers {
		slog.Info("fetchProviderModels: checking provider", "name", name, "type", fmt.Sprintf("%T", p))
		if f, ok := p.(freeModelFetcher); ok {
			if err := f.DiscoverFreeModels(ctx); err != nil {
				slog.Warn("failed to discover free models", "provider", name, "err", err)
			} else {
				slog.Info("discovered free models", "provider", name, "models", len(p.Models()))
			}
		} else if f, ok := p.(modelFetcher); ok {
			if err := f.FetchModels(ctx, nil); err != nil {
				slog.Warn("failed to fetch models", "provider", name, "err", err)
			} else {
				slog.Info("discovered models", "provider", name, "models", len(p.Models()))
			}
		}
	}
}

// discoverProviderModels returns a map of provider name → first model ID by
// inspecting each registered provider.  The result is passed to ScorerBridge
// so chain entries carry a concrete model ID instead of an empty string.
func discoverProviderModels(b *brain.Brain) map[string]string {
	models := make(map[string]string)
	for name, p := range b.Providers() {
		ml := p.Models()
		if len(ml) > 0 {
			models[name] = ml[0]
		}
	}
	return models
}

// parseDuration parses s as a time.Duration.  If s is empty or unparseable it
// returns the provided fallback value.
func parseDuration(s string, fallbackVal time.Duration) time.Duration {
	if s == "" {
		return fallbackVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallbackVal
	}
	return d
}

// resolveCLILang picks a 2-letter language tag for user-facing CLI
// strings based on the standard POSIX env precedence (LC_ALL > LANG).
// Falls back to "en". Never returns "" — the i18n Translator's fallback
// chain assumes a non-empty default-language tag.
//
// CONST-046 round-95: hardcoded English literals at CLI call sites are
// replaced by i18n.Translator.T(lang, key); this function supplies lang.
func resolveCLILang() string {
	for _, env := range []string{"LC_ALL", "LANG"} {
		v := os.Getenv(env)
		if len(v) >= 2 {
			return v[:2]
		}
	}
	return "en"
}
