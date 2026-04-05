# Lesson 3: Custom Tools

**Duration:** 30 minutes
**Prerequisites:** Lesson 2 (Built-in Tools)
**Learning Objectives:**
- Implement the Tool interface to create custom agent tools
- Define parameter schemas for structured input validation
- Register custom tools in the ToolRegistry
- Write tests for custom tools using httptest and the Gin test mode

---

## Scene 1: The Tool Interface (5 min)

**Narration:** "Every agent tool in HelixLLM implements a four-method interface. Let me walk through each method and its role."

**Screen:** Show the Tool interface from `internal/agents/tool.go`.

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (string, error)
}
```

**Narration:** "Name returns a unique identifier that the LLM uses to invoke the tool. Description explains when and why the LLM should use it -- this text is injected into the system prompt, so clarity matters. Parameters returns a JSON Schema-like map describing the expected arguments. Execute runs the actual tool logic and returns a string result."

**Key points:**
- `Name()` -- unique tool identifier, used by the LLM in tool calls
- `Description()` -- human-readable text included in the system prompt
- `Parameters()` -- JSON Schema-like definition of accepted arguments
- `Execute()` -- receives a context and argument map, returns a string result
- Return an error from Execute to signal a tool failure to the agent

---

## Scene 2: Building a Weather Tool (8 min)

**Narration:** "Let me build a complete custom tool step by step. We will create a weather tool that returns simulated weather data for a city."

**Screen:** Create the file in an editor.

```go
// internal/agents/tools/weather.go
package tools

import (
    "context"
    "fmt"
    "strings"
)

// WeatherTool returns simulated weather data for a given city.
type WeatherTool struct{}

func (t *WeatherTool) Name() string {
    return "weather"
}

func (t *WeatherTool) Description() string {
    return "Get the current weather conditions for a city. Returns temperature, conditions, and wind speed."
}

func (t *WeatherTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "city": map[string]interface{}{
            "type":        "string",
            "description": "The city name (e.g., 'Tokyo', 'Berlin', 'New York')",
        },
        "unit": map[string]interface{}{
            "type":        "string",
            "description": "Temperature unit: 'celsius' or 'fahrenheit'",
        },
    }
}

func (t *WeatherTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    city, ok := args["city"].(string)
    if !ok || city == "" {
        return "", fmt.Errorf("city parameter is required")
    }

    unit, _ := args["unit"].(string)
    if unit == "" {
        unit = "celsius"
    }

    // In a real tool, this would call a weather API.
    // For demonstration, return simulated data.
    temp := 22
    if strings.EqualFold(unit, "fahrenheit") {
        temp = 72
    }

    return fmt.Sprintf("Weather in %s: %d%s, partly cloudy, wind 15 km/h",
        city, temp, unitSymbol(unit)), nil
}

func unitSymbol(unit string) string {
    if strings.EqualFold(unit, "fahrenheit") {
        return "F"
    }
    return "C"
}
```

**Narration:** "The Parameters method defines two fields: city as a required string and unit as an optional string. The Execute method validates the input, performs the tool logic, and returns a formatted string. In a production tool, the Execute method would call an external API."

**Key points:**
- Always validate required parameters in Execute
- Return a descriptive error when validation fails
- The returned string is what the LLM sees as the tool observation
- Format the result as natural language for the LLM to interpret
- Use the context for timeouts and cancellation

---

## Scene 3: Registering the Tool (5 min)

**Narration:** "After implementing the tool, register it in the ToolRegistry so the agent can use it."

**Screen:** Show the registration code in `cmd/helixllm/main.go`.

```go
// In cmd/helixllm/main.go, in the agent setup section:
toolReg := agents.NewToolRegistry()

// Built-in tools
toolReg.Register(&tools.EchoTool{})
toolReg.Register(&tools.TimeTool{})
toolReg.Register(tools.NewKnowledgeQueryTool(pipeline, "default"))

// Custom tools
toolReg.Register(&tools.WeatherTool{})  // Add your tool here
```

**Narration:** "The ToolRegistry holds all registered tools. When the agent starts, it reads the tool names, descriptions, and parameters from the registry and includes them in the LLM system prompt. The LLM then knows about your tool and can decide when to call it."

**Demo steps:**

```bash
# Rebuild with the new tool
make build

# Start the server
make dev

# Verify the tool appears in the listing
curl -sk https://localhost:8443/v1/agents/tools | python3 -m json.tool
```

**Expected output (showing the new tool):**

```json
{
  "tools": [
    {"name": "echo", "description": "Echoes back the input message", "parameters": {"message": {"type": "string", "description": "Message to echo"}}},
    {"name": "time", "description": "Returns the current UTC time", "parameters": {}},
    {"name": "knowledge_query", "description": "Query the knowledge base for relevant documents", "parameters": {"query": {"type": "string", "description": "Search query"}}},
    {"name": "weather", "description": "Get the current weather conditions for a city. Returns temperature, conditions, and wind speed.", "parameters": {"city": {"type": "string", "description": "The city name (e.g., 'Tokyo', 'Berlin', 'New York')"}, "unit": {"type": "string", "description": "Temperature unit: 'celsius' or 'fahrenheit'"}}}
  ]
}
```

```bash
# Test the tool through the agent
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What is the weather in Tokyo?"}
    ]
  }' | python3 -m json.tool
