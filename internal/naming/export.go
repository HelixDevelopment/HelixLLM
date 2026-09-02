package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// This file holds what the per-consumer exports share: the shape of the input a
// caller supplies, the safety checks every export applies to it, and the
// available/withheld split.
//
// The exports themselves deliberately do NOT write to a user's configuration.
// A consumer's config file belongs to its user; the system produces the
// artefact and the user applies it (FR-018). Each export therefore returns the
// document, and each ships a Merge* function the user can run against their own
// file to get an updated one back.

// Errors reported when an instance cannot be exported.
var (
	// ErrUnsafeEndpoint reports a base URL that must not be written into a
	// consumer's configuration — because it carries a credential, or because it
	// would not survive the file format it is written into.
	ErrUnsafeEndpoint = errors.New("naming: unusable serving endpoint")

	// ErrHostMismatch reports an offer whose identity names a different host
	// than the instance serving it. Exporting it would point a consumer at the
	// wrong machine.
	ErrHostMismatch = errors.New("naming: offer does not belong to this instance")

	// ErrForeignAssignment reports an existing configuration that already sets
	// a managed value outside the managed block, where a merge could neither
	// win nor lose predictably.
	ErrForeignAssignment = errors.New("naming: configuration already sets this value elsewhere")
)

// Withheld reasons published when an option is not exported as usable.
//
// Machine tokens, not prose: a consuming tool renders them in its own locale.
const (
	// ReasonInstanceUnreachable is published for every option of an instance
	// that is not currently healthy and gave no more specific reason.
	ReasonInstanceUnreachable = "instance-unreachable"

	// ReasonModelUnavailable is published for an option the instance is not
	// serving right now and gave no more specific reason.
	ReasonModelUnavailable = "model-unavailable"
)

// Offer is one model an instance is offering.
type Offer struct {
	// Identity is the human-readable identity of the option. Its Host must
	// match the instance's.
	Identity Identity

	// Available reports whether the instance is serving this model right now.
	Available bool

	// Reason carries why it is not, when Available is false. Empty is
	// tolerated — a default token is published rather than an empty reason,
	// because "unavailable with no reason" is indistinguishable from a bug
	// (FR-019).
	Reason string
}

// Instance is one serving instance a consumer can be pointed at.
type Instance struct {
	// Host is the machine, lower-cased, matching every offer's Identity.Host.
	Host string

	// BaseURL is the instance's origin WITHOUT an API version path — consumers
	// differ on whether they want one appended, so each export adds what its
	// own client requires rather than the caller guessing.
	BaseURL string

	// Healthy reports whether the instance is reachable and serving. An
	// unhealthy instance exports no usable option at all (contract invariant 4).
	Healthy bool

	// Reason carries why the instance is unhealthy, when Healthy is false.
	Reason string

	// Offers are the models this instance offers, available or not.
	Offers []Offer
}

// Exported is one option written into a consumer's configuration.
type Exported struct {
	// Identifier is the consumer-safe identifier — what the consumer keys on.
	Identifier string

	// Identity is the human-readable identity it stands for, carried as a
	// VALUE (contract invariant 2).
	Identity string

	// WireModel is the model name the instance itself answers to, which is
	// what must reach the wire.
	WireModel string
}

// WithheldOption is one option deliberately NOT written, with the reason.
type WithheldOption struct {
	Identity string
	Reason   string
}

// validate reports whether the instance can be exported at all.
func (inst Instance) validate() error {
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		return ErrNoHost
	}
	if host != inst.Host || host != strings.ToLower(host) {
		return fmt.Errorf("%w: host %q is not normalised", ErrNotNormalised, inst.Host)
	}
	if _, err := safeEndpoint(inst.BaseURL); err != nil {
		return err
	}
	for _, o := range inst.Offers {
		if err := o.Identity.Validate(); err != nil {
			return err
		}
		if o.Identity.Host != host {
			return fmt.Errorf("%w: %q is served by %q, not %q",
				ErrHostMismatch, o.Identity.Model, o.Identity.Host, host)
		}
	}
	return nil
}

// safeEndpoint parses a base URL and refuses everything that must not reach a
// consumer's configuration file.
//
// The refusals are not stylistic. Userinfo, a query and a fragment are the
// three places a discovery secret or bearer token rides along in a URL, and
// writing one into a config file would leak it (contract invariant 5, §11.4.10)
// — so those are rejected rather than stripped, because silently dropping half
// of an operator's endpoint would point the consumer somewhere they did not
// ask for. Shell-significant characters and whitespace are rejected because one
// of the two artefacts here is an env file a shell sources.
//
// The returned error never quotes the offending URL: an error message is a
// place secrets leak too.
func safeEndpoint(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: no base URL", ErrUnsafeEndpoint)
	}
	if raw != strings.TrimSpace(raw) {
		return nil, fmt.Errorf("%w: base URL has surrounding whitespace", ErrUnsafeEndpoint)
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return nil, fmt.Errorf("%w: base URL contains whitespace or a control character", ErrUnsafeEndpoint)
		}
		if strings.ContainsRune("\"'`$\\", r) {
			return nil, fmt.Errorf("%w: base URL contains a shell-significant character", ErrUnsafeEndpoint)
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: base URL does not parse", ErrUnsafeEndpoint)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("%w: base URL scheme is not http or https", ErrUnsafeEndpoint)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: base URL names no host", ErrUnsafeEndpoint)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: base URL carries credentials in its userinfo", ErrUnsafeEndpoint)
	}
	if u.RawQuery != "" {
		return nil, fmt.Errorf("%w: base URL carries a query string", ErrUnsafeEndpoint)
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, fmt.Errorf("%w: base URL carries a fragment", ErrUnsafeEndpoint)
	}
	return u, nil
}

