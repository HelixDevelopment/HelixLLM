package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// stubProvider is a unit-test-only brain.Provider whose readiness is fixed by
// the test. Only Name/Available are exercised by the health check; the
// completion methods exist solely to satisfy the interface and fail loudly if
// a future change starts calling them from a health probe (a health check that
// issues completions would be a real defect, not a passing test).
type stubProvider struct {
	name      string
	available bool
}

func (s stubProvider) Name() string     { return s.name }
func (s stubProvider) Available() bool  { return s.available }
func (s stubProvider) Models() []string { return []string{s.name + "-model"} }
func (s stubProvider) Complete(context.Context, *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return nil, errors.New("stubProvider.Complete must never be called by a health check")
}
func (s stubProvider) CompleteStream(context.Context, *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, errors.New("stubProvider.CompleteStream must never be called by a health check")
}

func providerSet(ps ...brain.Provider) func() map[string]brain.Provider {
	m := make(map[string]brain.Provider, len(ps))
	for _, p := range ps {
		m[p.Name()] = p
	}
	return func() map[string]brain.Provider { return m }
}

// ---------------------------------------------------------------------------
// providersCheck
// ---------------------------------------------------------------------------

func TestProvidersCheck_OneReadyIsHealthy(t *testing.T) {
	check := providersCheck(providerSet(
		stubProvider{name: "llamacpp", available: false},
		stubProvider{name: "openrouter", available: true},
	))
	if err := check(context.Background()); err != nil {
		t.Fatalf("one available provider should be healthy, got %v", err)
	}
}

func TestProvidersCheck_AllUnavailableIsUnhealthy(t *testing.T) {
	check := providersCheck(providerSet(
		stubProvider{name: "llamacpp", available: false},
		stubProvider{name: "openrouter", available: false},
	))
	err := check(context.Background())
	if err == nil {
		t.Fatal("zero available providers MUST be unhealthy — the gateway cannot serve a single completion")
	}
	// The message must name the culprits, otherwise the report tells an
	// operator that something is wrong without saying what.
	for _, want := range []string{"llamacpp", "openrouter"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message %q does not name unavailable provider %q", err, want)
		}
	}
}

func TestProvidersCheck_NoneRegisteredIsUnhealthy(t *testing.T) {
	if err := providersCheck(providerSet())(context.Background()); err == nil {
		t.Fatal("an empty provider set MUST be unhealthy")
	}
}

func TestProvidersCheck_NilSourceIsUnhealthyNotHealthy(t *testing.T) {
	// A missing wiring must never read as healthy: that is the exact failure
	// mode HXC-244 was.
	if err := providersCheck(nil)(context.Background()); err == nil {
		t.Fatal("a nil provider source MUST be unhealthy, not silently healthy")
	}
}

