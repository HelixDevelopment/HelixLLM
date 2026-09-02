package runtime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/HelixDevelopment/HelixLLM/internal/lifecycle"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

// Admission — where a SELECTED option becomes a HELD reservation.
//
// Selection decides WHAT may be offered on this host; the VRAM broker decides
// whether it may become RESIDENT right now. Those are separate questions and
// were, until this seam, separately answered: a lane could offer a model the
// broker would then refuse, and the user met that refusal after choosing.
//
// Two things are joined here, and nothing else is invented:
//
//   - The figure the broker admits against is the CHOSEN OPTION'S recorded
//     memory requirement. Not a constant, not a fraction of one, not something
//     re-derived from the model's size — the same number selection compared
//     against the measurement when it decided to offer the option at all.
//   - The broker's refusal is carried out as ITS OWN KIND of answer. "This host
//     cannot run it" and "this host cannot run it right now, something else
//     holds the memory" are different facts with different remedies, and the
//     second may succeed in a minute.
//
// The arbitration itself is NOT reimplemented here (§11.4.74). internal/vrambroker
// already measures the card, applies the headroom, enforces single-owner burst
// classes and fails closed on an unreadable budget. This package asks it, and
// keeps hold of what it grants.

// The admission gate itself — Lease, Admitter and BrokerAdmitter — is the one
// declared in streaming_launch.go and is NOT redeclared here (§11.4.74: the
// working seam is extended, not duplicated). This file adds what turning a
// SELECTED OPTION into a HELD reservation needs on top of it.

// Compile-time proof that the broker's lease satisfies the seam this package
// admits through. It is the same guard internal/lifecycle keeps on the release
// side, restated here because this is where the reservation is TAKEN: if the
// broker's contract changes, both ends stop compiling rather than one end
// silently holding something the other cannot hand back.
var _ Lease = (*vrambroker.Lease)(nil)

// Budgeter is the optional live-reading half of an admission gate. When an
// Admitter also reports the budget, a refusal records what the card actually had
// free, so "it does not fit right now" is a statement with a number behind it.
// A gate that cannot report one still refuses for a stated reason; the figure is
// diagnostic, never the thing the refusal rests on.
type Budgeter interface {
	Budget() (total, used, free int64)
}

// Errors reported by Acquire before the gate is ever asked. Each is a defect in
// what the caller supplied rather than a statement about the host, which is why
// none of them is an AdmissionRefusal.
var (
	// ErrNoAdmitter: no admission gate was supplied. Admitting nothing at all is
	// not the same as admitting freely, so this refuses rather than proceeding.
	ErrNoAdmitter = errors.New("runtime: no admission gate — a reservation cannot be admitted by nobody")

	// ErrNoClass: the broker's answer depends on which service class is asking —
	// resident, warm and burst are arbitrated differently — so an unnamed class
	// has no answer.
	ErrNoClass = errors.New("runtime: no service class named for the admission")

	// ErrNoRecordedRequirement: the option carries no memory requirement. A zero
	// figure is not a model that needs nothing; it is a figure that was never
	// recorded, and admitting against it would walk anything past a gate that
	// admits on size (§11.4.6).
	ErrNoRecordedRequirement = errors.New("runtime: the chosen option records no memory requirement to admit against")

	// ErrRequirementOutOfRange: the recorded requirement does not fit the signed
	// byte count the broker admits in. Refused rather than truncated — a
	// truncated figure would be admitted as a smaller model than the real one.
	ErrRequirementOutOfRange = errors.New("runtime: the recorded memory requirement is too large to admit against")

	// ErrNoReservation: the gate reported success and handed back nothing. A
	// caller must never treat that as a hold it can later release.
	ErrNoReservation = errors.New("runtime: admission reported success but granted no reservation")
)

// AdmissionReason is why the gate refused a reservation.
//
// These are deliberately NOT RefusalReason values. A RefusalReason says no path
// serves this model on this host — a standing fact about the machine. An
// AdmissionReason says the path exists and the resource is not available at this
// moment. Collapsing them would tell a user to buy a bigger machine when the
// real answer is to wait for the job in front of them to finish.
type AdmissionReason string

const (
	// AdmissionBudgetExhausted: the measured free VRAM does not cover this
	// reservation plus the broker's safety headroom. Something else holds the
	// memory now; it may not in a minute.
	AdmissionBudgetExhausted AdmissionReason = "budget_exhausted_now"
	// AdmissionBurstInUse: a burst-class job owns the card and burst classes are
	// single-owner (§11.4.119). The queue in front of this request is one job long.
	AdmissionBurstInUse AdmissionReason = "burst_in_use"
	// AdmissionBudgetUnreadable: the live VRAM reading failed, so the gate
	// refused fail-closed rather than guess. Nothing is known about the card's
	// capacity, which is not the same as knowing it is full.
	AdmissionBudgetUnreadable AdmissionReason = "budget_unreadable"
	// AdmissionThermalUnsafe: the card is outside its safe thermal/power envelope
	// for taking on new work (§11.4.133).
	AdmissionThermalUnsafe AdmissionReason = "thermal_unsafe"
	// AdmissionRefused: the gate refused for a reason this package does not
	// recognise. Reported as itself rather than mapped onto a neighbour, because
	// guessing which of the above it resembles would state something untrue.
	AdmissionRefused AdmissionReason = "admission_refused"
)

