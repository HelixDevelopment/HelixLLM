package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

// mockLSPServer is a lightweight in-process JSON-RPC server that responds to
// LSP requests over a pair of synchronised pipes. It is wired up to an
// LSPClient via newLSPClientFromPipes so no subprocess is needed.
type mockLSPServer struct {
	t        *testing.T
	reader   *io.PipeReader  // server reads client writes here
	writer   *io.PipeWriter  // server writes client reads here
	handlers map[string]func(params json.RawMessage) interface{}
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// newMockLSPServer wires up two pipe pairs and starts the server loop.
// It returns (server, clientReader, clientWriter) ready to be passed to
// newLSPClientFromPipes.
func newMockLSPServer(t *testing.T) (*mockLSPServer, io.Reader, io.WriteCloser) {
	t.Helper()

	// Pipe 1: client writes, server reads.
	clientWriteR, clientWriteW := io.Pipe()
	// Pipe 2: server writes, client reads.
	serverWriteR, serverWriteW := io.Pipe()

	s := &mockLSPServer{
		t:        t,
		reader:   clientWriteR,
		writer:   serverWriteW,
		handlers: make(map[string]func(params json.RawMessage) interface{}),
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serve()
	}()

	t.Cleanup(func() {
		_ = clientWriteW.Close()
		_ = serverWriteW.Close()
		s.wg.Wait()
	})

	return s, serverWriteR, clientWriteW
}

// handle registers a handler for the given LSP method. The handler receives the
// raw params JSON and returns a value that will be marshalled as the result.
func (s *mockLSPServer) handle(method string, fn func(params json.RawMessage) interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = fn
}

// serve reads LSP frames from the client pipe and dispatches them.
func (s *mockLSPServer) serve() {
	buf := make([]byte, 0, 4096)
	rbuf := make([]byte, 4096)

	var accumulated []byte

	for {
		n, err := s.reader.Read(rbuf)
		if n > 0 {
			accumulated = append(accumulated, rbuf[:n]...)
		}
		if err != nil {
			return
		}

		// Process all complete frames in the accumulated buffer.
		for {
			consumed, body := extractFrame(accumulated)
			if consumed == 0 {
				break
			}
			accumulated = accumulated[consumed:]
			buf = body
			s.dispatch(buf)
		}
	}
}

// extractFrame attempts to parse one LSP frame from data. Returns the number of
// bytes consumed and the body. Returns (0, nil) if the frame is incomplete.
func extractFrame(data []byte) (int, []byte) {
	// Find the header/body separator (\r\n\r\n).
	idx := 0
	for i := 0; i < len(data)-3; i++ {
		if data[i] == '\r' && data[i+1] == '\n' && data[i+2] == '\r' && data[i+3] == '\n' {
			idx = i
			break
		}
	}
	if idx == 0 && len(data) < 4 {
		return 0, nil
	}

	headerSection := string(data[:idx])
	var contentLength int
	for _, line := range strings.Split(headerSection, "\r\n") {
		if strings.HasPrefix(line, "Content-Length:") {
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "Content-Length:"), " %d", &contentLength)
		}
	}
	if contentLength == 0 {
		return 0, nil
	}

	headerEnd := idx + 4
	if len(data) < headerEnd+contentLength {
		return 0, nil
	}

	body := make([]byte, contentLength)
	copy(body, data[headerEnd:headerEnd+contentLength])
	return headerEnd + contentLength, body
}