func TestProvidersCheck_HonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := providersCheck(providerSet(stubProvider{name: "llamacpp", available: true}))(ctx)
	if err == nil {
		t.Fatal("a cancelled context MUST NOT produce a healthy verdict")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should wrap context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// fallbackDependencyCheck
// ---------------------------------------------------------------------------

func TestFallbackDependencyCheck_InUseAndReachableIsHealthy(t *testing.T) {
	check := fallbackDependencyCheck("redis KV cache", "127.0.0.1:6379", true,
		func(context.Context) error { return nil })
	if err := check(context.Background()); err != nil {
		t.Fatalf("in-use + reachable should be healthy, got %v", err)
	}
}

func TestFallbackDependencyCheck_InUseAndUnreachableSurfacesProbeError(t *testing.T) {
	probeErr := errors.New("dial tcp 127.0.0.1:6379: connect: connection refused")
	check := fallbackDependencyCheck("redis KV cache", "127.0.0.1:6379", true,
		func(context.Context) error { return probeErr })
	err := check(context.Background())
	if err == nil {
		t.Fatal("in-use + unreachable MUST be unhealthy")
	}
	if !errors.Is(err, probeErr) {
		t.Errorf("the real probe error must reach the report, got %v", err)
	}
}

func TestFallbackDependencyCheck_ConfiguredButNotInUseIsUnhealthy(t *testing.T) {
	// The dependency answers, but the gateway degraded past it at startup and
	// is not using it. Reporting healthy here — on the grounds that nothing is
	// erroring right now — would be the absence-of-error verdict.
	check := fallbackDependencyCheck("vector store (qdrant)", "localhost:6333", false,
		func(context.Context) error { return nil })
	err := check(context.Background())
	if err == nil {
		t.Fatal("configured-but-not-in-use MUST be unhealthy even when the dependency is reachable")
	}
	if !strings.Contains(err.Error(), "NOT in use") {
		t.Errorf("message must state the dependency is not in use, got %q", err)
	}
	if !strings.Contains(err.Error(), "restart") {
		t.Errorf("message must tell the operator a restart is needed to re-attach, got %q", err)
	}
}

func TestFallbackDependencyCheck_NotInUseAndStillDownReportsProbeDetail(t *testing.T) {
	check := fallbackDependencyCheck("redis KV cache", "127.0.0.1:6379", false,
		func(context.Context) error { return errors.New("connection refused") })
	err := check(context.Background())
	if err == nil {
		t.Fatal("not-in-use MUST be unhealthy")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("message must fold in the live probe detail, got %q", err)
	}
}

func TestFallbackDependencyCheck_MissingProbeNeverReportsHealthy(t *testing.T) {
	check := fallbackDependencyCheck("redis KV cache", "127.0.0.1:6379", true, nil)
	if err := check(context.Background()); err == nil {
		t.Fatal("a status that was never measured MUST NOT be reported healthy")
	}
}

// ---------------------------------------------------------------------------
// registration policy
// ---------------------------------------------------------------------------

func TestRegisterHealthChecks_PublishesNamedComponents(t *testing.T) {
	verifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"scores":{}}`))
	}))
	defer verifier.Close()

	checker := health.NewChecker()
	registerHealthChecks(checker, healthCheckDeps{
		Providers:     providerSet(stubProvider{name: "llamacpp", available: true}),
		RedisAddr:     "127.0.0.1:6379",
		RedisProbe:    func(context.Context) error { return nil },
		RedisInUse:    true,
		VectorBackend: "qdrant",
		VectorTarget:  "localhost:6333",
		VectorProbe:   func(context.Context) error { return nil },
		VectorInUse:   true,
		VerifierURL:   verifier.URL,
		HTTPClient:    verifier.Client(),
	})

	report := checker.Check(context.Background())

	// HXC-244's core regression: the report must not be empty.
	if len(report.Components) == 0 {
		t.Fatal("health report has zero components — the HXC-244 defect is back")
	}

	// Every component must carry a name; an anonymous component is unusable in
	// a report and is what the standing guard rejects.
	names := make(map[string]bool, len(report.Components))
	for i, c := range report.Components {
		if strings.TrimSpace(c.Name) == "" {
			t.Errorf("component %d carries no name", i)
			continue
		}
		if names[c.Name] {
			t.Errorf("duplicate component name %q", c.Name)
		}
		names[c.Name] = true
	}

	for _, want := range []string{
		healthComponentProviders,
		healthComponentRedisKV,
		healthComponentVectorStorePrefix + "qdrant",
		healthComponentVerifier,
	} {
		if !names[want] {
			t.Errorf("component %q missing from report (got %v)", want, names)
		}
	}

	if report.Status != health.StatusHealthy {
		t.Errorf("all dependencies up should aggregate to healthy, got %q: %+v",
			report.Status, report.Components)
	}
}

func TestRegisterHealthChecks_UnconfiguredDependenciesAreNotPublished(t *testing.T) {
	// A component the deployment does not use must not appear at all — a
	// permanently-failing check for an absent dependency would be noise, and a
	// permanently-passing one would be a bluff.
	checker := health.NewChecker()
	registerHealthChecks(checker, healthCheckDeps{
		Providers:     providerSet(stubProvider{name: "llamacpp", available: true}),
		VectorBackend: "memory",
	})

	report := checker.Check(context.Background())
	if len(report.Components) != 1 {
		t.Fatalf("want exactly the required provider component, got %d: %+v",
			len(report.Components), report.Components)
	}
	if report.Components[0].Name != healthComponentProviders {
		t.Errorf("component = %q, want %q", report.Components[0].Name, healthComponentProviders)
	}
	// The required check is registered unconditionally, so the report can never
	// be empty however little else is configured.
	if report.Status != health.StatusHealthy {
		t.Errorf("status = %q, want healthy", report.Status)
	}
}

func TestRegisterHealthChecks_FailedProvidersMakeReportUnhealthy(t *testing.T) {
	checker := health.NewChecker()
	registerHealthChecks(checker, healthCheckDeps{
		Providers: providerSet(stubProvider{name: "llamacpp", available: false}),
	})

	report := checker.Check(context.Background())
	if report.Status != health.StatusUnhealthy {
		t.Fatalf("status = %q, want %q — llm_providers is REQUIRED",
			report.Status, health.StatusUnhealthy)
	}
}

func TestRegisterHealthChecks_FailedOptionalDependencyDegradesOnly(t *testing.T) {
	checker := health.NewChecker()
	registerHealthChecks(checker, healthCheckDeps{
		Providers:  providerSet(stubProvider{name: "llamacpp", available: true}),
		RedisAddr:  "127.0.0.1:6379",
		RedisProbe: func(context.Context) error { return errors.New("connection refused") },
		RedisInUse: true,
	})

	report := checker.Check(context.Background())
	if report.Status != health.StatusDegraded {
		t.Fatalf("status = %q, want %q — Redis is optional because the gateway "+
			"falls back to the in-memory KV cache and still serves every request",
			report.Status, health.StatusDegraded)
	}
}

// TestServedHealthReport_MatchesStandingGuardContract drives the SAME assertion
// the standing HXC-244 guard makes against the deployed service
// (scripts/testing/guard_hxc244_health_components_registered.sh), but
// in-process: real registration -> real server.New -> real HTTP handler ->
// decode the served JSON body.
//
// The guard's contract, restated: the body's "components" key must exist, be a
// list, be non-empty, and every entry must carry a non-empty "name". That is
// the falsifiable property — a report that names what it checked is evidence,
// while {"status":"healthy","components":[]} is a claim.
//
// This does not replace running the guard against a redeployed gateway (only
// that proves the runtime-on-clean-target layer); it proves the source and
// handler wiring produce a body the guard would accept.
func TestServedHealthReport_MatchesStandingGuardContract(t *testing.T) {
	checker := health.NewChecker()
	registerHealthChecks(checker, healthCheckDeps{
		Providers:  providerSet(stubProvider{name: "llamacpp", available: true}),
		RedisAddr:  "127.0.0.1:6379",
		RedisProbe: func(context.Context) error { return nil },
		RedisInUse: true,
	})

	srv := server.New(server.Options{Host: "127.0.0.1", Port: 0, Checker: checker})
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/internal/health", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Decode exactly as the guard's analyzer does: generic JSON, then assert
	// the shape rather than a Go struct that would paper over a missing key.
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("served body is not JSON: %v", err)
	}
	raw, ok := body["components"]
	if !ok {
		t.Fatal("served report omits the components field entirely")
	}
	components, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("components is %T, want a list", raw)
	}
	if len(components) == 0 {
		t.Fatalf("served report has zero components with status=%v — this is the "+
			"exact HXC-244 defect the guard reproduces", body["status"])
	}
	for i, c := range components {
		entry, ok := c.(map[string]interface{})
		if !ok {
			t.Errorf("component %d is %T, want an object", i, c)
			continue
		}
		name, _ := entry["name"].(string)
		if strings.TrimSpace(name) == "" {
			t.Errorf("component %d carries no name — the guard rejects unnamed components", i)
		}
	}
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func TestIsRemoteVectorBackend(t *testing.T) {
	for backend, want := range map[string]bool{
		"qdrant":   true,
		"weaviate": true, // an unknown-to-us backend still gets checked
		"memory":   false,
		"":         false,
	} {
		if got := isRemoteVectorBackend(backend); got != want {
			t.Errorf("isRemoteVectorBackend(%q) = %v, want %v", backend, got, want)
		}
	}
}

func TestVerifierProbeURL(t *testing.T) {
	for base, want := range map[string]string{
		"http://localhost:8100":  "http://localhost:8100/api/scores",
		"http://localhost:8100/": "http://localhost:8100/api/scores",
	} {
		if got := verifierProbeURL(base); got != want {
			t.Errorf("verifierProbeURL(%q) = %q, want %q", base, got, want)
		}
	}
}
