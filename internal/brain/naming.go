package brain

import (
	"sort"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// ServingHost is implemented by a provider that serves its models from a known
// machine — that is, by a HelixLLM-served backend rather than a remote vendor
// API.
//
// It is deliberately an OPTIONAL interface rather than a method on [Provider]:
// only some backends have a serving host, and adding the method to Provider
// would force every remote vendor client to invent one. A provider that does
// not implement it, or that returns an empty string, is treated as remote and
// keeps its upstream model ids untouched (see [Brain.ModelOptionsFor]).
type ServingHost interface {
	// ServingHost returns the host serving this provider's models.
	ServingHost() string
}

// UnavailableReasoner is implemented by a provider that can say WHY it is not
// currently serving. Also optional: a provider that cannot explain itself still
// gets a reason published, just a less specific one (FR-019).
type UnavailableReasoner interface {
	// UnavailableReason returns the reason this provider is withholding its
	// models, or "" when it is serving normally.
	UnavailableReason() string
}

// Withheld reasons published for an option that is not being served.
//
// These are stable machine tokens, not prose: a consuming tool renders them in
// its own locale rather than displaying an English sentence chosen here.
const (
	// ReasonProviderUnavailable is published when a provider reports itself
	// unavailable without giving a more specific reason.
	ReasonProviderUnavailable = "provider-unavailable"

	// ReasonUnnameable is published when a model's name cannot form a valid
	// identity — a control character in the name, say. Such an option is
	// withheld rather than published under a name that would corrupt any
	// line-oriented configuration it were written into.
	ReasonUnnameable = "unnameable-model"

	// ReasonIdentifierConflict is published when the derived identifier is
	// already bound to a DIFFERENT identity. It is astronomically unlikely
	// (see naming.digestHexLen), but a collision must surface as a withheld
	// option, never as a model that silently replaced another one.
	ReasonIdentifierConflict = "identifier-conflict"
)

// ModelOption is one offered option as the listing contract describes it: the
// identifier a consumer uses, the human-readable identity that identifier
// stands for, provenance, and whether it is actually being served.
//
// Identity is empty for a remote provider's model. That is not an omission: the
// `helixllm/<host>/…` identity exists precisely to distinguish a HelixLLM-served
// option from a remote one (FR-014), so stamping it on a vendor's model would
// destroy the distinction it is there to draw.
type ModelOption struct {
	// Identifier is the charset-safe identifier for the requesting consumer,
	// derived so it satisfies that consumer's rules AS THEY STAND (FR-014a).
	// For a remote provider's model it is the upstream id, unchanged.
	Identifier string

	// Identity is the human-readable `helixllm/<host>/<model>[:<variant>]`
	// VALUE, or "" when the option is not HelixLLM-served.
	Identity string

	// Host is the machine serving this model, empty for a remote provider's
	// model. It is carried separately from Identity because a consumer that does
	// not parse the identity still needs the host (FR-023), and parsing a value
	// to recover a field we already know is how the two drift apart.
	Host string

	// OwnedBy is the provider that offers the option.
	OwnedBy string

	// Available reports whether the option is actually being served right now.
	Available bool

	// Reason carries the withheld reason when Available is false, and is empty
	// otherwise (FR-019).
	Reason string
}

// Names returns the registry recording which identity each derived identifier
// stands for, so a caller can resolve one to the other rather than re-deriving
// it and hoping the two agree (contract invariant 3).
func (b *Brain) Names() *naming.Registry { return b.names }

// ModelOptions lists every offered option using the default consumer ruleset.
//
// The default is [naming.ClaudeToolkit], which is the INTERSECTION of that
// tool's two independent validators and therefore the strictest ruleset in
// play: an identifier that satisfies it satisfies the looser consumers too. A
// consumer with different rules calls [Brain.ModelOptionsFor] with its own.
func (b *Brain) ModelOptions() []ModelOption {
	return b.ModelOptionsFor(naming.ClaudeToolkit)
}

// ModelOptionsFor lists every offered option, available or not, with the
// identifier derived for the given consumer.
//
// Unlike [Brain.Models] it does NOT drop unavailable options: withholding them
// silently would leave a consuming tool unable to tell "not served right now"
// from "does not exist" (FR-019). Each unavailable option instead carries the
// reason it is being withheld, and is never marked Available.
func (b *Brain) ModelOptionsFor(rs naming.Ruleset) []ModelOption {
	var opts []ModelOption

	for _, p := range b.providers {
		available := p.Available()

		// Establish the withheld reason ONCE per provider, before looking at
		// any model, so an unavailable option can never reach the caller
		// carrying an empty reason.
		reason := ""
		if !available {
			reason = ReasonProviderUnavailable
			if r, ok := p.(UnavailableReasoner); ok {
				if specific := strings.TrimSpace(r.UnavailableReason()); specific != "" {
					reason = specific
				}
			}
		}

		host := ""
		if h, ok := p.(ServingHost); ok {
			host = strings.TrimSpace(h.ServingHost())
		}

		for _, m := range p.Models() {
			opt := ModelOption{
				Identifier: m,
				OwnedBy:    p.Name(),
				Host:       host,
				Available:  available,
				Reason:     reason,
			}

			// A remote vendor's model is not HelixLLM-served: it keeps its
			// upstream id and carries no identity.
			if host == "" {
				opts = append(opts, opt)
				continue
			}

			model, variant := splitModelVariant(m)
			id, err := naming.NewIdentity(host, model, variant)
			if err != nil {
				// The name cannot form a valid identity. Withhold the option
				// rather than publish something malformed.
				opt.Available = false
				opt.Reason = ReasonUnnameable
				opts = append(opts, opt)
				continue
			}

			identifier, err := b.names.Register(id, rs)
			if err != nil {
				opt.Identity = id.String()
				opt.Available = false
				opt.Reason = ReasonIdentifierConflict
				opts = append(opts, opt)
				continue
			}

			opt.Identifier = identifier
			opt.Identity = id.String()
			opts = append(opts, opt)
		}
	}

	return opts
}

// ResolveModelName maps a published identifier back to the model name the
// serving provider actually answers to.
//
// This is what makes publishing derived identifiers safe. Routing matches a
// request's model against the provider's own model names, so without this a
// client that listed an identifier and then asked for it would miss every
// exact match and fall through to whichever provider the router reached last —
// a silent misroute. Names not known as identifiers are returned unchanged,
// which is what keeps a pre-existing configuration holding a raw model name
// working exactly as before.
func (b *Brain) ResolveModelName(name string) (string, bool) {
	id, ok := b.resolveIdentity(name)
	if !ok {
		return name, false
	}
	return joinModelVariant(id.Model, id.Variant), true
}

// RegisterNames records an identifier for every HelixLLM-served model currently
// offered, so the registry can answer a request without deriving anything on
// the request path.
//
// This exists because resolution must not perform I/O. Deriving the option list
// asks EVERY provider whether it is available, and for a local runtime that is
// an HTTP call with a multi-second timeout — so a lazy "derive it if the
// registry misses" fallback made every unresolved request (a cloud model's
// upstream id included) probe a backend it was never going to use, and let one
// wedged local runtime add its whole timeout to unrelated traffic. Registration
// is therefore done ONCE, up front, from the model lists alone: no Available()
// call, no network, no per-request write lock.
//
// It is idempotent — [naming.Registry.Register] returns the existing identifier
// for an identity already recorded — so it is safe to call again whenever the
// model lists are refreshed, which is what keeps the registry current as hosts
// come and go.
func (b *Brain) RegisterNames() {
	b.RegisterNamesFor(naming.ClaudeToolkit)
}

// RegisterNamesFor is [Brain.RegisterNames] for one consumer's ruleset.
//
// Failures are deliberately silent here: an unnameable model or a digest
// collision is a WITHHELD OPTION, and [Brain.ModelOptionsFor] is the surface
// that reports it with its reason (FR-019). Registration only pre-populates
// what that surface would record anyway, so failing loudly in two places would
// report the same condition twice while giving this one nowhere to report it.
func (b *Brain) RegisterNamesFor(rs naming.Ruleset) {
	for _, p := range b.providers {
		h, hosted := p.(ServingHost)
		if !hosted {
			continue
		}
		host := strings.TrimSpace(h.ServingHost())
		if host == "" {
			continue
		}
		for _, m := range p.Models() {
			model, variant := splitModelVariant(m)
			id, err := naming.NewIdentity(host, model, variant)
			if err != nil {
				continue
			}
			_, _ = b.names.Register(id, rs)
		}
	}
}

// resolveIdentity maps a published identifier back to the identity it stands
// for, or reports false for anything that is not one of our identifiers.
//
// It is the shared half of [Brain.ResolveModelName] and [Brain.PinModel]: the
// first needs only the model name, the second also needs the HOST, and deriving
// the identity twice in two places is how the two would drift apart.
//
// It consults ONLY the registry, and performs no I/O — see [Brain.RegisterNames]
// for why, and for where the registry is filled.
func (b *Brain) resolveIdentity(name string) (naming.Identity, bool) {
	if name == "" {
		return naming.Identity{}, false
	}
	return b.names.IdentityFor(naming.ClaudeToolkit, name)
}

// PinModel reports whether a request named a SPECIFIC served model and, if so,
// which provider answers to it under which name.
//
// This is what makes the published identifier mean something on the real
// request path. Routing and the fallback chain both work in provider model
// names, and the chain additionally substitutes its own per-entry model when a
// request does not pin one — so a client that listed an identifier and then
// asked for it would otherwise be answered by an unrelated provider running an
// unrelated model, with no error and no way to tell.
//
// Three outcomes, and the difference between them is the whole contract:
//
//   - ok=false — nothing registered serves this name. The caller has not named
//     anything we can honour, so it keeps whatever routing it already had.
//     This is what preserves the fallback chain's score-ordered substitution
//     for requests that name no model, and for names this deployment does not
//     serve at all.
//   - ok=true, provider non-empty — the request named a served model; that
//     provider must answer it, under the returned name.
//   - ok=true, provider empty — the request named one of OUR identifiers, but
//     nothing currently registered serves the model on the host it names. The
//     caller asked for something specific and specific is unavailable; the
//     honest answer is an error, never a different model.
func (b *Brain) PinModel(requested string) (provider, model string, ok bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", "", false
	}

	model = requested
	wantHost := ""
	identifier := false
	if id, isIdentifier := b.resolveIdentity(requested); isIdentifier {
		model = joinModelVariant(id.Model, id.Variant)
		wantHost = id.Host
		identifier = true
	}

	// Sorted so the pin is deterministic. Map iteration order would let the
	// same request reach a different provider run to run, which is exactly the
	// class of surprise this function exists to remove.
	names := make([]string, 0, len(b.providers))
	for name := range b.providers {
		names = append(names, name)
	}
	sort.Strings(names)

	var candidates []string
	for _, name := range names {
		p := b.providers[name]
		if !serves(p, model) {
			continue
		}
		if wantHost != "" {
			// The identifier named a host. Two machines can serve the same
			// model name under DIFFERENT identifiers (the host is part of the
			// identity and of the digest), so honouring the host is what keeps
			// one identifier from landing on the other machine's copy.
			h, hosted := p.(ServingHost)
			if !hosted || !strings.EqualFold(strings.TrimSpace(h.ServingHost()), wantHost) {
				continue
			}
		}
		candidates = append(candidates, name)
	}

	switch {
	case len(candidates) == 1:
		// The only provider that serves it. Whether it is up is the caller's
		// problem to report, not a reason to look elsewhere — and asking here
		// would cost a health probe on every pinned request.
		return candidates[0], model, true

	case len(candidates) > 1:
		// Several providers serve this exact name. Sorted order is a tie-break
		// for determinism, NOT a reason to hand back a provider that is down
		// while a sibling is serving the SAME model: that is not the silent
		// substitution this function prevents, it is a request the deployment
		// could have served and didn't. Availability is consulted only in this
		// branch, so the single-provider case stays probe-free.
		for _, name := range candidates {
			if b.providers[name].Available() {
				return name, model, true
			}
		}
		// None of them are up. Name the first so the failure says which
		// provider was expected to answer.
		return candidates[0], model, true
	}

	// Nothing serves it. Two very different reasons to be here:
	if identifier || naming.ClaudeToolkit.HasIdentifierPrefix(requested) {
		// One of OUR identifiers — either resolved but its host is no longer
		// serving, or carrying our provenance prefix and standing for nothing
		// this deployment publishes (a STALE identifier: identifiers are
		// re-minted whenever the host segment of the identity changes, and a
		// client's configuration still holds the old one).
		//
		// Both are the same request: one specific model, named deliberately.
		// Reporting "nothing pinned" for either is what the chain reads as
		// "this caller named no model", which sends it to its own top-ranked
		// entry — the exact silent misroute the identifier exists to prevent,
		// aimed at the callers most likely to hit it. A name carrying our
		// prefix is never a request for any-available-model.
		return "", model, true
	}
	return "", "", false
}

// serves reports whether p offers a model under exactly this name.
func serves(p Provider, model string) bool {
	for _, m := range p.Models() {
		if m == model {
			return true
		}
	}
	return false
}

// splitModelVariant separates a served name into model and variant on its LAST
// colon, the convention local runtimes use for size and quantisation tags
// ("llama3:8b", "qwen2.5-coder:7b-instruct-q4_K_M"). It is exactly the variant
// segment FR-014 describes.
//
// The split is deterministic and reversed exactly by [joinModelVariant], so a
// served name maps to one identity and back again without ambiguity.
func splitModelVariant(name string) (model, variant string) {
	i := strings.LastIndex(name, ":")
	if i <= 0 || i == len(name)-1 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// joinModelVariant reverses [splitModelVariant].
func joinModelVariant(model, variant string) string {
	if variant == "" {
		return model
	}
	return model + ":" + variant
}