```

**Key points:**
- Register tools before the agent starts handling requests
- The tool description quality directly affects the LLM's ability to use it
- Rebuild the binary after adding new tools: `make build`
- Verify registration with `GET /v1/agents/tools`

---

## Scene 4: Testing Custom Tools (7 min)

**Narration:** "Every tool needs tests. Let me show you how to test both the tool in isolation and through the full agent HTTP pipeline."

**Screen:** Show the test file.

```go
// internal/agents/tools/weather_test.go
package tools

import (
    "context"
    "strings"
    "testing"
)

func TestWeatherTool_Name(t *testing.T) {
    tool := &WeatherTool{}
    if tool.Name() != "weather" {
        t.Errorf("expected name 'weather', got '%s'", tool.Name())
    }
}

func TestWeatherTool_Parameters(t *testing.T) {
    tool := &WeatherTool{}
    params := tool.Parameters()
    if _, ok := params["city"]; !ok {
        t.Error("expected 'city' parameter")
    }
}

func TestWeatherTool_Execute(t *testing.T) {
    tool := &WeatherTool{}

    tests := []struct {
        name    string
        args    map[string]interface{}
        want    string
        wantErr bool
    }{
        {
            name: "valid city celsius",
            args: map[string]interface{}{"city": "Tokyo", "unit": "celsius"},
            want: "Tokyo",
        },
        {
            name: "valid city fahrenheit",
            args: map[string]interface{}{"city": "New York", "unit": "fahrenheit"},
            want: "72F",
        },
        {
            name: "default unit",
            args: map[string]interface{}{"city": "Berlin"},
            want: "22C",
        },
        {
            name:    "missing city",
            args:    map[string]interface{}{},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := tool.Execute(context.Background(), tt.args)
            if (err != nil) != tt.wantErr {
                t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && !strings.Contains(result, tt.want) {
                t.Errorf("Execute() result = %q, want substring %q", result, tt.want)
            }
        })
    }
}
```

**Narration:** "This test file uses table-driven tests following Go conventions. It verifies the tool name, parameter schema, and execution with various inputs including error cases."

**Demo steps:**

```bash
# Run the tool tests
go test -v -run TestWeatherTool ./internal/agents/tools/
```

**Narration:** "For integration testing through the HTTP layer, use httptest with the full Gin route tree."

```go
// In an integration test file
func TestWeatherToolViaAgent(t *testing.T) {
    gin.SetMode(gin.TestMode)
    r := gin.New()

    // Set up the agent with the weather tool
    toolReg := agents.NewToolRegistry()
    toolReg.Register(&tools.WeatherTool{})
    agents.RegisterAgentRoutes(r, agents.Options{
        ToolRegistry: toolReg,
        // ... other options
    })

    req := httptest.NewRequest(http.MethodGet, "/v1/agents/tools", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
    // Verify weather tool appears in the response
}
```

**Key points:**
- Test tool methods independently (Name, Parameters, Execute)
- Use table-driven tests for multiple Execute scenarios
- Test error cases: missing required params, invalid types, empty strings
- Integration tests use httptest with the full Gin route tree
- Run with `go test -v ./internal/agents/tools/`

---

## Scene 5: Best Practices (5 min)

**Narration:** "Let me share some best practices for building production-quality tools."

**Screen:** Show the best practices list.

1. **Clear descriptions** -- The LLM reads your Description() to decide when to use the tool. Be specific about what the tool does and when it should be used.

2. **Validate all inputs** -- Never trust the arguments map. The LLM may send wrong types or missing fields.

3. **Return structured text** -- Format results as readable text, not raw JSON. The LLM will interpret and reformat it for the user.

4. **Handle timeouts** -- Use the context for deadline-aware operations. External API calls should respect cancellation.

5. **Idempotent execution** -- Tools may be called multiple times in the ReAct loop. Avoid side effects that cannot be safely repeated.

```go
// Good: timeout-aware execution
func (t *WeatherTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    // External API call respects context cancellation
    resp, err := httpClient.Do(req.WithContext(ctx))
    if err != nil {
        return "", fmt.Errorf("weather API unavailable: %w", err)
    }
    // ...
}
```

**Key points:**
- Write descriptions as if explaining the tool to a new team member
- Always validate and provide default values for optional parameters
- Use context.WithTimeout for external calls
- Return human-readable strings, not JSON
- Test edge cases: empty strings, wrong types, nil arguments

---

## Exercises

1. Implement a `calculator` tool that accepts `expression` as a string (e.g., "2 + 3") and returns the result, then register it and test through the agent
2. Write a table-driven test suite for your calculator tool covering valid expressions, division by zero, and malformed input
3. Create a tool that takes a URL parameter and returns a simulated HTTP status check, with proper context timeout handling