// wireModel reconstructs the name the serving instance itself answers to. It is
// the exact inverse of the model/variant split the identity was built from.
func wireModel(id Identity) string {
	if id.Variant == "" {
		return id.Model
	}
	return id.Model + ":" + id.Variant
}

// conforms re-checks a derived identifier against the consumer's rules at the
// export boundary.
//
// Derive already guarantees this, so the check is redundant in the happy path —
// which is the point. It is the assertion that fails loudly if the derivation
// or a ruleset is ever loosened to admit a richer name, rather than letting a
// `/` reach a consumer that would misparse it or a shell that would re-evaluate
// it (FR-014a).
func conforms(identifier string, rs Ruleset) error {
	if identifier == "" {
		return fmt.Errorf("%w: empty identifier for %q", ErrMalformed, rs.Name)
	}
	for _, r := range identifier {
		if !rs.Allow(r) {
			return fmt.Errorf("%w: identifier %q contains %q, which %q forbids",
				ErrBadRuleset, identifier, r, rs.Name)
		}
	}
	if rs.MustStartWithLetter && !unicode.IsLetter([]rune(identifier)[0]) {
		return fmt.Errorf("%w: identifier %q does not start with a letter", ErrBadRuleset, identifier)
	}
	if rs.MaxLength > 0 && len(identifier) > rs.MaxLength {
		return fmt.Errorf("%w: identifier %q is %d bytes, over the %d cap",
			ErrBadRuleset, identifier, len(identifier), rs.MaxLength)
	}
	return nil
}

// hostIdentifier derives a consumer-safe identifier for a HOST rather than a
// single option, for consumers that group an instance's models under one entry.
//
// It has the same shape as [Derive] — readable part plus a digest of a
// canonical string — and hashes `helixllm/<host>`, which can never equal an
// identity's canonical form (that always carries a model segment too), so the
// two derivations occupy distinct spaces.
func hostIdentifier(host string, rs Ruleset) (string, error) {
	if strings.TrimSpace(host) == "" {
		return "", ErrNoHost
	}
	if err := rs.Validate(); err != nil {
		return "", err
	}

	canonical := IdentityPrefix + "/" + escapeField(host)
	sum := sha256.Sum256([]byte(canonical))
	digest := hex.EncodeToString(sum[:])[:digestHexLen]

	sep := string(rs.Separator)
	readable := sanitise(host, rs)

	if rs.MaxLength > 0 {
		budget := rs.MaxLength - len(rs.Prefix) - 2*len(sep) - digestHexLen
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

// partition derives an identifier for every option the instance is actually
// serving, and reports the rest as withheld with the reason each is withheld
// for.
//
// An unhealthy instance yields no usable option at all: a consumer that listed
// one would present a stopped model as selectable, which is the exact thing
// FR-019 forbids. Results are sorted so re-running the export produces the same
// bytes (contract invariant 3).
func partition(inst Instance, rs Ruleset) ([]Exported, []WithheldOption, error) {
	if err := inst.validate(); err != nil {
		return nil, nil, err
	}

	var (
		exported []Exported
		withheld []WithheldOption
		seen     = make(map[string]string, len(inst.Offers))
	)

	for _, o := range inst.Offers {
		if !inst.Healthy || !o.Available {
			withheld = append(withheld, WithheldOption{
				Identity: o.Identity.String(),
				Reason:   withheldReason(inst, o),
			})
			continue
		}

		identifier, err := Derive(o.Identity, rs)
		if err != nil {
			return nil, nil, err
		}
		if err := conforms(identifier, rs); err != nil {
			return nil, nil, err
		}

		canonical := o.Identity.String()
		if prev, ok := seen[identifier]; ok {
			if prev != canonical {
				return nil, nil, fmt.Errorf("%w: %q would stand for both %q and %q",
					ErrConflict, identifier, prev, canonical)
			}
			// The same option offered twice is one entry, not two.
			continue
		}
		seen[identifier] = canonical

		exported = append(exported, Exported{
			Identifier: identifier,
			Identity:   canonical,
			WireModel:  wireModel(o.Identity),
		})
	}

	sort.Slice(exported, func(i, j int) bool { return exported[i].Identifier < exported[j].Identifier })
	sort.Slice(withheld, func(i, j int) bool { return withheld[i].Identity < withheld[j].Identity })
	return exported, withheld, nil
}

// withheldReason picks the most specific reason available, never an empty one.
func withheldReason(inst Instance, o Offer) string {
	if !inst.Healthy {
		if r := strings.TrimSpace(inst.Reason); r != "" {
			return r
		}
		return ReasonInstanceUnreachable
	}
	if r := strings.TrimSpace(o.Reason); r != "" {
		return r
	}
	return ReasonModelUnavailable
}
