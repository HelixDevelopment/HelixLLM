// Package a2a implements a minimal, spec-grounded Google A2A (Agent2Agent)
// server surface for HelixLLM, per the approved design
// docs/research/07.2026/00_master/ACP_A2A_PROVIDER.md (operator decision
// 03_open_clarifications.md C3 — "Google A2A, NOT Zed ACP").
//
// Wire facts (Agent Card path, JSON-RPC method names, TaskState values, Part
// kinds) are cited to the LATEST fetched A2A spec (a2a-protocol.org /latest/
// + /v0.3.0/, accessed 2026-07-07 — §11.4.99) and are NOT taken from model
// memory (§11.4.6).
//
// §11.4.101 documented scope decision: the design (ACP_A2A_PROVIDER.md) frames
// the A2A server as a HelixAgent capability that routes to the local HelixLLM
// coder at :18434. This task explicitly assigns the implementation to the
// HelixLLM submodule instead, and the live downstream at :18434 is the
// llama.cpp-router "coder" container (plain HTTP, OpenAI-compatible
// /v1/chat/completions) — not the full mTLS cmd/helixllm binary. Building the
// A2A server as a standalone HelixLLM binary that fronts the coder is the
// minimal, safe choice that matches the actually-deployed topology without
// touching (rebuilding/restarting) the live coder container (§11.4.119 /
// §11.4.122). This is a documented, non-silent choice, not a guess.
package a2a

// AgentCard is the public discovery document served at
// /.well-known/agent-card.json (A2A spec §4.4.1 / v0.3.0 binding, cited
// 2026-07-07 — NOT the older /.well-known/agent.json path some pre-1.0
// drafts used).
type AgentCard struct {
	Name               string                    `json:"name"`
	Description        string                    `json:"description"`
	Version            string                    `json:"version"`
	URL                string                    `json:"url"`
	Capabilities       AgentCapabilities         `json:"capabilities"`
	DefaultInputModes  []string                  `json:"defaultInputModes"`
	DefaultOutputModes []string                  `json:"defaultOutputModes"`
	Skills             []AgentSkill              `json:"skills"`
	SecuritySchemes    map[string]SecurityScheme `json:"securitySchemes"`
	Security           []map[string][]string     `json:"security"`
	PreferredTransport string                    `json:"preferredTransport"`
}

// AgentCapabilities are the boolean feature flags cited in the spec.
type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
	ExtendedAgentCard bool `json:"extendedAgentCard"`
}

// AgentSkill advertises one thing the agent can do.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
}

// SecurityScheme declares one auth mechanism (v1.0 §4.5; HelixLLM's A2A
// server advertises HTTPAuthSecurityScheme/Bearer per the design §1.7, to
// match the existing gateway Bearer posture — API_CONTRACT.md §3).
type SecurityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme,omitempty"`
}

// ---- JSON-RPC 2.0 envelope (transport = JSON-RPC 2.0 over HTTP, spec §1.5) ----

// RPCRequest is a JSON-RPC 2.0 request. ID is decoded generically (string or
// number, per the JSON-RPC 2.0 spec) and re-emitted verbatim on the response
// so we never guess its shape (§11.4.6).
type RPCRequest struct {
	JSONRPC string       `json:"jsonrpc"`
	Method  string       `json:"method"`
	ID      any          `json:"id"`
	Params  RPCParamsRaw `json:"params"`
	// idPresent/jsonrpcPresent/methodPresent are computed by rawHasField
	// (json.go) against the original request bytes so the "missing field"
	// check does not depend on Go's zero-value ambiguity (an explicit
	// jsonrpc:"" is a different defect than an absent jsonrpc key).
}

// RPCParamsRaw defers params decoding to the method-specific handler.
type RPCParamsRaw = mapAny

type mapAny = map[string]any

// RPCResponse is a JSON-RPC 2.0 response envelope.
type RPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError follows the JSON-RPC 2.0 error object shape.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes (JSON-RPC 2.0 spec, not A2A-specific).
const (
	RPCParseError     = -32700
	RPCInvalidRequest = -32600
	RPCMethodNotFound = -32601
	RPCInvalidParams  = -32602
	RPCInternalError  = -32603
)

// ---- A2A domain objects (spec §1.2-1.4) ----

// TaskState string values, cited verbatim from the v0.3.0 JSON-RPC binding
// (accessed 2026-07-07).
const (
	TaskStateSubmitted     = "submitted"
	TaskStateWorking       = "working"
	TaskStateInputRequired = "input-required"
	TaskStateCompleted     = "completed"
	TaskStateCanceled      = "canceled"
	TaskStateFailed        = "failed"
	TaskStateRejected      = "rejected"
	TaskStateAuthRequired  = "auth-required"
	TaskStateUnknown       = "unknown"
)

// Part kinds, cited verbatim (accessed 2026-07-07).
const (
	PartKindText = "text"
	PartKindFile = "file"
	PartKindData = "data"
)

// Message roles, cited verbatim (accessed 2026-07-07).
const (
	RoleUser  = "user"
	RoleAgent = "agent"
)

// Part is a discriminated union over kind; only the fields relevant to this
// proof (text) are populated for other kinds — data/file are accepted on
// input (never crash on them) but not required by this scope (see RESULTS.md
// honest scope note: file/data Parts and gRPC/REST bindings are not exercised
// by this proof).
type Part struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

// Message carries a role + parts (spec §1.4).
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Artifact is a Task output composed of Parts (spec §1.4).
type Artifact struct {
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}

// TaskStatus wraps the current lifecycle state.
type TaskStatus struct {
	State     string `json:"state"`
	Timestamp string `json:"timestamp"`
}

// Task is the unit of work with server-assigned id + status (spec §1.3).
type Task struct {
	ID        string     `json:"id"`
	Status    TaskStatus `json:"status"`
	History   []Message  `json:"history,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Error     string     `json:"error,omitempty"`
}
