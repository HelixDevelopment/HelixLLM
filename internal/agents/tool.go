package agents

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Tool defines the interface that all tools must implement. A tool has a name,
// description, parameter schema, and an Execute method that performs the action.
type Tool interface {
	// Name returns the unique identifier for this tool.
	Name() string

	// Description returns a human-readable description of what this tool does.
	// This is included in the system prompt so the LLM knows when to use it.
	Description() string

	// Parameters returns a JSON Schema-like map describing the tool's arguments.
	Parameters() map[string]interface{}

	// Execute runs the tool with the given arguments and returns a string result.
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolInfo is a read-only summary of a registered tool, returned by List().
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolRegistry holds registered tools and provides lookup and execution.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry. Returns an error if a tool with the
// same name is already registered.
func (r *ToolRegistry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = tool
	return nil
}

// Get returns a tool by name. The second return value is false if the tool is
// not found.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	return t, ok
}

// List returns a sorted list of ToolInfo summaries for all registered tools.
func (r *ToolRegistry) List() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]ToolInfo, 0, len(r.tools))
	for _, t := range r.tools {
		infos = append(infos, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}

// Execute looks up a tool by name and runs it with the given arguments.
// Returns an error if the tool is not found or if execution fails.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, args)
}
