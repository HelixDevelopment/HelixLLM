package catalogue

import (
	"errors"
	"fmt"
)

// CapabilityFamily is the kind of work a model performs. Selection guarantees a
// non-empty offer set per family that a host can serve acceptably (D5), so the
// family is the unit the guarantee is measured against — not a free-form label.
type CapabilityFamily string

// The families the catalogue records. Values are machine keys, never display
// text: presentation composes user-facing wording from these (CONST-046).
const (
	FamilyText            CapabilityFamily = "text"
	FamilyVision          CapabilityFamily = "vision"
	FamilyImageGeneration CapabilityFamily = "image-generation"
	FamilyVideoGeneration CapabilityFamily = "video-generation"
	FamilySpeechToText    CapabilityFamily = "speech-to-text"
	FamilyTextToSpeech    CapabilityFamily = "text-to-speech"
	// FamilyAudioGeneration and FamilyAudioClassification are deliberately
	// separate. Classification is processor-viable on every host tier;
	// generation has no processor-viable option at all. Merged, a host with no
	// accelerator must either be offered generation it cannot run or lose
	// classification it can, and the per-family guarantee (D5) — which is
	// measured per family — would no longer be able to state which of the two
	// was withheld or why.
	FamilyAudioGeneration     CapabilityFamily = "audio-generation"
	FamilyAudioClassification CapabilityFamily = "audio-classification"
	FamilyEmbedding           CapabilityFamily = "embedding"
	FamilyVector              CapabilityFamily = "vector"
)

// Known reports whether f is one of the recorded families.
func (f CapabilityFamily) Known() bool {
	switch f {
	case FamilyText, FamilyVision, FamilyImageGeneration, FamilyVideoGeneration,
		FamilySpeechToText, FamilyTextToSpeech, FamilyAudioGeneration,
		FamilyAudioClassification, FamilyEmbedding, FamilyVector:
		return true
	default:
		return false
	}
}

// Architecture describes how a model is built.
//
// It is DESCRIPTIVE ONLY. It MUST NOT be used, anywhere, to decide whether a
// model can be served by the disk-streaming runtime: that runtime supports a
// closed, named list of families, and several well-known mixture-of-experts
// models have no support path at all. An `architecture == MoE` predicate offers
// options that cannot run — a selection bug surfacing as a runtime failure (D1).
// Streaming eligibility comes from Roster membership and nowhere else.
type Architecture string

const (
	ArchitectureDense            Architecture = "dense"
	ArchitectureMixtureOfExperts Architecture = "mixture-of-experts"
	ArchitectureDiffusion        Architecture = "diffusion"
	ArchitectureEncoderDecoder   Architecture = "encoder-decoder"
)

// RuntimeKind is which runtime serves an entry. In-memory is tried first;
// streaming is a fallback taken only when it is the sole feasible path (D6).
type RuntimeKind string

const (
	RuntimeInMemory  RuntimeKind = "in-memory"
	RuntimeStreaming RuntimeKind = "streaming"
)

// Known reports whether r is a recorded runtime.
func (r RuntimeKind) Known() bool {
	return r == RuntimeInMemory || r == RuntimeStreaming
}

// UsagePurpose is a use a model's output may be put to. A user's declared usage
// is matched against an entry's terms before the entry may be offered (D4).
type UsagePurpose string

const (
	UsageCommercial UsagePurpose = "commercial"
	UsagePersonal   UsagePurpose = "personal"
	UsageResearch   UsagePurpose = "research"
	UsageEvaluation UsagePurpose = "evaluation"
)

// RestrictionTerm names a constraint a licence places on use. It is a machine
// key so a caller can name the exact restriction when withholding an entry
// (FR-055) and localise the wording itself (CONST-046).
type RestrictionTerm string

const (
	TermNonCommercial       RestrictionTerm = "non-commercial"
	TermRevenueCap          RestrictionTerm = "revenue-cap"
	TermAttributionRequired RestrictionTerm = "attribution-required"
	TermShareAlike          RestrictionTerm = "share-alike"
	TermResearchOnly        RestrictionTerm = "research-only"
)

// Amount is a bounded quantity carried by a capped term, such as the revenue
// ceiling above which a revenue-capped licence stops permitting commercial use.
// The zero Amount means the term carries no numeric bound.
type Amount struct {
	Value    uint64
	Currency string
	Period   string
}

// Zero reports whether a carries no numeric bound.
func (a Amount) Zero() bool { return a.Value == 0 && a.Currency == "" && a.Period == "" }

// Restriction is one constraint from a licence.
//
// Excludes is what makes a restriction actionable: a term that excludes no
// purpose (attribution, share-alike) constrains how output is used but never
// withholds an entry, and must not be reported as the reason one was withheld.
type Restriction struct {
	Term      RestrictionTerm
	Excludes  []UsagePurpose
	Threshold Amount
	// Reference identifies the clause, so a withheld entry can cite the source
	// of the restriction rather than asserting it.
	Reference string
}

