package control

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// defaultRuntime is the container runtime used when none is
// specified.
const defaultRuntime = "podman"

// Deployer deploys containers to hosts via SSH.
type Deployer struct {
	ssh     SSHClient
	sshUser string
	sshKey  string
	runtime string
}

// NewDeployer creates a Deployer that uses the given SSH client.
func NewDeployer(
	ssh SSHClient, sshUser, sshKey string,
) *Deployer {
	return &Deployer{
		ssh:     ssh,
		sshUser: sshUser,
		sshKey:  sshKey,
		runtime: defaultRuntime,
	}
}

// SetRuntime overrides the default container runtime.
func (d *Deployer) SetRuntime(runtime string) {
	if runtime != "" {
		d.runtime = runtime
	}
}

// Deploy deploys a single service to the host specified in the
// placement result. It removes any existing container with the
// same name, then starts a new one.
func (d *Deployer) Deploy(
	ctx context.Context,
	placement PlacementResult,
) (*DeploymentInfo, error) {
	host := placement.HostName
	name := placement.Service.Name
	image := placement.Service.Image
	rt := d.runtime

	if !d.ssh.IsReachable(ctx, host) {
		return &DeploymentInfo{
			ServiceName: name,
			HostName:    host,
			State:       "failed",
			// CONST-046: operator-facing Error field resolved via i18n.
			Error: remediationTranslator.T(remediationLang, i18n.KeyControlHostUnreachable,
				map[string]string{"host": host}),
		}, fmt.Errorf("host %q is unreachable", host)
	}

	// Step 1: Remove existing container (ignore errors).
	removeCmd := fmt.Sprintf(
		"%s rm -f %s 2>/dev/null || true", rt, name,
	)
	_, err := d.ssh.Run(ctx, host, removeCmd)
	if err != nil {
		return &DeploymentInfo{
			ServiceName: name,
			HostName:    host,
			State:       "failed",
			Error:       err.Error(),
		}, fmt.Errorf("deploy %s on %s: remove: %w", name, host, err)
	}

	// Step 2: Run the container.
	runCmd := fmt.Sprintf(
		"%s run -d --name %s %s", rt, name, image,
	)
	_, err = d.ssh.Run(ctx, host, runCmd)
	if err != nil {
		return &DeploymentInfo{
			ServiceName: name,
			HostName:    host,
			State:       "failed",
			Error:       err.Error(),
		}, fmt.Errorf("deploy %s on %s: run: %w", name, host, err)
	}

	return &DeploymentInfo{
		ServiceName: name,
		HostName:    host,
		State:       "running",
		DeployedAt:  time.Now(),
	}, nil
}

// DeployAll deploys multiple placements concurrently. Returns
// successful deployments and any errors.
func (d *Deployer) DeployAll(
	ctx context.Context,
	placements []PlacementResult,
) ([]DeploymentInfo, []error) {
	if len(placements) == 0 {
		return nil, nil
	}

	type result struct {
		info *DeploymentInfo
		err  error
	}

	results := make([]result, len(placements))
	var wg sync.WaitGroup

	for i, p := range placements {
		wg.Add(1)
		go func(idx int, pl PlacementResult) {
			defer wg.Done()
			info, err := d.Deploy(ctx, pl)
			results[idx] = result{info: info, err: err}
		}(i, p)
	}

	wg.Wait()

	var infos []DeploymentInfo
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
		}
		if r.info != nil && r.info.State == "running" {
			infos = append(infos, *r.info)
		}
	}

	return infos, errs
}

// RestartContainer stops and re-starts the named container on host,
// satisfying the Remediator interface used by Monitor.Remediate.
func (d *Deployer) RestartContainer(
	ctx context.Context, host, service string,
) error {
	rt := d.runtime
	cmd := fmt.Sprintf(
		"%s restart %s", rt, service,
	)
	_, err := d.ssh.Run(ctx, host, cmd)
	if err != nil {
		return fmt.Errorf("restart %s on %s: %w", service, host, err)
	}
	return nil
}

// Undeploy stops and removes a container on its host.
func (d *Deployer) Undeploy(
	ctx context.Context,
	deployment DeploymentInfo,
) error {
	rt := d.runtime
	cmd := fmt.Sprintf(
		"%s stop %s 2>/dev/null; %s rm -f %s 2>/dev/null || true",
		rt, deployment.ServiceName,
		rt, deployment.ServiceName,
	)

	_, err := d.ssh.Run(ctx, deployment.HostName, cmd)
	if err != nil {
		return fmt.Errorf(
			"undeploy %s on %s: %w",
			deployment.ServiceName, deployment.HostName, err,
		)
	}
	return nil
}
