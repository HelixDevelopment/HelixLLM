package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validConfig returns a HelixConfig that is valid in EVERY respect, so that a
// test which mutates exactly one field is provably exercising the check it
// claims to. (A config that is merely empty fails on Mode long before reaching
// anything else — that would be a false pass.)
func validConfig() *HelixConfig {
	return &HelixConfig{
		Mode: "full",
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8443,
		},
		Log: LogConfig{
			Level:        "info",
			Format:       "text",
			OTELEndpoint: "http://localhost:4317",
		},
		Auth: AuthConfig{
			JWTSecret: "s3cr3t-value-from-the-vault-32b!",
		},
		DB: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Name:     "helixllm",
			User:     "helix",
			Password: "a-real-password",
		},
	}
}

// TestValidate_BaselineConfigIsOtherwiseValid guards the guards: if this ever
// fails, every "one bad field" test below is passing for the wrong reason.
func TestValidate_BaselineConfigIsOtherwiseValid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("baseline validConfig() must validate cleanly, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Entry point 1: Validate() — the startup path (cmd/helixllm calls
// config.Load() then cfg.Validate()) and any programmatic construction.
// ---------------------------------------------------------------------------

func TestValidate_RefusesUnexpandedPlaceholder(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*HelixConfig)
		wantField  string
		wantEnvVar string
	}{
		{
			name:       "jwt secret, plain ${VAR} form",
			mutate:     func(c *HelixConfig) { c.Auth.JWTSecret = "${HELIXLLM_JWT_SECRET}" },
			wantField:  "Auth.JWTSecret",
			wantEnvVar: "HELIXLLM_JWT_SECRET",
		},
		{
			name:       "db password, compose ${VAR:-default} form",
			mutate:     func(c *HelixConfig) { c.DB.Password = "${HELIX_DB_PASSWORD:-helixllm}" },
			wantField:  "DB.Password",
			wantEnvVar: "HELIX_DB_PASSWORD",
		},
		{
			name:       "embedded / partially substituted value",
			mutate:     func(c *HelixConfig) { c.Log.OTELEndpoint = "http://${OTEL_HOST}:4317" },
			wantField:  "Log.OTELEndpoint",
			wantEnvVar: "OTEL_HOST",
		},
		{
			name:       "top-level (non-nested) field",
			mutate:     func(c *HelixConfig) { c.Mode = "${HELIX_MODE}" },
			wantField:  "Mode",
			wantEnvVar: "HELIX_MODE",
		},
		{
			name:       "provider api key",
			mutate:     func(c *HelixConfig) { c.LLM.OpenAIKey = "${HELIX_LLM_OPENAI_KEY}" },
			wantField:  "LLM.OpenAIKey",
			wantEnvVar: "HELIX_LLM_OPENAI_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want refusal for unexpanded placeholder")
			}
			if !isPlaceholderError(err) {
				t.Fatalf("Validate() failed for the wrong reason: %v", err)
			}
			// The operator must be able to fix this in one step: the message
			// names the field AND the environment variable that was supposed
			// to fill it.
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Errorf("error does not name the field %q: %v", tt.wantField, err)
			}
			if !strings.Contains(err.Error(), tt.wantEnvVar) {
				t.Errorf("error does not name the env var %q: %v", tt.wantEnvVar, err)
			}
		})
	}
}

// TestValidate_AcceptsExpandedValues is the other half of the pair: the guard
// must not be satisfiable by simply rejecting everything. A correctly
// substituted config — including values that are structurally close to a
// placeholder — still loads.
func TestValidate_AcceptsExpandedValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*HelixConfig)
	}{
		{
			name:   "ordinary substituted secret",
			mutate: func(c *HelixConfig) { c.Auth.JWTSecret = "kJ8s0Zq2-real-secret-padded-32by" },
		},
		{
			name:   "secret containing a bare dollar",
			mutate: func(c *HelixConfig) { c.Auth.JWTSecret = "pa$$word-with-dollars-padded-32b" },
		},
		{
			name:   "secret containing braces but no substitution token",
			mutate: func(c *HelixConfig) { c.Auth.JWTSecret = "{json-ish}-secret-padded-to-32by" },
		},
		{
			name:   "shell-style $VAR without braces is not the compose token shape",
			mutate: func(c *HelixConfig) { c.LLM.ModelsDir = "$HOME/models" },
		},
		{
			name:   "empty optional secret (unset is a separate concern)",
			mutate: func(c *HelixConfig) { c.Auth.JWTSecret = "" },
		},
		{
			name:   "endpoint fully substituted",
			mutate: func(c *HelixConfig) { c.Log.OTELEndpoint = "http://otel-collector:4317" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil for a correctly-expanded config", err)
			}
		})
	}
}

