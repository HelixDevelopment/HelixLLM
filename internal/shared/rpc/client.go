package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// Client sends JSON-RPC 2.0 requests over a persistent TCP connection.
type Client struct {
	addr    string
	conn    net.Conn
	scanner *bufio.Scanner
	mu      sync.Mutex
	nextID  atomic.Int64
}

// NewClient dials addr and returns a ready-to-use Client.
func NewClient(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rpc client dial %s: %w", addr, err)
	}
	return &Client{
		addr:    addr,
		conn:    conn,
		scanner: bufio.NewScanner(conn),
	}, nil
}

// Call invokes method on the server, marshaling params as the JSON-RPC params
// field and unmarshaling the result into result.
// Call is safe to call concurrently; it serialises access to the connection.
func (c *Client) Call(_ context.Context, method string, params interface{}, result interface{}) error {
	id := c.nextID.Add(1)

	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("rpc marshal params: %w", err)
	}

	req := request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("rpc marshal request: %w", err)
	}
	reqData = append(reqData, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.conn.Write(reqData); err != nil {
		return fmt.Errorf("rpc write: %w", err)
	}

	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return fmt.Errorf("rpc read: %w", err)
		}
		return fmt.Errorf("rpc: connection closed by server")
	}

	var resp response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return fmt.Errorf("rpc unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("rpc unmarshal result: %w", err)
		}
	}
	return nil
}

// Close closes the underlying TCP connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
