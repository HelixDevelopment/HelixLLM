package tools

import (
	"context"
	"fmt"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// KnowledgeQueryTool queries the RAG pipeline and returns matching context.
type KnowledgeQueryTool struct {
	pipeline          *knowledge.Pipeline
	defaultCollection string
}

// NewKnowledgeQueryTool creates a KnowledgeQueryTool that queries the given
// pipeline. If collection is empty in the arguments, defaultCollection is used.
func NewKnowledgeQueryTool(pipeline *knowledge.Pipeline, defaultCollection string) *KnowledgeQueryTool {
	return &KnowledgeQueryTool{
		pipeline:          pipeline,
		defaultCollection: defaultCollection,
	}
}

func (k *KnowledgeQueryTool) Name() string { return "knowledge_query" }
func (k *KnowledgeQueryTool) Description() string {
	return "Searches the knowledge base for relevant information. Use this when you need to look up facts, documentation, or previously ingested content."
}

func (k *KnowledgeQueryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "The search query to find relevant knowledge",
			"required":    true,
		},
		"collection": map[string]interface{}{
			"type":        "string",
			"description": "The knowledge collection to search. Defaults to the configured collection.",
			"required":    false,
		},
	}
}

func (k *KnowledgeQueryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("knowledge_query: arguments must not be nil")
	}

	queryVal, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("knowledge_query: missing required parameter 'query'")
	}
	query, ok := queryVal.(string)
	if !ok || query == "" {
		return "", fmt.Errorf("knowledge_query: 'query' must be a non-empty string")
	}

	if k.pipeline == nil {
		return "", fmt.Errorf("knowledge_query: knowledge pipeline is not available")
	}

	collection := k.defaultCollection
	if colVal, ok := args["collection"]; ok {
		if colStr, ok := colVal.(string); ok && colStr != "" {
			collection = colStr
		}
	}

	result, err := k.pipeline.Query(ctx, knowledge.QueryRequest{
		Query:      query,
		Collection: collection,
		TopK:       5,
	})
	if err != nil {
		return "", fmt.Errorf("knowledge_query: %w", err)
	}

	if result.Context == "" {
		return "No relevant information found.", nil
	}
	return result.Context, nil
}
