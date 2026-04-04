package api

// MessageRequest matches Anthropic's /v1/messages request.
type MessageRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	System        string             `json:"system,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    interface{}        `json:"tool_choice,omitempty"`
}

type AnthropicMessage struct {
	Role    string      `json:"role"`    // user, assistant
	Content interface{} `json:"content"` // string or []ContentBlock
}

type ContentBlock struct {
	Type      string      `json:"type"` // text, image, tool_use, tool_result
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   interface{} `json:"content,omitempty"` // for tool_result
}

type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

// MessageResponse matches Anthropic's response.
type MessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // message
	Role         string         `json:"role"` // assistant
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason"`   // end_turn, max_tokens, stop_sequence, tool_use
	StopSequence *string        `json:"stop_sequence"` //nolint:tagliatelle
	Usage        AnthropicUsage `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// MessageStreamEvent matches Anthropic's streaming SSE events.
type MessageStreamEvent struct {
	Type         string           `json:"type"` // message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop
	Message      *MessageResponse `json:"message,omitempty"`
	Index        *int             `json:"index,omitempty"`
	ContentBlock *ContentBlock    `json:"content_block,omitempty"`
	Delta        *StreamDelta     `json:"delta,omitempty"`
	Usage        *AnthropicUsage  `json:"usage,omitempty"`
}

type StreamDelta struct {
	Type       string  `json:"type,omitempty"` // text_delta, input_json_delta
	Text       string  `json:"text,omitempty"`
	StopReason *string `json:"stop_reason,omitempty"`
}
