package brain

import (
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
	if name == "" {
		return name, false
	}
	id, ok := b.names.IdentityFor(naming.ClaudeToolkit, name)
	if !ok {
		// The identifier may not have been listed yet in this process. Derive
		// over the current options rather than requiring a prior listing.
		for _, opt := range b.ModelOptionsFor(naming.ClaudeToolkit) {
			if opt.Identifier == name && opt.Identity != "" {
				parsed, err := naming.ParseIdentity(opt.Identity)
				if err != nil {
					return name, false
				}
				id, ok = parsed, true
				break
			}
		}
		if !ok {
			return name, false
		}
	}
	return joinModelVariant(id.Model, id.Variant), true
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
