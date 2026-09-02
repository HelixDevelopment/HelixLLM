package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	watcherpkg "digital.vasic.watcher/pkg/watcher"
)

// ConfigWatcher watches a JSON config file and calls onChange whenever the
// file is written.  It uses digital.vasic.watcher (fsnotify-backed) for
// efficient kernel-level notifications with built-in debouncing.
type ConfigWatcher struct {
	path     string
	onChange func(*HelixConfig)

	watcher watcherpkg.Watcher
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	once    sync.Once
}

// NewConfigWatcher creates a ConfigWatcher that will call onChange with the
// freshly-parsed config every time the file at path is modified.
//
// Call Start to begin watching; call Stop to release resources.
func NewConfigWatcher(path string, onChange func(*HelixConfig)) *ConfigWatcher {
	return &ConfigWatcher{
		path:     path,
		onChange: onChange,
	}
}

// Start begins watching the config file.  It returns an error if the watcher
// cannot be initialised or the path cannot be watched.  Start must be called
// at most once.
func (cw *ConfigWatcher) Start() error {
	cfg := watcherpkg.DefaultConfig()
	cfg.Recursive = false // only need to watch the single file's parent dir

	w, err := watcherpkg.New(cfg)
	if err != nil {
		return fmt.Errorf("config watcher: create fsnotify watcher: %w", err)
	}
	cw.watcher = w

	ctx, cancel := context.WithCancel(context.Background())
	cw.cancel = cancel

	// Watch the directory that contains the config file; fsnotify watches
	// directories, not individual files on all platforms.
	dir := dirOf(cw.path)
	if err := w.Watch(ctx, dir); err != nil {
		cancel()
		return fmt.Errorf("config watcher: watch %q: %w", dir, err)
	}

	cw.wg.Add(1)
	go cw.loop(ctx)

	return nil
}

// Stop tears down the watcher and waits for the background goroutine to exit.
// It is safe to call Stop multiple times.
func (cw *ConfigWatcher) Stop() {
	cw.once.Do(func() {
		if cw.cancel != nil {
			cw.cancel()
		}
		if cw.watcher != nil {
			_ = cw.watcher.Close()
		}
	})
	cw.wg.Wait()
}

// loop receives filesystem events and reloads the config on Write events that
// match the watched file path.
func (cw *ConfigWatcher) loop(ctx context.Context) {
	defer cw.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-cw.watcher.Events():
			if !ok {
				return
			}
			// We only care about writes to the exact config file.
			if ev.Type != watcherpkg.Write && ev.Type != watcherpkg.Create {
				continue
			}
			if !sameFile(ev.Path, cw.path) {
				continue
			}
			cfg, err := loadFromFile(cw.path)
			if err != nil {
				// Log-worthy but non-fatal: keep watching.
				continue
			}
			cw.onChange(cfg)

		case _, ok := <-cw.watcher.Errors():
			if !ok {
				return
			}
			// Watcher errors are non-fatal; keep running.
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// loadFromFile reads and parses a HelixConfig from a JSON file.
func loadFromFile(path string) (*HelixConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	cfg := &HelixConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	// The placeholder guard runs on the file path too, so a hot reload cannot
	// install a config whose secret is a literal "${...}" token. Only the
	// placeholder check runs here, not the full Validate(): the watcher has
	// always accepted partial configs (a JSON file carrying only the fields an
	// operator wants to change), and tightening that is a separate decision
	// from this security fix. The placeholder rule is the part that must hold
	// on every path.
	if err := checkNoUnexpandedPlaceholders(cfg); err != nil {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	return cfg, nil
}

// dirOf returns the directory component of a file path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			if i == 0 {
				return string(path[0])
			}
			return path[:i]
		}
	}
	return "."
}

// sameFile reports whether a and b refer to the same filesystem path using
// os.SameFile to handle symlinks and platform differences.
func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}
