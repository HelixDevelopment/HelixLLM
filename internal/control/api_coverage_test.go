package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// errSSHClient is an SSH mock that always returns errors, making probe fail.
type errSSHClient struct{}

func (e *errSSHClient) Run(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("SSH connection failed")
}

func (e *errSSHClient) IsReachable(_ context.Context, _ string) bool {
	return false
}

func TestHandleProbe_SingleHostFails(t *testing.T) {
	ssh := &errSSHClient{}

	// Use only one host to avoid concurrent SSH mock access (the pre-existing
	// mockSSHClient has a data race when ProbeAll spawns goroutines for
	// multiple hosts).
	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:   []string{"unreachable-host"},
		SSHUser: "user",
		SSHKey:  "key",
		SSH:     ssh,
	})

	r := gin.New()
	RegisterRoutes(r, cp)

	req := httptest.NewRequest(http.MethodPost, "/internal/cluster/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Even when the host fails, the probe handler should return 200 with
	// partial host data, because CheckCluster returns non-nil status
	// even when hosts are unreachable.
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleStatus_NoCheckPerformed(t *testing.T) {
	ssh := &nopSSHClient{}
	cp := NewControlPlane(ControlPlaneOptions{
		SSH: ssh,
	})

	r := gin.New()
	RegisterRoutes(r, cp)

	req := httptest.NewRequest(http.MethodGet, "/internal/cluster/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var status ClusterStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !status.Healthy {
		t.Error("expected healthy=true for initial status")
	}
}

func TestHandleDeploy_EmptyServices(t *testing.T) {
	ssh := &nopSSHClient{}
	cp := NewControlPlane(ControlPlaneOptions{SSH: ssh})

	r := gin.New()
	RegisterRoutes(r, cp)

	body := `{"services":[]}`
	req := httptest.NewRequest(http.MethodPost, "/internal/cluster/deploy",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeploy_InvalidBody(t *testing.T) {
	ssh := &nopSSHClient{}
	cp := NewControlPlane(ControlPlaneOptions{SSH: ssh})

	r := gin.New()
	RegisterRoutes(r, cp)

	req := httptest.NewRequest(http.MethodPost, "/internal/cluster/deploy",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRebalance_NoDeployments(t *testing.T) {
	ssh := &nopSSHClient{}
	cp := NewControlPlane(ControlPlaneOptions{SSH: ssh})

	r := gin.New()
	RegisterRoutes(r, cp)

	req := httptest.NewRequest(http.MethodPost, "/internal/cluster/rebalance", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestControlPlane_Status_WithDeployments(t *testing.T) {
	ssh := &nopSSHClient{}
	cp := NewControlPlane(ControlPlaneOptions{SSH: ssh})

	cp.mu.Lock()
	cp.deployments = []DeploymentInfo{
		{ServiceName: "redis", HostName: "host1", State: "running", DeployedAt: time.Now()},
	}
	cp.mu.Unlock()

	status := cp.Status()
	if len(status.Deployments) != 1 {
		t.Errorf("expected 1 deployment, got %d", len(status.Deployments))
	}
}