// Known reports whether a is one of the recorded admission reasons.
func (a AdmissionReason) Known() bool {
	switch a {
	case AdmissionBudgetExhausted, AdmissionBurstInUse, AdmissionBudgetUnreadable,
		AdmissionThermalUnsafe, AdmissionRefused:
		return true
	default:
		return false
	}
}

// Admission remedies. They are values of the same Remedy type as the path
// refusals' remedies and share none of them: a refusal that asked for the same
// thing as a path refusal would be a path refusal wearing another name.
const (
	// RemedyRetryWhenCardFrees: nothing about this host or model needs to
	// change. The resource is held by something else and the request can be made
	// again.
	RemedyRetryWhenCardFrees Remedy = "retry-when-the-accelerator-frees"
	// RemedyRestoreAcceleratorReading: the capacity could not be read, so it
	// could not be admitted against. The reading is what needs fixing.
	RemedyRestoreAcceleratorReading Remedy = "restore-the-accelerator-reading"
	// RemedyLetTheCardCool: the card is too hot or drawing too much to take on
	// more work safely.
	RemedyLetTheCardCool Remedy = "let-the-accelerator-cool"
	// RemedyInvestigateAdmission: an unrecognised refusal has no known remedy,
	// and saying otherwise would send the user somewhere on a guess.
	RemedyInvestigateAdmission Remedy = "investigate-the-admission-gate"
)

// Remedy returns what a asks of the caller, or the empty Remedy for an
// unrecorded reason.
func (a AdmissionReason) Remedy() Remedy {
	switch a {
	case AdmissionBudgetExhausted, AdmissionBurstInUse:
		return RemedyRetryWhenCardFrees
	case AdmissionBudgetUnreadable:
		return RemedyRestoreAcceleratorReading
	case AdmissionThermalUnsafe:
		return RemedyLetTheCardCool
	case AdmissionRefused:
		return RemedyInvestigateAdmission
	default:
		return ""
	}
}

// Retryable reports whether the same request, unchanged, could succeed later.
//
// It is stated per reason rather than assumed for all of them: a budget held by
// another job and a card that is too hot both clear on their own, while a VRAM
// reading that cannot be taken will not start working because time passed, and
// an unrecognised refusal is not known to clear at all (§11.4.6).
func (a AdmissionReason) Retryable() bool {
	switch a {
	case AdmissionBudgetExhausted, AdmissionBurstInUse, AdmissionThermalUnsafe:
		return true
	default:
		return false
	}
}

// AdmissionRefusal is the answer when the option is servable here but its memory
// could not be reserved now. It is an error so callers can use errors.As, and it
// wraps the broker's own sentinel so errors.Is(err, vrambroker.ErrBudgetExceeded)
// keeps working through it.
type AdmissionRefusal struct {
	Reason AdmissionReason
	Class  vrambroker.Class

	ModelID string
	Variant string
	// Identity is the option's host-qualified name, carried so a refusal names
	// the same thing the offer did.
	Identity     string
	HostIdentity string

	// NeedBytes is the figure that was refused — the chosen option's recorded
	// memory requirement, which is what makes this refusal checkable.
	NeedBytes int64

	// ObservedFreeBytes is what the gate reported free, read AFTER the refusal
	// for diagnosis. BudgetObserved says whether it was readable at all; the
	// figure describes a moment just after the decision, not the decision's own
	// instant, and is not the number the gate refused on.
	ObservedFreeBytes int64
	BudgetObserved    bool

	// Err is the gate's own error, kept whole.
	Err error
}

func (a *AdmissionRefusal) Error() string {
	return fmt.Sprintf(
		"runtime: %q is servable on host %q but its %d bytes could not be reserved right now: %s (remedy: %s): %v",
		a.model(), a.HostIdentity, a.NeedBytes, a.Reason, a.Reason.Remedy(), a.Err)
}

// Unwrap exposes the gate's error so a caller can match the broker's sentinels
// with errors.Is without this package re-enumerating them.
func (a *AdmissionRefusal) Unwrap() error { return a.Err }

// Retryable reports whether the same request could succeed later.
func (a *AdmissionRefusal) Retryable() bool { return a.Reason.Retryable() }

func (a *AdmissionRefusal) model() string {
	if a.Identity != "" {
		return a.Identity
	}
	if a.Variant == "" {
		return a.ModelID
	}
	return a.ModelID + ":" + a.Variant
}

