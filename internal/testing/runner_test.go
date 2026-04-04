package testing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	helixtest "github.com/HelixDevelopment/HelixLLM/internal/testing"
)

// sampleBank is a minimal YAML bank used in tests.
const sampleBank = `
version: "1.0"
name: test-bank
challenges:
  - id: ch-001
    name: Health Check
    category: api
    priority: high
    steps:
      - name: ping
        action: GET /health
        expected: "ok"
  - id: ch-002
    name: Post Echo
    category: api
    priority: low
    steps:
      - name: echo
        action: POST /echo
        expected: "echo"
  - id: ch-003
    name: Security Category
    category: security
    priority: high
    steps:
      - name: check-headers
        action: GET /health
        expected: ""
`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"echo"}`))
	})
	return httptest.NewServer(mux)
}

func writeTempBank(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp bank: %v", err)
	}
	return path
}

func TestRunner_LoadBank(t *testing.T) {
	dir := t.TempDir()
	path := writeTempBank(t, dir, "bank.yaml", sampleBank)

	r := helixtest.NewRunner("http://localhost")
	if err := r.LoadBank(path); err != nil {
		t.Fatalf("LoadBank: %v", err)
	}
}

func TestRunner_LoadBank_NotFound(t *testing.T) {
	r := helixtest.NewRunner("http://localhost")
	if err := r.LoadBank("/no/such/file.yaml"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestRunner_LoadBank_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTempBank(t, dir, "bad.yaml", "}{invalid yaml{{")

	r := helixtest.NewRunner("http://localhost")
	if err := r.LoadBank(path); err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestRunner_LoadBanksDir(t *testing.T) {
	dir := t.TempDir()
	writeTempBank(t, dir, "a.yaml", sampleBank)
	writeTempBank(t, dir, "b.yml", sampleBank)
	writeTempBank(t, dir, "skip.txt", "not yaml")

	r := helixtest.NewRunner("http://localhost")
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}
}

func TestRunner_LoadBanksDir_NotFound(t *testing.T) {
	r := helixtest.NewRunner("http://localhost")
	if err := r.LoadBanksDir("/no/such/dir"); err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

func TestRunner_RunAll(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	writeTempBank(t, dir, "bank.yaml", sampleBank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	results := r.RunAll(context.Background())
	if len(results) != 3 {
		t.Fatalf("RunAll: got %d results, want 3", len(results))
	}

	for _, res := range results {
		if res.Status != "passed" {
			t.Errorf("challenge %s: status=%s error=%s", res.ID, res.Status, res.Error)
		}
	}
}

func TestRunner_RunByCategory(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	writeTempBank(t, dir, "bank.yaml", sampleBank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	results := r.RunByCategory(context.Background(), "security")
	if len(results) != 1 {
		t.Fatalf("RunByCategory(security): got %d results, want 1", len(results))
	}
	if results[0].ID != "ch-003" {
		t.Errorf("wrong challenge returned: %s", results[0].ID)
	}
	if results[0].Status != "passed" {
		t.Errorf("challenge status = %s, want passed", results[0].Status)
	}
}

func TestRunner_RunByPriority(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	writeTempBank(t, dir, "bank.yaml", sampleBank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	results := r.RunByPriority(context.Background(), "high")
	if len(results) != 2 {
		t.Fatalf("RunByPriority(high): got %d results, want 2", len(results))
	}
}

func TestRunner_RunAll_StepFail(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	bank := `
version: "1.0"
name: fail-bank
challenges:
  - id: ch-fail
    name: Will Fail
    category: api
    priority: high
    steps:
      - name: bad-expect
        action: GET /health
        expected: "THIS_WILL_NOT_MATCH"
`
	dir := t.TempDir()
	writeTempBank(t, dir, "fail.yaml", bank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	results := r.RunAll(context.Background())
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("status = %s, want failed", results[0].Status)
	}
	if results[0].Error == "" {
		t.Error("expected non-empty error on failure")
	}
}

func TestRunner_RunAll_UnsupportedAction(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	bank := `
version: "1.0"
name: skip-bank
challenges:
  - id: ch-skip
    name: Unsupported
    category: misc
    priority: low
    steps:
      - name: shell-cmd
        action: EXEC echo hello
        expected: ""
`
	dir := t.TempDir()
	writeTempBank(t, dir, "skip.yaml", bank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	results := r.RunAll(context.Background())
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	// Challenge with only skipped steps should be marked passed
	// (no step explicitly failed).
	if results[0].Status != "passed" {
		t.Errorf("status = %s, want passed", results[0].Status)
	}
	if len(results[0].Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(results[0].Steps))
	}
	if results[0].Steps[0].Status != "skipped" {
		t.Errorf("step status = %s, want skipped", results[0].Steps[0].Status)
	}
}

func TestRunner_RunAll_ContextCancelled(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	writeTempBank(t, dir, "bank.yaml", sampleBank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results := r.RunAll(ctx)
	// Results are still returned (one per challenge), but steps should be
	// skipped or the HTTP call should fail gracefully.
	if len(results) != 3 {
		t.Fatalf("want 3 results even with cancelled ctx, got %d", len(results))
	}
}

func TestRunner_Duration(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	writeTempBank(t, dir, "bank.yaml", sampleBank)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}

	results := r.RunAll(context.Background())
	for _, res := range results {
		if res.Duration < 0 {
			t.Errorf("challenge %s: negative duration %v", res.ID, res.Duration)
		}
	}
}
