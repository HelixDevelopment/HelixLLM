package brain_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
)

// TestLlamaServer_HealthCheck_Healthy verifies that HealthCheck returns true
// when the mock server responds with HTTP 200.
func TestLlamaServer_HealthCheck_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Build a LlamaServer that points at the httptest server URL by injecting
	// the host/port. We use a port that the test server is actually listening on
	// via a custom client override — the simplest approach is to replace baseURL
	// using NewLlamaServer and relying on the real HTTP client, but since
	// httptest.NewServer binds to a random port we create the server with a
	// matching port extracted from the test server URL.
	//
	// Because LlamaServer.HealthCheck uses the internal http.Client with
	// baseURL = "http://127.0.0.1:{Port}", we instead use a helper that creates
	// the server and overrides its baseURL field via the exported constructor path.
	// The cleanest approach without unexported-field access: point the config
	// port at the httptest server port is not straightforward since httptest uses
	// a random port on 127.0.0.1.
	//
	// We use the exported BaseURL() method to confirm the base URL is set, and
	// exercise HealthCheck by pointing a LlamaServer at an httptest server
	// through a dedicated test helper that builds the server with the right URL.
	// Since LlamaServer does not expose a way to override the base URL directly
	// we test via the real port: httptest.Server.URL already uses 127.0.0.1 so
	// we parse the port and build a matching config.
	port := extractPort(t, srv.URL)

	cfg := brain.LlamaServerConfig{
		Port:         port,
		ModelsDir:    "/models",
		PresetsPath:  "/presets.yaml",
		MaxModels:    4,
		Threads:      8,
		ThreadsBatch: 512,
	}
	s := brain.NewLlamaServer(cfg)

	if !s.HealthCheck() {
		t.Error("HealthCheck() = false, want true (mock server returns 200)")
	}
}

// TestLlamaServer_HealthCheck_Unhealthy verifies that HealthCheck returns
// false when the target port is not listening (port 1 is reserved and always
// connection-refused in user-space).
func TestLlamaServer_HealthCheck_Unhealthy(t *testing.T) {
	cfg := brain.LlamaServerConfig{
		Port:         1, // nothing listening here
		ModelsDir:    "/models",
		PresetsPath:  "/presets.yaml",
		MaxModels:    4,
		Threads:      8,
		ThreadsBatch: 512,
	}
	s := brain.NewLlamaServer(cfg)

	if s.HealthCheck() {
		t.Error("HealthCheck() = true, want false (nothing listening on port 1)")
	}
}

// TestLlamaServerConfig_BuildArgs verifies that BuildArgs returns all expected
// flags in the correct order with the correct values.
func TestLlamaServerConfig_BuildArgs(t *testing.T) {
	cfg := brain.LlamaServerConfig{
		Port:         8090,
		ModelsDir:    "/var/lib/models",
		PresetsPath:  "/etc/presets.yaml",
		MaxModels:    8,
		Threads:      16,
		ThreadsBatch: 512,
	}

	args := cfg.BuildArgs()

	want := []string{
		"--host", "0.0.0.0",
		"--port", "8090",
		"--models-dir", "/var/lib/models",
		"--models-preset", "/etc/presets.yaml",
		"--models-max", "8",
		"--models-autoload",
		"--threads", "16",
		"--threads-batch", "512",
		"--metrics",
	}

	if len(args) != len(want) {
		t.Fatalf("BuildArgs() returned %d args, want %d\ngot:  %v\nwant: %v",
			len(args), len(want), args, want)
	}

	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, args[i], w)
		}
	}
}

// TestNewLlamaServer_BaseURL verifies that NewLlamaServer sets the expected
// base URL based on the configured port.
func TestNewLlamaServer_BaseURL(t *testing.T) {
	cfg := brain.LlamaServerConfig{Port: 8090}
	s := brain.NewLlamaServer(cfg)

	want := "http://127.0.0.1:8090"
	if s.BaseURL() != want {
		t.Errorf("BaseURL() = %q, want %q", s.BaseURL(), want)
	}
}

