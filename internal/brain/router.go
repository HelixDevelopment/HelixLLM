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
//  2. Exact model match against registered providers' model lists (if available)
//  3. Prefix-match rules built dynamically from provider models (if available)
//  4. Fallback provider (if available)
//  5. Any available provider
//  6. Error
type Router struct {
	providers map[string]Provider
	rules     []RoutingRule
	fallback  string
}

// NewRouter creates a Router with the given fallback provider name. Routing
// rules are built dynamically from registered providers' model lists — there
// are no hardcoded prefix mappings.
func NewRouter(fallback string) *Router {
	return &Router{
		providers: make(map[string]Provider),
		fallback:  fallback,
	}
}

// Register adds a provider under the given name. Subsequent calls with the
// same name overwrite the previous registration. After every registration the
// routing rules are rebuilt from all providers' model lists.
func (r *Router) Register(name string, p Provider) {
	r.providers[name] = p
	r.rebuildRules()
}

// rebuildRules derives prefix-based routing rules from the model IDs reported
// by every registered provider. For each model, the longest dash-delimited
// prefix (e.g. "gpt-" from "gpt-4o", "deepseek-" from "deepseek-chat") is
// used as a routing prefix. Exact-match routing is handled directly in Route
// without rules.
func (r *Router) rebuildRules() {
	seen := make(map[string]string) // prefix → provider name (first wins)
	var rules []RoutingRule

	for name, p := range r.providers {
		for _, model := range p.Models() {
			// Build a prefix from the model ID. Use everything up to and
			// including the first '-'. For IDs without a dash the whole
			// string is used as a prefix so exact matches still work.
			prefix := model
			if idx := strings.Index(model, "-"); idx >= 0 {
				prefix = model[:idx+1]
			}
			if _, ok := seen[prefix]; !ok {
				seen[prefix] = name
				rules = append(rules, RoutingRule{
					Prefix:   prefix,
					Provider: name,
				})
			}
		}
	}

	r.rules = rules
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

	// 2. Exact model match — check every provider's model list.
	for _, p := range r.providers {
		for _, m := range p.Models() {
			if m == req.Model && p.Available() {
				return p, nil
			}
		}
	}

	// 3. Prefix-match rules (built dynamically from provider models).
	for _, rule := range r.rules {
		if strings.HasPrefix(req.Model, rule.Prefix) {
			if p, ok := r.providers[rule.Provider]; ok && p.Available() {
				return p, nil
			}
		}
	}

	// 4. Fallback provider.
	if r.fallback != "" {
		if p, ok := r.providers[r.fallback]; ok && p.Available() {
			return p, nil
		}
	}

	// 5. Any available provider.
	for _, p := range r.providers {
		if p.Available() {
			return p, nil
		}
	}

	return nil, fmt.Errorf("router: no available provider for model %q", req.Model)
}
