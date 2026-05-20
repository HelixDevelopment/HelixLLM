package control

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// controlTranslator localises user-facing control-plane API error
// messages. CONST-046: HTTP error strings MUST NOT be hardcoded English
// literals — each handler resolves them through this translator using
// the request's Accept-Language header.
var controlTranslator = i18n.New("en")

// controlLang extracts the preferred language tag from the request's
// Accept-Language header, falling back to "en" when absent.
func controlLang(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "en"
	}
	header := c.GetHeader("Accept-Language")
	if header == "" {
		return "en"
	}
	first := strings.TrimSpace(strings.Split(header, ",")[0])
	first = strings.TrimSpace(strings.Split(first, ";")[0])
	if first == "" || first == "*" {
		return "en"
	}
	if idx := strings.IndexAny(first, "-_"); idx > 0 {
		first = first[:idx]
	}
	return strings.ToLower(first)
}

// ControlPlaneOptions configures the ControlPlane.
type ControlPlaneOptions struct {
	// Hosts is the list of cluster hosts to manage.
	Hosts []string
	// SSHUser is the SSH username for all hosts.
	SSHUser string
	// SSHKey is the path to the SSH private key.
	SSHKey string
	// Strategy is the global scheduling strategy.
	Strategy string
	// SSH is the SSH client to use. If nil, a no-op client is
	// created (useful only for compilation; real usage requires
	// a real SSH client).
	SSH SSHClient
}

// ControlPlane coordinates probing, scheduling, deploying, and
// monitoring across the cluster.
type ControlPlane struct {
	prober    *HostProber
	scheduler *Scheduler
	deployer  *Deployer
	monitor   *Monitor
	hosts     []string

	mu          sync.RWMutex
	profiles    []HostProfile
	deployments []DeploymentInfo
}

// NewControlPlane creates a fully wired ControlPlane.
func NewControlPlane(opts ControlPlaneOptions) *ControlPlane {
	ssh := opts.SSH
	if ssh == nil {
		ssh = &nopSSHClient{}
	}

	prober := NewHostProber(ssh, opts.SSHUser, opts.SSHKey)
	deployer := NewDeployer(ssh, opts.SSHUser, opts.SSHKey)
	monitor := NewMonitor(prober, ssh, 30*time.Second)

	return &ControlPlane{
		prober:    prober,
		scheduler: NewScheduler(opts.Strategy),
		deployer:  deployer,
		monitor:   monitor,
		hosts:     opts.Hosts,
	}
}

// Status returns the current cluster status, populating deployments
// from the control plane's local cache.  Returns a fresh empty status
// when no health check has been performed yet.
func (cp *ControlPlane) Status() *ClusterStatus {
	status := cp.monitor.Status()
	if status == nil {
		status = &ClusterStatus{
			CheckedAt: time.Now(),
			Healthy:   true,
		}
	}
	cp.mu.RLock()
	status.Deployments = cp.deployments
	cp.mu.RUnlock()
	return status
}

// RegisterRoutes registers the control API routes on the Gin
// engine.
func RegisterRoutes(r *gin.Engine, cp *ControlPlane) {
	g := r.Group("/internal/cluster")
	g.GET("/status", cp.handleStatus)
	g.POST("/probe", cp.handleProbe)
	g.POST("/deploy", cp.handleDeploy)
	g.POST("/rebalance", cp.handleRebalance)
}

// handleStatus returns the current cluster status.
func (cp *ControlPlane) handleStatus(c *gin.Context) {
	status := cp.monitor.Status()
	if status == nil {
		// No check has been performed yet; return a fresh status.
		status = &ClusterStatus{
			CheckedAt: time.Now(),
			Healthy:   true,
		}
	}

	cp.mu.RLock()
	status.Deployments = cp.deployments
	cp.mu.RUnlock()

	c.JSON(http.StatusOK, status)
}

// handleProbe triggers a probe of all configured hosts.
func (cp *ControlPlane) handleProbe(c *gin.Context) {
	clusterStatus, err := cp.monitor.CheckCluster(
		c.Request.Context(), cp.hosts,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	cp.mu.Lock()
	cp.profiles = clusterStatus.Hosts
	cp.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"hosts": clusterStatus.Hosts,
	})
}

