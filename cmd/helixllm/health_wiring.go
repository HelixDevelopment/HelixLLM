package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

// healthProbeTimeout backstops any health probe whose context arrives without
// a deadline. It sits below the aggregator's own 5 s per-component timeout so
// a wedged dependency surfaces as that dependency's own error rather than as a
// generic aggregator timeout.
const healthProbeTimeout = 4 * time.Second

// HXC-244 — the /internal/health endpoint must report on real dependencies.
//
// Before this file existed, main.go constructed health.NewChecker() and handed
// it to the server without ever calling Register/RegisterOptional. The
// aggregator had nothing to aggregate, so GET /internal/health answered
// {"status":"healthy","components":[]} unconditionally — Redis, the vector
// store, the verifier and every LLM provider could be dead and the endpoint
// would still tell an orchestrator, a load balancer, or an on-call human that
// the gateway was fine. That is strictly worse than having no health endpoint:
// no endpoint is an honest gap, while that one asserted a state it never
// measured.
//
// Every check registered here does REAL work — a Redis RESP PING, a Qdrant
// liveness request, an HTTP GET against the verifier path the gateway actually
// consumes, a llama.cpp /health round-trip via the provider. None of them is
// satisfied by reading configuration back.

// Component names published in the health report. They are part of the
// endpoint's observable contract (dashboards and alerts key off them), so
// treat a rename as a breaking change.
const (
	healthComponentProviders = "llm_providers"
	healthComponentRedisKV   = "kv_cache_redis"
	healthComponentVerifier  = "llms_verifier"
	// healthComponentVectorStorePrefix is suffixed with the configured backend
	// name (e.g. "vector_store_qdrant") so the report says WHICH backend it
	// checked, not merely that some store was checked.
	healthComponentVectorStorePrefix = "vector_store_"
)

// healthCheckDeps carries the already-constructed dependencies whose liveness
// /internal/health reports on. Probe fields are plain funcs rather than
// concrete clients so the registration policy below — which components exist
// and which of them are required — is testable without standing up Redis or
// Qdrant.
type healthCheckDeps struct {
	// Providers returns the live LLM provider set. Required: with no provider
	// able to serve, the gateway cannot answer a single completion request.
	Providers func() map[string]brain.Provider

	// RedisAddr is the configured "host:port". Empty means Redis is not
	// configured and no Redis component is published.
	RedisAddr string
	// RedisProbe performs a real Redis round-trip. Nil when Redis is not
	// configured.
	RedisProbe func(context.Context) error
	// RedisInUse reports whether the KV cache is actually backed by Redis, as
	// decided once at startup.
	RedisInUse bool

	// VectorBackend is the configured backend name ("qdrant", "memory", ...).
	VectorBackend string
	// VectorTarget is the configured "host:port" of that backend, for messages.
	VectorTarget string
	// VectorProbe performs a real round-trip to the vector store. Nil when the
	// gateway holds no client for it (unreachable at startup, or purely
	// in-process).
	VectorProbe func(context.Context) error
	// VectorInUse reports whether the RAG pipeline is actually backed by the
	// configured remote store, as decided once at startup.
	VectorInUse bool

	// VerifierURL is the LLMsVerifier base URL. Empty means no verifier is
	// configured and no verifier component is published.
	VerifierURL string
	// HTTPClient is used for the verifier probe. Required when VerifierURL is
	// set.
	HTTPClient *http.Client
}

// registerHealthChecks wires every dependency check onto checker.
//
// Required vs optional is decided by ONE question: can the gateway still serve
// its users when this dependency is down?
//
//   - llm_providers is REQUIRED. It is the only component whose failure means
//     the gateway is genuinely unable to do its job, so it is the only one that
//     may turn /internal/health into a 503.
//   - Redis, the vector store and the verifier are OPTIONAL, because the
//     gateway is BUILT to degrade past each of them: the KV cache falls back to
//     in-memory, the RAG store falls back to the in-process memory store, and
//     the fallback chain falls back to a static score table. Marking any of
//     them required would make the endpoint answer 503 for a deployment that is
//     serving every request correctly — a false alarm is a defect in a health
//     endpoint just as much as a false all-clear.
//
// Safe to call after the server has been constructed with the same checker:
// the underlying aggregator guards its component list with a mutex, and no
// request is served until ListenAndServe runs.
func registerHealthChecks(checker *health.Checker, deps healthCheckDeps) {
	checker.Register(healthComponentProviders, providersCheck(deps.Providers))

	if deps.RedisAddr != "" {
		checker.RegisterOptional(healthComponentRedisKV, fallbackDependencyCheck(
			"redis KV cache", deps.RedisAddr, deps.RedisInUse, deps.RedisProbe))
	}

	if isRemoteVectorBackend(deps.VectorBackend) {
		checker.RegisterOptional(
			healthComponentVectorStorePrefix+deps.VectorBackend,
			fallbackDependencyCheck(
				"vector store ("+deps.VectorBackend+")",
				deps.VectorTarget, deps.VectorInUse, deps.VectorProbe))
	}

	if deps.VerifierURL != "" && deps.HTTPClient != nil {
		checker.RegisterOptional(healthComponentVerifier,
			health.HTTPGetCheck(deps.HTTPClient, verifierProbeURL(deps.VerifierURL)))
	}
}