// ---------------------------------------------------------------------------
// ResolveLlamaServerBinary (HXC-233)
// ---------------------------------------------------------------------------
//
// These tests reproduce the two real-world states that mattered in the
// HXC-233 investigation: (a) an explicit HELIX_LLAMA_SERVER_BINARY_PATH that
// does/doesn't resolve, and (b) the default $PATH-lookup fallback that
// matches brain.LlamaServer.Start's own "" -> "llama-server" behaviour. On
// the live host investigated for HXC-233, "llama-server" was confirmed
// absent from every directory on $PATH (docs/qa/live_systemd_boot_.../
// 23_llama_server_search.txt) — TestResolveLlamaServerBinary_PATHLookup_NotFound
// pins that exact failure mode deterministically (own PATH, not the host's).

// newFakeExecutable creates an executable regular file at dir/name and
// returns its path. §11.4.81 honest gap: chmod-based executable bits are a
// POSIX concept; this helper is Unix-only, matching the deployment target
// investigated for HXC-233 (a systemd --user Linux host).
func newFakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("SKIP-OK: HXC-233 — chmod-executable-bit semantics are POSIX-only; no Windows equivalent test authored yet")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("newFakeExecutable: write %s: %v", p, err)
	}
	return p
}

// TestResolveLlamaServerBinary_ExplicitPath_Found verifies that a valid,
// executable HELIX_LLAMA_SERVER_BINARY_PATH resolves successfully and the
// $PATH environment is never consulted.
func TestResolveLlamaServerBinary_ExplicitPath_Found(t *testing.T) {
	dir := t.TempDir()
	bin := newFakeExecutable(t, dir, "llama-server")

	// Deliberately break $PATH so a pass here can only be explained by the
	// explicit path being honoured, not an accidental PATH hit.
	t.Setenv("PATH", t.TempDir())

	got, err := brain.ResolveLlamaServerBinary(bin)
	if err != nil {
		t.Fatalf("ResolveLlamaServerBinary(%q) returned error: %v", bin, err)
	}
	if got == "" {
		t.Fatal("ResolveLlamaServerBinary returned an empty path on success")
	}
}

// TestResolveLlamaServerBinary_ExplicitPath_NotFound verifies that a
// configured-but-nonexistent HELIX_LLAMA_SERVER_BINARY_PATH is reported as an
// error rather than silently falling back to a $PATH lookup.
func TestResolveLlamaServerBinary_ExplicitPath_NotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "llama-server-does-not-exist")

	_, err := brain.ResolveLlamaServerBinary(missing)
	if err == nil {
		t.Fatalf("ResolveLlamaServerBinary(%q) = nil error, want an error (path does not exist)", missing)
	}
}

// TestResolveLlamaServerBinary_PATHLookup_Found verifies the empty-path
// fallback: "" resolves "llama-server" via $PATH, mirroring
// brain.LlamaServer.Start's own internal fallback.
func TestResolveLlamaServerBinary_PATHLookup_Found(t *testing.T) {
	dir := t.TempDir()
	newFakeExecutable(t, dir, "llama-server")
	t.Setenv("PATH", dir)

	got, err := brain.ResolveLlamaServerBinary("")
	if err != nil {
		t.Fatalf("ResolveLlamaServerBinary(\"\") returned error with llama-server on PATH: %v", err)
	}
	if got == "" {
		t.Fatal("ResolveLlamaServerBinary returned an empty path on success")
	}
}

// TestResolveLlamaServerBinary_PATHLookup_NotFound reproduces the exact
// HXC-233 defect condition: llama-server is genuinely absent from every
// directory on $PATH. This is the RED case — the pre-flight MUST report an
// error here so main.go can skip the model download + Start() attempt instead
// of discovering the absence only after both have run.
func TestResolveLlamaServerBinary_PATHLookup_NotFound(t *testing.T) {
	empty := t.TempDir() // contains no "llama-server" (or anything else)
	t.Setenv("PATH", empty)

	_, err := brain.ResolveLlamaServerBinary("")
	if err == nil {
		t.Fatal("ResolveLlamaServerBinary(\"\") = nil error, want an error (llama-server not on PATH) — HXC-233 regression")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// extractPort parses the port number from a URL string such as
// "http://127.0.0.1:54321" and returns it as an int.
func extractPort(t *testing.T, rawURL string) int {
	t.Helper()
	// Format is always "http://127.0.0.1:<port>" from httptest.NewServer.
	var port int
	if _, err := fmt.Sscanf(rawURL, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("extractPort: cannot parse port from %q: %v", rawURL, err)
	}
	return port
}
