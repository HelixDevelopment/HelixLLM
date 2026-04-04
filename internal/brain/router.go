package brain

import (
	"fmt"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// RoutingRule maps a model-name prefix to a named provider.
type RoutingRule struct {
	Prefix   string // model name prefix, e.g. "gpt-", "claude-", "llama-"
	Provider string // provider name as registered with Register
}

// Router selects the right Provider for each InternalChatRequest.
//
// Selection priority:
//  1. req.Provider explicit override (if that provider is registered and available)
//  2. First RoutingRule whose Prefix matches the model name (if available)
//  3. Fallback provider (if available)
//  4. Any available provider
//  5. Error
type Router struct {
	providers map[string]Provider
	rules     []RoutingRule
	fallback  string
}

// defaultRules are the built-in prefix→provider mappings.
var defaultRules = []RoutingRule{
	{Prefix: "gpt-", Provider: "openai"},
	{Prefix: "claude-", Provider: "anthropic"},
	{Prefix: "llama-", Provider: "llamacpp"},
	{Prefix: "qwen", Provider: "llamacpp"},
}

// NewRouter creates a Router with the given fallback provider name and the
// default prefix routing rules.
func NewRouter(fallback string) *Router {
	return &Router{
		providers: make(map[string]Provider),
		rules:     append([]RoutingRule(nil), defaultRules...),
		fallback:  fallback,
	}
}

// Register adds a provider under the given name. Subsequent calls with the
// same name overwrite the previous registration.
func (r *Router) Register(name string, p Provider) {
	r.providers[name] = p
}

// Route selects a Provider for the given request following the priority rules
// described on Router.
func (r *Router) Route(req *types.InternalChatRequest) (Provider, error) {
	// 1. Explicit provider override.
	if req.Provider != "" {
		if p, ok := r.providers[string(req.Provider)]; ok && p.Available() {
			return p, nil
		}
	}

	// 2. Prefix-match rules.
	for _, rule := range r.rules {
		if strings.HasPrefix(req.Model, rule.Prefix) {
			if p, ok := r.providers[rule.Provider]; ok && p.Available() {
				return p, nil
			}
		}
	}

	// 3. Fallback provider.
	if r.fallback != "" {
		if p, ok := r.providers[r.fallback]; ok && p.Available() {
			return p, nil
		}
	}

	// 4. Any available provider.
	for _, p := range r.providers {
		if p.Available() {
			return p, nil
		}
	}

	return nil, fmt.Errorf("router: no available provider for model %q", req.Model)
}
