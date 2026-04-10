package brain

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

// LlamaServerConfig holds the configuration needed to launch a llama-server
// process and connect to its HTTP API.
type LlamaServerConfig struct {
	BinaryPath   string
	Port         int
	ModelsDir    string
	PresetsPath  string
	MaxModels    int
	Threads      int
	ThreadsBatch int
}

// BuildArgs returns the command-line arguments for llama-server derived from
// the configuration. The argument order matches the expected positional order:
// --host, --port, --models-dir, --models-preset, --models-max,
// --models-autoload, --threads, --threads-batch, --metrics.
func (c *LlamaServerConfig) BuildArgs() []string {
	return []string{
		"--host", "0.0.0.0",
		"--port", strconv.Itoa(c.Port),
		"--models-dir", c.ModelsDir,
		"--models-preset", c.PresetsPath,
		"--models-max", strconv.Itoa(c.MaxModels),
		"--models-autoload",
		"--threads", strconv.Itoa(c.Threads),
		"--threads-batch", strconv.Itoa(c.ThreadsBatch),
		"--metrics",
	}
}

// LlamaServer manages the lifecycle of a llama-server child process and
// provides helpers to wait for readiness and perform health checks.
type LlamaServer struct {
	cfg     LlamaServerConfig
	cmd     *exec.Cmd
	baseURL string
	client  *http.Client
	mu      sync.Mutex
	wg      sync.WaitGroup
	cancel  context.CancelFunc
}

// NewLlamaServer creates a LlamaServer from the given configuration. The
// returned server is not yet started; call Start to launch the process.
func NewLlamaServer(cfg LlamaServerConfig) *LlamaServer {
	return &LlamaServer{
		cfg:     cfg,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Start spawns llama-server as a child process using exec.CommandContext. The
// process is supervised in a goroutine tracked by the internal WaitGroup so
// that Stop can wait for the goroutine to finish.
//
// If BinaryPath is empty, "llama-server" is resolved via PATH.
func (s *LlamaServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		return fmt.Errorf("llama-server: already started")
	}

	binary := s.cfg.BinaryPath
	if binary == "" {
		binary = "llama-server"
	}

	serverCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	cmd := exec.CommandContext(serverCtx, binary, s.cfg.BuildArgs()...)
	s.cmd = cmd

	if err := cmd.Start(); err != nil {
		cancel()
		s.cancel = nil
		return fmt.Errorf("llama-server: start process: %w", err)
	}

	log.WithFields(log.Fields{
		"pid":  cmd.Process.Pid,
		"port": s.cfg.Port,
	}).Info("llama-server process started")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := cmd.Wait(); err != nil {
			// Ignore errors caused by context cancellation (expected shutdown path).
			if serverCtx.Err() == nil {
				log.WithError(err).Warn("llama-server process exited unexpectedly")
			}
		} else {
			log.Info("llama-server process exited cleanly")
		}
	}()

	return nil
}

// WaitReady polls the /health endpoint every 500 ms until the server responds
// with HTTP 200 or the timeout elapses. It returns an error if the server does
// not become ready within the timeout.
func (s *LlamaServer) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("llama-server: context cancelled while waiting for ready: %w", ctx.Err())
		case t := <-ticker.C:
			if t.After(deadline) {
				return fmt.Errorf("llama-server: timed out after %s waiting for /health", timeout)
			}
			if s.HealthCheck() {
				log.WithField("port", s.cfg.Port).Info("llama-server is ready")
				return nil
			}
		}
	}
}

// Stop signals the llama-server process to terminate by calling the cancel
// function (which cancels the CommandContext) and sending SIGTERM. It then
// waits for the supervisor goroutine to finish via the WaitGroup.
func (s *LlamaServer) Stop() error {
	s.mu.Lock()
	cancel := s.cancel
	cmd := s.cmd
	s.mu.Unlock()

	if cancel == nil {
		return nil
	}

	cancel()

	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.WithError(err).Debug("llama-server: SIGTERM failed (process may have already exited)")
		}
	}

	s.wg.Wait()

	log.WithField("port", s.cfg.Port).Info("llama-server stopped")
	return nil
}

// HealthCheck performs a single GET request to /health and returns true if the
// server responds with HTTP 200.
func (s *LlamaServer) HealthCheck() bool {
	resp, err := s.client.Get(s.baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// BaseURL returns the base HTTP URL used to reach the llama-server API
// (e.g. "http://127.0.0.1:8080").
func (s *LlamaServer) BaseURL() string {
	return s.baseURL
}
