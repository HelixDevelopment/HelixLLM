// Package config provides HelixLLM configuration loading from environment
// variables with defaults and validation.
package config

import (
	"fmt"
	"strings"

	"digital.vasic.config/pkg/env"
)

// HelixConfig holds all configuration sections for HelixLLM.
// Top-level fields are loaded directly; nested structs rely on their own
// env tags (no prefix applied to nested fields).
type HelixConfig struct {
	Mode             string `env:"HELIX_MODE" default:"full"`
	Hosts            string `env:"HELIX_HOSTS" default:"nezha.local"`
	SSHUser          string `env:"HELIX_SSH_USER" default:"milosvasic"`
	SSHKey           string `env:"HELIX_SSH_KEY" default:"~/.ssh/id_ed25519"`
	ContainerRuntime string `env:"HELIX_CONTAINER_RUNTIME" default:"auto"`
	ScheduleStrategy string `env:"HELIX_SCHEDULE_STRATEGY" default:"auto"`
	Server           ServerConfig
	LLM              LLMConfig
	Knowledge        KnowledgeConfig
	DB               DatabaseConfig
	Cache            CacheConfig
	Messaging        MessagingConfig
	Log              LogConfig
	Auth             AuthConfig
	Features         FeatureConfig
	Analytics        AnalyticsConfig
	Proxy            ProxyConfig
	Concurrency      ConcurrencyConfig
}

// ServerConfig holds HTTP/TLS server settings.
type ServerConfig struct {
	Host          string `env:"HELIX_HOST" default:"0.0.0.0"`
	Port          int    `env:"HELIX_PORT" default:"8443"`
	TLSCert       string `env:"HELIX_TLS_CERT" default:"./certs/cert.pem"`
	TLSKey        string `env:"HELIX_TLS_KEY" default:"./certs/key.pem"`
	RatePerMinute int    `env:"HELIX_RATE_PER_MINUTE" default:"0"`
}

// LLMConfig holds large-language-model provider settings.
type LLMConfig struct {
	LocalModel     string `env:"HELIX_LLM_LOCAL_MODEL" default:"Llama-3.1-70B-Instruct-Q4_K_M"`
	LocalRPCHost   string `env:"HELIX_LLM_LOCAL_RPC_HOST" default:"localhost"`
	LocalRPCPort   int    `env:"HELIX_LLM_LOCAL_RPC_PORT" default:"50052"`
	OpenAIKey      string `env:"HELIX_LLM_OPENAI_KEY"`
	OpenAIBaseURL  string `env:"HELIX_LLM_OPENAI_BASE_URL"`
	AnthropicKey   string `env:"HELIX_LLM_ANTHROPIC_KEY"`
	ChutesKey      string `env:"HELIX_LLM_CHUTES_KEY"`
	OpenRouterKey  string `env:"HELIX_LLM_OPENROUTER_KEY"`
	HuggingFaceKey string `env:"HELIX_LLM_HUGGINGFACE_KEY"`
	NvidiaKey      string `env:"HELIX_LLM_NVIDIA_KEY"`
	CerebrasKey    string `env:"HELIX_LLM_CEREBRAS_KEY"`
	SambaNovaKey   string `env:"HELIX_LLM_SAMBANOVA_KEY"`
	TogetherKey    string `env:"HELIX_LLM_TOGETHER_KEY"`
	// Fallback chain
	// Defaults updated to match HelixAgent's canonical port registry
	// (HELIXAGENT_PORT_HTTP = 8100 in the 81xx band). Operators
	// can override via HELIX_LLM_VERIFIER_URL / HELIX_LLM_MEMORY_URL
	// if HelixAgent is running on a non-default port or host.
	VerifierURL          string `env:"HELIX_LLM_VERIFIER_URL" default:"http://localhost:8100"`
	ScoreRefreshInterval string `env:"HELIX_LLM_SCORE_REFRESH_INTERVAL" default:"5m"`
	MemorySyncEnabled    bool   `env:"HELIX_LLM_MEMORY_SYNC_ENABLED" default:"false"`
	MemoryURL            string `env:"HELIX_LLM_MEMORY_URL" default:"http://localhost:8100"`
	DefaultProvider      string `env:"HELIX_LLM_DEFAULT_PROVIDER" default:"local"`
	ModelsDir            string `env:"HELIX_MODELS_DIR" default:"/models"`
	ModelsAutoDownload   bool   `env:"HELIX_MODELS_AUTO_DOWNLOAD" default:"true"`
	ModelsMax            int    `env:"HELIX_MODELS_MAX" default:"3"`
	ComplexityEnabled    bool   `env:"HELIX_COMPLEXITY_ENABLED" default:"true"`
	ComplexityDefault    string `env:"HELIX_COMPLEXITY_DEFAULT_TIER" default:"fast"`
	LlamaServerPort      int    `env:"HELIX_LLAMA_SERVER_PORT" default:"8080"`
	LlamaServerEmbed     bool   `env:"HELIX_LLAMA_SERVER_EMBEDDED" default:"true"`
	// LlamaServerBinaryPath is the explicit path to the llama-server
	// executable. Empty (default) resolves "llama-server" via $PATH, exactly
	// like brain.LlamaServer.Start's own fallback. Set this when llama-server
	// is installed outside $PATH instead of editing system-wide PATH (HXC-233).
	LlamaServerBinaryPath string `env:"HELIX_LLAMA_SERVER_BINARY_PATH"`
	InferenceMode         string `env:"HELIX_INFERENCE_MODE" default:"auto"`
	CPUThreads            int    `env:"HELIX_CPU_THREADS" default:"0"`
	NUMAEnabled           bool   `env:"HELIX_NUMA_ENABLED" default:"false"`
	MMAPEnabled           bool   `env:"HELIX_MMAP_ENABLED" default:"true"`
	MLockEnabled          bool   `env:"HELIX_MLOCK_ENABLED" default:"false"`
}

