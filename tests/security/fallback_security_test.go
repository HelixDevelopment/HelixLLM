// Package security_test contains security tests for the HelixLLM fallback chain.
// These tests always run — they do not require live services.
package security_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// TestFallbackSecurity_APIKeyNotLeakedInError verifies that a ProviderError
// returned by OpenAICompatProvider does NOT include the API key in its
// Error() string.  Leaking credentials in error messages is a security
// vulnerability that could expose keys in log aggregators.
func TestFallbackSecurity_APIKeyNotLeakedInError(t *testing.T) {
	const secretKey = "sk-super-secret-api-key-do-not-leak"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "test-provider",
		BaseURL:    srv.URL,
		APIKey:     secretKey,
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	}

	_, err := p.Complete(ctx, req)
	if err == nil {
		t.Fatal("expected an error for 401 response, got nil")
	}

	errStr := err.Error()
	if strings.Contains(errStr, secretKey) {
		t.Errorf("API key leaked in error string: %q", errStr)
	}

	// Also verify the key is not in any wrapped error messages.
	var pe *brain.ProviderError
	if brain.AsProviderError(err, &pe) {
		if strings.Contains(pe.Body, secretKey) {
			t.Errorf("API key leaked in ProviderError.Body: %q", pe.Body)
		}
		if strings.Contains(pe.Error(), secretKey) {
			t.Errorf("API key leaked in ProviderError.Error(): %q", pe.Error())
		}
		// StatusCode should be 401, not 0.
		if pe.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected StatusCode=401, got %d", pe.StatusCode)
		}
	}
}

// TestFallbackSecurity_RateLimitHeaderInjection verifies that malformed or
// adversarial Retry-After / x-ratelimit-* header values do not cause panics
// or incorrect behavior in the RateLimitTracker.
func TestFallbackSecurity_RateLimitHeaderInjection(t *testing.T) {
	malformedValues := []string{
		"",
		"not-a-number",
		"-1",
		"99999999999999999999999999999999", // overflow
		"1; DROP TABLE providers",
		"\x00\x01\x02",
		strings.Repeat("A", 10000), // very long value
		"1.5",
		"0",
	}

	rl := fallback.NewRateLimitTracker(5, 100)

	for _, val := range malformedValues {
		t.Run("retry-after="+val[:min(len(val), 30)], func(t *testing.T) {
			h := make(http.Header)
			h.Set("Retry-After", val)
			h.Set("x-ratelimit-remaining-requests", val)
			h.Set("x-ratelimit-remaining-tokens", val)
			h.Set("x-ratelimit-reset-requests", val)

			// Must not panic.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic with malformed header %q: %v", val, r)
				}
			}()

			rl.UpdateFromHeaders("test-provider", h)
			backoff := rl.ParseRetryAfter(h)
			_ = backoff // any value is acceptable as long as no panic
		})
	}
}

// TestFallbackSecurity_ChainNoLeakOnAllExhausted verifies that when all
// providers in the chain return errors, the aggregate error message does not
// contain any API key material from the underlying ProviderError.
func TestFallbackSecurity_ChainNoLeakOnAllExhausted(t *testing.T) {
	const secretKey = "sk-chain-secret-key-99999"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "chain-provider",
		BaseURL:    srv.URL,
		APIKey:     secretKey,
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{"chain-provider": p}, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "chain-provider",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	}

	_, err := chain.Complete(ctx, req)
	if err == nil {
		t.Fatal("expected error when all providers exhausted")
	}

	if strings.Contains(err.Error(), secretKey) {
		t.Errorf("API key leaked in chain error: %q", err.Error())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
