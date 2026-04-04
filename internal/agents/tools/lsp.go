package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// LSPProvider is the interface that wraps the core LSP operations. LSPClient
// implements it; tests use a mock.
type LSPProvider interface {
	Definition(ctx context.Context, file string, line, col int) ([]Location, error)
	References(ctx context.Context, file string, line, col int) ([]Location, error)
	Hover(ctx context.Context, file string, line, col int) (string, error)
	Diagnostics(ctx context.Context, file string) ([]Diagnostic, error)
	Close() error
}

// Location represents a source location returned by LSP.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Range is a span in a text document, expressed as start and end positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position is a zero-based (line, character) coordinate in a text document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Diagnostic represents a compiler or linter diagnostic reported by the server.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// lspRequest is a JSON-RPC 2.0 request object sent to the language server.
type lspRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// lspResponse is a JSON-RPC 2.0 response received from the language server.
type lspResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *lspError       `json:"error,omitempty"`
}

// lspNotification is a JSON-RPC 2.0 notification (no ID) from the server.
type lspNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// lspError represents the error field of a JSON-RPC 2.0 response.
type lspError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *lspError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// LSPClient connects to a language server via stdio transport and communicates
// using the JSON-RPC 2.0 framing defined by the Language Server Protocol.
type LSPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	// mu guards writes to stdin so concurrent callers don't interleave frames.
	mu     sync.Mutex
	nextID atomic.Int64

	// pending maps in-flight request IDs to the channel that will receive the
	// raw JSON result (or an error marshalled into the channel via a sentinel).
	pendMu  sync.Mutex
	pending map[int64]chan json.RawMessage

	// diagnostics stores the latest publishDiagnostics notification per URI.
	diagMu      sync.RWMutex
	diagnostics map[string][]Diagnostic

	// done is closed when the reader goroutine exits.
	done chan struct{}
}

// NewLSPClient starts the language server subprocess and returns a connected
// LSPClient. The caller must call Initialize before issuing any LSP requests.
func NewLSPClient(command string, args ...string) (*LSPClient, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %q: %w", command, err)
	}

	c := &LSPClient{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdoutPipe),
		pending:     make(map[int64]chan json.RawMessage),
		diagnostics: make(map[string][]Diagnostic),
		done:        make(chan struct{}),
	}

	go c.readLoop()

	return c, nil
}

// NewLSPClientFromPipes creates an LSPClient using pre-existing IO pipes. This
// is used in tests where we want to drive the server side ourselves without
// exec-ing a subprocess.
func NewLSPClientFromPipes(r io.Reader, w io.WriteCloser) *LSPClient {
	c := &LSPClient{
		stdin:       w,
		stdout:      bufio.NewReader(r),
		pending:     make(map[int64]chan json.RawMessage),
		diagnostics: make(map[string][]Diagnostic),
		done:        make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Initialize sends the LSP initialize / initialized handshake. rootURI should
// be a file:// URI pointing to the workspace root, e.g. "file:///home/user/project".
func (c *LSPClient) Initialize(ctx context.Context, rootURI string) error {
	params := map[string]interface{}{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"definition":  map[string]interface{}{},
				"references":  map[string]interface{}{},
				"hover":       map[string]interface{}{},
				"publishDiagnostics": map[string]interface{}{},
			},
		},
	}

	if _, err := c.call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("lsp initialize: %w", err)
	}

	// Send the initialized notification (no response expected).
	return c.notify("initialized", map[string]interface{}{})
}

// Close shuts down the language server gracefully and waits for the process to
// exit. It is safe to call Close more than once.
func (c *LSPClient) Close() error {
	// Best-effort shutdown sequence: ignore errors because the server may have
	// already exited.
	ctx := context.Background()
	_, _ = c.call(ctx, "shutdown", nil)
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()

	if c.cmd != nil {
		return c.cmd.Wait()
	}
	return nil
}

// Definition returns the definition location(s) of the symbol at (line, col)
// in file. Uses the LSP textDocument/definition request.
func (c *LSPClient) Definition(ctx context.Context, file string, line, col int) ([]Location, error) {
	params := c.textDocumentPositionParams(file, line, col)
	raw, err := c.call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, fmt.Errorf("lsp definition: %w", err)
	}
	return parseLocations(raw)
}

// References returns all references to the symbol at (line, col) in file.
// Uses the LSP textDocument/references request.
func (c *LSPClient) References(ctx context.Context, file string, line, col int) ([]Location, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": fileURI(file)},
		"position":     map[string]interface{}{"line": line, "character": col},
		"context":      map[string]interface{}{"includeDeclaration": true},
	}
	raw, err := c.call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, fmt.Errorf("lsp references: %w", err)
	}
	return parseLocations(raw)
}

// Hover returns the hover documentation for the symbol at (line, col) in file.
// Uses the LSP textDocument/hover request.
func (c *LSPClient) Hover(ctx context.Context, file string, line, col int) (string, error) {
	params := c.textDocumentPositionParams(file, line, col)
	raw, err := c.call(ctx, "textDocument/hover", params)
	if err != nil {
		return "", fmt.Errorf("lsp hover: %w", err)
	}
	if raw == nil || string(raw) == "null" {
		return "", nil
	}

	var result struct {
		Contents interface{} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("lsp hover: decode: %w", err)
	}

	return extractHoverContents(result.Contents), nil
}

