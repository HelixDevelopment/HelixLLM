package control

import (
	"testing"
	"time"
)

func TestRenderStatus_WithHostsAndDeployments(t *testing.T) {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "user", "key")
	monitor := NewMonitor(prober, mock, 30*time.Second)

	// Set monitor status directly to avoid the racy concurrent ProbeAll call.
	monitor.setStatus(&ClusterStatus{
		CheckedAt: time.Now(),
		Healthy:   true,
		Hosts: []HostProfile{
			{
				Name:  "host1",
				State: "online",
				CPU:   CPUInfo{Cores: 32},
				Memory: MemoryInfo{TotalBytes: 64000 * 1024 * 1024},
			},
			{
				Name:  "host2",
				State: "online",
				CPU:   CPUInfo{Cores: 16},
				Memory: MemoryInfo{TotalBytes: 32000 * 1024 * 1024},
			},
		},
	})

	cp := &ControlPlane{
		prober:    prober,
		scheduler: NewScheduler(""),
		deployer:  NewDeployer(mock, "user", "key"),
		monitor:   monitor,
		hosts:     []string{"host1", "host2"},
		deployments: []DeploymentInfo{
			{ServiceName: "redis", HostName: "host1", State: "running"},
			{ServiceName: "app", HostName: "host2", State: "running"},
		},
	}

	// Should not panic when rendering hosts with deployments.
	renderStatus(cp)
}

func TestRenderStatus_DegradedCluster(t *testing.T) {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "user", "key")
	monitor := NewMonitor(prober, mock, 30*time.Second)

	// Set a degraded status directly.
	monitor.setStatus(&ClusterStatus{
		CheckedAt: time.Now(),
		Healthy:   false,
		Hosts: []HostProfile{
			{
				Name:  "host1",
				State: "online",
				CPU:   CPUInfo{Cores: 32},
				Memory: MemoryInfo{TotalBytes: 64000 * 1024 * 1024},
			},
			{
				Name:  "host2",
				State: "offline",
				CPU:   CPUInfo{},
				Memory: MemoryInfo{},
			},
		},
	})

	cp := &ControlPlane{
		prober:    prober,
		scheduler: NewScheduler(""),
		deployer:  NewDeployer(mock, "user", "key"),
		monitor:   monitor,
		hosts:     []string{"host1", "host2"},
	}

	// Should not panic; should show DEGRADED status.
	renderStatus(cp)
}

func TestRenderStatus_NoHostsConfigured(t *testing.T) {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "user", "key")

	cp := &ControlPlane{
		prober:    prober,
		scheduler: NewScheduler(""),
		deployer:  NewDeployer(mock, "user", "key"),
		monitor:   NewMonitor(prober, mock, 30*time.Second),
		hosts:     nil,
	}

	// Should print "No hosts configured." without panicking.
	renderStatus(cp)
}
