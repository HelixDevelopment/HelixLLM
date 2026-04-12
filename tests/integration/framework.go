// Package integration provides a test framework for HelixLLM integration tests.
//
// TestServer creates an httptest.Server with the full HelixLLM route tree wired
// (Gateway, Knowledge, Agents, Control, Health), using in-memory components and
// mock providers so the tests run without external services.
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/control"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	srvmw "github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

// TestServer wraps an httptest.Server with the full HelixLLM route tree.
type TestServer struct {
	Server   *httptest.Server
	Brain    *brain.Brain
	Pipeline *knowledge.Pipeline
}

// NewTestServer creates an httptest.Server with all HelixLLM routes wired.
// It uses a mock provider so Brain calls succeed without external services.
func NewTestServer() *TestServer {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(srvmw.RequestID())

	// Health endpoint.
	checker := health.NewChecker()
	engine.GET("/internal/health", func(c *gin.Context) {
		report := checker.Check(c.Request.Context())
		status := http.StatusOK
		if report.Status != health.StatusHealthy {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, report)
	})

	// Brain with a mock provider that returns canned responses.
	b := brain.New(brain.Config{DefaultProvider: "mock"})
	b.RegisterProvider("mock", &mockIntegrationProvider{})

	// Knowledge embedder for gateway /v1/embeddings.
	embedderGW := knowledge.NewHashEmbedder(768)

	// Gateway routes (OpenAI + Anthropic).
	gateway.RegisterRoutes(engine, gateway.RouterOptions{
		Brain:    b,
		Embedder: embedderGW,
	})

	// Knowledge pipeline with in-memory components.
	embedder := knowledge.NewHashEmbedder(768)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(500, 50)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	knowledge.RegisterKnowledgeRoutes(engine, pipeline)

	// Agent routes.
	toolReg := agents.NewToolRegistry()
	toolReg.Register(&tools.EchoTool{})
	toolReg.Register(&tools.TimeTool{})
	toolReg.Register(tools.NewKnowledgeQueryTool(pipeline, "default"))

	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    toolReg,
		RAGHook:  knowledge.RAGHook(pipeline, "default"),
		MaxTurns: 5,
	})
	convCtx := agents.NewConversationContext(100)
	agents.RegisterAgentRoutes(engine, agent, convCtx)

	// Control routes.
	cp := control.NewControlPlane(control.ControlPlaneOptions{
		Strategy: "auto",
	})
	control.RegisterRoutes(engine, cp)

	srv := httptest.NewServer(engine)

	return &TestServer{
		Server:   srv,
		Brain:    b,
		Pipeline: pipeline,
	}
}

// Close shuts down the test server.
func (ts *TestServer) Close() {
	ts.Server.Close()
}

// URL returns the base URL for requests.
func (ts *TestServer) URL() string {
	return ts.Server.URL
}

// PostJSON sends a POST request with JSON body and returns the response.
func (ts *TestServer) PostJSON(path string, body interface{}) (*http.Response, error) {
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", ts.URL()+path, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

// Get sends a GET request and returns the response.
func (ts *TestServer) Get(path string) (*http.Response, error) {
	return http.Get(ts.URL() + path)
}

// ReadJSON reads the response body into the target struct.
func ReadJSON(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

// ReadBody reads the full response body as a string.
func ReadBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
