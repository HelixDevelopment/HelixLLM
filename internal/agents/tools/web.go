package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// WebSearchTool
// ---------------------------------------------------------------------------

// WebSearchTool is a placeholder for web search functionality. In local mode
// no search API key is available, so it returns a message indicating that.
type WebSearchTool struct{}

// NewWebSearchTool creates a WebSearchTool.
func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{}
}

func (w *WebSearchTool) Name() string { return "web_search" }
func (w *WebSearchTool) Description() string {
	return "Search the web for information. Currently a placeholder in local mode."
}

func (w *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query",
			},
		},
		"required": []string{"query"},
	}
}

func (w *WebSearchTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("web_search: arguments must not be nil")
	}

	query, err := requireString(args, "query")
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}

	return fmt.Sprintf("Web search is not available in local mode. Query was: %q. "+
		"Use fetch_url to retrieve a specific URL instead.", query), nil
}

// ---------------------------------------------------------------------------
// FetchURLTool
// ---------------------------------------------------------------------------

// FetchURLTool fetches the content of a URL via HTTP GET.
type FetchURLTool struct{}

// NewFetchURLTool creates a FetchURLTool.
func NewFetchURLTool() *FetchURLTool {
	return &FetchURLTool{}
}

func (f *FetchURLTool) Name() string { return "fetch_url" }
func (f *FetchURLTool) Description() string {
	return "Fetch the content of a URL via HTTP GET. Returns the response body truncated to 10KB."
}

func (f *FetchURLTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch",
			},
		},
		"required": []string{"url"},
	}
}

func (f *FetchURLTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("fetch_url: arguments must not be nil")
	}

	rawURL, err := requireString(args, "url")
	if err != nil {
		return "", fmt.Errorf("fetch_url: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("fetch_url: invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "HelixLLM/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch_url: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("fetch_url: HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// Read up to 10KB.
	limited := io.LimitReader(resp.Body, 10240)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("fetch_url: failed to read response: %w", err)
	}

	result := string(body)
	if len(body) >= 10240 {
		result += "\n...[response truncated to 10KB]"
	}

	return result, nil
}
