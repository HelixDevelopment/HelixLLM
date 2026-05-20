package tools

import (
	"context"
	"fmt"
)

// EchoTool returns its input unchanged. Useful for testing the tool pipeline.
type EchoTool struct{}

// NewEchoTool creates a new EchoTool.
func NewEchoTool() *EchoTool {
	return &EchoTool{}
}

func (e *EchoTool) Name() string        { return "echo" }
func (e *EchoTool) Description() string { return tr(keyEchoDesc) }

func (e *EchoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"message": map[string]interface{}{
			"type":        "string",
			"description": tr(keyEchoParamMsg),
			"required":    true,
		},
	}
}

func (e *EchoTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("echo: arguments must not be nil")
	}
	msg, ok := args["message"]
	if !ok {
		return "", fmt.Errorf("echo: missing required parameter 'message'")
	}
	return fmt.Sprintf("%v", msg), nil
}
