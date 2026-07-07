package a2a

import "fmt"

// CardConfig carries the config-injected values (CONST-045/046 — no
// hardcoded host/port/model literal) used to compose the Agent Card.
type CardConfig struct {
	// PublicURL is the externally-reachable base URL of this A2A server,
	// e.g. "http://localhost:18441" (config-injected, never hardcoded here).
	PublicURL string
	// DownstreamModelID is the model id the live coder actually reports on
	// its own /v1/models — sourced dynamically at server startup
	// (downstream.go), never a hardcoded literal (CONST-036/040 spirit: the
	// skill names the REAL served model, not a guessed one).
	DownstreamModelID string
	// BearerConfigured reports whether Bearer auth is enforced, so the
	// Agent Card's security declaration matches server behaviour exactly
	// (an Agent Card MUST NOT claim auth it does not enforce).
	BearerConfigured bool
}

// BuildAgentCard composes the discovery document served at
// /.well-known/agent-card.json (spec §4.4.1 / v0.3.0 binding).
func BuildAgentCard(cfg CardConfig) AgentCard {
	card := AgentCard{
		Name: "helixllm-coder-a2a",
		Description: "HelixLLM CPU/GPU coder fleet exposed as a Google A2A " +
			"(Agent2Agent) peer — accepts a code-generation Task and returns " +
			"a completed Task whose Artifact carries the real model output.",
		Version:            "0.1.0",
		URL:                cfg.PublicURL,
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities: AgentCapabilities{
			// Honest scope (§11.4.6 / RESULTS.md): streaming (message/stream),
			// push notifications, and the extended-card flow are NOT
			// implemented by this proof — the card declares that truthfully
			// rather than advertising a capability that would fail on use.
			Streaming:         false,
			PushNotifications: false,
			ExtendedAgentCard: false,
		},
		Skills: []AgentSkill{
			{
				ID:   "generate-code",
				Name: "Generate code",
				Description: fmt.Sprintf(
					"Accepts a natural-language coding request and returns "+
						"generated source via the live model %q.",
					cfg.DownstreamModelID),
				Tags: []string{"code-generation", "coder"},
				Examples: []string{
					"Write a Go function that returns the nth Fibonacci number.",
				},
			},
		},
		PreferredTransport: "JSONRPC",
	}
	if cfg.BearerConfigured {
		card.SecuritySchemes = map[string]SecurityScheme{
			"bearer": {Type: "http", Scheme: "bearer"},
		}
		card.Security = []map[string][]string{{"bearer": {}}}
	}
	return card
}
