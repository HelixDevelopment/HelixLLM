// Package naming gives every offered model option a human-readable identity and,
// separately, a charset-safe identifier for each consuming tool.
//
// The split is the whole point of this package, and it is not a stylistic one.
//
// The identity `helixllm/<host>/<model>[:<variant>]` is a VALUE — a field on a
// model record, a catalogue entry, a label on screen. It exists so a user
// reading nothing but a name in their tool's model list can say "that is
// HelixLLM-served, and that host is serving it" (FR-014, SC-015).
//
// It is deliberately NOT an identifier. Consumer tools restrict identifier
// character sets, and at least one of them does so as a security control: the
// Claude Toolkit interpolates a provider id into an alias body that is re-parsed
// when the alias is invoked, and confines it to [A-Za-z0-9._-] "so a hostile
// catalog/--id value can't inject shell commands" (claude_toolkit/scripts/lib.sh).
// Both `/` and `:` are rejected by that guard and by the toolkit's alias-name
// check. Using the identity as an identifier would therefore mean either
// widening an injection guard or failing to build — so instead, where a consumer
// needs an identifier, [Derive] produces a separate one that satisfies that
// consumer's rules AS THEY STAND, and [Registry] records which identity it
// stands for so the two cannot drift apart (FR-014a).
//
// Relaxing a consumer's validation to admit a richer name is forbidden. If a
// future change makes a derived identifier fail a consumer's check, the fix
// belongs in the derivation, never in the consumer's guard.
package naming

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// IdentityPrefix is the fixed provenance segment every identity opens with. It
// is what makes a HelixLLM-served option distinguishable from a remote
// provider's model at a glance (FR-014).
//
// This value is part of the naming scheme users have written into their tool
// configurations. Changing it is a breaking change requiring a migration path,
// not a cosmetic adjustment (FR-015).
const IdentityPrefix = "helixllm"

// Errors reported when an identity cannot be formed or read back.
var (
	ErrNoHost        = errors.New("naming: identity requires a serving host")
	ErrNoModel       = errors.New("naming: identity requires a model name")
	ErrControlRune   = errors.New("naming: identity fields may not contain control characters")
	ErrNotNormalised = errors.New("naming: identity fields are not normalised")
	ErrMalformed     = errors.New("naming: malformed identity string")
)

// Identity is the human-readable identity of one offered model option.
//
// It is a value type: two identities with equal fields are equal and render
// identically, which is what lets [Registry] use it as a map key.
type Identity struct {
	// Host is the machine serving the model, normalised to lower case because
	// hostnames are case-insensitive (RFC 4343) and one machine must not appear
	// as two options because an operator capitalised it differently.
	Host string

	// Model is the model name as the serving host reports it. Case is preserved:
	// upstream registries treat model names as case-sensitive, so folding here
	// would merge two genuinely different models into one option.
	Model string

	// Variant is optional and carries size or quantisation ("8b", "q4_K_M").
	Variant string
}

