package fallback

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

// ScorerBridgeConfig holds configuration for the ScorerBridge.
type ScorerBridgeConfig struct {
	// VerifierURL is the base URL of the LLMsVerifier service
	// (e.g. "http://localhost:8090").  Leave empty to use static scores only.
	VerifierURL string

	// RefreshInterval controls how often the refresh loop re-fetches scores.
	// Defaults to 5 minutes when zero.
	RefreshInterval time.Duration
}

// ScorerBridge fetches provider quality scores from LLMsVerifier and converts
// them into ordered []ChainEntry lists suitable for FallbackChain.SetEntries.
//
// On every network or decode failure FetchScores returns the built-in static
// scores instead of propagating an error, so the chain always has a usable
// ordering even when the verifier is unreachable.
type ScorerBridge struct {
	verifierURL     string
	refreshInterval time.Duration
	httpClient      *http.Client
	wg              sync.WaitGroup
}

// NewScorerBridge creates a ScorerBridge from cfg, applying defaults:
//   - RefreshInterval defaults to 5 minutes.
//   - HTTP client timeout defaults to 10 seconds.
func NewScorerBridge(cfg ScorerBridgeConfig) *ScorerBridge {
	interval := cfg.RefreshInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &ScorerBridge{
		verifierURL:     cfg.VerifierURL,
		refreshInterval: interval,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// StaticFallbackScores returns hardcoded quality scores used when the verifier
// is unreachable.  Scores are on an arbitrary 0-100 scale; higher is better.
// llamacpp is intentionally the lowest so BuildEntries always places it last.
func (sb *ScorerBridge) StaticFallbackScores() map[string]float64 {
	return map[string]float64{
		"openrouter":  90,
		"chutes":      85,
		"huggingface": 80,
		"nvidia":      75,
		"cerebras":    70,
		"sambanova":   65,
		"together":    60,
		"llamacpp":    10,
	}
}

// verifierResponse matches the JSON shape returned by LLMsVerifier's /api/scores.
type verifierResponse struct {
	Scores map[string]struct {
		Total float64 `json:"total"`
	} `json:"scores"`
}

// FetchScores contacts {verifierURL}/api/scores and parses the response into a
// provider → score map.  On any failure (network error, non-200, decode error)
// it logs a warning and returns StaticFallbackScores() with a nil error so the
// caller always receives a usable score map.
func (sb *ScorerBridge) FetchScores(ctx context.Context) (map[string]float64, error) {
	static := sb.StaticFallbackScores()

	if sb.verifierURL == "" {
		return static, nil
	}

	url := sb.verifierURL + "/api/scores"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("scorer_bridge: failed to build request, using static scores",
			"url", url, "error", err)
		return static, nil
	}

	resp, err := sb.httpClient.Do(req)
	if err != nil {
		slog.Warn("scorer_bridge: verifier unreachable, using static scores",
			"url", url, "error", err)
		return static, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("scorer_bridge: verifier returned non-200, using static scores",
			"url", url, "status", resp.StatusCode)
		return static, nil
	}

	var vr verifierResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		slog.Warn("scorer_bridge: failed to decode verifier response, using static scores",
			"url", url, "error", err)
		return static, nil
	}

	scores := make(map[string]float64, len(vr.Scores))
	for name, data := range vr.Scores {
		scores[name] = data.Total
	}
	if len(scores) == 0 {
		slog.Warn("scorer_bridge: verifier returned empty scores, using static scores",
			"url", url)
		return static, nil
	}
	return scores, nil
}

// BuildEntries creates a []ChainEntry from the given scores and model mappings.
//
// Ordering rules:
//  1. Cloud providers are sorted by score descending.
//  2. llamacpp (IsLocalFallback=true) is always placed last regardless of score.
//
// Each entry receives a fresh CircuitBreaker(3 failures, 2-minute open window).
// Providers present in scores but absent from models receive an empty ModelID
// (the chain will use whatever the provider reports as its default model).
func (sb *ScorerBridge) BuildEntries(scores map[string]float64, models map[string]string) []ChainEntry {
	type scored struct {
		name  string
		score float64
	}

	var cloud []scored
	var local []scored

	for name, score := range scores {
		if name == "llamacpp" {
			local = append(local, scored{name, score})
		} else {
			cloud = append(cloud, scored{name, score})
		}
	}

	// Sort cloud entries by score descending; break ties alphabetically for
	// deterministic output.
	sort.Slice(cloud, func(i, j int) bool {
		if cloud[i].score != cloud[j].score {
			return cloud[i].score > cloud[j].score
		}
		return cloud[i].name < cloud[j].name
	})

	entries := make([]ChainEntry, 0, len(cloud)+len(local))
	for _, s := range cloud {
		entries = append(entries, ChainEntry{
			ProviderName:    s.name,
			ModelID:         models[s.name],
			Score:           s.score,
			Status:          EntryActive,
			CircuitBreaker:  NewCircuitBreaker(3, 2*time.Minute),
			IsLocalFallback: false,
		})
	}
	for _, s := range local {
		entries = append(entries, ChainEntry{
			ProviderName:    s.name,
			ModelID:         models[s.name],
			Score:           s.score,
			Status:          EntryActive,
			CircuitBreaker:  NewCircuitBreaker(3, 2*time.Minute),
			IsLocalFallback: true,
		})
	}

	return entries
}

// StartRefreshLoop launches a background goroutine that re-fetches scores every
// refreshInterval and calls chain.SetEntries with the rebuilt list.  The loop
// exits when ctx is cancelled.  Call Wait() to block until the goroutine has
// fully exited.
func (sb *ScorerBridge) StartRefreshLoop(ctx context.Context, chain *Chain, models map[string]string) {
	sb.wg.Add(1)
	go func() {
		defer sb.wg.Done()

		// Perform an immediate fetch so the chain is populated before the first
		// ticker fires.  This also makes tests that only wait a short time
		// reliable without depending on ticker scheduling.
		sb.doRefresh(ctx, chain, models)

		ticker := time.NewTicker(sb.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sb.doRefresh(ctx, chain, models) // loopCtx checked inside
			}
		}
	}()
}

// Wait blocks until the refresh loop goroutine (if any) has exited.
func (sb *ScorerBridge) Wait() {
	sb.wg.Wait()
}

// doRefresh is the shared body of a single refresh cycle: fetch scores, build
// entries, and update the chain.  loopCtx cancellation stops the loop; each
// individual HTTP fetch gets its own timeout-bounded context derived from
// context.Background() so that a loop shutdown does not cancel an in-flight
// request mid-flight (the HTTP client timeout already bounds it).
func (sb *ScorerBridge) doRefresh(loopCtx context.Context, chain *Chain, models map[string]string) {
	// If the loop has already been cancelled, skip silently.
	select {
	case <-loopCtx.Done():
		return
	default:
	}

	// Give each individual fetch its own deadline derived from the HTTP client
	// timeout so it is independent from the loop lifecycle context.
	fetchCtx, cancel := context.WithTimeout(context.Background(), sb.httpClient.Timeout)
	defer cancel()

	scores, err := sb.FetchScores(fetchCtx)
	if err != nil {
		slog.Warn("scorer_bridge: refresh fetch error", "error", err)
		return
	}
	entries := sb.BuildEntries(scores, models)
	chain.SetEntries(entries)
	slog.Debug("scorer_bridge: chain entries refreshed", "count", len(entries))
}
