package gateway

import (
	"net/http"

	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
)

// completerErrorStatus maps a Completer failure onto the HTTP status that
// tells the truth about it.
//
// An unservable request means the service cannot answer it RIGHT NOW — either
// every configured provider was skipped, unavailable, or failed (an exhausted
// chain), or the SPECIFIC model the request pinned has no serving provider up.
// Both are availability conditions, which is 503 Service Unavailable
// (RFC 9110 §15.6.4). Clients, load
// balancers, and readiness probes all read 503 as "retry with backoff" and
// 500 as "this build is broken"; reporting a warming-up or unreachable
// backend as 500 tells every one of them the wrong thing.
//
// Anything else — a provider that WAS reached and returned a fault — stays
// 500, because that genuinely is an internal failure rather than an
// availability condition. Collapsing the two is what this function exists to
// prevent.
func completerErrorStatus(err error) int {
	if fallback.IsUnservable(err) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
