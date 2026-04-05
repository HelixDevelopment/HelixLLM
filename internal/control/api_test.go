package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

// setupOfflineRouter creates a router where all hosts are unreachable,
// so after probing no online hosts are available for scheduling.
func setupOfflineRouter() *gin.Engine {
	mock := newMockSSHClient()
	// Leave mock.reachable empty so all hosts appear unreachable.
	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"offline1", "offline2"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "auto",
		SSH:      mock,
	})
	r := gin.New()
	RegisterRoutes(r, cp)
	return r
}

func TestAPI_PostClusterDeploy_NoOnlineHosts_ScheduleError(t *testing.T) {
	// Probe first with all offline hosts — profiles will have no online hosts.
	r := setupOfflineRouter()

	probeReq := httptest.NewRequest(http.MethodPost, "/internal/cluster/probe", nil)
	probeW := httptest.NewRecorder()
	r.ServeHTTP(probeW, probeReq)

	// Now deploy — scheduler should fail because no online hosts.
	body := deployRequest{
		Services: []ServiceRequirement{
			{Name: "redis", Image: "redis:7", CPUCores: 2, MemoryMB: 4096},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/cluster/deploy",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Scheduler should return an error because all hosts are offline.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from scheduler error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_PostClusterRebalance_NoOnlineHosts_ScheduleError(t *testing.T) {
	// Set up a router with real hosts first, deploy, then swap to offline hosts
	// so that rebalance finds no schedulable hosts.
	r, _ := setupTestRouter()

	// Probe and deploy with online hosts.
	probeReq := httptest.NewRequest(http.MethodPost, "/internal/cluster/probe", nil)
	r.ServeHTTP(httptest.NewRecorder(), probeReq)

	deployBody := deployRequest{
		Services: []ServiceRequirement{
			{Name: "nginx", Image: "nginx:1", CPUCores: 1, MemoryMB: 512},
		},
	}
	deployBytes, _ := json.Marshal(deployBody)
	deployReq := httptest.NewRequest(http.MethodPost, "/internal/cluster/deploy",
		bytes.NewReader(deployBytes))
	deployReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), deployReq)

	// Now create a new offline router with existing deployments pre-set.
	mock2 := newMockSSHClient() // no reachable hosts
	cp2 := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"gone1"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "auto",
		SSH:      mock2,
	})
	// Inject a deployment so rebalance has work to do.
	cp2.deployments = []DeploymentInfo{
		{ServiceName: "nginx", HostName: "gone1", State: "running"},
	}
	r2 := gin.New()
	RegisterRoutes(r2, cp2)

	// Probe to set offline profiles.
	r2.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/internal/cluster/probe", nil))

	req := httptest.NewRequest(http.MethodPost, "/internal/cluster/rebalance", nil)
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from rebalance scheduler error, got %d: %s", w.Code, w.Body.String())
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

// partialFailSSH succeeds for probe commands (returning probe output)
// but makes the deploy run command fail by counting calls per host.
type partialFailSSH struct {
	mu          sync.Mutex
	probeOutput string
	runCounts   map[string]int
}

func newPartialFailSSH(probeOutput string) *partialFailSSH {
	return &partialFailSSH{
		probeOutput: probeOutput,
		runCounts:   make(map[string]int),
	}
}

func (p *partialFailSSH) Run(
	ctx context.Context, host, command string,
) (string, error) {
	p.mu.Lock()
	p.runCounts[host]++
	count := p.runCounts[host]
	p.mu.Unlock()
	// The first Run call per host is the probe compound command.
	// Subsequent calls are deployer commands — fail the second host.
	if count == 1 {
		return p.probeOutput, nil
	}
	// Fail deployments on the second host to exercise the errors loop.
	if host == "badhost" {
		return "", fmt.Errorf("ssh: deploy failed on %s", host)
	}
	return "", nil
}

func (p *partialFailSSH) IsReachable(_ context.Context, host string) bool {
	return true
}

func TestAPI_PostClusterDeploy_AutoProbe_NoProfiles(t *testing.T) {
	// Deploy WITHOUT probing first → handleDeploy auto-probes and
	// then schedules. Uses the standard two-host setup so both
	// hosts are reachable and the auto-probe path is exercised.
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.outputs["host1"] = linuxProbeOutput()

	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"host1"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "auto",
		SSH:      mock,
	})
	r := gin.New()
	RegisterRoutes(r, cp)

	// Do NOT probe first — profiles are empty.
	body := deployRequest{
		Services: []ServiceRequirement{
			{Name: "redis", Image: "redis:7", CPUCores: 1, MemoryMB: 512},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/deploy",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should auto-probe and succeed.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}
}

func TestAPI_PostClusterDeploy_PartialDeployFailure(t *testing.T) {
	// Two hosts: host1 deploys successfully, badhost fails.
	// Exercises the errors-appending loop in handleDeploy.
	mock := newPartialFailSSH(linuxProbeOutput())
	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"host1", "badhost"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "spread",
		SSH:      mock,
	})

	// Probe first to populate profiles.
	r := gin.New()
	RegisterRoutes(r, cp)
	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost,
			"/internal/cluster/probe", nil))

	body := deployRequest{
		Services: []ServiceRequirement{
			{Name: "svc1", Image: "img:1", CPUCores: 1, MemoryMB: 512},
			{Name: "svc2", Image: "img:2", CPUCores: 1, MemoryMB: 512},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/internal/cluster/deploy",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Some deployments should fail but the endpoint itself returns 200.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: %s",
			w.Code, http.StatusOK, w.Body.String())
	}
	var resp deployResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// At least one error should be reported.
	if len(resp.Errors) == 0 {
		t.Error("expected at least one deploy error in response")
	}
}

func TestAPI_PostClusterRebalance_PartialDeployFailure(t *testing.T) {
	// Exercises the errors-appending loop in handleRebalance.
	// host1 deploys successfully; badhost fails its deploy commands.
	mock := newPartialFailSSH(linuxProbeOutput())
	cp := NewControlPlane(ControlPlaneOptions{
		Hosts:    []string{"host1", "badhost"},
		SSHUser:  "user",
		SSHKey:   "key",
		Strategy: "spread",
		SSH:      mock,
	})

	// Probe to populate profiles.
	r := gin.New()
	RegisterRoutes(r, cp)
	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost,
			"/internal/cluster/probe", nil))

	// Seed deployments so rebalance has work to do.
	cp.deployments = []DeploymentInfo{
		{ServiceName: "svc1", HostName: "host1", State: "running"},
		{ServiceName: "svc2", HostName: "badhost", State: "running"},
	}

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
	// At least one error from the partial deploy failure.
	if len(resp.Errors) == 0 {
		t.Error("expected at least one rebalance deploy error in response")
	}
}
