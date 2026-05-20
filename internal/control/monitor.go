package control

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// remediationLang is the locale used to render operator-facing
// RemediationAction.Reason strings. Background remediation runs without
// a request context, so it falls back to the server's default locale.
// CONST-046: remediation reasons MUST NOT be hardcoded English literals.
const remediationLang = "en"

// remediationTranslator resolves the localised RemediationAction.Reason
// templates. It shares the English defaults registered in the shared
// i18n package; operators MAY call LoadMessages to register more locales.
var remediationTranslator = i18n.New(remediationLang)

// RemediationAction records what the monitor decided to do in response
// to an unhealthy container.
type RemediationAction struct {
	// Type is one of "restart", "reschedule", or "alert".
	Type string
	// Host is the hostname where the action was (or would be) taken.
	Host string
	// Service is the name of the affected service / container.
	Service string
	// Reason describes why this action was chosen.
	Reason string
}

// remediationState tracks per-service restart attempt counts so that
// the monitor can escalate from restart → reschedule → alert.
type remediationState struct {
	restartAttempts map[string]int // service name → consecutive failures
}

// Remediator is the interface the Monitor calls to restart a container
// on a given host.  The Deployer satisfies this interface in production;
// tests supply a mock.
type Remediator interface {
	// RestartContainer stops and re-starts the named container on host.
	// Returns nil on success.
	RestartContainer(ctx context.Context, host, service string) error
}

// Monitor provides health monitoring for the HelixLLM cluster.
// It wraps HostProber and caches the latest cluster status.
type Monitor struct {
	prober     *HostProber
	ssh        SSHClient
	interval   time.Duration
	remediator Remediator // optional; nil disables active remediation

	mu     sync.RWMutex
	status *ClusterStatus

	remMu    sync.Mutex
	remState remediationState

	cancel context.CancelFunc
	done   chan struct{}
}

// NewMonitor creates a Monitor with the given prober and check
// interval.
func NewMonitor(
	prober *HostProber,
	ssh SSHClient,
	interval time.Duration,
) *Monitor {
	return &Monitor{
		prober:   prober,
		ssh:      ssh,
		interval: interval,
		remState: remediationState{
			restartAttempts: make(map[string]int),
		},
	}
}

// WithRemediator attaches a Remediator so that Remediate can perform
// active restarts.  Returns the receiver for chaining.
func (m *Monitor) WithRemediator(r Remediator) *Monitor {
	m.remediator = r
	return m
}

// maxRestartAttempts is the number of consecutive restart failures
// before the monitor escalates to rescheduling.
const maxRestartAttempts = 3

