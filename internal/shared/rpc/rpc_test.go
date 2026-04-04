package rpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/rpc"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// freePort asks the OS for a free TCP port.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// startServer creates a Server, registers h for method, starts it in the
// background, and returns the server and a cancel func.
func startServer(t *testing.T, addr string) (*rpc.Server, context.CancelFunc) {
	t.Helper()
	srv := rpc.NewServer(addr)
	ctx, cancel := context.WithCancel(context.Background())

	ready := make(chan struct{})
	go func() {
		// Signal readiness just before blocking in ListenAndServe.
		// We use a tiny sleep so the goroutine has time to bind.
		close(ready)
		_ = srv.ListenAndServe(ctx)
	}()
	<-ready
	// Give the OS a moment to actually bind.
	time.Sleep(10 * time.Millisecond)
	return srv, cancel
}

// ---------------------------------------------------------------------------
// Server + Client round-trip
// ---------------------------------------------------------------------------

func TestRoundTrip_Echo(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	srv.Handle("echo", func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
		return params, nil
	})

	client, err := rpc.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	type payload struct {
		Msg string `json:"msg"`
	}
	var result payload
	if err := client.Call(context.Background(), "echo", payload{Msg: "hello"}, &result); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Msg != "hello" {
		t.Errorf("result.Msg = %q, want %q", result.Msg, "hello")
	}
}

func TestRoundTrip_MethodNotFound(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	client, err := rpc.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	err = client.Call(context.Background(), "nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
}

func TestRoundTrip_HandlerError(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	srv.Handle("fail", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return nil, fmt.Errorf("intentional failure")
	})

	client, err := rpc.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	err = client.Call(context.Background(), "fail", nil, nil)
	if err == nil {
		t.Fatal("expected error propagated from handler, got nil")
	}
}

func TestRoundTrip_MultipleSequentialCalls(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	var counter int
	srv.Handle("inc", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		counter++
		return json.Marshal(counter)
	})

	client, err := rpc.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	for i := 1; i <= 3; i++ {
		var got int
		if err := client.Call(context.Background(), "inc", nil, &got); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got != i {
			t.Errorf("call %d: got %d, want %d", i, got, i)
		}
	}
}

// ---------------------------------------------------------------------------
// BrainProxy with a mock server
// ---------------------------------------------------------------------------

func TestBrainProxy_Complete(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	want := types.InternalChatResponse{
		ID:           "test-id",
		Model:        "llama-3.1-70b",
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "pong"},
		FinishReason: "stop",
		Provider:     types.ProviderLocal,
	}

	srv.Handle("brain.Complete", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(want)
	})

	proxy, err := rpc.NewBrainProxy(addr)
	if err != nil {
		t.Fatalf("NewBrainProxy: %v", err)
	}
	defer proxy.Close()

	req := &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "ping"}},
	}
	resp, err := proxy.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.ID != want.ID {
		t.Errorf("resp.ID = %q, want %q", resp.ID, want.ID)
	}
	if resp.Message.Content != want.Message.Content {
		t.Errorf("resp.Message.Content = %q, want %q", resp.Message.Content, want.Message.Content)
	}
}

func TestBrainProxy_CompleteStream_Unsupported(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	proxy, err := rpc.NewBrainProxy(addr)
	if err != nil {
		t.Fatalf("NewBrainProxy: %v", err)
	}
	defer proxy.Close()

	_, err = proxy.CompleteStream(context.Background(), &types.InternalChatRequest{})
	if err == nil {
		t.Fatal("CompleteStream should return error (unsupported), got nil")
	}
}

func TestBrainProxy_RefreshModels(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	srv.Handle("brain.Models", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.Marshal([]string{"llama-3.1-70b", "llama-3.1-8b"})
	})

	proxy, err := rpc.NewBrainProxy(addr)
	if err != nil {
		t.Fatalf("NewBrainProxy: %v", err)
	}
	defer proxy.Close()

	if err := proxy.RefreshModels(context.Background()); err != nil {
		t.Fatalf("RefreshModels: %v", err)
	}
	models := proxy.Models()
	if len(models) != 2 {
		t.Errorf("Models() len = %d, want 2", len(models))
	}
}

func TestBrainProxy_Name(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	proxy, err := rpc.NewBrainProxy(addr)
	if err != nil {
		t.Fatalf("NewBrainProxy: %v", err)
	}
	defer proxy.Close()

	if proxy.Name() == "" {
		t.Error("Name() returned empty string")
	}
}

func TestBrainProxy_Available(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	proxy, err := rpc.NewBrainProxy(addr)
	if err != nil {
		t.Fatalf("NewBrainProxy: %v", err)
	}
	defer proxy.Close()

	if !proxy.Available() {
		t.Error("Available() = false, want true")
	}
}
