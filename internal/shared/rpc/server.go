// Package rpc provides a lightweight JSON-RPC 2.0 over TCP transport for
// inter-layer communication in HelixLLM distributed mode.
//
// The wire format is newline-delimited JSON: each request and response is a
// single JSON object followed by a newline character (\n).
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
)

// Handler processes an RPC call. It receives the raw JSON params and returns
// raw JSON result or an error.
type Handler func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

// request is the JSON-RPC 2.0 request envelope.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is the JSON-RPC 2.0 response envelope.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server listens on a TCP address and dispatches JSON-RPC 2.0 requests to
// registered handlers.
type Server struct {
	addr     string
	handlers map[string]Handler
	mu       sync.RWMutex
	listener net.Listener
}

// NewServer creates a new Server that will listen on addr.
func NewServer(addr string) *Server {
	return &Server{
		addr:     addr,
		handlers: make(map[string]Handler),
	}
}

// Handle registers handler h for the given RPC method name. It is safe to
// call Handle before or after ListenAndServe has started, but registering
// after the first connection is established may miss in-flight calls.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	s.handlers[method] = h
	s.mu.Unlock()
}

// ListenAndServe binds to the configured address and accepts connections until
// ctx is cancelled or Stop is called.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("rpc server listen %s: %w", s.addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Return nil on expected shutdown (context cancelled / listener closed).
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("rpc server accept: %w", err)
			}
		}
		go s.serveConn(ctx, conn)
	}
}

// Stop closes the listener, causing ListenAndServe to return.
func (s *Server) Stop() {
	s.mu.RLock()
	ln := s.listener
	s.mu.RUnlock()
	if ln != nil {
		ln.Close()
	}
}

// serveConn handles all requests on a single TCP connection.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(conn, response{
				JSONRPC: "2.0",
				ID:      0,
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		s.mu.RLock()
		h, ok := s.handlers[req.Method]
		s.mu.RUnlock()

		if !ok {
			writeResponse(conn, response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32601, Message: "method not found: " + req.Method},
			})
			continue
		}

		result, err := h(ctx, req.Params)
		if err != nil {
			writeResponse(conn, response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32000, Message: err.Error()},
			})
			continue
		}

		writeResponse(conn, response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		})
	}
}

// writeResponse encodes resp as JSON and writes it with a trailing newline.
func writeResponse(w io.Writer, resp response) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = w.Write(data)
}