// KnowledgeConfig holds RAG / vector-store settings.
type KnowledgeConfig struct {
	VectorDB string `env:"HELIX_VECTOR_DB" default:"qdrant"`
	// VectorDBHost/VectorDBPort resolve the Qdrant (or other vector-store
	// backend) network address (§11.4.111 resolve-by-config, never
	// hardcoded). Defaults preserve the pre-existing behaviour
	// (localhost:6333) for zero-config local development.
	VectorDBHost      string `env:"HELIX_VECTOR_DB_HOST" default:"localhost"`
	VectorDBPort      int    `env:"HELIX_VECTOR_DB_PORT" default:"6333"`
	EmbeddingProvider string `env:"HELIX_EMBEDDING_PROVIDER" default:"local"`
	EmbeddingModel    string `env:"HELIX_EMBEDDING_MODEL" default:"all-mpnet-base-v2"`
	EmbeddingBaseURL  string `env:"HELIX_EMBEDDING_BASE_URL"`
	RAGChunkSize      int    `env:"HELIX_RAG_CHUNK_SIZE" default:"1000"`
	RAGChunkOverlap   int    `env:"HELIX_RAG_CHUNK_OVERLAP" default:"200"`
	RAGTopK           int    `env:"HELIX_RAG_TOP_K" default:"5"`
	IngestDir         string `env:"HELIX_INGEST_DIR"`
	// RerankEnabled/RerankBaseURL/RerankFetchMultiplier configure the
	// optional cross-encoder reranking stage (embed -> retrieve -> RERANK
	// -> ground). When RerankEnabled is false (default) or RerankBaseURL
	// is empty, Pipeline.Query behaves exactly as before this feature was
	// wired (no reranking, no behaviour change, no extra retrieval cost).
	RerankEnabled         bool   `env:"HELIX_RAG_RERANK_ENABLED" default:"false"`
	RerankBaseURL         string `env:"HELIX_RAG_RERANK_BASE_URL"`
	RerankFetchMultiplier int    `env:"HELIX_RAG_RERANK_FETCH_MULTIPLIER" default:"3"`
}

// DatabaseConfig holds PostgreSQL settings.
type DatabaseConfig struct {
	Host     string `env:"HELIX_DB_HOST" default:"localhost"`
	Port     int    `env:"HELIX_DB_PORT" default:"5432"`
	Name     string `env:"HELIX_DB_NAME" default:"helixllm"`
	User     string `env:"HELIX_DB_USER" default:"helix"`
	Password string `env:"HELIX_DB_PASSWORD"`
}

// CacheConfig holds Redis settings.
type CacheConfig struct {
	RedisHost     string `env:"HELIX_REDIS_HOST" default:"localhost"`
	RedisPort     int    `env:"HELIX_REDIS_PORT" default:"6379"`
	RedisPassword string `env:"HELIX_REDIS_PASSWORD"`
}

