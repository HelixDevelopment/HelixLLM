package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These tests build on validConfig() from placeholder_test.go: a config that is
// valid in EVERY respect, mutated in exactly one field. Anything built from an
// empty HelixConfig would die on Mode long before reaching a credential check,
// which would be a false pass.

// ---------------------------------------------------------------------------
// Entry point 1: Validate() — the startup path.
// ---------------------------------------------------------------------------

func TestValidate_RefusesBlankSecret(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*HelixConfig)
		wantField  string
		wantEnvVar string
	}{
		{
			// The operator's motivating case: a signing key that is present but
			// carries nothing. Refused even though nothing consumes it yet, so
			// the guard is already in place when JWT auth is wired.
			name:       "jwt signing key, single space",
			mutate:     func(c *HelixConfig) { c.Auth.JWTSecret = " " },
			wantField:  "Auth.JWTSecret",
			wantEnvVar: "HELIX_AUTH_JWT_SECRET",
		},
		{
			// The anchor case. A whitespace-only key list is neither open access
			// nor key-protected: the middleware rejects EVERY request, including
			// one presenting the key the operator believes is configured.
			name:       "api key list, spaces only",
			mutate:     func(c *HelixConfig) { c.Auth.APIKeys = "   " },
			wantField:  "Auth.APIKeys",
			wantEnvVar: "HELIX_AUTH_API_KEYS",
		},
		{
			name:       "provider key, tab",
			mutate:     func(c *HelixConfig) { c.LLM.AnthropicKey = "\t" },
			wantField:  "LLM.AnthropicKey",
			wantEnvVar: "HELIX_LLM_ANTHROPIC_KEY",
		},
		{
			name:       "database password, newline",
			mutate:     func(c *HelixConfig) { c.DB.Password = "\n" },
			wantField:  "DB.Password",
			wantEnvVar: "HELIX_DB_PASSWORD",
		},
		{
			name:       "redis password, mixed whitespace",
			mutate:     func(c *HelixConfig) { c.Cache.RedisPassword = " \t\n " },
			wantField:  "Cache.RedisPassword",
			wantEnvVar: "HELIX_REDIS_PASSWORD",
		},
		{
			name:       "path to a private key",
			mutate:     func(c *HelixConfig) { c.Server.TLSKey = "  " },
			wantField:  "Server.TLSKey",
			wantEnvVar: "HELIX_TLS_KEY",
		},
		{
			name:       "top-level (non-nested) credential field",
			mutate:     func(c *HelixConfig) { c.SSHKey = " " },
			wantField:  "SSHKey",
			wantEnvVar: "HELIX_SSH_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want refusal for a blank credential")
			}
			if !isBlankSecretError(err) {
				t.Fatalf("Validate() failed for the wrong reason: %v", err)
			}
			// The operator must be able to fix this in one step: the message
			// names the field AND the variable to export.
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Errorf("error does not name the field %q: %v", tt.wantField, err)
			}
			if !strings.Contains(err.Error(), tt.wantEnvVar) {
				t.Errorf("error does not name the env var %q: %v", tt.wantEnvVar, err)
			}
		})
	}
}

// TestValidate_AcceptsEmptyOptionalCredentials is the NEGATIVE CONTROL, and the
// most important test here. Without it the guard could be satisfied by refusing
// everything — which would be an outage, because absence is how this codebase
// says "this feature is off". Every case below is a configuration that works
// today and must keep working.
//
// These cases pin the CONTRACT ("an absent credential is never refused"), not
// the downstream behaviour of the components cited in their comments. The
// citations say WHY absence is legitimate; they are not exercised here — a
// strict-direction regression in the guard is what trips these cases. Several
// are already the baseline's state, which is deliberate: they must keep holding
// if someone later changes validConfig().
func TestValidate_AcceptsEmptyOptionalCredentials(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*HelixConfig)
	}{
		{
			// internal/brain/brain.go:91-129 — an unset provider key means the
			// provider is simply not registered. The normal case for all but
			// the one or two providers an operator actually uses.
			name: "optional provider key unset",
			mutate: func(c *HelixConfig) {
				c.LLM.AnthropicKey = ""
				c.LLM.OpenAIKey = ""
				c.LLM.ChutesKey = ""
				c.LLM.TogetherKey = ""
			},
		},
		{
			// configuration.md:130 — "Leave empty to disable JWT auth."
			name:   "jwt secret unset (documented off-switch)",
			mutate: func(c *HelixConfig) { c.Auth.JWTSecret = "" },
		},
		{
			// auth.go:29-33 — empty is open-access mode by design.
			name:   "api keys unset (open-access mode)",
			mutate: func(c *HelixConfig) { c.Auth.APIKeys = "" },
		},
		{
			name:   "database password unset",
			mutate: func(c *HelixConfig) { c.DB.Password = "" },
		},
		{
			name:   "redis password unset (auth-less redis)",
			mutate: func(c *HelixConfig) { c.Cache.RedisPassword = "" },
		},
		{
			// The shape of the shipped .env.example: every credential blank.
			name: "every credential unset at once",
			mutate: func(c *HelixConfig) {
				c.SSHKey = ""
				c.Auth = AuthConfig{}
				c.DB.Password = ""
				c.Cache.RedisPassword = ""
				c.LLM.OpenAIKey = ""
				c.LLM.AnthropicKey = ""
			},
		},
		{
			// A real secret that merely contains whitespace is untouched.
			name:   "real secret containing spaces",
			mutate: func(c *HelixConfig) { c.Auth.APIKeys = "key-one, key-two" },
		},
		{
			name:   "real secret with leading and trailing spaces",
			mutate: func(c *HelixConfig) { c.Auth.JWTSecret = "  a-real-secret-padded-to-32by!  " },
		},
		{
			// brain.go:80-88 — a deliberately keyless OpenAI-compatible
			// provider (Ollama) is a supported configuration. Validate() does not
			// read OpenAIBaseURL; the field is set here so the case documents the
			// real-world shape it protects.
			name: "keyless openai-compatible provider",
			mutate: func(c *HelixConfig) {
				c.LLM.OpenAIKey = ""
				c.LLM.OpenAIBaseURL = "http://localhost:11434/v1"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil — this configuration works today", err)
			}
		})
	}
}

