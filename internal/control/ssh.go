package control

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// RealSSHClient implements SSHClient using golang.org/x/crypto/ssh.
// It connects to each host on the configured port using the configured
// user and key, executing commands over individual SSH sessions.
type RealSSHClient struct {
	port    int
	user    string
	keyPath string
	signer  ssh.Signer
}

// NewSSHClient creates a RealSSHClient. keyPath supports "~" expansion.
// It reads and parses the private key at construction time so that
// key loading errors are surfaced eagerly.
func NewSSHClient(host string, port int, user, keyPath string) (*RealSSHClient, error) {
	expanded := expandTilde(keyPath)
	keyData, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("ssh: read key %q: %w", expanded, err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("ssh: parse key %q: %w", expanded, err)
	}

	return &RealSSHClient{
		port:    port,
		user:    user,
		keyPath: expanded,
		signer:  signer,
	}, nil
}

// Run executes command on host, returning combined stdout output.
// The host parameter from the interface is used for the connection;
// the host argument passed to NewSSHClient is unused at runtime.
func (c *RealSSHClient) Run(ctx context.Context, host, command string) (string, error) {
	client, err := c.dial(ctx, host)
	if err != nil {
		return "", fmt.Errorf("ssh run %s: %w", host, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh session %s: %w", host, err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout

	if err := session.Run(command); err != nil {
		return "", fmt.Errorf("ssh exec %s: %w", host, err)
	}

	return stdout.String(), nil
}

// IsReachable returns true if an SSH connection to host can be
// established within a short timeout (5 seconds).
func (c *RealSSHClient) IsReachable(ctx context.Context, host string) bool {
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client, err := c.dial(tctx, host)
	if err != nil {
		return false
	}
	client.Close()
	return true
}

// dial opens an authenticated SSH connection to the given host.
func (c *RealSSHClient) dial(ctx context.Context, host string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: c.user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(c.signer),
		},
		// HostKeyCallback is set to InsecureIgnoreHostKey for cluster-internal
		// use. Replace with a known_hosts callback for stricter security.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, c.port)

	// Respect context cancellation during TCP dial.
	var conn net.Conn
	var err error
	d := &net.Dialer{}
	conn, err = d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", addr, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

// expandTilde replaces a leading "~" with the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
