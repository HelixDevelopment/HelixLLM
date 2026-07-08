package mcpgateway

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolGenerateInput is the JSON-Schema-inferred input for helixllm_generate.
type ToolGenerateInput struct {
	Prompt    string `json:"prompt" jsonschema:"the user prompt to send to the live HelixLLM coder"`
	MaxTokens int    `json:"max_tokens,omitempty" jsonschema:"maximum tokens to generate (default 256)"`
}

// ToolGenerateOutput is the structured output for helixllm_generate.
type ToolGenerateOutput struct {
	ModelID string `json:"model_id"`
	Content string `json:"content"`
}

// ToolListModelsInput is the (empty) input for helixllm_list_models.
type ToolListModelsInput struct{}

// ToolListModelsOutput is the structured output for helixllm_list_models.
type ToolListModelsOutput struct {
	Models []string `json:"models"`
}

// NewServer builds the MCP server exposing HelixLLM's local capabilities as
// MCP tools/resources, per the GO'd design memo. coder is REQUIRED (the
// generate/list-models tools proxy real HTTP calls to it); okf may be a
// store with an empty Root (disables OKF resources, never an error).
func NewServer(name, version string, coder *CoderClient, okf *OKFStore) *mcp.Server {
	if coder == nil {
		panic("mcpgateway.NewServer: coder client must not be nil")
	}
	if okf == nil {
		okf = NewOKFStore("")
	}

	s := mcp.NewServer(&mcp.Implementation{
		Name:    name,
		Version: version,
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "helixllm_generate",
		Description: "Generate a real chat completion from the live HelixLLM coder (OpenAI-compatible /v1/chat/completions). Never a simulated response.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ToolGenerateInput) (*mcp.CallToolResult, ToolGenerateOutput, error) {
		result, err := coder.Generate(ctx, in.Prompt, in.MaxTokens)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("helixllm_generate failed: %v", err)}},
			}, ToolGenerateOutput{}, nil
		}
		out := ToolGenerateOutput{ModelID: result.ModelID, Content: result.Content}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: result.Content}},
		}, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "helixllm_list_models",
		Description: "List the real models currently served by the live HelixLLM coder (OpenAI-compatible /v1/models). Never a hardcoded list (CONST-036).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ToolListModelsInput) (*mcp.CallToolResult, ToolListModelsOutput, error) {
		models, err := coder.ListModels(ctx)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("helixllm_list_models failed: %v", err)}},
			}, ToolListModelsOutput{}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%v", models)}},
		}, ToolListModelsOutput{Models: models}, nil
	})

	registerOKFResources(s, okf)

	return s
}

// registerOKFResources exposes every Markdown concept file under okf.Root as
// an MCP resource (read-only), per MCP_OKF_GATEWAY_MEMO.md §2.2 item 2: OKF
// is served through MCP's existing `resources` capability, never a new
// protocol surface. A store with an empty Root registers zero resources.
func registerOKFResources(s *mcp.Server, okf *OKFStore) {
	resources, err := okf.List()
	if err != nil || len(resources) == 0 {
		return
	}
	for _, r := range resources {
		res := r
		s.AddResource(&mcp.Resource{
			URI:      res.URI,
			Name:     res.Name,
			MIMEType: "text/markdown",
		}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			body, readErr := okf.Read(req.Params.URI)
			if readErr != nil {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{URI: req.Params.URI, MIMEType: "text/markdown", Text: body},
				},
			}, nil
		})
	}
}