// Held is a chosen option whose memory the gate has admitted, and the hold on
// it.
//
// It is itself a Lease, which is the whole answer to who releases the
// reservation: the hold is what lifecycle.Manager.Track is given, so the
// lifecycle unload path and the lane's own deferred cleanup are the SAME
// release, guarded once here — not two independent releases racing to hand back
// one reservation. The broker's lease is idempotent too, so a double release is
// refused twice over rather than relied on to be prevented once.
type Held struct {
	// Option is what was chosen and admitted.
	Option selection.Option
	// Class is the service class the reservation was admitted under.
	Class vrambroker.Class
	// NeedBytes is the figure admitted against — Option.Cost.MemoryRequiredBytes.
	NeedBytes int64

	lease    Lease
	released atomic.Bool
}

// Held is what lifecycle is handed, so it must satisfy the interface lifecycle
// releases through. If either contract moves, this stops compiling.
var (
	_ Lease              = (*Held)(nil)
	_ lifecycle.Releaser = (*Held)(nil)
)

// Release hands the reservation back to the broker that granted it. It is
// idempotent and safe on a nil hold — a lane that defers Release before knowing
// whether admission succeeded must not panic, and a hold already released by a
// lifecycle unload must not credit the budget a second time.
func (h *Held) Release() {
	if h == nil {
		return
	}
	if !h.released.CompareAndSwap(false, true) {
		return
	}
	if h.lease != nil {
		h.lease.Release()
	}
}

// Released reports whether the reservation has been handed back. A nil hold
// holds nothing, so it reads as released.
func (h *Held) Released() bool { return h == nil || h.released.Load() }

// Acquire admits the chosen option's recorded memory requirement under class and
// returns the hold, or an *AdmissionRefusal saying why the memory could not be
// reserved now.
//
// A refusal returns NO hold — not an empty one — so there is nothing to release
// and nothing to leak on the failing path.
func Acquire(ctx context.Context, gate Admitter, class vrambroker.Class, opt selection.Option) (*Held, error) {
	if gate == nil {
		return nil, ErrNoAdmitter
	}
	if class == "" {
		return nil, fmt.Errorf("%w: option=%s", ErrNoClass, opt.Identity)
	}

	need, err := needBytesFor(opt)
	if err != nil {
		return nil, err
	}

	lease, err := gate.Acquire(ctx, class, need)
	if err != nil {
		return nil, refuseAdmission(gate, class, opt, need, err)
	}
	if lease == nil {
		return nil, fmt.Errorf("%w: class=%s option=%s", ErrNoReservation, class, opt.Identity)
	}

	return &Held{
		Option:    opt,
		Class:     class,
		NeedBytes: need,
		lease:     lease,
	}, nil
}

// needBytesFor is the figure the gate admits against: the CHOSEN OPTION'S
// recorded memory requirement, read off the option and nothing else.
//
// It is a function of one line and its own name so the join has exactly one
// place, and so the §1.1 paired mutation — replacing it with a static figure —
// is a single visible edit rather than a change spread through the call.
func needBytesFor(opt selection.Option) (int64, error) {
	need := opt.Cost.MemoryRequiredBytes
	if need == 0 {
		return 0, fmt.Errorf("%w: option=%s", ErrNoRecordedRequirement, opt.Identity)
	}
	if need > math.MaxInt64 {
		return 0, fmt.Errorf("%w: option=%s bytes=%d", ErrRequirementOutOfRange, opt.Identity, need)
	}
	return int64(need), nil
}

// refuseAdmission turns the gate's error into this package's own kind of answer,
// keeping the gate's error whole underneath it.
func refuseAdmission(gate Admitter, class vrambroker.Class, opt selection.Option, need int64, err error) *AdmissionRefusal {
	r := &AdmissionRefusal{
		Reason:       classifyAdmission(err),
		Class:        class,
		ModelID:      opt.ModelID,
		Variant:      opt.Variant,
		Identity:     opt.Identity,
		HostIdentity: opt.HostIdentity,
		NeedBytes:    need,
		Err:          err,
	}
	// The reading is diagnostic and optional: a gate that cannot report a budget
	// still refuses for a stated reason, and an unreadable budget is reported as
	// unobserved rather than as zero free (§11.4.6).
	if b, ok := gate.(Budgeter); ok {
		total, _, free := b.Budget()
		if total > 0 {
			r.ObservedFreeBytes = free
			r.BudgetObserved = true
		}
	}
	return r
}

// classifyAdmission maps the broker's sentinels onto the reasons above. An error
// it does not recognise is reported as unrecognised rather than folded into the
// nearest neighbour.
func classifyAdmission(err error) AdmissionReason {
	switch {
	case errors.Is(err, vrambroker.ErrBudgetExceeded):
		return AdmissionBudgetExhausted
	case errors.Is(err, vrambroker.ErrBurstInUse):
		return AdmissionBurstInUse
	case errors.Is(err, vrambroker.ErrBudgetUnavailable):
		return AdmissionBudgetUnreadable
	case errors.Is(err, vrambroker.ErrThermalUnsafe):
		return AdmissionThermalUnsafe
	default:
		return AdmissionRefused
	}
}
