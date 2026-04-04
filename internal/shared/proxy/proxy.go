// Package proxy provides outbound HTTP proxy configuration for HelixLLM.
//
// Apply sets the standard proxy environment variables that the Go net/http
// stack reads automatically, so all HTTP clients in the process honour the
// configured proxy without further changes.
package proxy

import (
	"net/http"
	"os"
)

// Config holds outbound proxy settings.  Fields map to well-known proxy
// environment variables; the HELIX_* names allow HelixLLM-specific overrides
// that are applied via Apply without polluting the host environment until the
// operator explicitly calls Apply.
type Config struct {
	HTTPProxy  string `env:"HELIX_HTTP_PROXY"`
	HTTPSProxy string `env:"HELIX_HTTPS_PROXY"`
	NoProxy    string `env:"HELIX_NO_PROXY"`
	SOCKSProxy string `env:"HELIX_SOCKS_PROXY"`
}

// Apply sets the standard proxy environment variables (HTTP_PROXY,
// HTTPS_PROXY, NO_PROXY) so that all net/http clients in the process use the
// configured proxy.  Only non-empty fields are applied; existing environment
// variables are not cleared.
func Apply(cfg Config) {
	if cfg.HTTPProxy != "" {
		os.Setenv("HTTP_PROXY", cfg.HTTPProxy)
	}
	if cfg.HTTPSProxy != "" {
		os.Setenv("HTTPS_PROXY", cfg.HTTPSProxy)
	}
	if cfg.NoProxy != "" {
		os.Setenv("NO_PROXY", cfg.NoProxy)
	}
}

// Transport returns an *http.Transport configured to respect the process-level
// proxy environment variables (as set by Apply or by the host environment).
// Callers that need a custom Dialer or TLS config should build on top of the
// returned transport rather than using http.DefaultTransport directly.
func Transport(_ Config) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
}
