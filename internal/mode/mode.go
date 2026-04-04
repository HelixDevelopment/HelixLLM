package mode

import (
	"fmt"
	"strings"
)

// Mode represents an operating mode for the HelixLLM binary.
type Mode int

const (
	Full      Mode = iota + 1 // All-in-one, single process
	Gateway                   // API surface
	Brain                     // LLM coordination
	Knowledge                 // RAG pipeline
	Agents                    // Multi-agent system
	Control                   // Cluster management
)

var (
	modeNames = map[Mode]string{
		Full:      "full",
		Gateway:   "gateway",
		Brain:     "brain",
		Knowledge: "knowledge",
		Agents:    "agents",
		Control:   "control",
	}
	nameModes = map[string]Mode{}
)

func init() {
	for m, name := range modeNames {
		nameModes[name] = m
	}
}

func (m Mode) String() string {
	if name, ok := modeNames[m]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", int(m))
}

// Parse converts a string to a Mode. Case-insensitive.
func Parse(s string) (Mode, error) {
	if m, ok := nameModes[strings.ToLower(strings.TrimSpace(s))]; ok {
		return m, nil
	}
	return 0, fmt.Errorf("unknown mode: %q (valid: full, gateway, brain, knowledge, agents, control)", s)
}

// All returns all valid modes.
func All() []Mode {
	return []Mode{Full, Gateway, Brain, Knowledge, Agents, Control}
}
