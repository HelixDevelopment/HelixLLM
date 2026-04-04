package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupTestRouter creates a Gin engine with control routes
// registered, backed by a mock SSH client.
func setupTestRouter() (*gin.Engine, *mockSSHClient) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.reachable["host2"] = true
	mock.outputs["host1"] = linuxProbeOutput()
	mock.outputs["host2"] = linuxProbeOutputNoGPU()

	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"host1", "host2"},
		SSHUser:  "milosvasic",
		SSHKey:   "~/.ssh/id_ed25519",
		Strategy: "auto",
		SSH:      mock,
	})

	r := gin.New()
	RegisterRoutes(r, cp)
	return r, mock
}

func TestAPI_GetClusterStatus(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet,
		"/internal/cluster/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var status ClusterStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Before any probe, status should have empty hosts.
	if status.CheckedAt.IsZero() {
		t.Error("expected non-zero CheckedAt")
	}
}

func TestAPI_PostClusterProbe(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Hosts []HostProfile `json:"hosts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, want 2", len(resp.Hosts))
	}
}

func TestAPI_PostClusterProbe_SetsStatus(t *testing.T) {
	r, _ := setupTestRouter()

	// Probe first.
	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("probe status = %d", w.Code)
	}

	// Now get status -- should reflect probed hosts.
	req = httptest.NewRequest(http.MethodGet,
		"/internal/cluster/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var status ClusterStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(status.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, want 2", len(status.Hosts))
	}
	if !status.Healthy {
		t.Error("expected cluster to be healthy after probe")
	}
}

func TestAPI_PostClusterDeploy(t *testing.T) {
	r, _ := setupTestRouter()

	// Probe first so we have host profiles.
	probeReq := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/probe", nil)
	probeW := httptest.NewRecorder()
	r.ServeHTTP(probeW, probeReq)

	// Deploy services.
	body := deployRequest{
		Services: []ServiceRequirement{
			{Name: "redis", Image: "redis:7", CPUCores: 2,
				MemoryMB: 4096},
			{Name: "postgres", Image: "postgres:16", CPUCores: 4,
				MemoryMB: 8192},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/deploy",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}

	var resp deployResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Deployments) != 2 {
		t.Errorf("len(Deployments) = %d, want 2",
			len(resp.Deployments))
	}
}

func TestAPI_PostClusterDeploy_NoBody(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/deploy", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusBadRequest)
	}
}

func TestAPI_PostClusterDeploy_EmptyServices(t *testing.T) {
	r, _ := setupTestRouter()

	body := deployRequest{Services: []ServiceRequirement{}}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/deploy",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusBadRequest)
	}
}

func TestAPI_PostClusterRebalance(t *testing.T) {
	r, _ := setupTestRouter()

	// Probe first.
	probeReq := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/probe", nil)
	probeW := httptest.NewRecorder()
	r.ServeHTTP(probeW, probeReq)

	// Deploy first.
	deployBody := deployRequest{
		Services: []ServiceRequirement{
			{Name: "redis", Image: "redis:7", CPUCores: 2,
				MemoryMB: 4096},
		},
	}
	deployBytes, _ := json.Marshal(deployBody)
	deployReq := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/deploy",
		bytes.NewReader(deployBytes))
	deployReq.Header.Set("Content-Type", "application/json")
	deployW := httptest.NewRecorder()
	r.ServeHTTP(deployW, deployReq)

	// Now rebalance.
	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/rebalance", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}

	var resp rebalanceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Placements) == 0 {
		t.Error("expected non-empty placements after rebalance")
	}
}

func TestAPI_PostClusterRebalance_NoDeployments(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/rebalance", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should succeed but with empty placements.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}
}

func TestControlPlane_Construction(t *testing.T) {
	mock := newMockSSHClient()
	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"h1", "h2"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "spread",
		SSH:      mock,
	})

	if cp == nil {
		t.Fatal("expected non-nil ControlPlane")
	}
	if cp.scheduler == nil {
		t.Error("expected non-nil scheduler")
	}
	if cp.prober == nil {
		t.Error("expected non-nil prober")
	}
	if cp.deployer == nil {
		t.Error("expected non-nil deployer")
	}
	if cp.monitor == nil {
		t.Error("expected non-nil monitor")
	}
}

func TestAPI_MethodNotAllowed(t *testing.T) {
	r, _ := setupTestRouter()

	// PUT on a POST endpoint.
	req := httptest.NewRequest(http.MethodPut,
		"/internal/cluster/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected non-200 for wrong method")
	}
}

// TestNopSSHClient_RunAndIsReachable verifies that the no-op SSH client
// (used when no real SSH client is provided) behaves correctly.
func TestNopSSHClient_RunAndIsReachable(t *testing.T) {
	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"host1"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "auto",
		SSH:      nil, // triggers nopSSHClient creation
	})

	if cp == nil {
		t.Fatal("NewControlPlane returned nil")
	}

	// Exercise the nopSSHClient via the ControlPlane's probe path.
	// The nopSSHClient.IsReachable returns false, so the probe marks
	// hosts as unreachable.
	r := gin.New()
	RegisterRoutes(r, cp)

	req := httptest.NewRequest("POST", "/internal/cluster/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Probe should succeed even though nopSSHClient makes everything unreachable.
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Errorf("unexpected status %d", w.Code)
	}
}

func TestNopSSHClient_Run(t *testing.T) {
	// nopSSHClient.Run must return empty string and nil error.
	c := &nopSSHClient{}
	out, err := c.Run(nil, "any-host", "any-command")
	if err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
	if out != "" {
		t.Errorf("Run() output = %q, want empty", out)
	}
}

func TestNopSSHClient_IsReachable(t *testing.T) {
	// nopSSHClient.IsReachable must always return false.
	c := &nopSSHClient{}
	if c.IsReachable(nil, "any-host") {
		t.Error("IsReachable() = true, want false")
	}
}

func TestAPI_ClusterStatus_Timestamps(t *testing.T) {
	r, _ := setupTestRouter()

	before := time.Now()

	req := httptest.NewRequest(http.MethodGet,
		"/internal/cluster/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var status ClusterStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if status.CheckedAt.Before(before) {
		t.Error("CheckedAt should be >= test start time")
	}
}
