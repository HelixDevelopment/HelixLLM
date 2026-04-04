package knowledge

import (
	"context"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// RAGHook returns a middleware function that prepends retrieved RAG context
// as a system message to the incoming request. It looks up the last user
// message, queries the pipeline for the given collection, and prepends the
// result as a system message when context is found.
//
// If pipeline is nil, the request is returned unchanged.
func RAGHook(pipeline *Pipeline, collection string) func(req *types.InternalChatRequest) *types.InternalChatRequest {
	return func(req *types.InternalChatRequest) *types.InternalChatRequest {
		if pipeline == nil || len(req.Messages) == 0 {
			return req
		}

		// Find the last user message to use as the query.
		var query string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == types.RoleUser {
				query = req.Messages[i].Content
				break
			}
		}
		if query == "" {
			return req
		}

		result, err := pipeline.Query(context.Background(), QueryRequest{
			Query:      query,
			Collection: collection,
			TopK:       3,
		})
		if err != nil || result.Context == "" {
			return req
		}

		contextMsg := types.InternalMessage{
			Role:    types.RoleSystem,
			Content: "Relevant context:\n" + result.Context,
		}

		modified := *req
		modified.Messages = append([]types.InternalMessage{contextMsg}, req.Messages...)
		return &modified
	}
}
