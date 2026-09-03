package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

// Errors reported when an identifier cannot be derived or recorded.
var (
	ErrBadRuleset = errors.New("naming: unusable consumer ruleset")
	ErrConflict   = errors.New("naming: identifier already stands for a different identity")
)

// digestHexLen is how much of the SHA-256 of the canonical identity is carried
// in the identifier, in hex characters.
//
// This is the entire collision-resistance argument, so it is worth stating
// plainly. The readable part of an identifier is lossy — it has to be, because
// the consumer's charset forbids characters that appear in real model names —
// and any purely lossy scheme maps "llama3:8b" and "llama3-8b" onto one string.
// The digest is taken over the FULL canonical identity, so two different
// identities differ in the digest even when their readable parts are identical.
//
// 12 hex characters is 48 bits. For a catalogue of 10,000 options the birthday
// probability of any collision is about 2e-10 — far below the rate of the
// hardware failures this system already has to tolerate — while keeping the
// identifier short enough to type as a shell alias. The digest is never
// truncated further by the length cap; only the readable part is trimmed.
const digestHexLen = 12

// Ruleset describes one consuming tool's identifier rules. It is a parameter
// rather than a constant because consumers genuinely differ, and each one's
// rules are respected exactly as they stand (FR-014a).
type Ruleset struct {
	// Name identifies the consumer. [Registry] keys its mappings by it, so two
	// consumers can hold different identifiers for the same identity.
	Name string

	// Prefix opens every identifier. It supplies the leading letter that
	// MustStartWithLetter needs and marks the identifier's provenance.
	Prefix string

	// Separator joins the identifier's parts. It must itself be Allowed.
	Separator rune

	// Allow reports whether a rune may appear in an identifier.
	Allow func(rune) bool

	// MustStartWithLetter mirrors an anchored ^[a-zA-Z] rule.
	MustStartWithLetter bool

	// MaxLength caps the identifier in bytes; zero means no cap. Only the
	// readable part is trimmed to meet it.
	MaxLength int
}

// ClaudeToolkit is the ruleset for the Claude Toolkit.
//
// The toolkit applies TWO independent checks, and a value used as both an alias
// name and a provider id has to pass both (claude_toolkit/scripts/lib.sh):
//
//	cma_validate_alias        ^[a-zA-Z][a-zA-Z0-9_-]*$
//	provider-id charset       [A-Za-z0-9._-] only, non-empty
//
// The second is a shell-injection guard: the provider id is interpolated into
// an alias body that is re-parsed on invocation. Neither is relaxed to fit a
// name, so this ruleset is their INTERSECTION — [A-Za-z0-9_-], opening with a
// letter — which satisfies both at once. Note that `.` is deliberately excluded
// even though the provider-id guard permits it, because the alias rule does not.
var ClaudeToolkit = Ruleset{
	Name:      "claude-toolkit",
	Prefix:    IdentityPrefix,
	Separator: '-',
	Allow: func(r rune) bool {
		return r == '_' || r == '-' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
	},
	MustStartWithLetter: true,
	MaxLength:           64,
}

// IdentifierPrefix is the literal every identifier this ruleset derives opens
// with — the provenance prefix followed by the separator ("helixllm-" for
// [ClaudeToolkit]). It is derived from the ruleset rather than written out as a
// constant because the prefix and the separator are both ruleset fields: a
// consumer with a different separator would otherwise be matched against the
// wrong literal.
func (rs Ruleset) IdentifierPrefix() string {
	return rs.Prefix + string(rs.Separator)
}

// HasIdentifierPrefix reports whether name is SHAPED like one of this ruleset's
// identifiers — that is, whether it carries our provenance prefix.
//
// Shape is not membership: a name can carry the prefix and stand for nothing
// this deployment publishes, which is exactly what a stale identifier is. That
// distinction is the point of the method. A caller that cannot resolve such a
// name now knows the difference between "a model name we do not recognise",
// which may still be a served upstream id, and "one of OUR identifiers that
// resolves to nothing" — a request for one specific thing that cannot be
// honoured, and must not be quietly answered by something else.
func (rs Ruleset) HasIdentifierPrefix(name string) bool {
	return strings.HasPrefix(name, rs.IdentifierPrefix())
}

// Validate reports whether the ruleset can actually produce a conforming
// identifier. A ruleset whose own prefix or separator fails its own rules would
// emit identifiers the consumer rejects at the far end, so it fails here
// instead — loudly, and before anything reaches a user's configuration.
func (rs Ruleset) Validate() error {
	if rs.Allow == nil {
		return fmt.Errorf("%w: %q has no character predicate", ErrBadRuleset, rs.Name)
	}
	if rs.Prefix == "" {
		return fmt.Errorf("%w: %q has no prefix", ErrBadRuleset, rs.Name)
	}
	for _, r := range rs.Prefix {
		if !rs.Allow(r) {
			return fmt.Errorf("%w: prefix %q contains %q, which the ruleset forbids",
				ErrBadRuleset, rs.Prefix, r)
		}
	}
	if rs.MustStartWithLetter {
		first := []rune(rs.Prefix)[0]
		if !unicode.IsLetter(first) {
			return fmt.Errorf("%w: prefix %q starts with %q, not a letter",
				ErrBadRuleset, rs.Prefix, first)
		}
	}
	if !rs.Allow(rs.Separator) {
		return fmt.Errorf("%w: separator %q is not permitted by the ruleset itself",
			ErrBadRuleset, rs.Separator)
	}
	if min := rs.minLength(); rs.MaxLength > 0 && rs.MaxLength < min {
		return fmt.Errorf("%w: MaxLength %d cannot hold the prefix and digest (needs %d)",
			ErrBadRuleset, rs.MaxLength, min)
	}
	return nil
}

