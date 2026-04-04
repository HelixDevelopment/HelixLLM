package brain_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// newMock is a convenience helper for router tests.
func newMock(name string, available bool, models ...string) *mockProvider {
	return &mockProvider{
		name:      name,
		available: available,
		models:    models,
	}
}

func TestRouter_RouteByModelPrefix_GPT(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	r.Register("openai", newMock("openai", true, "gpt-4o"))
	r.Register("anthropic", newMock("anthropic", true, "claude-sonnet-4-5"))
	r.Register("llamacpp", newMock("llamacpp", true, "llama-3.1-70b"))

	p, err := r.Route(&types.InternalChatRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "openai")
	}
}

func TestRouter_RouteByModelPrefix_Claude(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	r.Register("openai", newMock("openai", true, "gpt-4o"))
	r.Register("anthropic", newMock("anthropic", true, "claude-sonnet-4-5"))
	r.Register("llamacpp", newMock("llamacpp", true, "llama-3.1-70b"))

	p, err := r.Route(&types.InternalChatRequest{Model: "claude-sonnet-4-5"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "anthropic")
	}
}

func TestRouter_RouteByModelPrefix_Llama(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	r.Register("openai", newMock("openai", true, "gpt-4o"))
	r.Register("anthropic", newMock("anthropic", true, "claude-sonnet-4-5"))
	r.Register("llamacpp", newMock("llamacpp", true, "llama-3.1-70b"))

	p, err := r.Route(&types.InternalChatRequest{Model: "llama-3.1-70b"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "llamacpp")
	}
}

func TestRouter_RouteByModelPrefix_Qwen(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	r.Register("llamacpp", newMock("llamacpp", true, "qwen2.5-72b"))

	p, err := r.Route(&types.InternalChatRequest{Model: "qwen2.5-72b"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "llamacpp")
	}
}

func TestRouter_ExplicitProviderOverride(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	r.Register("openai", newMock("openai", true, "gpt-4o"))
	r.Register("anthropic", newMock("anthropic", true, "claude-sonnet-4-5"))
	r.Register("llamacpp", newMock("llamacpp", true, "llama-3.1-70b"))

	// Request a gpt model but force anthropic via explicit provider field.
	p, err := r.Route(&types.InternalChatRequest{
		Model:    "gpt-4o",
		Provider: types.ProviderAnthropic,
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "anthropic")
	}
}

func TestRouter_FallbackWhenPreferredUnavailable(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	// openai is unavailable; llamacpp is the fallback and is available.
	r.Register("openai", newMock("openai", false, "gpt-4o"))
	r.Register("llamacpp", newMock("llamacpp", true, "llama-3.1-70b"))

	p, err := r.Route(&types.InternalChatRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "llamacpp")
	}
}

func TestRouter_FallbackToAnyAvailable(t *testing.T) {
	// No fallback name set; the only available provider should be chosen.
	r := brain.NewRouter("")
	r.Register("openai", newMock("openai", false, "gpt-4o"))
	r.Register("anthropic", newMock("anthropic", true, "claude-sonnet-4-5"))

	// Model has no matching prefix rule (no prefix match for "unknown-model").
	p, err := r.Route(&types.InternalChatRequest{Model: "unknown-model"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "anthropic")
	}
}

func TestRouter_ErrorWhenNoProvidersAvailable(t *testing.T) {
	r := brain.NewRouter("openai")
	r.Register("openai", newMock("openai", false))
	r.Register("anthropic", newMock("anthropic", false))

	_, err := r.Route(&types.InternalChatRequest{Model: "gpt-4o"})
	if err == nil {
		t.Fatal("expected error when no providers available, got nil")
	}
}

func TestRouter_ErrorWhenNoProvidersRegistered(t *testing.T) {
	r := brain.NewRouter("openai")

	_, err := r.Route(&types.InternalChatRequest{Model: "gpt-4o"})
	if err == nil {
		t.Fatal("expected error when no providers registered, got nil")
	}
}

func TestRouter_ExplicitProviderUnavailableFallsThrough(t *testing.T) {
	r := brain.NewRouter("llamacpp")
	// Explicit provider is registered but unavailable; should fall through to prefix rule.
	r.Register("openai", newMock("openai", false, "gpt-4o"))
	r.Register("llamacpp", newMock("llamacpp", true, "llama-3.1-70b"))

	p, err := r.Route(&types.InternalChatRequest{
		Model:    "gpt-4o",
		Provider: types.ProviderOpenAI, // openai unavailable → falls through
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	// gpt- prefix matches openai (unavailable), then fallback llamacpp is used.
	if p.Name() != "llamacpp" {
		t.Errorf("Route() provider = %q, want %q", p.Name(), "llamacpp")
	}
}
