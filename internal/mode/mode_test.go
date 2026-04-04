package mode_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/mode"
)

func TestModeString(t *testing.T) {
	tests := []struct {
		m    mode.Mode
		want string
	}{
		{mode.Full, "full"},
		{mode.Gateway, "gateway"},
		{mode.Brain, "brain"},
		{mode.Knowledge, "knowledge"},
		{mode.Agents, "agents"},
		{mode.Control, "control"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("Mode.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		input   string
		want    mode.Mode
		wantErr bool
	}{
		{"full", mode.Full, false},
		{"gateway", mode.Gateway, false},
		{"brain", mode.Brain, false},
		{"knowledge", mode.Knowledge, false},
		{"agents", mode.Agents, false},
		{"control", mode.Control, false},
		{"FULL", mode.Full, false},
		{"Gateway", mode.Gateway, false},
		{"invalid", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := mode.Parse(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestModeAll(t *testing.T) {
	all := mode.All()
	if len(all) != 6 {
		t.Errorf("All() returned %d modes, want 6", len(all))
	}
}
