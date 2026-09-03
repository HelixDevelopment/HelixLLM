package api

// ChatCompletionRequest matches OpenAI's /v1/chat/completions request.
type ChatCompletionRequest struct {
	Model            string      `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Temperature      *float64    `json:"temperature,omitempty"`
	TopP             *float64    `json:"top_p,omitempty"`
	N                *int        `json:"n,omitempty"`
	Stream           bool        `json:"stream,omitempty"`
	Stop             interface{} `json:"stop,omitempty"` // string or []string
	MaxTokens        *int        `json:"max_tokens,omitempty"`
	PresencePenalty  *float64    `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64    `json:"frequency_penalty,omitempty"`
	User             string      `json:"user,omitempty"`
	Tools            []Tool      `json:"tools,omitempty"`
	ToolChoice       interface{} `json:"tool_choice,omitempty"`
}

type ChatMessage struct {
	Role       string      `json:"role"`    // system, user, assistant, tool
	Content    interface{} `json:"content"` // string or []ContentPart
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

type ContentPart struct {
	Type     string    `json:"type"` // text, image_url
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"` // function
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"` // JSON Schema
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // function
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse matches OpenAI's response.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"` // chat.completion
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *Usage                 `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"` // stop, length, tool_calls
}

// ChatCompletionChunk is a streaming response chunk.
type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"` // chat.completion.chunk
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
}

type ChatCompletionChunkChoice struct {
	Index        int              `json:"index"`
	Delta        ChatMessageDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason"` // null until done
}

type ChatMessageDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Model matches OpenAI's model object.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // model
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
	// ModelIdentity is the human-readable helixllm/<host>/<model>[:<variant>]
	// value for a locally-served model. It is a VALUE, never an identifier: ID
	// carries the derived, charset-safe identifier a consumer can actually use.
	//
	// Omitted for remote vendor models, deliberately. Stamping the helixllm
	// identity on a vendor's model would destroy the one distinction the identity
	// exists to draw.
	ModelIdentity string `json:"model_identity,omitempty"`
	// Host is the machine serving this model. Omitted for a remote provider's
	// model, which has no HelixLLM host — inventing one would be a fabricated
	// finding, not a convenience.
	Host string `json:"host,omitempty"`
	// Availability is the serving layer's affirmative report: [AvailabilityServing]
	// for a model being served now, [AvailabilityWithheld] for one that exists
	// but is not. An EMPTY value means the serving layer reported nothing, which
	// a consumer must not read as "serving" — "it said yes" and "it said nothing"
	// are different claims, and only the first is a basis for routing a request.
	Availability string `json:"availability,omitempty"`
	// WithheldReason names why a model is not served, as one of the recorded
	// machine keys below. They are kept distinct because their remedies differ;
	// collapsing them destroys what the user needs to act on.
	WithheldReason string `json:"withheld_reason,omitempty"`
}

// Availability values. A consumer admits ONLY these two and treats anything
// else — including the empty string — as "not reported", which is never usable.
const (
	// AvailabilityServing means the model is being served right now. It is the
	// only value that licenses routing a request to the model.
	AvailabilityServing = "serving"
	// AvailabilityWithheld means the model exists but is not being served.
	// WithheldReason says why.
	AvailabilityWithheld = "withheld"
)