// isRemoteVectorBackend reports whether the configured vector backend is one
// the gateway reaches over the network (and can therefore lose).
//
// The test is "not the in-process store" rather than an allow-list of known
// backend names, so a backend added to knowledge.NewVectorStore later is
// health-checked automatically instead of silently dropping out of the report.
// A misspelled backend name also lands here — deliberately: NewVectorStore
// resolves an unrecognised name to the in-memory store, and an operator who
// asked for a persistent store and silently got a volatile one should see that
// on the health endpoint.
func isRemoteVectorBackend(backend string) bool {
	return backend != "" && backend != "memory"
}

// verifierProbeURL returns the exact URL the gateway's ScorerBridge consumes.
// Probing the real consumed path (rather than a generic /health) means the
// check fails in the same conditions the feature fails.
func verifierProbeURL(baseURL string) string {
	return strings.TrimSuffix(baseURL, "/") + "/api/scores"
}

// providersCheck reports the gateway unable to serve when NO registered LLM
// provider is ready.
//
// Honest boundary: what "ready" means is each provider's own readiness
// contract, and it is not uniform. The local llama.cpp provider answers with a
// real HTTP GET to its /health endpoint, so a green result there is genuine
// liveness. Cloud providers answer with credential presence, so a green result
// there proves the gateway is configured to call them — NOT that the vendor's
// API is currently up. This check therefore detects "the gateway has nothing to
// route to"; it does not, and does not claim to, detect a remote vendor outage.
func providersCheck(providers func() map[string]brain.Provider) health.CheckFunc {
	return func(ctx context.Context) error {
		if providers == nil {
			return fmt.Errorf("llm providers: no provider source wired into the health checker")
		}

		registered := providers()
		if len(registered) == 0 {
			return fmt.Errorf("llm providers: none registered — every completion request will fail")
		}

		ready := 0
		unavailable := make([]string, 0, len(registered))
		for name, p := range registered {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("llm providers: probe cancelled after %d of %d providers: %w",
					ready+len(unavailable), len(registered), err)
			}
			if p.Available() {
				ready++
				continue
			}
			unavailable = append(unavailable, name)
		}

		if ready == 0 {
			sort.Strings(unavailable)
			return fmt.Errorf(
				"llm providers: all %d registered providers report unavailable (%s) — every completion request will fail",
				len(registered), strings.Join(unavailable, ", "))
		}
		return nil
	}
}

// fallbackDependencyCheck builds the check for a remote dependency the gateway
// is configured to use but can degrade away from at startup.
//
// Three distinct states, three distinct verdicts:
//
//   - in use and reachable            -> healthy
//   - in use and unreachable          -> unhealthy, carrying the probe error
//   - configured but NOT in use       -> unhealthy, naming the degradation
//
// The third state matters and is easy to lose. When a backend is unreachable
// at startup the gateway swaps in its in-process substitute for the whole
// process lifetime, so the operator's configured dependency is not being used
// and will not be until a restart. Reporting that as healthy — on the grounds
// that nothing is currently erroring — would be the same absence-of-error
// verdict this whole change removes. Its staticness is not a weakness of the
// check: the underlying fact is genuinely fixed for the process lifetime, and
// the live probe result is still folded into the message so the operator can
// tell "still down" from "back up, restart to re-attach".
//
// A missing probe never yields healthy: a status that was never measured is
// not a status.
func fallbackDependencyCheck(
	desc, target string,
	inUse bool,
	probe func(context.Context) error,
) health.CheckFunc {
	return func(ctx context.Context) error {
		var probeErr error
		if probe != nil {
			probeErr = probe(ctx)
		}

		if !inUse {
			reachability := "no client was constructed for it, so its current state is unknown"
			switch {
			case probe != nil && probeErr == nil:
				reachability = "it is reachable again now; restart the gateway to re-attach"
			case probe != nil:
				reachability = "it is still unreachable: " + probeErr.Error()
			}
			return fmt.Errorf(
				"%s at %s is configured but NOT in use — the gateway fell back to its in-process substitute at startup and will not re-attach without a restart (%s)",
				desc, target, reachability)
		}

		if probe == nil {
			return fmt.Errorf(
				"%s at %s: no liveness probe wired — refusing to report a status this check never measured",
				desc, target)
		}
		return probeErr
	}
}