// minLength is the shortest identifier the ruleset can produce: prefix,
// separator and the full digest, with no readable part at all.
func (rs Ruleset) minLength() int {
	return len(rs.Prefix) + len(string(rs.Separator)) + digestHexLen
}

// Derive produces the identifier a consumer should use for one identity.
//
// The result is `<prefix><sep><readable><sep><digest>`: a lossy, human-friendly
// rendering of the host and model so the entry is recognisable in a
// configuration file, plus a digest of the full canonical identity so that two
// distinct identities can never become one entry.
//
// It is deterministic — the same identity and ruleset always yield the same
// identifier, which is what makes these names stable in users' configurations
// across releases (FR-015) — and it never emits the identity verbatim.
func Derive(id Identity, rs Ruleset) (string, error) {
	if err := id.Validate(); err != nil {
		return "", err
	}
	if err := rs.Validate(); err != nil {
		return "", err
	}

	canonical := id.String()
	sum := sha256.Sum256([]byte(canonical))
	digest := hex.EncodeToString(sum[:])[:digestHexLen]

	sep := string(rs.Separator)
	fixed := rs.Prefix + sep + digest

	parts := make([]string, 0, 3)
	for _, field := range []string{id.Host, id.Model, id.Variant} {
		if s := sanitise(field, rs); s != "" {
			parts = append(parts, s)
		}
	}
	readable := strings.Join(parts, sep)

	if rs.MaxLength > 0 {
		// Trim the readable part only. Shortening the digest instead would
		// quietly weaken the collision resistance the digest exists to provide.
		budget := rs.MaxLength - len(fixed) - len(sep)
		if budget < 1 {
			readable = ""
		} else if len(readable) > budget {
			readable = strings.TrimRight(readable[:budget], sep)
		}
	}

	if readable == "" {
		return rs.Prefix + sep + digest, nil
	}
	return rs.Prefix + sep + readable + sep + digest, nil
}

// sanitise renders one identity field into the ruleset's charset. Runs of
// forbidden characters collapse to a single separator, and the result is
// lower-cased so an identifier is predictable to type.
//
// This is lossy by design and carries none of the collision resistance — that
// is entirely the digest's job in [Derive].
func sanitise(field string, rs Ruleset) string {
	var b strings.Builder
	b.Grow(len(field))
	pendingSep := false

	for _, r := range strings.ToLower(field) {
		if rs.Allow(r) && r != rs.Separator {
			if pendingSep && b.Len() > 0 {
				b.WriteRune(rs.Separator)
			}
			pendingSep = false
			b.WriteRune(r)
			continue
		}
		pendingSep = true
	}
	return b.String()
}

// Registry records which identity each derived identifier stands for, per
// consumer, so the two cannot drift apart (FR-014).
//
// Without it the mapping would exist only implicitly, in the hash function: a
// consumer holding an identifier could not say what it names, and a collision —
// however unlikely — would silently drop a model from a user's configuration
// rather than being reported. A Registry is safe for concurrent use, because
// options arrive from concurrent host discovery.
type Registry struct {
	mu sync.RWMutex
	// byIdentifier maps consumer name -> identifier -> identity.
	byIdentifier map[string]map[string]Identity
	// byIdentity maps consumer name -> canonical identity string -> identifier.
	byIdentity map[string]map[string]string
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byIdentifier: make(map[string]map[string]Identity),
		byIdentity:   make(map[string]map[string]string),
	}
}

// Register derives the identifier for an identity under a consumer's ruleset
// and records the mapping, returning the identifier.
//
// Re-registering an identity is idempotent and returns the identifier already
// recorded. If the derived identifier is already bound to a DIFFERENT identity,
// Register fails with [ErrConflict] rather than overwriting: a collision must
// surface as an error, never as a model that quietly disappeared.
func (r *Registry) Register(id Identity, rs Ruleset) (string, error) {
	identifier, err := Derive(id, rs)
	if err != nil {
		return "", err
	}
	if err := r.Adopt(rs, identifier, id); err != nil {
		return "", err
	}
	return identifier, nil
}

// Adopt records an explicit identifier-to-identity binding for a consumer.
//
// It exists so an identifier obtained elsewhere — read back from an existing
// configuration, say — can be checked against this registry instead of being
// trusted. Binding an identifier that already stands for a different identity
// fails with [ErrConflict].
func (r *Registry) Adopt(rs Ruleset, identifier string, id Identity) error {
	if err := id.Validate(); err != nil {
		return err
	}
	if identifier == "" {
		return fmt.Errorf("%w: empty identifier for %q", ErrMalformed, id.String())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byIdentifier[rs.Name]; !ok {
		r.byIdentifier[rs.Name] = make(map[string]Identity)
		r.byIdentity[rs.Name] = make(map[string]string)
	}

	if existing, ok := r.byIdentifier[rs.Name][identifier]; ok && existing != id {
		return fmt.Errorf("%w: %q already stands for %q, cannot rebind to %q",
			ErrConflict, identifier, existing.String(), id.String())
	}

	r.byIdentifier[rs.Name][identifier] = id
	r.byIdentity[rs.Name][id.String()] = identifier
	return nil
}

// IdentityFor returns the identity a consumer's identifier stands for.
func (r *Registry) IdentityFor(rs Ruleset, identifier string) (Identity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byIdentifier[rs.Name][identifier]
	return id, ok
}

// IdentifierFor returns the identifier recorded for an identity under a
// consumer's ruleset.
func (r *Registry) IdentifierFor(rs Ruleset, id Identity) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	identifier, ok := r.byIdentity[rs.Name][id.String()]
	return identifier, ok
}
