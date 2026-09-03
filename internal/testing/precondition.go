package testing

// Declared, runtime-probed preconditions for challenge steps.
//
// # Why this exists
//
// A challenge is a black-box assertion against a deployed instance, so some
// of what it asserts is not a property of the code at all but a property of
// the DEPLOYMENT it happens to be pointed at. The auth challenges are the
// motivating case: `known-bug-regression/auth-empty-bearer-token` and
// `owasp-top10-security/auth-bypass-malformed-bearer` assert a hard 401,
// which is reachable only when the server was started with
// HELIX_AUTH_API_KEYS set. With no keys configured the auth middleware runs
// in open-access mode BY DESIGN (internal/gateway/middleware/auth.go) and no
// request can ever be answered 401 — so on the default deployment those
// challenges were asserting a behaviour the target had never been asked to
// have. Captured on this host, same binary, two servers:
//
//	open-access  POST /v1/chat/completions  Authorization: "Bearer "        -> 503
//	keyed        POST /v1/chat/completions  Authorization: "Bearer "        -> 401
//
// The assertion is right. What was missing was the challenge saying which
// deployment it needs.
//
// # What a precondition is NOT
//
// It is not a way to make a red challenge green. A step whose precondition
// is unsatisfied is SKIPPED, and a skip is not absorbed into a pass: a
// challenge whose every step skipped is itself reported "skipped", and
// Runner.Verify names it and drives a non-zero exit — the same treatment
// the benchmark/chaos steps already get. The number does not improve by
// declaring a precondition; only the diagnosis does.
//
// # Probed, never assumed
//
// A precondition is decided by a real request against the real target at run
// time, never by configuration the harness was told about and never by
// guessing from the failure it is trying to explain. If the probe itself
// cannot be performed the step FAILS rather than skipping — an unprobeable
// target is a broken run, not an unmet precondition.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Precondition names a deployment property a step needs in order for its
// assertions to mean anything. The set is closed: an unrecognised name is a
// LOAD error, so a typo can never silently disable a challenge.
const (
	// PreconditionKeyedAuth requires the target to have been started with at
	// least one API key configured (HELIX_AUTH_API_KEYS), so that an
	// unauthenticated or malformed Bearer token is answered 401. Probed by
	// presenting a token the target cannot possibly have configured.
	PreconditionKeyedAuth = "keyed_auth"
)

// preconditionHelp maps each precondition to the operator-facing sentence
// naming exactly what is missing and how to provide it. It is the whole
// point of a declared precondition: a skip that does not say what would make
// the challenge run is barely better than a silent one.
var preconditionHelp = map[string]string{
	PreconditionKeyedAuth: "the target is running in open-access mode: no API keys are " +
		"configured, so the auth middleware admits every request by design " +
		"(internal/gateway/middleware/auth.go) and no request can be answered 401. " +
		"Start the target with HELIX_AUTH_API_KEYS=<key> (see .env.example) and " +
		"re-run to execute this challenge. Covered unconditionally on every " +
		"`go test ./internal/testing/` run by TestKeyedAuthChallengesPassOnKeyedDeployment, " +
		"which stands up a real keyed gateway and runs these same assertions against it.",
}

// knownPrecondition reports whether name is in the closed set.
func knownPrecondition(name string) bool {
	_, ok := preconditionHelp[name]
	return ok
}

// knownPreconditions returns the closed set, sorted, for error messages.
func knownPreconditions() []string {
	out := make([]string, 0, len(preconditionHelp))
	for k := range preconditionHelp {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validatePreconditions rejects any unrecognised precondition at LOAD time.
func validatePreconditions(names []string) error {
	for i, n := range names {
		if !knownPrecondition(n) {
			return fmt.Errorf("requires[%d]: unknown precondition %q (known: %s)",
				i, n, strings.Join(knownPreconditions(), ", "))
		}
	}
	return nil
}

// preconditionOutcome is the result of probing one precondition.
type preconditionOutcome struct {
	satisfied bool
	// detail records what the probe actually observed, so both the skip
	// reason and a probe failure carry evidence rather than an assertion.
	detail string
	// err is non-nil when the probe could not be performed at all. That is a
	// FAILURE, never a skip: an unreachable target must not look like an
	// unmet precondition.
	err error
}

// preconditionProbes holds the per-Runner memoised probe results. Probing is
// a real network round trip, so it happens once per precondition per run.
type preconditionProbes struct {
	mu  sync.Mutex
	out map[string]preconditionOutcome
}

// checkPreconditions probes every precondition the step declares and returns
// the first unsatisfied one, or an error if a probe could not be performed.
func (r *Runner) checkPreconditions(ctx context.Context, names []string) (string, preconditionOutcome, bool) {
	for _, n := range names {
		got := r.probe(ctx, n)
		if got.err != nil || !got.satisfied {
			return n, got, false
		}
	}
	return "", preconditionOutcome{satisfied: true}, true
}

// probe evaluates one precondition against the live target, memoising the
// result for the rest of the run.
func (r *Runner) probe(ctx context.Context, name string) preconditionOutcome {
	r.probes.mu.Lock()
	defer r.probes.mu.Unlock()
	if r.probes.out == nil {
		r.probes.out = map[string]preconditionOutcome{}
	}
	if got, ok := r.probes.out[name]; ok {
		return got
	}

	var got preconditionOutcome
	switch name {
	case PreconditionKeyedAuth:
		got = r.probeKeyedAuth(ctx)
	default:
		// Unreachable: validatePreconditions rejects unknown names at load.
		got = preconditionOutcome{err: fmt.Errorf(
			"internal error: precondition %q has no probe", name)}
	}
	r.probes.out[name] = got
	return got
}

// probeKeyedAuthToken is a Bearer token no deployment can have configured.
// It is deliberately not a plausible key: the probe must be answered 401 by
// key checking, never accepted by coincidence.
const probeKeyedAuthToken = "helixllm-precondition-probe-token-that-is-never-configured"

// probeKeyedAuth presents an impossible Bearer token to an authenticated
// route and reads the answer.
//
//	401 -> keys ARE configured; the auth layer is live and rejecting.
//	any other status -> the token was admitted, so no keys are configured.
//
// GET /v1/models is used because it sits behind the same /v1 auth middleware
// as every other route, is read-only, and costs the target nothing.
func (r *Runner) probeKeyedAuth(ctx context.Context) preconditionOutcome {
	sample := r.doRequest(ctx, httpRequestSpec{
		Method:  http.MethodGet,
		Path:    "/v1/models",
		Headers: map[string]string{"Authorization": "Bearer " + probeKeyedAuthToken},
	})
	if sample.Err != nil {
		return preconditionOutcome{err: fmt.Errorf(
			"could not probe precondition %q: GET /v1/models: %w",
			PreconditionKeyedAuth, sample.Err)}
	}
	if sample.Status == http.StatusUnauthorized {
		return preconditionOutcome{
			satisfied: true,
			detail: fmt.Sprintf(
				"GET /v1/models with an unconfigured Bearer token returned %d",
				sample.Status),
		}
	}
	return preconditionOutcome{
		satisfied: false,
		detail: fmt.Sprintf(
			"GET /v1/models with an unconfigured Bearer token returned %d, not 401",
			sample.Status),
	}
}

// skipDetail renders the operator-facing reason a step was skipped: what was
// probed, what came back, and what would make it run.
func skipDetail(name string, got preconditionOutcome) string {
	return fmt.Sprintf("precondition %q not satisfied: %s. %s",
		name, got.detail, preconditionHelp[name])
}
