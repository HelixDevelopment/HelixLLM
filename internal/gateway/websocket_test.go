package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// dialWS upgrades a plain httptest.Server URL to ws:// and returns the
// gorilla connection.
func dialWS(t *testing.T, srv *httptest.Server) *gorillaws.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	dialer := gorillaws.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	return conn
}

func TestHandleWebSocket_NoBrain_StubResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", gateway.HandleWebSocket(nil))

	srv := httptest.NewServer(r)
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.Close()

	// Send a valid InternalChatRequest.
	req := types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "hello"},
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["message"] == "" {
		t.Errorf("expected stub message field, got %v", resp)
	}
}

func TestHandleWebSocket_InvalidJSON_ErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", gateway.HandleWebSocket(nil))

	srv := httptest.NewServer(r)
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.Close()

	// Send garbage that cannot be decoded as InternalChatRequest.
	if err := conn.WriteMessage(gorillaws.TextMessage, []byte("not json at all")); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["error"], "invalid request") {
		t.Errorf("expected 'invalid request' error, got %q", resp["error"])
	}
}

func TestHandleWebSocket_NonWebSocketRequest_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", gateway.HandleWebSocket(nil))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	// No Upgrade header — the upgrade must fail.
	r.ServeHTTP(w, req)

	if w.Code == http.StatusSwitchingProtocols {
		t.Error("expected non-101 for plain GET without Upgrade header")
	}
}
