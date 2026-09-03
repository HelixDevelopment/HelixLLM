package config

import (
	"fmt"
	"reflect"
	"strings"
)

// checkNoBlankSecrets walks every string field of a config struct (recursing
// into nested structs) and refuses any credential-class field that is set but
// carries no actual credential — a value that is non-empty yet contains only
// whitespace.
//
// This is the companion to checkNoUnexpandedPlaceholders. That guard catches a
// secret that was never substituted and still reads "${HELIX_AUTH_JWT_SECRET}".
// This one catches the adjacent case the operator decided must also be a
// refusal: a secret that is present but empty of content. It is the more
// dangerous of the two precisely because it looks deliberate — there is no
// suspicious token to notice, so nobody investigates.
//
// WHICH SECRETS ARE REQUIRED — the classification, and why it is this one
// =====================================================================
//
// The rule enforced here is: a credential is REQUIRED TO BE NON-BLANK ONCE IT
// IS SUPPLIED. It is not required to be supplied at all. That asymmetry is not
// a hedge; it is what the code actually does, established field by field.
//
// Every credential in HelixConfig is SELF-GATING: the presence of the value is
// itself the enable-switch for the feature that consumes it, and absence is a
// documented, supported "feature off" state.
//
//   - Provider API keys are registered only when non-empty —
//     internal/brain/brain.go:91 (Anthropic), :98 (Chutes), :103 (OpenRouter),
//     :108 (HuggingFace), :113 (Nvidia), :118 (Cerebras), :123 (SambaNova),
//     :128 (Together). An empty key means "this provider is not configured",
//     which is the normal case for all but the one or two an operator uses.
//     internal/brain/brain.go:80-88 goes further and supports a deliberately
//     KEYLESS OpenAI-compatible provider (Ollama), where an empty key is the
//     correct value.
//
//   - Auth.APIKeys empty is open-access mode by design —
//     internal/gateway/middleware/auth.go:28-32, and cmd/helixllm/main.go:470
//     documents the choice explicitly ("behaviour is unchanged for open
//     deployments").
//
//   - Auth.JWTSecret empty is the documented off-switch —
//     website/content/docs/user-guide/configuration.md: "Leave empty to
//     disable JWT auth."
//
//     UPDATED: when this guard was written, that field had NO production
//     consumer — no Go file outside tests imported a JWT package and
//     golang-jwt was not a direct dependency. It now has one: internal/auth
//     mints and verifies tokens from it, and internal/gateway/middleware
//     enforces them. The off-switch reasoning is UNCHANGED and is now load
//     bearing rather than vacuous — an unset secret really does disable a
//     real capability. Note what did NOT change: this guard still only
//     refuses a secret that is supplied-but-blank. The adjacent case of a
//     supplied-but-too-short secret is refused by validateJWT in config.go,
//     which is where the algorithm's key-size requirement belongs.
//
//   - Cache.RedisPassword is only reached when a Redis host is set
//     (cmd/helixllm/main.go:310-314), and an auth-less Redis is ordinary.
//
//   - DB.Password has no consumer anywhere in the tree.
//
// So requiring any of these to be SUPPLIED would refuse deployments that work
// today — including the project's own .env.example, which ships
// HELIX_AUTH_JWT_SECRET= and HELIX_AUTH_API_KEYS= blank (.env.example:106-107).
// That is an outage, not a fix, so this guard does not do it.
//
// A BLANK-BUT-SUPPLIED secret is the opposite: it is never legitimate, and it
// is actively dangerous, because it opens the self-gate while carrying nothing.
// Captured runtime evidence for the anchor case, HELIX_AUTH_API_KEYS:
//
//	configuredKeys=""         -> 200  (open access, as documented)
//	configuredKeys="   "      -> 401  (EVERY request, including a correct key)
//	configuredKeys="real-key" -> 200
//
// A whitespace-only key list is not open access and not closed-with-a-key: it
// is a total lockout of every client, produced by a config that validates
// cleanly and looks intentional. The same shape applies elsewhere — a
// whitespace-only provider key passes the != "" gate at brain.go:91 and the
// provider is registered with a credential the vendor will reject on every
// call, degrading silently through the fallback chain.
//
// Where blank values come from in practice: an interpolated compose value whose
// referenced variable is unset (`KEY=${MISSING}` with surrounding quotes or
// spaces), a heredoc or YAML block that preserved indentation, a copy-paste
// that captured only whitespace. The env loader preserves them verbatim —
// HELIX_AUTH_API_KEYS="   " arrives as a 3-character string, not "".
//
// SCOPE — credential-class fields only, identified by name suffix (Secret,
// Password, Key/Keys, Token, Credential/Credentials). Two reasons for a suffix
// rule rather than a curated list of fields, mirroring the reasoning in
// placeholder.go:
//
//  1. A curated list drifts. The next secret added to HelixConfig would be
//     silently exempt until somebody remembered to extend it. A suffix rule
//     covers new credential fields the moment they are named.
//  2. The false-positive surface is empty. No legitimate secret, and no
//     legitimate path to a key file (SSHKey, Server.TLSKey), consists solely of
//     whitespace. Non-credential fields are deliberately untouched: a blank
//     Hosts or Mode is a different problem with its own loud failure, and
//     sweeping them in here would make this guard "refuse everything" rather
//     than "refuse blank credentials".
//
// The error names the field and the environment variable to export, matching
// the placeholder guard's shape so operators see one consistent style. No field
// value is ever included — not the offending field's, and not any neighbouring
// credential's, since the config being refused is full of real secrets.
func checkNoBlankSecrets(cfg any) error {
	v := reflect.ValueOf(cfg)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	return walkForBlankSecrets(v, "")
}

