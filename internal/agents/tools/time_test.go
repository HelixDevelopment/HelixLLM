package tools_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

func TestTimeToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.TimeTool)(nil)
}

func TestTimeToolName(t *testing.T) {
	tool := tools.NewTimeTool()
	if tool.Name() != "time" {
		t.Errorf("expected name 'time', got %s", tool.Name())
	}
}

func TestTimeToolDescription(t *testing.T) {
	tool := tools.NewTimeTool()
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestTimeToolParameters(t *testing.T) {
	tool := tools.NewTimeTool()
	params := tool.Parameters()
	// Time tool takes no required parameters (optional timezone).
	if params == nil {
		t.Fatal("expected non-nil parameters map")
	}
}

func TestTimeToolExecuteDefault(t *testing.T) {
	tool := tools.NewTimeTool()
	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time string")
	}
	// Verify it's a valid time by checking it contains the current year.
	year := time.Now().Format("2006")
	if !strings.Contains(result, year) {
		t.Errorf("expected result to contain year %s, got %s", year, result)
	}
}

func TestTimeToolExecuteWithTimezone(t *testing.T) {
	tool := tools.NewTimeTool()
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"timezone": "UTC",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, "UTC") {
		t.Errorf("expected UTC in result, got %s", result)
	}
}

func TestTimeToolExecuteInvalidTimezone(t *testing.T) {
	tool := tools.NewTimeTool()
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"timezone": "Not/A/Timezone",
	})
	if err == nil {
		t.Error("expected error for invalid timezone, got nil")
	}
}

func TestTimeToolExecuteNilArgs(t *testing.T) {
	tool := tools.NewTimeTool()
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute with nil args failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time string with nil args")
	}
}