// TestValidate_PlaceholderErrorDoesNotLeakSurroundingValue: the message must
// carry the placeholder token (which is public information) but never the rest
// of the field value, which may be a real, partially-substituted secret.
func TestValidate_PlaceholderErrorDoesNotLeakSurroundingValue(t *testing.T) {
	const realPart = "sk-live-do-not-log-this"

	cfg := validConfig()
	cfg.LLM.AnthropicKey = realPart + "${MISSING_SUFFIX}"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want refusal")
	}
	if !strings.Contains(err.Error(), "${MISSING_SUFFIX}") {
		t.Errorf("error should name the placeholder token: %v", err)
	}
	if strings.Contains(err.Error(), realPart) {
		t.Errorf("error leaked the surrounding field value: %v", err)
	}
}

// TestLoad_EnvPath_RefusesUnexpandedPlaceholder exercises the real operator
// path: an environment variable that was never substituted (compose
// `env_file:` is not interpolated, a Kubernetes ConfigMap does no expansion,
// a misspelled variable) reaches os.Getenv as a literal token.
func TestLoad_EnvPath_RefusesUnexpandedPlaceholder(t *testing.T) {
	t.Setenv("HELIX_AUTH_JWT_SECRET", "${HELIXLLM_JWT_SECRET}")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Everything else is at its default, so Validate() has no other reason to
	// fail — this isolates the placeholder.
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want refusal for an unsubstituted env var")
	}
	if !isPlaceholderError(err) {
		t.Fatalf("Validate() failed for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "HELIX_AUTH_JWT_SECRET") {
		t.Errorf("error should name the config field's env var: %v", err)
	}
	if !strings.Contains(err.Error(), "HELIXLLM_JWT_SECRET") {
		t.Errorf("error should name the unsubstituted variable: %v", err)
	}
}

func TestLoad_EnvPath_AcceptsExpandedValue(t *testing.T) {
	t.Setenv("HELIX_AUTH_JWT_SECRET", "an-actually-substituted-secret32")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for a substituted secret", err)
	}
	if cfg.Auth.JWTSecret != "an-actually-substituted-secret32" {
		t.Errorf("Auth.JWTSecret = %q, want the substituted value", cfg.Auth.JWTSecret)
	}
}

// ---------------------------------------------------------------------------
// Entry point 2: loadFromFile() — the JSON path used by ConfigWatcher on every
// hot reload. A reload must not be able to install a placeholder secret either.
// ---------------------------------------------------------------------------

func writeJSON(t *testing.T, cfg *HelixConfig) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadFromFile_RefusesUnexpandedPlaceholder(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.JWTSecret = "${HELIXLLM_JWT_SECRET}"
	path := writeJSON(t, cfg)

	got, err := loadFromFile(path)
	if err == nil {
		t.Fatalf("loadFromFile() = %+v, nil; want refusal for unexpanded placeholder", got)
	}
	if !isPlaceholderError(err) {
		t.Fatalf("loadFromFile() failed for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "Auth.JWTSecret") {
		t.Errorf("error does not name the field: %v", err)
	}
	if !strings.Contains(err.Error(), "HELIXLLM_JWT_SECRET") {
		t.Errorf("error does not name the env var: %v", err)
	}
	if got != nil {
		t.Errorf("loadFromFile() returned a config alongside the error: %+v", got)
	}
}

// TestLoadFromFile_AcceptsExpandedValues proves the file path is not simply
// rejecting every config — a correctly substituted file still loads, and the
// watcher's long-standing tolerance for partial configs is preserved.
func TestLoadFromFile_AcceptsExpandedValues(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.JWTSecret = "a-real-file-provided-secret-32by"
	path := writeJSON(t, cfg)

	got, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile() error = %v, want nil", err)
	}
	if got.Auth.JWTSecret != "a-real-file-provided-secret-32by" {
		t.Errorf("Auth.JWTSecret = %q, want the substituted value", got.Auth.JWTSecret)
	}
	if got.Mode != "full" {
		t.Errorf("Mode = %q, want %q", got.Mode, "full")
	}
}

// TestLoadFromFile_StillAcceptsPartialConfig pins the deliberate scope choice:
// only the placeholder rule was added to the file path, not full Validate().
// A partial config (the shape ConfigWatcher's callers have always written)
// keeps loading.
func TestLoadFromFile_StillAcceptsPartialConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"Mode":"gateway"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile() error = %v, want nil for a partial config", err)
	}
	if got.Mode != "gateway" {
		t.Errorf("Mode = %q, want %q", got.Mode, "gateway")
	}
}
