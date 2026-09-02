package runtime

import (
	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// The disk-streaming path's own admission, as a unit.
//
// Choose owns the ORDER of the decision — in-memory first, always. This file
// owns what the streaming path itself demands once that order has brought us
// here: who it can serve at all (roster), and what the host must have for it
// (memory and disk, separately). Choose delegates to Admit rather than
// repeating these checks, so there is one implementation of them and not two
// that can drift apart while both look right.

// StreamingEligibility is the streaming runtime's answer to "can you serve this
// model at all", together with the family name that was looked up.
//
// The name is carried on the negative answer as well as the positive one. "Not
// eligible" with nothing named is indistinguishable from "no lookup happened",
// and a reader cannot check a decision they cannot see the inputs to.
type StreamingEligibility struct {
	// Eligible is roster membership. It is never an inference.
	Eligible bool
	// FamilyName is the name resolved against the runtime's declared supported
	// set. Empty when the catalogue records no roster standing for the entry at
	// all — that is, when there was no name to look up.
	FamilyName string
}

// StreamingEligibilityOf reports whether the disk-streaming runtime can serve e.
//
// The answer is a CATALOGUE LOOKUP: the entry's recorded standing in that
// runtime's declared, closed, named set of supported families. It is never an
// architecture predicate.
//
// This matters because the two are not equivalent and the difference is not
// academic. Several widely-known mixture-of-experts models — Qwen3-30B-A3B,
// Llama 4 Scout, gpt-oss-120b — are architecturally exactly what the streaming
// path is for and have no support path in it. An `architecture == MoE` test
// calls them eligible and offers them; the offer then fails when the runtime is
// asked to load them. That converts a selection-time answer we could have given
// cheaply and correctly into a load-time failure the user meets after waiting
// (D1).
//
// Because eligibility is data, a runtime release that adds a family is a
// catalogue change and not a change to this function.
func StreamingEligibilityOf(e catalogue.Entry) StreamingEligibility {
	return StreamingEligibility{
		Eligible:   e.StreamingEligible(),
		FamilyName: e.StreamingRoster.FamilyName,
	}
}

// StreamingVerdict is the streaming path's answer for one model on one host.
//
// When Admitted is false, Reason says which of the two distinct things went
// wrong and exactly one of Unsupported / Shortfall carries its detail. Both are
// nil when Admitted is true: an admission is not a refusal with an empty reason
// attached.
//
// The two reasons are kept apart deliberately and must stay apart all the way
// out to the user. They ask for different things (FR-055, D6):
//
//	unsupported_configuration → no runtime here serves this model at all.
//	                            Remedy: a different model.
//	insufficient_resources    → the path exists; this host is too small for it.
//	                            Remedy: a bigger host, or a smaller model.
//
// Collapsed into one generic "cannot run", the only part of the answer the user
// could have acted on is the part that was thrown away.
type StreamingVerdict struct {
	Admitted bool
	Reason   RefusalReason

	// Exactly one of these is set when Admitted is false, matching Reason.
	Unsupported *Unsupported
	Shortfall   *Shortfall
}

// Remedy is what this verdict asks of the user, or the empty Remedy for an
// admission. It is stated here as well as on the reason so a caller weighing
// two verdicts can compare what they ASK, which is the property that has to
// differ — two reasons with the same remedy are one reason wearing two names.
func (v StreamingVerdict) Remedy() Remedy {
	if v.Admitted {
		return ""
	}
	return v.Reason.Remedy()
}

// admittedStreaming is the single admitted verdict. Building it in one place
// keeps "admitted" from acquiring a stray detail.
func admittedStreaming() StreamingVerdict { return StreamingVerdict{Admitted: true} }

// Admit reports whether the disk-streaming runtime serves e on host, and when
// it does not, which of its two distinct refusals applies.
//
// It answers ONLY the streaming path's own question. It does not ask whether
// the model fits in memory — that question is answered before this is reached,
// and is what brought us here. Calling Admit for a model that fits in memory
// would be asking the fallback whether it could serve something the preferred
// path already serves (D6).
//
// Order is roster first, then the host's two axes. That order is not incidental:
// a model with no support path is not short of anything, and asking how much
// memory it lacks would produce a resource answer for a configuration problem.
func (m StreamingMinimums) Admit(host capability.HostCapabilityProfile, e catalogue.Entry) StreamingVerdict {
	// (b) Eligibility — roster membership, and only roster membership (D1).
	if eligibility := StreamingEligibilityOf(e); !eligibility.Eligible {
		return StreamingVerdict{
			Reason: ReasonUnsupportedConfiguration,
			Unsupported: &Unsupported{
				Requirement: RequirementStreamingRoster,
				// The family name that was looked up, so a reader sees WHAT was
				// checked rather than only that a check failed.
				Detail: eligibility.FamilyName,
			},
		}
	}

	// (c) The runtime's own per-model floors. Two axes, checked separately,
	// because they are separately true: streaming reduces the MEMORY a model
	// needs while requiring its whole footprint on DISK. Neither figure implies
	// the other, and a refusal that names the wrong one sends the user to fix
	// the resource that was fine (D2).
	//
	// Failing either is a resource answer, not a configuration one: the path
	// exists here and a larger host would take it. That is precisely what
	// separates this refusal from the roster miss above.
	if resident := m.ResidentMemoryBytes(e); resident > uint64(host.MemoryAvailable) {
		return StreamingVerdict{
			Reason: ReasonInsufficientResources,
			Shortfall: &Shortfall{
				Resource:       ResourceMemory,
				RequiredBytes:  resident,
				AvailableBytes: uint64(host.MemoryAvailable),
			},
		}
	}
	if s, short := storageShortfall(host, m.StorageBytes(e)); short {
		return StreamingVerdict{
			Reason:    ReasonInsufficientResources,
			Shortfall: &s,
		}
	}

	return admittedStreaming()
}
