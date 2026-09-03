package config

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/auth"
)

// Guards for the third credential guard in Validate(), alongside
// checkNoUnexpandedPlaceholders and checkNoBlankSecrets: a JWT signing secret
// that was SUPPLIED but is too short to sign with under RFC 7518.
//
// Why this belongs with the other two rather than being left to runtime: all
// three describe the same operator mistake — a credential that LOOKS
// configured and protects less than the operator believes. A placeholder is
// caught, a whitespace-only value is caught, and a four-character HMAC key
// used to be waved through.

// validSecret is exactly at the floor, so it also pins that the boundary is
// inclusive.
func validSecret() string { return strings.Repeat("k", minJWTSecretBytes) }

func baseValidConfig() *HelixConfig {
	return &HelixConfig{
		Mode:   "full",
		Server: ServerConfig{Port: 8443},
		Log:    LogConfig{Level: "info"},
	}
}

func TestValidate_RefusesShortJWTSecret(t *testing.T) {
	cfg := baseValidConfig()
	short := strings.Repeat("k", minJWTSecretBytes-1)
	cfg.Auth.JWTSecret = short

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() = nil for a %d-byte JWT secret; RFC 7518 §3.2 requires "+
			"at least %d for HS256, and a shorter key protects far less than the "+
			"operator who set it believes", len(short), minJWTSecretBytes)
	}
	// The operator needs to know which variable and how far short it is.
	if !strings.Contains(err.Error(), "HELIX_AUTH_JWT_SECRET") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if !strings.Contains(err.Error(), "RFC 7518") {
		t.Errorf("error does not cite the standard it enforces: %v", err)
	}
	// It must NOT quote the secret itself — this string reaches logs.
	if strings.Contains(err.Error(), short) {
		t.Errorf("error quotes the secret value: %v", err)
	}
}

func TestValidate_AcceptsJWTSecretAtTheFloor(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Auth.JWTSecret = validSecret()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v for a secret of exactly %d bytes; the floor is "+
			"inclusive (RFC 7518 says \"or larger\")", err, minJWTSecretBytes)
	}
}

// TestValidate_UnsetJWTSecretStaysLegitimate is the compatibility half. An
// unset secret is the documented off-switch and it is what every shipped
// config in this repo uses (.env.example ships it blank) — the new guard must
// not have turned that into a refusal.
func TestValidate_UnsetJWTSecretStaysLegitimate(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Auth.JWTSecret = ""

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v with the JWT secret unset; that is the documented "+
			"off-switch and refusing it would refuse every current deployment", err)
	}
}

// TestValidate_ZeroTTLIsNotAnError pins the deliberate decision NOT to
// validate the TTL. A hand-built config has JWTTTLMinutes == 0 because the
// `default:"1440"` tag is applied by env.Load, not by the struct; auth.New
// treats non-positive as "use the default" so there is no unsafe value to
// refuse. Coupling Validate to the loader would break programmatic callers.
func TestValidate_ZeroTTLIsNotAnError(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Auth.JWTSecret = validSecret()
	cfg.Auth.JWTTTLMinutes = 0

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v for JWTTTLMinutes=0; a hand-built config always "+
			"has 0 there and auth.New defaults it", err)
	}
}

// TestValidate_PlaceholderAndBlankGuardsStillWinOnTheJWTSecret pins the ORDER
// of the three guards. A secret that is an unexpanded placeholder is 22 bytes
// and would also trip the length rule — the operator must be told about the
// failed substitution, which is the actionable cause, not about key size.
func TestValidate_PlaceholderAndBlankGuardsStillWinOnTheJWTSecret(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Auth.JWTSecret = "${HELIX_AUTH_JWT_SECRET}"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil for an unexpanded placeholder secret")
	}
	if strings.Contains(err.Error(), "RFC 7518") {
		t.Errorf("the length guard reported first on a placeholder; the substitution "+
			"failure is the actionable cause and must be reported instead: %v", err)
	}

	cfg.Auth.JWTSecret = "   "
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil for a whitespace-only secret")
	}
	if !isBlankSecretError(err) {
		t.Errorf("the blank-secret guard did not report first on a whitespace-only "+
			"secret: %v", err)
	}
}

// TestMinSecretBytesAgreesWithTheAuthPackage: the floor is duplicated in two
// packages on purpose (this package must not import internal/auth just to read
// a constant), so it needs a test that they cannot silently diverge. Both are
// pinned to the SAME standard, not to each other.
func TestMinSecretBytesAgreesWithTheAuthPackage(t *testing.T) {
	if minJWTSecretBytes != auth.MinSecretBytes {
		t.Fatalf("config.minJWTSecretBytes = %d but auth.MinSecretBytes = %d; the two "+
			"copies of the RFC 7518 §3.2 floor have diverged, so a secret could pass "+
			"config validation and then be refused by auth.New at startup",
			minJWTSecretBytes, auth.MinSecretBytes)
	}
}