// Excludable reports whether this restriction can withhold an entry.
func (r Restriction) Excludable() bool { return len(r.Excludes) > 0 }

// UsageTerms are the terms a model may be used under. Capability and resource
// fit alone are insufficient grounds to offer a model (D4).
type UsageTerms struct {
	// LicenseID identifies the licence — an SPDX id where one exists, otherwise
	// the vendor's own identifier.
	LicenseID string
	// Permitted is the set of purposes the licence grants.
	Permitted []UsagePurpose
	// Restrictions are the licence's constraints, exclusionary or not.
	Restrictions []Restriction
}

// Permits reports whether the terms allow p: p must be granted and no
// restriction may exclude it.
func (t UsageTerms) Permits(p UsagePurpose) bool {
	if _, restricted := t.RestrictionFor(p); restricted {
		return false
	}
	for _, granted := range t.Permitted {
		if granted == p {
			return true
		}
	}
	return false
}

// RestrictionFor returns the restriction that excludes p, so a caller
// withholding an entry can name the term that caused it rather than reporting a
// generic unavailability (FR-055). The second result is false when no
// restriction excludes p — including when p is simply not granted, which is an
// absence of permission and not a restriction.
func (t UsageTerms) RestrictionFor(p UsagePurpose) (Restriction, bool) {
	for _, r := range t.Restrictions {
		for _, excluded := range r.Excludes {
			if excluded == p {
				return r, true
			}
		}
	}
	return Restriction{}, false
}

// RosterMembership records an entry's standing in the streaming runtime's
// declared supported set.
//
// The runtime supports a closed, named list of model families. Membership is
// therefore catalogue DATA: a runtime release that adds a family is a data
// change, not a code change (D1).
type RosterMembership struct {
	// FamilyName is the name the streaming runtime uses for this model's family
	// in its own supported list. Empty when the runtime names no such family.
	FamilyName string
	// Listed records whether FamilyName appears in that supported list.
	Listed bool
}

// Eligible reports whether the streaming runtime can serve this entry. The
// answer derives from roster membership alone — never from architecture.
func (m RosterMembership) Eligible() bool { return m.Listed && m.FamilyName != "" }

// Roster is the streaming runtime's closed, named set of supported model
// families, held as data.
type Roster struct {
	members map[string]struct{}
}

// NewRoster builds a roster from the family names the runtime declares.
func NewRoster(familyNames ...string) Roster {
	members := make(map[string]struct{}, len(familyNames))
	for _, name := range familyNames {
		if name == "" {
			continue
		}
		members[name] = struct{}{}
	}
	return Roster{members: members}
}

// Admits reports whether familyName is in the declared set. This is a lookup by
// name — the only admissible eligibility test (D1).
func (r Roster) Admits(familyName string) bool {
	if familyName == "" {
		return false
	}
	_, ok := r.members[familyName]
	return ok
}

// Membership resolves familyName against the roster. The result carries the
// name, so a later reader can see which family was looked up.
func (r Roster) Membership(familyName string) RosterMembership {
	return RosterMembership{FamilyName: familyName, Listed: r.Admits(familyName)}
}

// Len reports how many families the roster declares.
func (r Roster) Len() int { return len(r.members) }

// Descriptor holds the facts a human-meaningful description is composed FROM.
//
// The description itself is never a fixed string in this package: it is
// composed at the presentation boundary from these facts plus host measurement,
// in the user's language (CONST-046).
type Descriptor struct {
	// ParameterCount is total parameters.
	ParameterCount uint64
	// ActiveParameters is parameters active per token. Zero for dense models,
	// where it equals ParameterCount and carries no extra information.
	ActiveParameters uint64
	// Quantisation is the weight format key, empty at full precision.
	Quantisation string
	// Specialisations are machine keys for what the model is tuned for.
	Specialisations []string
}

// VideoOutput is what a video-generation entry produces, in video's own terms.
//
// A generated clip's capability is a frame size, a frame count and a rate.
// Expressed as tokens per second it is not merely imprecise, it is a different
// quantity, and FR-005 asks for options comparable on evidence rather than on a
// coerced number. The zero value means no video output is recorded, which is
// distinct from a clip of zero length.
//
// Duration is derived rather than stored: it is exactly FrameCount/FramesPerSecond,
// and a stored copy could disagree with the frames and rate beside it.
type VideoOutput struct {
	FrameWidth      int
	FrameHeight     int
	FrameCount      int
	FramesPerSecond float64
}