// dispatch handles a single decoded body.
func (s *mockLSPServer) dispatch(body []byte) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int64          `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return
	}

	// Notifications have no ID — no response needed.
	if req.ID == nil {
		return
	}

	s.mu.Lock()
	fn, ok := s.handlers[req.Method]
	s.mu.Unlock()

	var result interface{}
	if ok {
		result = fn(req.Params)
	}

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      *req.ID,
		"result":  result,
	}
	respBody, _ := json.Marshal(resp)
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(respBody))
	s.mu.Lock()
	_, _ = s.writer.Write([]byte(frame))
	_, _ = s.writer.Write(respBody)
	s.mu.Unlock()
}

// sendDiagnostics pushes a textDocument/publishDiagnostics notification to the
// client, exercising the background reader's notification path.
func (s *mockLSPServer) sendDiagnostics(uri string, diags []tools.Diagnostic) {
	params := map[string]interface{}{
		"uri":         uri,
		"diagnostics": diags,
	}
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params":  params,
	})
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	s.mu.Lock()
	_, _ = s.writer.Write([]byte(frame))
	_, _ = s.writer.Write(body)
	s.mu.Unlock()
}

// newTestClient creates a mock server + wired-up LSPClient for testing.
func newTestClient(t *testing.T) (*mockLSPServer, tools.LSPProvider) {
	t.Helper()
	srv, r, w := newMockLSPServer(t)

	// Register a default initialize handler.
	srv.handle("initialize", func(_ json.RawMessage) interface{} {
		return map[string]interface{}{
			"capabilities": map[string]interface{}{},
		}
	})

	client := tools.NewLSPClientFromPipes(r, w)
	t.Cleanup(func() { _ = client.Close() })
	return srv, client
}

// ---------------------------------------------------------------------------
// Tests for JSON-RPC framing (encode / decode round-trip)
// ---------------------------------------------------------------------------

func TestLSPFrameRoundTrip(t *testing.T) {
	srv, client := newTestClient(t)

	called := false
	srv.handle("test/ping", func(_ json.RawMessage) interface{} {
		called = true
		return map[string]string{"pong": "yes"}
	})

	// Use Definition as a proxy for a generic call; we just want the transport
	// to work. Override with a definition handler that returns a location.
	srv.handle("textDocument/definition", func(_ json.RawMessage) interface{} {
		return []map[string]interface{}{
			{
				"uri": "file:///test.go",
				"range": map[string]interface{}{
					"start": map[string]int{"line": 1, "character": 2},
					"end":   map[string]int{"line": 1, "character": 5},
				},
			},
		}
	})

	locs, err := client.Definition(context.Background(), "/test.go", 0, 0)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if locs[0].URI != "file:///test.go" {
		t.Errorf("unexpected URI: %s", locs[0].URI)
	}
	_ = called
}

// ---------------------------------------------------------------------------
// Tests for LSPClient methods
// ---------------------------------------------------------------------------

func TestLSPClient_Definition(t *testing.T) {
	srv, client := newTestClient(t)

	srv.handle("textDocument/definition", func(_ json.RawMessage) interface{} {
		return []map[string]interface{}{
			{
				"uri": "file:///pkg/foo.go",
				"range": map[string]interface{}{
					"start": map[string]int{"line": 10, "character": 4},
					"end":   map[string]int{"line": 10, "character": 7},
				},
			},
		}
	})

	locs, err := client.Definition(context.Background(), "/src/main.go", 5, 12)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if locs[0].Range.Start.Line != 10 {
		t.Errorf("expected line 10, got %d", locs[0].Range.Start.Line)
	}
}

func TestLSPClient_DefinitionEmpty(t *testing.T) {
	srv, client := newTestClient(t)

	srv.handle("textDocument/definition", func(_ json.RawMessage) interface{} {
		return nil
	})

	locs, err := client.Definition(context.Background(), "/src/main.go", 0, 0)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected empty locations, got %d", len(locs))
	}
}

func TestLSPClient_References(t *testing.T) {
	srv, client := newTestClient(t)

	srv.handle("textDocument/references", func(_ json.RawMessage) interface{} {
		return []map[string]interface{}{
			{
				"uri": "file:///a.go",
				"range": map[string]interface{}{
					"start": map[string]int{"line": 3, "character": 0},
					"end":   map[string]int{"line": 3, "character": 5},
				},
			},
			{
				"uri": "file:///b.go",
				"range": map[string]interface{}{
					"start": map[string]int{"line": 7, "character": 2},
					"end":   map[string]int{"line": 7, "character": 7},
				},
			},
		}
	})

	locs, err := client.References(context.Background(), "/a.go", 3, 0)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 references, got %d", len(locs))
	}
}

func TestLSPClient_HoverString(t *testing.T) {
	srv, client := newTestClient(t)

	srv.handle("textDocument/hover", func(_ json.RawMessage) interface{} {
		return map[string]interface{}{
			"contents": map[string]interface{}{
				"kind":  "markdown",
				"value": "func Foo() string",
			},
		}
	})

	text, err := client.Hover(context.Background(), "/src/main.go", 2, 6)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if text != "func Foo() string" {
		t.Errorf("unexpected hover text: %q", text)
	}
}

func TestLSPClient_HoverNull(t *testing.T) {
	srv, client := newTestClient(t)

	srv.handle("textDocument/hover", func(_ json.RawMessage) interface{} {
		return nil
	})

	text, err := client.Hover(context.Background(), "/src/main.go", 0, 0)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty hover, got %q", text)
	}
}

func TestLSPClient_DiagnosticsFromNotification(t *testing.T) {
	srv, client := newTestClient(t)

	// Send a publishDiagnostics notification from the server side.
	diags := []tools.Diagnostic{
		{
			Range:    tools.Range{Start: tools.Position{Line: 4, Character: 0}, End: tools.Position{Line: 4, Character: 10}},
			Severity: 1,
			Message:  "undefined: Foo",
			Source:   "gopls",
		},
	}
	srv.sendDiagnostics("file:///src/main.go", diags)

	// Poll with real sleeps — the notification arrives asynchronously via the
	// reader goroutine so we need to actually yield the CPU.
	var got []tools.Diagnostic
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err = client.Diagnostics(context.Background(), "/src/main.go")
		if err != nil {
			t.Fatalf("Diagnostics: %v", err)
		}
		if len(got) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(got))
	}
	if got[0].Message != "undefined: Foo" {
		t.Errorf("unexpected message: %q", got[0].Message)
	}
	if got[0].Severity != 1 {
		t.Errorf("expected severity 1, got %d", got[0].Severity)
	}
}

func TestLSPClient_DiagnosticsEmpty(t *testing.T) {
	_, client := newTestClient(t)

	got, err := client.Diagnostics(context.Background(), "/no/file.go")
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no diagnostics, got %d", len(got))
	}
}

func TestLSPClient_ContextCancellation(t *testing.T) {
	// Use a server that never responds to references.
	srv, client := newTestClient(t)
	_ = srv // no handler registered for references

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.References(ctx, "/src/main.go", 0, 0)
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}