// MessagingConfig holds Kafka settings.
type MessagingConfig struct {
	KafkaBrokers string `env:"HELIX_KAFKA_BROKERS" default:"localhost:9092"`
}

// LogConfig holds logging and observability settings.
type LogConfig struct {
	Level        string `env:"HELIX_LOG_LEVEL" default:"info"`
	Format       string `env:"HELIX_LOG_FORMAT" default:"text"`
	OTELExporter string `env:"HELIX_OTEL_EXPORTER" default:"none"`
	OTELEndpoint string `env:"HELIX_OTEL_ENDPOINT" default:"http://localhost:4317"`
}

// AuthConfig holds authentication settings.
//
// Both credentials are self-gating and INDEPENDENT: each is off when unset and
// enforced when set, and either one alone requires callers to authenticate.
//
//	JWTSecret unset, APIKeys unset -> open access (the shipped default)
//	JWTSecret unset, APIKeys set   -> API key required
//	JWTSecret set,   APIKeys unset -> JWT required
//	both set                       -> either credential accepted
//
// See internal/gateway/middleware.APIKeyOrJWTAuth for the enforcement and
// internal/auth for the token format.
type AuthConfig struct {
	JWTSecret string `env:"HELIX_AUTH_JWT_SECRET"`
	APIKeys   string `env:"HELIX_AUTH_API_KEYS"`
	// JWTTTLMinutes is the lifetime stamped on tokens minted by
	// POST /v1/auth/token. The 1440-minute (24h) default matches
	// helix_code's AuthConfig.TokenExpiry so the two services in this
	// platform do not disagree about how long a token lives. Ignored when
	// JWTSecret is unset.
	JWTTTLMinutes int `env:"HELIX_AUTH_JWT_TTL_MINUTES" default:"1440"`
}

// FeatureConfig holds feature-flag settings.
type FeatureConfig struct {
	GRPC bool `env:"HELIX_FEATURE_GRPC" default:"true"`
	// TOON controls whether TOON (Token-Oriented Object Notation) content
	// negotiation is available. The TOON submodule (digital.vasic.toon) provides
	// a compact serialization format optimized for LLM token efficiency. When
	// enabled, clients can request "Accept: application/toon" and the
	// ContentNegotiation middleware in internal/gateway/middleware/negotiation.go
	// will encode responses using the TOON encoder. The current TOON
	// implementation uses compact JSON as a wire format; when the native TOON
	// encoder lands, the plumbing is already in place.
	//
	// The gateway's RegisterRoutes conditionally applies ContentNegotiation()
	// only when this flag is true.
	TOON        bool `env:"HELIX_FEATURE_TOON" default:"true"`
	SelfImprove bool `env:"HELIX_FEATURE_SELFIMPROVE" default:"false"`
}

// AnalyticsConfig holds ClickHouse analytics settings.
type AnalyticsConfig struct {
	ClickHouseAddr     string `env:"HELIX_CLICKHOUSE_ADDR"`
	ClickHouseDatabase string `env:"HELIX_CLICKHOUSE_DATABASE" default:"helixllm"`
}

// ProxyConfig holds outbound HTTP/SOCKS proxy settings.
type ProxyConfig struct {
	HTTPProxy  string `env:"HELIX_HTTP_PROXY"`
	HTTPSProxy string `env:"HELIX_HTTPS_PROXY"`
	NoProxy    string `env:"HELIX_NO_PROXY"`
	SOCKSProxy string `env:"HELIX_SOCKS_PROXY"`
}

// ConcurrencyConfig holds concurrency limits and lazy-infrastructure settings.
// A value of 0 for any max-concurrent field means unlimited.
type ConcurrencyConfig struct {
	LLMMaxConcurrent        int  `env:"HELIX_LLM_MAX_CONCURRENT" default:"10"`
	EmbeddingMaxConcurrent  int  `env:"HELIX_EMBEDDING_MAX_CONCURRENT" default:"20"`
	AgentMaxConcurrentTools int  `env:"HELIX_AGENT_MAX_CONCURRENT_TOOLS" default:"5"`
	SSHMaxConcurrent        int  `env:"HELIX_SSH_MAX_CONCURRENT" default:"10"`
	LazyInfra               bool `env:"HELIX_LAZY_INFRA" default:"false"`
	IdleShutdownMinutes     int  `env:"HELIX_IDLE_SHUTDOWN_MINUTES" default:"0"`
}