// Recorded reports whether this entry states a video output at all.
func (v VideoOutput) Recorded() bool {
	return v.FrameWidth > 0 && v.FrameHeight > 0 && v.FrameCount > 0 && v.FramesPerSecond > 0
}

// DurationSeconds is the clip length implied by the frame count and rate. It is
// zero when no rate is recorded, rather than an undefined division.
func (v VideoOutput) DurationSeconds() float64 {
	if v.FramesPerSecond <= 0 {
		return 0
	}
	return float64(v.FrameCount) / v.FramesPerSecond
}

// ExpectedCapability states what the user gets, in terms comparable between
// options so a choice can be made on evidence rather than on a model's name
// (FR-005).
type ExpectedCapability struct {
	// ContextTokens is the usable context window.
	ContextTokens int
	// ThroughputTokensPerSecond is the throughput this entry is expected to
	// deliver on the host it is offered for. Zero where throughput is not the
	// meaningful unit for the family.
	ThroughputTokensPerSecond float64
	// Modalities are the input kinds accepted, as machine keys.
	Modalities []string
	// Video is what a video-generation entry produces. Its zero value means no
	// video output is recorded, so a text entry never reads as an empty clip.
	Video VideoOutput
}

// DigestAlgorithm is how an integrity expectation is computed.
type DigestAlgorithm string

const (
	DigestSHA256 DigestAlgorithm = "sha256"
	DigestBLAKE3 DigestAlgorithm = "blake3"
)

// IntegrityExpectation is the value a weight file is verified against before it
// is loaded (SC-011). An entry without one cannot be served: there would be no
// way to tell an intact download from a corrupted or substituted one.
type IntegrityExpectation struct {
	Algorithm DigestAlgorithm
	// Digest is the expected digest, lowercase hex.
	Digest string
	// SizeBytes is the expected weight-file size, checked before the digest so a
	// truncated file fails cheaply.
	SizeBytes uint64
}

// Complete reports whether the expectation can actually verify a file.
func (i IntegrityExpectation) Complete() bool {
	return i.Algorithm != "" && i.Digest != "" && i.SizeBytes > 0
}

// Entry is one model the catalogue records as runnable.
//
// It carries only what is true of the model itself. Everything host-dependent —
// whether it fits, which host serves it, its identity string — is produced by
// joining an Entry against a measured host profile, and never written back here.
type Entry struct {
	// ModelID is the model portion of the identity value helixllm/<host>/<model>.
	// It is a value, never a consumer identifier: consumers get a separately
	// derived, charset-safe id (D7).
	ModelID string
	// Variant distinguishes builds of the same model, empty when there is one.
	Variant string

	Family CapabilityFamily
	// Architecture is descriptive only. See the Architecture doc comment: it is
	// never an input to streaming eligibility.
	Architecture Architecture
	Descriptor   Descriptor

	// MemoryRequiredBytes is checked against the host's AVAILABLE memory.
	MemoryRequiredBytes uint64
	// StorageRequiredBytes is checked against free storage SEPARATELY. A model's
	// disk footprint is not implied by its memory figure — the streaming path
	// exists precisely for weights whose on-disk size dwarfs memory (D2).
	StorageRequiredBytes uint64
	// RequiresAccelerator records whether an accelerator is mandatory for
	// acceptable service, as opposed to merely faster.
	RequiresAccelerator bool

	// Source is where the weights are obtained from. FR-012 requires an entry to
	// record its source alongside its expected integrity value: without it the
	// allowlist gate has to be handed a source from somewhere else, and SC-011's
	// "no model is obtained from a source outside the allowlist" is only half
	// enforceable — the check exists but nothing supplies the value it checks.
	//
	// It is deliberately not required by Validate. An entry is perfectly valid to
	// OFFER while its weights have not been located yet, which is the ordinary
	// state of a researched-but-unacquired model. It becomes required at
	// acquisition — the moment the allowlist actually gates something. See
	// ValidateForAcquisition.
	Source string

	UsageTerms UsageTerms

	// Runtime is which runtime serves this entry.
	Runtime RuntimeKind
	// StreamingRoster is this entry's standing in the streaming runtime's
	// declared supported set.
	StreamingRoster RosterMembership

	ExpectedCapability ExpectedCapability
	Integrity          IntegrityExpectation

	// Notes carry provenance the catalogue must not resolve silently — most
	// importantly the "UNVERIFIED:" markers research left on figures that were
	// taken from a vendor claim rather than measured. Dropping them at load would
	// turn an unverified figure into an apparently-established one.
	Notes []string
	// Annotations carry recorded material this type deliberately does not model:
	// a model's headline capability that is NOT the configuration being offered,
	// a frame-count rule, a licence clause reference. It is carried and shown,
	// never read to make a decision — anything that gates an offer belongs in a
	// named field where it can be validated.
	Annotations map[string]any
}