// Diagnostics returns the most recently received diagnostics for file. LSP
// delivers diagnostics via a push notification (textDocument/publishDiagnostics)
// so this method returns cached data. The cache is populated by the background
// reader goroutine.
func (c *LSPClient) Diagnostics(_ context.Context, file string) ([]Diagnostic, error) {
	uri := fileURI(file)

	c.diagMu.RLock()
	defer c.diagMu.RUnlock()

	diags, ok := c.diagnostics[uri]
	if !ok {
		return []Diagnostic{}, nil
	}
	result := make([]Diagnostic, len(diags))
	copy(result, diags)
	return result, nil
}

// --- internal helpers ---------------------------------------------------------

// call sends a JSON-RPC request and blocks until the response arrives or ctx is
// cancelled. It returns the raw JSON result field.
func (c *LSPClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := lspRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	ch := make(chan json.RawMessage, 1)

	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	if err := c.writeRequest(req); err != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, ctx.Err()
	case raw := <-ch:
		// A nil value in the channel signals a transport-level error or server
		// shutdown; in normal operation the channel carries the raw result JSON.
		return raw, nil
	case <-c.done:
		return nil, fmt.Errorf("lsp: server closed")
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (c *LSPClient) notify(method string, params interface{}) error {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("lsp notify marshal: %w", err)
	}
	return c.writeFrame(body)
}

// writeRequest serialises req and writes the LSP content-length framed message.
func (c *LSPClient) writeRequest(req lspRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("lsp: marshal request: %w", err)
	}
	return c.writeFrame(body)
}

// writeFrame writes a single LSP-framed message: "Content-Length: N\r\n\r\n<body>".
func (c *LSPClient) writeFrame(body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return fmt.Errorf("lsp: write header: %w", err)
	}
	if _, err := c.stdin.Write(body); err != nil {
		return fmt.Errorf("lsp: write body: %w", err)
	}
	return nil
}

// readLoop reads messages from stdout in the background, dispatching responses
// to waiting callers and caching diagnostic notifications.
func (c *LSPClient) readLoop() {
	defer close(c.done)

	for {
		body, err := c.readFrame()
		if err != nil {
			// EOF or closed pipe — signal all waiting callers.
			c.pendMu.Lock()
			for _, ch := range c.pending {
				ch <- nil
			}
			c.pending = make(map[int64]chan json.RawMessage)
			c.pendMu.Unlock()
			return
		}

		// Determine whether it is a response (has "id") or a notification.
		var peek struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &peek); err != nil {
			continue
		}

		if peek.ID != nil {
			// Response to a pending call.
			var resp lspResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				continue
			}
			c.pendMu.Lock()
			ch, ok := c.pending[resp.ID]
			if ok {
				delete(c.pending, resp.ID)
			}
			c.pendMu.Unlock()
			if ok {
				if resp.Error != nil {
					// Encode the error into the result so the caller can detect it.
					errMsg, _ := json.Marshal(map[string]string{"_lspError": resp.Error.Error()})
					ch <- errMsg
				} else {
					ch <- resp.Result
				}
			}
		} else if peek.Method == "textDocument/publishDiagnostics" {
			// Push notification — cache it.
			var notif lspNotification
			if err := json.Unmarshal(body, &notif); err != nil {
				continue
			}
			c.handlePublishDiagnostics(notif.Params)
		}
		// Other notifications (window/logMessage, etc.) are silently ignored.
	}
}

// readFrame reads one LSP message from stdout. It parses the Content-Length
// header line and then reads exactly that many bytes of JSON body.
func (c *LSPClient) readFrame() ([]byte, error) {
	var contentLength int

	// Read headers until the blank line separator.
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil {
					return nil, fmt.Errorf("lsp: invalid Content-Length: %w", err)
				}
				contentLength = n
			}
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("lsp: missing or zero Content-Length")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, fmt.Errorf("lsp: read body: %w", err)
	}
	return body, nil
}

// handlePublishDiagnostics stores diagnostics received from the server.
func (c *LSPClient) handlePublishDiagnostics(raw json.RawMessage) {
	var params struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return
	}
	c.diagMu.Lock()
	c.diagnostics[params.URI] = params.Diagnostics
	c.diagMu.Unlock()
}

// textDocumentPositionParams builds the standard TextDocumentPositionParams map.
func (c *LSPClient) textDocumentPositionParams(file string, line, col int) map[string]interface{} {
	return map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": fileURI(file)},
		"position":     map[string]interface{}{"line": line, "character": col},
	}
}

// fileURI converts a filesystem path to a file:// URI. Paths that already start
// with "file://" are returned unchanged.
func fileURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		return "file:///" + path
	}
	return "file://" + path
}

// parseLocations decodes a JSON array (or single object) of LSP Locations.
func parseLocations(raw json.RawMessage) ([]Location, error) {
	if raw == nil || string(raw) == "null" {
		return []Location{}, nil
	}

	// Try array first.
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err == nil {
		return locs, nil
	}

	// Fall back to single location.
	var loc Location
	if err := json.Unmarshal(raw, &loc); err != nil {
		return nil, fmt.Errorf("lsp: decode locations: %w", err)
	}
	return []Location{loc}, nil
}

// extractHoverContents converts the polymorphic LSP hover contents into a plain
// string. Handles MarkedString, MarkupContent, and arrays thereof.
func extractHoverContents(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case map[string]interface{}:
		if val, ok := t["value"]; ok {
			if s, ok := val.(string); ok {
				return s
			}
		}
		if val, ok := t["language"]; ok {
			_ = val
			if s, ok := t["value"].(string); ok {
				return s
			}
		}
	case []interface{}:
		var parts []string
		for _, item := range t {
			s := extractHoverContents(item)
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", v)
}
