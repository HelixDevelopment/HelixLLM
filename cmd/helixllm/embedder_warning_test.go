package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
)

// F07 (§11.4.146): HELIX_EMBEDDING_PROVIDER=local (the default, zero-config
// path) silently resolves to knowledge.HashEmbedder, a deterministic but
// NON-SEMANTIC embedder. This test proves, with REAL captured log output
// from the REAL buildEmbedder code path (no mocks — genuine
// knowledge.NewEmbedder + knowledge.HashEmbedder/OpenAIEmbedder
// construction), that:
//  1. the default-provider path still returns a HashEmbedder (the default
//     itself is UNCHANGED by this fix — only its visibility changed), AND
//     emits the transparency warning; and
//  2. a real (non-hash) provider configured does NOT emit the warning.
//
// digital.vasic.embeddings/pkg/openai.NewClient does not perform any
// network I/O at construction time (verified: NewOpenAIEmbedder only
// checks apiKey != "" locally), so case 2 constructs a genuine
// *knowledge.OpenAIEmbedder without requiring network access or a real API
// key — deterministic, hermetic, re-runnable (§11.4.98).

func TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)

	cfg := &config.HelixConfig{}
	cfg.Knowledge.EmbeddingProvider = "local" // the real HELIX_EMBEDDING_PROVIDER default
	cfg.Knowledge.EmbeddingModel = "all-mpnet-base-v2"

	embedder := buildEmbedder(cfg, log)

	if _, isHash := embedder.(*knowledge.HashEmbedder); !isHash {
		t.Fatalf("buildEmbedder with EmbeddingProvider=%q returned %T, want *knowledge.HashEmbedder — default behaviour must NOT change", cfg.Knowledge.EmbeddingProvider, embedder)
	}

	out := buf.String()
	if !strings.Contains(out, "non-semantic") || !strings.Contains(out, "HashEmbedder") {
		t.Fatalf("expected a non-semantic HashEmbedder warning in captured log output, got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "warn") {
		t.Fatalf("expected the log line to be at WARN level, got:\n%s", out)
	}
	t.Logf("captured startup warning (real log output):\n%s", out)
}

func TestBuildEmbedder_EmptyProvider_AlsoWarns(t *testing.T) {
	// knowledge.NewEmbedder treats "" identically to "local" — the
	// discriminator is the RETURNED TYPE, not a string match on the config
	// value, so this path must also warn.
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)

	cfg := &config.HelixConfig{}
	cfg.Knowledge.EmbeddingProvider = ""

	embedder := buildEmbedder(cfg, log)

	if _, isHash := embedder.(*knowledge.HashEmbedder); !isHash {
		t.Fatalf("buildEmbedder with empty EmbeddingProvider returned %T, want *knowledge.HashEmbedder", embedder)
	}
	if !strings.Contains(buf.String(), "non-semantic") {
		t.Fatalf("expected a non-semantic warning for empty EmbeddingProvider, got:\n%s", buf.String())
	}
}

func TestBuildEmbedder_RealProviderConfigured_NoWarning(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)

	cfg := &config.HelixConfig{}
	cfg.Knowledge.EmbeddingProvider = "openai"
	cfg.Knowledge.EmbeddingModel = "text-embedding-3-small"
	cfg.LLM.OpenAIKey = "sk-test-key-not-a-real-secret-construction-only" // #nosec G101 -- test-only placeholder, never used for a network call

	embedder := buildEmbedder(cfg, log)

	if _, isHash := embedder.(*knowledge.HashEmbedder); isHash {
		t.Fatalf("buildEmbedder with EmbeddingProvider=openai returned a HashEmbedder — real provider was not honoured")
	}
	if _, isOpenAI := embedder.(*knowledge.OpenAIEmbedder); !isOpenAI {
		t.Fatalf("buildEmbedder with EmbeddingProvider=openai returned %T, want *knowledge.OpenAIEmbedder", embedder)
	}

	out := buf.String()
	if strings.Contains(out, "non-semantic") {
		t.Fatalf("did NOT expect a non-semantic-embedder warning when a real provider is configured, got:\n%s", out)
	}
	t.Logf("captured log output (no warning, as expected):\n%q", out)
}

func TestBuildEmbedder_LlamaProviderConfigured_NoWarning(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)

	cfg := &config.HelixConfig{}
	cfg.Knowledge.EmbeddingProvider = "llama"
	cfg.Knowledge.EmbeddingModel = "bge-base"
	cfg.Knowledge.EmbeddingBaseURL = "http://127.0.0.1:19999" // never dialed at construction time

	embedder := buildEmbedder(cfg, log)

	if _, isHash := embedder.(*knowledge.HashEmbedder); isHash {
		t.Fatalf("buildEmbedder with EmbeddingProvider=llama returned a HashEmbedder — real provider was not honoured")
	}

	out := buf.String()
	if strings.Contains(out, "non-semantic") {
		t.Fatalf("did NOT expect a non-semantic-embedder warning when the llama provider is configured, got:\n%s", out)
	}
}