// StreamingEligible reports whether the disk-streaming runtime can serve this
// entry.
//
// The answer is roster membership, and only roster membership. Architecture is
// deliberately not consulted: mixture-of-experts models exist that the streaming
// runtime does not support, and inferring eligibility from architecture offers
// them anyway (D1).
func (e Entry) StreamingEligible() bool { return e.StreamingRoster.Eligible() }

// Identity is the model portion of the naming scheme, with the variant when one
// is set. The host-qualified identity is formed at the selection boundary, which
// is the only place that knows the host.
func (e Entry) Identity() string {
	if e.Variant == "" {
		return e.ModelID
	}
	return e.ModelID + ":" + e.Variant
}

// Errors reported by Validate. They are distinct because they have distinct
// remedies: a data defect in the roster is not the same problem as a missing
// digest, and collapsing them loses the remedy.
var (
	ErrMissingModelID       = errors.New("catalogue: entry has no model id")
	ErrUnknownFamily        = errors.New("catalogue: entry family is not a recorded capability family")
	ErrUnknownRuntime       = errors.New("catalogue: entry runtime is not a recorded runtime")
	ErrNoMemoryRequirement  = errors.New("catalogue: entry states no memory requirement")
	ErrNoStorageRequirement = errors.New("catalogue: entry states no storage requirement")
	ErrNoLicense            = errors.New("catalogue: entry states no licence")
	ErrNoPermittedUsage     = errors.New("catalogue: entry permits no usage purpose")
	ErrContradictoryTerms   = errors.New("catalogue: entry permits a purpose its own restrictions exclude")
	ErrIncompleteIntegrity  = errors.New("catalogue: entry has no verifiable integrity expectation")
	ErrStreamingNotRostered = errors.New("catalogue: entry is served by the streaming runtime but is not on its roster")
	ErrNoSource             = errors.New("catalogue: entry records no source, so its acquisition cannot be checked against the allowlist")
)

// Validate reports the first defect that would make this entry unusable to
// OFFER, with the entry's identity attached so a catalogue-wide sweep names the
// offender.
//
// It deliberately does NOT require a source or an integrity expectation. Both are
// captured when weights are first fetched, so demanding them here made the
// catalogue unloadable in its ordinary initial state: nothing could load, so no
// download could be triggered, so no digest could ever be captured. Those two
// belong to ValidateForAcquisition, which is the gate that actually precedes
// touching a weight file.
func (e Entry) Validate() error {
	if err := e.validate(); err != nil {
		return fmt.Errorf("entry %q: %w", e.Identity(), err)
	}
	return nil
}

func (e Entry) validate() error {
	if e.ModelID == "" {
		return ErrMissingModelID
	}
	if !e.Family.Known() {
		return fmt.Errorf("%w: %q", ErrUnknownFamily, string(e.Family))
	}
	if !e.Runtime.Known() {
		return fmt.Errorf("%w: %q", ErrUnknownRuntime, string(e.Runtime))
	}
	if e.MemoryRequiredBytes == 0 {
		return ErrNoMemoryRequirement
	}
	// Storage is checked in its own right, never derived from the memory figure.
	if e.StorageRequiredBytes == 0 {
		return ErrNoStorageRequirement
	}
	if e.UsageTerms.LicenseID == "" {
		return ErrNoLicense
	}
	if len(e.UsageTerms.Permitted) == 0 {
		return ErrNoPermittedUsage
	}
	for _, granted := range e.UsageTerms.Permitted {
		if r, restricted := e.UsageTerms.RestrictionFor(granted); restricted {
			return fmt.Errorf("%w: %q excluded by %q", ErrContradictoryTerms, string(granted), string(r.Term))
		}
	}
	// An entry the streaming runtime is said to serve must be on that runtime's
	// declared roster. Otherwise the catalogue promises a path the runtime does
	// not have, and the failure surfaces at load time instead of here.
	if e.Runtime == RuntimeStreaming && !e.StreamingEligible() {
		return ErrStreamingNotRostered
	}
	return nil
}

// ValidateForAcquisition reports whether the entry is complete enough to FETCH,
// as opposed to merely complete enough to offer. It is Validate plus the two
// things acquisition needs and offering does not: a source to check against the
// allowlist, and an integrity expectation to verify what arrives.
//
// The split exists because those two states have different remedies. An entry
// that fails Validate has a research gap. An entry that passes Validate but
// fails here is well-researched and simply not located yet.
func (e Entry) ValidateForAcquisition() error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Source == "" {
		return fmt.Errorf("%w: %s", ErrNoSource, e.Identity())
	}
	if !e.Integrity.Complete() {
		return fmt.Errorf("%w: %s", ErrIncompleteIntegrity, e.Identity())
	}
	return nil
}
