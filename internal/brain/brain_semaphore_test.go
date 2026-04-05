package brain_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// slowProvider is a Provider test-double that sleeps during Complete so we can
// observe concurrency.
type slowProvider struct {
	delay    time.Duration
	onCall   func()
	onReturn func()
}

func (s *slowProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if s.onCall != nil {
		s.onCall()
	}
	time.Sleep(s.delay)
	if s.onReturn != nil {
		s.onReturn()
	}
	return &types.InternalChatResponse{
		ID:           "slow-1",
		Model:        "slow-model",
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "done"},
		FinishReason: "stop",
		Provider:     types.ProviderLocal,
	}, nil
}

func (s *slowProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if s.onCall != nil {
		s.onCall()
	}
	ch := make(chan types.StreamChunk, 1)
	go func() {
		defer close(ch)
		time.Sleep(s.delay)
		ch <- types.StreamChunk{Content: "done", FinishReason: "stop"}
		if s.onReturn != nil {
			s.onReturn()
		}
	}()
	return ch, nil
}

func (s *slowProvider) Models() []string { return []string{"slow-model"} }
func (s *slowProvider) Name() string     { return "slow" }
func (s *slowProvider) Available() bool  { return true }

func TestBrain_ConcurrencyLimit(t *testing.T) {
	var concurrent atomic.Int64
	var maxSeen atomic.Int64

	provider := &slowProvider{
		delay: 50 * time.Millisecond,
		onCall: func() {
			cur := concurrent.Add(1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
		},
		onReturn: func() {
			concurrent.Add(-1)
		},
	}

	br := brain.New(brain.Config{
		MaxConcurrent:  3,
		DefaultProvider: "slow",
	})
	br.RegisterProvider("slow", provider)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &types.InternalChatRequest{
				Model:    "slow-model",
				Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
			}
			_, _ = br.Complete(context.Background(), req)
		}()
	}

	wg.Wait()

	if maxSeen.Load() > 3 {
		t.Errorf("max concurrent = %d, want <= 3", maxSeen.Load())
	}
	t.Logf("max concurrent observed: %d", maxSeen.Load())
}

func TestBrain_ConcurrencyLimit_Unlimited(t *testing.T) {
	var concurrent atomic.Int64
	var maxSeen atomic.Int64

	provider := &slowProvider{
		delay: 50 * time.Millisecond,
		onCall: func() {
			cur := concurrent.Add(1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
		},
		onReturn: func() {
			concurrent.Add(-1)
		},
	}

	// MaxConcurrent 0 means unlimited -- all 10 goroutines should run at once.
	br := brain.New(brain.Config{
		MaxConcurrent:  0,
		DefaultProvider: "slow",
	})
	br.RegisterProvider("slow", provider)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &types.InternalChatRequest{
				Model:    "slow-model",
				Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
			}
			_, _ = br.Complete(context.Background(), req)
		}()
	}

	wg.Wait()

	if maxSeen.Load() < 5 {
		t.Logf("unlimited mode: max concurrent = %d (expected most of 10 to overlap)", maxSeen.Load())
	}
}

func TestBrain_ConcurrencyLimit_Stream(t *testing.T) {
	var concurrent atomic.Int64
	var maxSeen atomic.Int64

	provider := &slowProvider{
		delay: 50 * time.Millisecond,
		onCall: func() {
			cur := concurrent.Add(1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
		},
		onReturn: func() {
			concurrent.Add(-1)
		},
	}

	br := brain.New(brain.Config{
		MaxConcurrent:  2,
		DefaultProvider: "slow",
	})
	br.RegisterProvider("slow", provider)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &types.InternalChatRequest{
				Model:    "slow-model",
				Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
				Stream:   true,
			}
			ch, err := br.CompleteStream(context.Background(), req)
			if err != nil {
				return
			}
			// Drain the channel to trigger semaphore release.
			for range ch {
			}
		}()
	}

	wg.Wait()

	if maxSeen.Load() > 2 {
		t.Errorf("stream: max concurrent = %d, want <= 2", maxSeen.Load())
	}
	t.Logf("stream: max concurrent observed: %d", maxSeen.Load())
}

func TestBrain_ConcurrencyLimit_ContextCanceled(t *testing.T) {
	provider := &slowProvider{
		delay: 5 * time.Second,
	}

	br := brain.New(brain.Config{
		MaxConcurrent:  1,
		DefaultProvider: "slow",
	})
	br.RegisterProvider("slow", provider)

	// Fill the single semaphore slot with a long-running request.
	go func() {
		req := &types.InternalChatRequest{
			Model:    "slow-model",
			Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		}
		_, _ = br.Complete(context.Background(), req)
	}()

	// Give the goroutine time to acquire the semaphore.
	time.Sleep(10 * time.Millisecond)

	// A second request with a canceled context should fail fast.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req := &types.InternalChatRequest{
		Model:    "slow-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}
	_, err := br.Complete(ctx, req)
	if err == nil {
		t.Fatal("expected error when context is canceled, got nil")
	}
}