// The closed set of withheld reasons this wire carries.
//
// # Why there are five and not three
//
// The first three describe why a host CANNOT RUN a model — the answer is about
// the model's demands against the machine, and each implies a different remedy:
// a different host or a smaller option; a different approach entirely; a
// different model or a different declared usage.
//
// The last two describe why a model that could otherwise run IS NOT BEING
// SERVED — the answer is about the serving layer's own state. They were added
// when the serving layer began publishing withheld options at all, and they are
// NOT reducible to the first three. `provider_unavailable` in particular is the
// distinction the whole field exists to draw for a consuming tool: a backend
// that is loading or restarting is coming BACK, and mapping it onto
// `unsupported_configuration` ("nothing about this host's configuration can run
// it; more memory does not help") would state the opposite of the truth and
// destroy the only signal that separates "wait" from "gone".
//
// Each of the five has a producer. A key nothing writes is the defect this set
// was extended to fix, so one is not added speculatively — in particular there
// is no key for "this model's name cannot form a valid identity", because such
// an option has no id it could safely be published UNDER; see modelsFromOptions.
//
// # Both ends of the wire
//
// This set is the contract, and a consumer validates against it: a reason
// outside it is discarded, because a consumer acting on a reason the contract
// cannot give a remedy for is acting on nothing. Adding a value therefore
// requires the consumer's validator to admit it IN THE SAME CHANGE, or the
// producer emits something the consumer silently drops.
//
// Note the underscore spelling. The serving layer's own internal reason tokens
// are hyphenated and belong to a DIFFERENT vocabulary for a DIFFERENT field;
// they are translated onto these keys at the wire boundary rather than emitted
// raw, because a hyphenated value here would be discarded by a consumer as
// unrecognised while looking entirely deliberate.
const (
	// WithheldInsufficientResources: the host lacks the memory or storage the
	// model needs. A different host, or a smaller model, resolves it.
	WithheldInsufficientResources = "insufficient_resources"
	// WithheldUnsupportedConfiguration: nothing about this host's configuration
	// can run the model. More memory does not help.
	WithheldUnsupportedConfiguration = "unsupported_configuration"
	// WithheldExcludedByUsageTerms: the host could serve it, but the model's
	// terms forbid the declared usage. The remedy is never hardware.
	WithheldExcludedByUsageTerms = "excluded_by_usage_terms"

	// WithheldProviderUnavailable: the provider offering this model is not
	// serving right now — starting, loading, restarting, or unreachable. The
	// model is expected back; nothing about it has been withdrawn.
	WithheldProviderUnavailable = "provider_unavailable"
	// WithheldIdentifierConflict: the identifier derived for this model is
	// already bound to a DIFFERENT identity. Astronomically unlikely, but a
	// collision must surface as a withheld option rather than as a model that
	// silently replaced another one.
	WithheldIdentifierConflict = "identifier_conflict"
)

type ModelList struct {
	Object string  `json:"object"` // list
	Data   []Model `json:"data"`
	// Reason states why Data is empty when the server has nothing to list.
	//
	// An empty list with no explanation is indistinguishable from a broken
	// server: the client cannot tell "no backend is configured" from "the
	// request went wrong". This field carries that distinction. It is set ONLY
	// alongside an empty Data and is omitted otherwise — a reason next to a
	// populated list would state a withholding that did not happen.
	Reason string `json:"reason,omitempty"`
}

// CompletionRequest matches OpenAI's /v1/completions.
type CompletionRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
}

type CompletionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"` // text_completion
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []CompletionChoice `json:"choices"`
	Usage   *Usage             `json:"usage,omitempty"`
}

type CompletionChoice struct {
	Text         string `json:"text"`
	Index        int    `json:"index"`
	FinishReason string `json:"finish_reason"`
}

// EmbeddingRequest matches OpenAI's /v1/embeddings.
type EmbeddingRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string
}

type EmbeddingResponse struct {
	Object string          `json:"object"` // list
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage,omitempty"`

	// SemanticEmbeddings is a HelixLLM extension field (not part of the
	// OpenAI schema; standard OpenAI clients tolerate unknown JSON
	// fields). It reports whether Data was produced by a real semantic
	// embedding provider (true) or the deterministic, non-semantic
	// HashEmbedder fallback / zero-vector fallback (false) — HXC-235: the
	// caller must be able to distinguish real embeddings from hash
	// fallback at the point of use (this response), not only from a
	// server-side startup log line.
	SemanticEmbeddings bool `json:"semantic_embeddings"`
}

type EmbeddingData struct {
	Object    string    `json:"object"` // embedding
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// ErrorResponse matches OpenAI's error format.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}
