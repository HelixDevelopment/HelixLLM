package config_test

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
)

// writeConfigFile marshals cfg as JSON and writes it to path.
func writeConfigFile(t *testing.T, path string, cfg *config.HelixConfig) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

func TestConfigWatcher_CallsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/helix.json"

	initial := &config.HelixConfig{Mode: "full"}
	writeConfigFile(t, path, initial)

	var (
		mu      sync.Mutex
		got     *config.HelixConfig
		gotOnce sync.Once
		done    = make(chan struct{})
	)

	cw := config.NewConfigWatcher(path, func(c *config.HelixConfig) {
		mu.Lock()
		got = c
		mu.Unlock()
		gotOnce.Do(func() { close(done) })
	})

	if err := cw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer cw.Stop()

	// Give the watcher a moment to initialise before writing.
	time.Sleep(50 * time.Millisecond)

	updated := &config.HelixConfig{Mode: "gateway"}
	writeConfigFile(t, path, updated)

	select {
	case <-done:
		// callback fired
	case <-time.After(3 * time.Second):
		t.Fatal("onChange not called within 3 seconds after file write")
	}

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("onChange received nil config")
	}
	if got.Mode != "gateway" {
		t.Errorf("Mode = %q, want %q", got.Mode, "gateway")
	}
}

func TestConfigWatcher_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/helix.json"

	writeConfigFile(t, path, &config.HelixConfig{Mode: "full"})

	var (
		mu    sync.Mutex
		modes []string
	)

	cw := config.NewConfigWatcher(path, func(c *config.HelixConfig) {
		mu.Lock()
		modes = append(modes, c.Mode)
		mu.Unlock()
	})

	if err := cw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer cw.Stop()

	time.Sleep(50 * time.Millisecond)

	writes := []string{"brain", "agents", "control"}
	for _, mode := range writes {
		writeConfigFile(t, path, &config.HelixConfig{Mode: mode})
		time.Sleep(200 * time.Millisecond) // allow debounce + delivery
	}

	// Wait for all callbacks to arrive (generous timeout).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(modes)
		mu.Unlock()
		if n >= len(writes) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(modes) == 0 {
		t.Fatal("onChange never called after multiple writes")
	}
	// The last mode seen must be the last write.
	last := modes[len(modes)-1]
	if last != "control" {
		t.Errorf("last mode = %q, want %q", last, "control")
	}
}

func TestConfigWatcher_Stop_IdempotentAndSafe(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/helix.json"
	writeConfigFile(t, path, &config.HelixConfig{Mode: "full"})

	cw := config.NewConfigWatcher(path, func(_ *config.HelixConfig) {})

	if err := cw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Multiple Stop calls must not panic or deadlock.
	cw.Stop()
	cw.Stop()
	cw.Stop()
}

func TestConfigWatcher_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/missing.json"

	// Write a valid file so Start can watch the directory, but the file
	// itself is absent initially.  The watcher watches the directory.
	// Start should succeed (directory exists); a write event for a missing
	// file should just be skipped gracefully.
	cw := config.NewConfigWatcher(path, func(_ *config.HelixConfig) {})
	if err := cw.Start(); err != nil {
		t.Fatalf("Start with missing file: %v", err)
	}
	defer cw.Stop()
}

func TestNewConfigWatcher_ReturnsNonNil(t *testing.T) {
	cw := config.NewConfigWatcher("/tmp/test.json", func(_ *config.HelixConfig) {})
	if cw == nil {
		t.Fatal("NewConfigWatcher returned nil")
	}
}
