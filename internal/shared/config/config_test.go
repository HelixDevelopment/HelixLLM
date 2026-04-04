package config_test

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != "full" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "full")
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8443 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8443)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("HELIX_MODE", "gateway")
	os.Setenv("HELIX_PORT", "9999")
	os.Setenv("HELIX_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("HELIX_MODE")
		os.Unsetenv("HELIX_PORT")
		os.Unsetenv("HELIX_LOG_LEVEL")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != "gateway" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "gateway")
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 9999)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := &config.HelixConfig{Mode: "invalid"}
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail for invalid mode")
	}

	cfg = &config.HelixConfig{Mode: "full"}
	cfg.Server.Port = 8443
	cfg.Log.Level = "info"
	cfg.Log.Format = "text"
	err = cfg.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v for valid config", err)
	}
}

func TestConfigValidation_InvalidPort(t *testing.T) {
	cfg := &config.HelixConfig{Mode: "full"}
	cfg.Server.Port = 0
	cfg.Log.Level = "info"
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail for port 0")
	}

	cfg.Server.Port = 70000
	err = cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail for port > 65535")
	}
}

func TestConfigValidation_InvalidLogLevel(t *testing.T) {
	cfg := &config.HelixConfig{Mode: "full"}
	cfg.Server.Port = 8443
	cfg.Log.Level = "trace"
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail for invalid log level")
	}
}

func TestConfigValidation_AllModes(t *testing.T) {
	modes := []string{"full", "gateway", "brain", "knowledge", "agents", "control"}
	for _, m := range modes {
		cfg := &config.HelixConfig{Mode: m}
		cfg.Server.Port = 8443
		cfg.Log.Level = "info"
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error for valid mode %q: %v", m, err)
		}
	}
}

func TestHostList(t *testing.T) {
	cfg := &config.HelixConfig{Hosts: "host1,host2,host3"}
	hosts := cfg.HostList()
	if len(hosts) != 3 {
		t.Fatalf("HostList() len = %d, want 3", len(hosts))
	}
	if hosts[0] != "host1" || hosts[1] != "host2" || hosts[2] != "host3" {
		t.Errorf("HostList() = %v, want [host1 host2 host3]", hosts)
	}
}

func TestHostList_Empty(t *testing.T) {
	cfg := &config.HelixConfig{Hosts: ""}
	hosts := cfg.HostList()
	if hosts != nil {
		t.Errorf("HostList() = %v, want nil for empty hosts", hosts)
	}
}

func TestHostList_TrimSpaces(t *testing.T) {
	cfg := &config.HelixConfig{Hosts: " host1 , host2 "}
	hosts := cfg.HostList()
	if len(hosts) != 2 {
		t.Fatalf("HostList() len = %d, want 2", len(hosts))
	}
	if hosts[0] != "host1" || hosts[1] != "host2" {
		t.Errorf("HostList() = %v, want [host1 host2]", hosts)
	}
}

func TestHostList_Single(t *testing.T) {
	cfg := &config.HelixConfig{Hosts: "nezha.local"}
	hosts := cfg.HostList()
	if len(hosts) != 1 {
		t.Fatalf("HostList() len = %d, want 1", len(hosts))
	}
	if hosts[0] != "nezha.local" {
		t.Errorf("HostList()[0] = %q, want %q", hosts[0], "nezha.local")
	}
}

func TestLoad_EnvParseError(t *testing.T) {
	// Setting HELIX_PORT to a non-integer forces env.Load to return a parse
	// error, exercising the `return nil, fmt.Errorf("loading config: %w", err)`
	// branch in Load().
	os.Setenv("HELIX_PORT", "not-a-number")
	defer os.Unsetenv("HELIX_PORT")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() should return error when HELIX_PORT is not a valid integer")
	}
}
