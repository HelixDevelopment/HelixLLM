package config

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// unexpandedPlaceholder matches a complete shell-style variable-substitution
// token — the exact shape a substitution step (docker/podman compose
// interpolation, envsubst, a shell) consumes and replaces. Capture group 1 is
// the referenced variable name, so the error can tell the operator which
// variable was supposed to fill the field.
//
// Matched: "${HELIX_AUTH_JWT_SECRET}", "${HELIX_DB_PASSWORD:-helixllm}", and
// "https://${OTEL_HOST}:4317" (embedded / partially substituted).
// Not matched: a bare "$", a bare "{", "${}", "${ spaced }", "$HOME"
// (that last one is a shell form nothing here ever expands either, but it is
// not the compose/envsubst token shape and is far more likely to be a
// legitimate literal, so it stays out of scope).
var unexpandedPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^{}]*\}`)

// checkNoUnexpandedPlaceholders walks every string field of a config struct
// (recursing into nested structs) and refuses any value that still contains an
// unexpanded ${...} substitution token.
//
// Why this is a refusal and not a warning: nothing in this codebase performs
// variable substitution. Values arrive already-substituted or not at all — see
// digital.vasic.config/pkg/env, which does a plain os.Getenv, and loadFromFile,
// which does a plain json.Unmarshal. So when substitution does not happen (the
// variable is unset, misspelled, or the expansion step was skipped — e.g. a
// compose `env_file:` entry, which is NOT interpolated, or a Kubernetes
// ConfigMap), the literal string "${HELIX_AUTH_JWT_SECRET}" becomes the value.
// For a secret that is the dangerous case: a known, published, attacker-readable
// constant sitting in a git-tracked config file, with every health check
// reporting green. A missing secret must be a loud refusal at startup, not a
// silent shared password.
//
// SCOPE — every string field, not only the security-relevant ones. Three
// reasons, stated explicitly so the choice is defensible rather than incidental:
//
//  1. An unexpanded placeholder is a mistake in EVERY field. A host of
//     "${HELIX_DB_HOST}" or a model path of "${HELIX_MODELS_DIR}/x.gguf" is a
//     broken deployment; the difference is only that a broken hostname fails
//     loudly on first use while a broken secret fails silently, forever.
//  2. A curated "security-relevant fields" list would drift. The next secret
//     field added to HelixConfig would be silently exempt until somebody
//     remembered to extend the list. Reflection over every string field has no
//     such failure mode.
//  3. The false-positive surface is bounded and, here, empty. Rejection needs a
//     COMPLETE, well-formed ${IDENTIFIER...} token; a value that merely
//     contains "$" or "{" is untouched. The only legitimate value this refuses
//     is one deliberately containing a whole substitution token — and since
//     this codebase never expands anything, such a value is indistinguishable
//     from the bug it is meant to catch, so refusing it loudly is the correct
//     outcome rather than a regression.
//
// The error names the offending field AND the environment variable that was
// supposed to fill it, so the operator can fix it in one step. It reports the
// matched placeholder token only — never the surrounding field value — so a
// partially-substituted secret is not leaked into logs.
func checkNoUnexpandedPlaceholders(cfg any) error {
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
	return walkForPlaceholders(v, "")
}

func walkForPlaceholders(v reflect.Value, path string) error {
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
			if err := walkForPlaceholders(fieldVal, fieldPath); err != nil {
				return err
			}
			continue
		}

		if fieldVal.Kind() != reflect.String {
			continue
		}

		m := unexpandedPlaceholder.FindStringSubmatch(fieldVal.String())
		if m == nil {
			continue
		}

		// m[0] is the placeholder token, m[1] the referenced variable name.
		// The surrounding field value is deliberately NOT included.
		envVar := m[1]
		if tag := field.Tag.Get("env"); tag != "" && tag != envVar {
			return fmt.Errorf(
				"config field %s (env %s) contains an unexpanded placeholder %s: "+
					"environment variable %s was not substituted — set it, or fix the expansion step; "+
					"refusing to start with a literal placeholder as a value",
				fieldPath, tag, m[0], envVar)
		}
		return fmt.Errorf(
			"config field %s contains an unexpanded placeholder %s: "+
				"environment variable %s was not substituted — set it, or fix the expansion step; "+
				"refusing to start with a literal placeholder as a value",
			fieldPath, m[0], envVar)
	}
	return nil
}

// isPlaceholderError reports whether err came from the placeholder guard, so
// tests can distinguish "refused for the right reason" from "refused for some
// unrelated reason" without matching the whole message.
func isPlaceholderError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unexpanded placeholder")
}