// NewIdentity normalises and validates the parts of an identity.
//
// Surrounding whitespace is trimmed, the host is lower-cased, and control
// characters are refused — a newline in a name would corrupt any line-oriented
// configuration or listing the identity is written into. Every other awkward
// character, including the `/` and `:` separators and any Unicode, is accepted
// and escaped by [Identity.String]; a model genuinely named "org/llama3:8b" is
// a real thing to serve, not an error to reject.
func NewIdentity(host, model, variant string) (Identity, error) {
	id := Identity{
		Host:    strings.ToLower(strings.TrimSpace(host)),
		Model:   strings.TrimSpace(model),
		Variant: strings.TrimSpace(variant),
	}
	if err := id.Validate(); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// Validate reports whether the identity is well-formed and normalised.
//
// It is stricter than a nil check on purpose: a caller that builds an Identity
// literally rather than through [NewIdentity] still has to be holding a
// normalised value, or [Derive] would hash an un-normalised string and produce
// a different identifier for what is really the same option.
func (i Identity) Validate() error {
	if i.Host == "" {
		return ErrNoHost
	}
	if i.Model == "" {
		return ErrNoModel
	}
	for _, field := range []string{i.Host, i.Model, i.Variant} {
		if strings.TrimSpace(field) != field {
			return fmt.Errorf("%w: %q has surrounding whitespace", ErrNotNormalised, field)
		}
		for _, r := range field {
			if unicode.IsControl(r) {
				return fmt.Errorf("%w: %q contains %q", ErrControlRune, field, r)
			}
		}
	}
	if i.Host != strings.ToLower(i.Host) {
		return fmt.Errorf("%w: host %q is not lower-cased", ErrNotNormalised, i.Host)
	}
	return nil
}

// String renders the identity as `helixllm/<host>/<model>[:<variant>]`.
//
// The three structural characters — `/`, `:` and the `\` escape itself — are
// backslash-escaped inside a field. In the ordinary case ("gpu-01", "llama3",
// "8b") nothing is escaped and the result reads exactly as specified; escaping
// only appears when a name would otherwise be ambiguous, which is what keeps
// model "llama3" variant "8b" distinguishable from a model literally named
// "llama3:8b". [ParseIdentity] reverses it exactly.
//
// String is the canonical form: it is what [Derive] hashes, so any change to
// this rendering changes every derived identifier and is a breaking change
// under FR-015.
func (i Identity) String() string {
	var b strings.Builder
	b.WriteString(IdentityPrefix)
	b.WriteByte('/')
	b.WriteString(escapeField(i.Host))
	b.WriteByte('/')
	b.WriteString(escapeField(i.Model))
	if i.Variant != "" {
		b.WriteByte(':')
		b.WriteString(escapeField(i.Variant))
	}
	return b.String()
}

// ParseIdentity reads back a string produced by [Identity.String].
//
// Its existence is what makes the escaping above defensible rather than
// decorative: an escaping scheme that cannot be reversed does not "handle" an
// awkward name, it silently corrupts it.
func ParseIdentity(s string) (Identity, error) {
	rest, ok := strings.CutPrefix(s, IdentityPrefix+"/")
	if !ok {
		return Identity{}, fmt.Errorf("%w: %q does not begin with %q", ErrMalformed, s, IdentityPrefix+"/")
	}

	fields, err := splitUnescaped(rest)
	if err != nil {
		return Identity{}, err
	}
	if len(fields) != 3 {
		return Identity{}, fmt.Errorf("%w: %q has %d fields, want host, model and optional variant",
			ErrMalformed, s, len(fields))
	}

	id := Identity{Host: fields[0], Model: fields[1], Variant: fields[2]}
	if err := id.Validate(); err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return id, nil
}

// escapeField backslash-escapes the characters that carry structure.
func escapeField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '/', ':':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// splitUnescaped splits `<host>/<model>[:<variant>]` on its UNESCAPED
// separators and unescapes each field, returning exactly three values (the
// third empty when there is no variant).
func splitUnescaped(s string) ([]string, error) {
	fields := []string{""}
	escaped := false

	for _, r := range s {
		switch {
		case escaped:
			fields[len(fields)-1] += string(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '/' || r == ':':
			if len(fields) == 3 {
				return nil, fmt.Errorf("%w: %q has too many separators", ErrMalformed, s)
			}
			// A ':' may only introduce the variant, i.e. the third field.
			if r == ':' && len(fields) != 2 {
				return nil, fmt.Errorf("%w: %q has a variant separator before the model", ErrMalformed, s)
			}
			if r == '/' && len(fields) != 1 {
				return nil, fmt.Errorf("%w: %q has a field separator after the model", ErrMalformed, s)
			}
			fields = append(fields, "")
		default:
			fields[len(fields)-1] += string(r)
		}
	}
	if escaped {
		return nil, fmt.Errorf("%w: %q ends in a dangling escape", ErrMalformed, s)
	}
	for len(fields) < 3 {
		fields = append(fields, "")
	}
	return fields, nil
}