func walkForBlankSecrets(v reflect.Value, path string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		fieldVal := v.Field(i)

		fieldPath := field.Name
		if path != "" {
			fieldPath = path + "." + field.Name
		}

		if fieldVal.Kind() == reflect.Struct {
			if err := walkForBlankSecrets(fieldVal, fieldPath); err != nil {
				return err
			}
			continue
		}

		if fieldVal.Kind() != reflect.String || !isCredentialField(field.Name) {
			continue
		}

		s := fieldVal.String()
		// Absent is legitimate (the documented off-switch). Only a value that
		// was supplied yet carries nothing is refused.
		if s == "" || strings.TrimSpace(s) != "" {
			continue
		}

		if tag := field.Tag.Get("env"); tag != "" {
			return fmt.Errorf(
				"config field %s (env %s) is set but contains only whitespace: "+
					"export %s with the real credential value, or unset it entirely if the "+
					"feature is meant to be disabled; refusing to start with a blank credential "+
					"that the code will treat as configured",
				fieldPath, tag, tag)
		}
		return fmt.Errorf(
			"config field %s is set but contains only whitespace: "+
				"set it to the real credential value, or unset it entirely if the feature is "+
				"meant to be disabled; refusing to start with a blank credential that the code "+
				"will treat as configured",
			fieldPath)
	}
	return nil
}

// credentialSuffixes are the field-name endings that mark a value as a
// credential (or a path to one). Matching is on the field name, so it extends
// to credential fields added later without anyone remembering to update a list.
var credentialSuffixes = []string{
	"Secret", "Secrets",
	"Password", "Passwords",
	"Key", "Keys",
	"Token", "Tokens",
	"Credential", "Credentials",
}

// isCredentialField reports whether a struct field name denotes a credential.
func isCredentialField(name string) bool {
	for _, suffix := range credentialSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// isBlankSecretError reports whether err came from the blank-secret guard, so
// tests can distinguish "refused for the right reason" from "refused for some
// unrelated reason" without matching the whole message.
func isBlankSecretError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "contains only whitespace")
}