// Remediate inspects the current cluster status and attempts to heal
// any unhealthy deployments.  The logic is:
//
//  1. For each deployment in "failed" state, attempt a restart on the
//     same host (via the attached Remediator).
//  2. After maxRestartAttempts consecutive failures on one host, emit
//     a "reschedule" action (actual rescheduling is left to the caller).
//  3. When no healthy hosts are available at all, emit an "alert".
//
// Remediate is safe to call from multiple goroutines.
func (m *Monitor) Remediate(ctx context.Context) []RemediationAction {
	m.mu.RLock()
	status := m.status
	m.mu.RUnlock()

	if status == nil || len(status.Deployments) == 0 {
		return nil
	}

	// Collect healthy hosts for reschedule candidates.
	healthyHosts := make([]string, 0, len(status.Hosts))
	for _, h := range status.Hosts {
		if h.IsHealthy() {
			healthyHosts = append(healthyHosts, h.Name)
		}
	}

	m.remMu.Lock()
	defer m.remMu.Unlock()

	var actions []RemediationAction

	for _, dep := range status.Deployments {
		if dep.State != "failed" {
			// Container is healthy — reset its restart counter.
			delete(m.remState.restartAttempts, dep.ServiceName)
			continue
		}

		attempts := m.remState.restartAttempts[dep.ServiceName]

		if attempts >= maxRestartAttempts {
			// Too many restarts: try to reschedule.
			if len(healthyHosts) == 0 {
				actions = append(actions, RemediationAction{
					Type:    "alert",
					Host:    dep.HostName,
					Service: dep.ServiceName,
					Reason: remediationTranslator.T(remediationLang, i18n.KeyControlRemediationAlertNoHosts, map[string]string{
						"service":  dep.ServiceName,
						"attempts": strconv.Itoa(attempts),
					}),
				})
			} else {
				actions = append(actions, RemediationAction{
					Type:    "reschedule",
					Host:    healthyHosts[0],
					Service: dep.ServiceName,
					Reason: remediationTranslator.T(remediationLang, i18n.KeyControlRemediationReschedule, map[string]string{
						"service":  dep.ServiceName,
						"attempts": strconv.Itoa(attempts),
						"host":     dep.HostName,
						"target":   healthyHosts[0],
					}),
				})
				// Reset counter after rescheduling decision.
				delete(m.remState.restartAttempts, dep.ServiceName)
			}
			continue
		}

		// Attempt a restart.
		var restartErr error
		if m.remediator != nil {
			restartErr = m.remediator.RestartContainer(ctx, dep.HostName, dep.ServiceName)
		}

		if restartErr != nil {
			m.remState.restartAttempts[dep.ServiceName] = attempts + 1
			actions = append(actions, RemediationAction{
				Type:    "restart",
				Host:    dep.HostName,
				Service: dep.ServiceName,
				Reason: remediationTranslator.T(remediationLang, i18n.KeyControlRemediationRestartFailed, map[string]string{
					"attempt": strconv.Itoa(attempts + 1),
					"detail":  restartErr.Error(),
				}),
			})
		} else {
			// Restart succeeded (or no remediator); reset counter.
			delete(m.remState.restartAttempts, dep.ServiceName)
			actions = append(actions, RemediationAction{
				Type:    "restart",
				Host:    dep.HostName,
				Service: dep.ServiceName,
				Reason: remediationTranslator.T(remediationLang, i18n.KeyControlRemediationRestartOK, map[string]string{
					"service": dep.ServiceName,
					"host":    dep.HostName,
					"attempt": strconv.Itoa(attempts + 1),
				}),
			})
		}
	}

	return actions
}

// CheckCluster probes all hosts and returns the cluster status.
// The result is also cached for later retrieval via Status().
func (m *Monitor) CheckCluster(
	ctx context.Context, hosts []string,
) (*ClusterStatus, error) {
	status := &ClusterStatus{
		CheckedAt: time.Now(),
		Healthy:   true,
	}

	if len(hosts) == 0 {
		m.setStatus(status)
		return status, nil
	}

	profiles, errs := m.prober.ProbeAll(ctx, hosts)

	// Build a set of successfully probed hosts.
	probed := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		probed[p.Name] = true
		status.Hosts = append(status.Hosts, p)
	}

	// Add offline entries for hosts that failed probing.
	if len(errs) > 0 {
		for _, host := range hosts {
			if !probed[host] {
				status.Hosts = append(status.Hosts, HostProfile{
					Name:     host,
					Address:  host,
					State:    "offline",
					ProbedAt: time.Now(),
				})
			}
		}
	}

	// Determine overall health: all hosts must be online.
	for i := range status.Hosts {
		if !status.Hosts[i].IsHealthy() {
			status.Healthy = false
			break
		}
	}

	m.setStatus(status)
	return status, nil
}

// CheckHost probes a single host and returns its profile.
func (m *Monitor) CheckHost(
	ctx context.Context, host string,
) (*HostProfile, error) {
	return m.prober.ProbeHost(ctx, host)
}

// Status returns the last known cluster status, or nil if no check
// has been performed yet.
func (m *Monitor) Status() *ClusterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// Start begins periodic monitoring in the background. The monitor
// runs a check immediately, then repeats at the configured
// interval until the context is cancelled or Stop is called.
func (m *Monitor) Start(ctx context.Context, hosts []string) {
	mCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})

	go func() {
		defer close(m.done)

		// Run immediately then remediate any issues found.
		m.CheckCluster(mCtx, hosts)
		m.Remediate(mCtx)

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-mCtx.Done():
				return
			case <-ticker.C:
				m.CheckCluster(mCtx, hosts)
				m.Remediate(mCtx)
			}
		}
	}()
}

// Stop halts periodic monitoring. Safe to call multiple times.
func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.done != nil {
		<-m.done
		m.done = nil
	}
}

// setStatus stores the cluster status under a lock.
func (m *Monitor) setStatus(status *ClusterStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
}