// Load reads configuration from environment variables, applying defaults
// where no value is set.
func Load() (*HelixConfig, error) {
	cfg := &HelixConfig{}
	if err := env.Load(cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg, nil
}

// Validate checks that the configuration values are semantically valid.
func (c *HelixConfig) Validate() error {
	// Checked first so that a value like HELIX_MODE="${HELIX_MODE}" is reported
	// as the substitution failure it is, rather than as a confusing
	// "invalid mode" further down. See placeholder.go for the scope rationale.
	if err := checkNoUnexpandedPlaceholders(c); err != nil {
		return err
	}
	// Same reasoning one step further: a credential that was supplied but is
	// blank opens the feature's gate while carrying nothing, which is worse
	// than a placeholder because it looks deliberate. See requiredsecret.go
	// for which fields this covers and why absence stays legitimate.
	if err := checkNoBlankSecrets(c); err != nil {
		return err
	}
	// A JWT signing secret that is present but too weak to sign with is the
	// same class of defect as the two guards above: it looks configured, it
	// passes every other check, and it protects far less than the operator
	// who set it believes. Refused here, at the same gate, rather than
	// discovered later.
	if err := c.validateJWT(); err != nil {
		return err
	}
	validModes := map[string]bool{
		"full": true, "gateway": true, "brain": true,
		"knowledge": true, "agents": true, "control": true,
	}
	if !validModes[strings.ToLower(c.Mode)] {
		return fmt.Errorf("invalid mode: %q", c.Mode)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Server.Port)
	}
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[strings.ToLower(c.Log.Level)] {
		return fmt.Errorf("invalid log level: %q", c.Log.Level)
	}
	return nil
}

// minJWTSecretBytes is duplicated from internal/auth.MinSecretBytes rather
// than imported, because internal/auth imports nothing from this package today
// and this package must not start depending on it just to read one constant.
// The two are pinned to the same standard, not to each other: RFC 7518
// (JSON Web Algorithms) §3.2 requires an HS256 key at least as large as the
// hash output, i.e. 256 bits / 32 bytes. A test asserts they agree.
const minJWTSecretBytes = 32

// validateJWT refuses a JWT configuration that cannot do the job it appears to
// be doing.
//
// An UNSET secret is not an error — it is the documented off-switch
// (website/content/docs/user-guide/configuration.md), and it is what every
// config shipped in this repo uses. Only a SUPPLIED secret is checked, which
// is the same asymmetry requiredsecret.go applies and for the same reason:
// requiring the secret to be present would refuse every currently-working
// deployment.
//
// No value is ever included in the error. The length is, because the operator
// needs to know how far short they are and a length is not the secret.
func (c *HelixConfig) validateJWT() error {
	if c.Auth.JWTSecret == "" {
		return nil
	}
	if len(c.Auth.JWTSecret) < minJWTSecretBytes {
		return fmt.Errorf(
			"HELIX_AUTH_JWT_SECRET is %d bytes; JWT auth requires at least %d "+
				"(RFC 7518 §3.2: an HS256 key MUST be at least as large as the "+
				"hash output, 256 bits). Generate one with: openssl rand -hex 32. "+
				"Refusing to start rather than signing tokens with a key that is "+
				"outside the algorithm's specification",
			len(c.Auth.JWTSecret), minJWTSecretBytes)
	}
	// JWTTTLMinutes is deliberately NOT validated here.
	//
	// A non-positive TTL is not an error: auth.New treats it as "use the
	// 24h default" rather than minting already-expired tokens, so there is
	// no unsafe value to refuse. Refusing zero would also make Validate()
	// depend on env.Load having run first — a hand-built HelixConfig has
	// JWTTTLMinutes == 0 because the `default:"1440"` tag is applied by the
	// loader, not by the struct. Several existing tests construct a config
	// directly and legitimately expect it to validate; coupling Validate to
	// the loader would break every programmatic caller for no safety gain.
	return nil
}

// HostList splits the comma-separated Hosts string into a slice.
func (c *HelixConfig) HostList() []string {
	if c.Hosts == "" {
		return nil
	}
	hosts := strings.Split(c.Hosts, ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
	}
	return hosts
}