// TestValidate_BlankNonCredentialFieldIsNotThisGuardsBusiness pins the scope:
// the guard refuses blank CREDENTIALS, not every blank string. A blank
// non-credential field is a different problem with its own loud failure, and
// sweeping it in here would turn this into a "refuse everything" check.
func TestValidate_BlankNonCredentialFieldIsNotThisGuardsBusiness(t *testing.T) {
	cfg := validConfig()
	cfg.Hosts = "   "

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil: Hosts is not a credential", err)
	}
}

// TestValidate_BlankSecretErrorDoesNotLeakAnyValue enforces the promise made in
// requiredsecret.go: no field value ever reaches the message — not the
// offending field's, and not a neighbouring credential's. The offending value
// is a distinctive run of control characters, all of which are whitespace (so
// it is refused) and none of which occur in the real single-line message, so
// the assertion fires the moment the value is echoed.
func TestValidate_BlankSecretErrorDoesNotLeakAnyValue(t *testing.T) {
	const offending = "\t\v\f\r\n"

	cfg := validConfig()
	cfg.Auth.APIKeys = offending

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want refusal")
	}
	if !isBlankSecretError(err) {
		t.Fatalf("Validate() failed for the wrong reason: %v", err)
	}
	if strings.ContainsAny(err.Error(), offending) {
		t.Errorf("error echoed the offending field's value: %q", err.Error())
	}
	// The config being refused also carries real credentials in neighbouring
	// fields; none of them may appear either.
	for _, secret := range []string{
		"s3cr3t-value-from-the-vault-32b!", // Auth.JWTSecret
		"a-real-password",                  // DB.Password
	} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaked a neighbouring credential value: %v", err)
		}
	}
}

