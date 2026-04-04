package control

import (
	"context"
	"sync"
	"time"
)

// Monitor provides health monitoring for the HelixLLM cluster.
// It wraps HostProber and caches the latest cluster status.
type Monitor struct {
	prober   *HostProber
	ssh      SSHClient
	interval time.Duration

	mu     sync.RWMutex
	status *ClusterStatus

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
	}
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

		// Run immediately.
		m.CheckCluster(mCtx, hosts)

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-mCtx.Done():
				return
			case <-ticker.C:
				m.CheckCluster(mCtx, hosts)
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
