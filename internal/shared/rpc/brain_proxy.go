package rpc

import (
	"context"
	"fmt"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// BrainProxy implements brain.Provider by forwarding all calls over JSON-RPC
// to a remote Brain service.  It satisfies the brain.Provider interface so it
// can be injected into a Brain router in place of a local backend.
type BrainProxy struct {
	client    *Client
	nameStr   string
	modelList []string
}

// NewBrainProxy dials addr and returns a BrainProxy.
func NewBrainProxy(addr string) (*BrainProxy, error) {
	c, err := NewClient(addr)
	if err != nil {
		return nil, fmt.Errorf("brain proxy: %w", err)
	}
	return &BrainProxy{
		client:  c,
		nameStr: "remote-brain",
	}, nil
}

// Complete forwards an InternalChatRequest to the remote Brain and returns the
// InternalChatResponse.
func (p *BrainProxy) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	var resp types.InternalChatResponse
	if err := p.client.Call(ctx, "brain.Complete", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CompleteStream is not supported over a simple JSON-RPC connection; it
// returns an error directing callers to use the non-streaming path.
func (p *BrainProxy) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, fmt.Errorf("BrainProxy: streaming not supported over JSON-RPC; use Complete")
}

// Models returns the model list cached from the remote Brain.  The list is
// populated by calling RefreshModels; until then it returns an empty slice.
func (p *BrainProxy) Models() []string {
	return p.modelList
}

// RefreshModels fetches the current model list from the remote Brain and
// caches it locally.
func (p *BrainProxy) RefreshModels(ctx context.Context) error {
	var models []string
	if err := p.client.Call(ctx, "brain.Models", nil, &models); err != nil {
		return err
	}
	p.modelList = models
	return nil
}

// Name returns the provider name reported to the Brain router.
func (p *BrainProxy) Name() string {
	return p.nameStr
}

// Available always returns true: the proxy itself is always structurally
// present.  Real availability is determined by the remote Brain's health.
func (p *BrainProxy) Available() bool {
	return true
}

// Close shuts down the underlying RPC connection.
func (p *BrainProxy) Close() error {
	return p.client.Close()
}
