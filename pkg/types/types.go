package types

// Role represents a chat message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Provider represents an LLM provider.
type Provider string

const (
	ProviderLocal     Provider = "local"
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// InternalMessage is the canonical message format within HelixLLM.
type InternalMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// InternalChatRequest is the canonical chat request format.
type InternalChatRequest struct {
	Model       string            `json:"model"`
	Messages    []InternalMessage `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Provider    Provider          `json:"provider,omitempty"`
}

// InternalChatResponse is the canonical chat response format.
type InternalChatResponse struct {
	ID           string          `json:"id"`
	Model        string          `json:"model"`
	Message      InternalMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
	Usage        InternalUsage   `json:"usage"`
	Provider     Provider        `json:"provider"`
}

type InternalUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a single streaming token.
type StreamChunk struct {
	Content      string `json:"content"`
	FinishReason string `json:"finish_reason,omitempty"`
}