// deployRequest is the request body for POST /internal/cluster/deploy.
type deployRequest struct {
	Services []ServiceRequirement `json:"services"`
}

// deployResponse is the response body for POST /internal/cluster/deploy.
type deployResponse struct {
	Deployments []DeploymentInfo  `json:"deployments"`
	Placements  []PlacementResult `json:"placements"`
	Errors      []string          `json:"errors,omitempty"`
}

// handleDeploy schedules and deploys services to the cluster.
func (cp *ControlPlane) handleDeploy(c *gin.Context) {
	var req deployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": controlTranslator.T(controlLang(c), i18n.KeyGatewayInvalidRequestBody,
				map[string]string{"detail": err.Error()}),
		})
		return
	}
	if len(req.Services) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": controlTranslator.T(controlLang(c), i18n.KeyControlNoServicesSpecified),
		})
		return
	}

	// Get current host profiles.
	cp.mu.RLock()
	profiles := cp.profiles
	cp.mu.RUnlock()

	// If no profiles, probe first.
	if len(profiles) == 0 {
		clusterStatus, err := cp.monitor.CheckCluster(
			c.Request.Context(), cp.hosts,
		)
		if err == nil {
			profiles = clusterStatus.Hosts
			cp.mu.Lock()
			cp.profiles = profiles
			cp.mu.Unlock()
		}
	}

	// Schedule placement.
	placements, err := cp.scheduler.Schedule(profiles, req.Services)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": controlTranslator.T(controlLang(c), i18n.KeyControlSchedulingFailed,
				map[string]string{"detail": err.Error()}),
		})
		return
	}

	// Deploy.
	infos, errs := cp.deployer.DeployAll(
		c.Request.Context(), placements,
	)

	cp.mu.Lock()
	cp.deployments = append(cp.deployments, infos...)
	cp.mu.Unlock()

	resp := deployResponse{
		Deployments: infos,
		Placements:  placements,
	}
	for _, e := range errs {
		resp.Errors = append(resp.Errors, e.Error())
	}

	c.JSON(http.StatusOK, resp)
}

// rebalanceResponse is the response body for POST
// /internal/cluster/rebalance.
type rebalanceResponse struct {
	Placements  []PlacementResult `json:"placements"`
	Deployments []DeploymentInfo  `json:"deployments"`
	Errors      []string          `json:"errors,omitempty"`
}

// handleRebalance re-evaluates and rebalances placement of existing
// deployments.
func (cp *ControlPlane) handleRebalance(c *gin.Context) {
	cp.mu.RLock()
	deployments := cp.deployments
	profiles := cp.profiles
	cp.mu.RUnlock()

	if len(deployments) == 0 {
		c.JSON(http.StatusOK, rebalanceResponse{})
		return
	}

	// Convert existing deployments to service requirements.
	services := make(
		[]ServiceRequirement, 0, len(deployments),
	)
	for _, d := range deployments {
		services = append(services, ServiceRequirement{
			Name:  d.ServiceName,
			Image: d.ServiceName, // Placeholder; real impl would track the original image.
		})
	}

	// Re-schedule.
	placements, err := cp.scheduler.Schedule(profiles, services)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": controlTranslator.T(controlLang(c), i18n.KeyControlRebalanceFailed,
				map[string]string{"detail": err.Error()}),
		})
		return
	}

	// Re-deploy.
	infos, errs := cp.deployer.DeployAll(
		c.Request.Context(), placements,
	)

	cp.mu.Lock()
	cp.deployments = infos
	cp.mu.Unlock()

	resp := rebalanceResponse{
		Placements:  placements,
		Deployments: infos,
	}
	for _, e := range errs {
		resp.Errors = append(resp.Errors, e.Error())
	}

	c.JSON(http.StatusOK, resp)
}

// nopSSHClient is a no-op SSH client used when no real SSH client
// is provided. All hosts appear unreachable.
type nopSSHClient struct{}

func (n *nopSSHClient) Run(
	_ context.Context, host, command string,
) (string, error) {
	return "", nil
}

func (n *nopSSHClient) IsReachable(
	_ context.Context, host string,
) bool {
	return false
}
