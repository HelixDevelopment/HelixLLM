package types_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestRoleConstants(t *testing.T) {
	if types.RoleSystem != "system" {
		t.Error("RoleSystem wrong")
	}
	if types.RoleUser != "user" {
		t.Error("RoleUser wrong")
	}
	if types.RoleAssistant != "assistant" {
		t.Error("RoleAssistant wrong")
	}
	if types.RoleTool != "tool" {
		t.Error("RoleTool wrong")
	}
}

func TestProviderConstants(t *testing.T) {
	if types.ProviderLocal != "local" {
		t.Error("ProviderLocal wrong")
	}
	if types.ProviderOpenAI != "openai" {
		t.Error("ProviderOpenAI wrong")
	}
	if types.ProviderAnthropic != "anthropic" {
		t.Error("ProviderAnthropic wrong")
	}
}

func TestInternalChatRequestDefaults(t *testing.T) {
	req := types.InternalChatRequest{
		Model: "llama-3.1-70b",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "test"},
		},
	}
	if req.Model != "llama-3.1-70b" {
		t.Error("Model not set")
	}
	if len(req.Messages) != 1 {
		t.Error("Messages not set")
	}
}