// TestValidate_RefusesUnicodeWhitespaceOnlySecret: "blank" is judged by
// unicode.IsSpace (via strings.TrimSpace), not by ASCII space alone. A secret
// that is only a non-breaking space -- the shape produced by pasting from a
// rendered web page or a word processor -- looks like a real value in every
// editor and diff, so it is the sharpest form of "looks intentional, nobody
// investigates".
func TestValidate_RefusesUnicodeWhitespaceOnlySecret(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.JWTSecret = "\u00a0 " // non-breaking space + ordinary space

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want refusal for a non-breaking-space-only secret")
	}
	if !isBlankSecretError(err) {
		t.Fatalf("Validate() failed for the wrong reason: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The real operator path: an environment variable that arrived carrying only
// whitespace (an interpolated compose value whose referenced variable was
// unset, a YAML block that preserved indentation, a truncated paste).
// ---------------------------------------------------------------------------

func TestLoad_EnvPath_RefusesBlankSecret(t *testing.T) {
	t.Setenv("HELIX_AUTH_API_KEYS", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Everything else is at its default, so Validate() has no other reason to
	// fail — this isolates the blank credential.
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want refusal for a blank credential from the environment")
	}
	if !isBlankSecretError(err) {
		t.Fatalf("Validate() failed for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "HELIX_AUTH_API_KEYS") {
		t.Errorf("error should name the variable to export: %v", err)
	}
}

// TestLoad_EnvPath_AcceptsUnsetSecrets is the negative control on the
// environment path, and the direct anti-outage pin: the shipped .env.example
// exports HELIX_AUTH_JWT_SECRET= and HELIX_AUTH_API_KEYS= blank, and every
// deployment that has never set them must still start.
func TestLoad_EnvPath_AcceptsUnsetSecrets(t *testing.T) {
	t.Setenv("HELIX_AUTH_JWT_SECRET", "")
	t.Setenv("HELIX_AUTH_API_KEYS", "")
	t.Setenv("HELIX_LLM_ANTHROPIC_KEY", "")
	t.Setenv("HELIX_DB_PASSWORD", "")
	t.Setenv("HELIX_REDIS_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil — this is the shipped .env.example shape", err)
	}
}

func TestLoad_EnvPath_AcceptsRealSecret(t *testing.T) {
	t.Setenv("HELIX_AUTH_API_KEYS", "a-real-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for a real credential", err)
	}
	if cfg.Auth.APIKeys != "a-real-key" {
		t.Errorf("Auth.APIKeys = %q, want the supplied value", cfg.Auth.APIKeys)
	}
}

// ---------------------------------------------------------------------------
// Entry point 2: loadFromFile() — the hot-reload path. A live reload must not
// be able to install a blank credential into a running process.
// ---------------------------------------------------------------------------

func TestLoadFromFile_RefusesBlankSecret(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.APIKeys = " "
	path := writeJSON(t, cfg)

	got, err := loadFromFile(path)
	if err == nil {
		t.Fatalf("loadFromFile() = %+v, nil; want refusal for a blank credential", got)
	}
	if !isBlankSecretError(err) {
		t.Fatalf("loadFromFile() failed for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "Auth.APIKeys") {
		t.Errorf("error does not name the field: %v", err)
	}
	if !strings.Contains(err.Error(), "HELIX_AUTH_API_KEYS") {
		t.Errorf("error does not name the env var: %v", err)
	}
	if got != nil {
		t.Errorf("loadFromFile() returned a config alongside the error: %+v", got)
	}
}

// TestLoadFromFile_AcceptsUnsetSecrets is the negative control on the reload
// path: a config that omits credentials entirely, and one that carries real
// ones, both keep loading. The watcher's long-standing tolerance for partial
// configs is preserved.
func TestLoadFromFile_AcceptsUnsetSecrets(t *testing.T) {
	t.Run("partial config with no credentials at all", func(t *testing.T) {
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
	})

	t.Run("credentials explicitly empty", func(t *testing.T) {
		cfg := validConfig()
		cfg.Auth = AuthConfig{}
		cfg.DB.Password = ""
		path := writeJSON(t, cfg)

		if _, err := loadFromFile(path); err != nil {
			t.Fatalf("loadFromFile() error = %v, want nil: absence is the documented off-switch", err)
		}
	})

	t.Run("real credentials", func(t *testing.T) {
		cfg := validConfig()
		cfg.Auth.APIKeys = "key-one, key-two"
		path := writeJSON(t, cfg)

		got, err := loadFromFile(path)
		if err != nil {
			t.Fatalf("loadFromFile() error = %v, want nil", err)
		}
		if got.Auth.APIKeys != "key-one, key-two" {
			t.Errorf("Auth.APIKeys = %q, want the supplied value", got.Auth.APIKeys)
		}
	})
}

// ---------------------------------------------------------------------------
// Guards on the guard itself.
// ---------------------------------------------------------------------------

// TestCheckNoBlankSecrets_FieldWithoutEnvTag covers the fallback message. Every
// STRING field of HelixConfig carries an `env` tag (the struct-valued container
// fields carry none, but they are recursed into and never reach the error
// site), so without an ad-hoc struct that branch would be untested code shipped
// on a security path.
func TestCheckNoBlankSecrets_FieldWithoutEnvTag(t *testing.T) {
	type untagged struct {
		Nested struct {
			APIToken string
		}
	}
	var c untagged
	c.Nested.APIToken = " "

	err := checkNoBlankSecrets(&c)
	if err == nil {
		t.Fatal("checkNoBlankSecrets() = nil, want refusal")
	}
	if !isBlankSecretError(err) {
		t.Fatalf("failed for the wrong reason: %v", err)
	}
	if !strings.Contains(err.Error(), "Nested.APIToken") {
		t.Errorf("error does not name the field path: %v", err)
	}
	if strings.Contains(err.Error(), "(env ") {
		t.Errorf("an untagged field must not claim an env var: %v", err)
	}
}

// TestConfigFieldKindsAreCoveredByTheWalkers guards the silent-exemption drift
// that both credential walkers share: they recurse into structs, inspect
// strings, and skip every other kind without a word. Today HelixConfig holds
// only string/struct/int/bool fields, so nothing is silently exempt. If someone
// later adds a *Struct, []string or map — `APIKeys []string`, say — it would
// slip past BOTH this guard and the placeholder guard with no signal at all.
// This test is that signal.
func TestConfigFieldKindsAreCoveredByTheWalkers(t *testing.T) {
	handled := map[reflect.Kind]string{
		reflect.String: "inspected",
		reflect.Struct: "recursed into",
		reflect.Int:    "scalar, cannot carry a credential",
		reflect.Bool:   "scalar, cannot carry a credential",
	}

	var check func(typ reflect.Type, path string)
	check = func(typ reflect.Type, path string) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			fieldPath := f.Name
			if path != "" {
				fieldPath = path + "." + f.Name
			}
			if _, ok := handled[f.Type.Kind()]; !ok {
				t.Errorf("field %s has kind %s, which walkForBlankSecrets and "+
					"walkForPlaceholders both skip silently — extend the walkers, or "+
					"add the kind here with the reason it cannot carry a credential",
					fieldPath, f.Type.Kind())
				continue
			}
			if f.Type.Kind() == reflect.Struct {
				check(f.Type, fieldPath)
			}
		}
	}
	check(reflect.TypeOf(HelixConfig{}), "")
}
