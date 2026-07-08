package vrambroker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// HeadroomBytes is the safety margin (2 GiB, design §4) kept free above every
// admitted reservation so an admission never drives the card to the edge.
const HeadroomBytes int64 = 2 * 1024 * MiB

// Class is the service class requesting VRAM (design §4).
type Class string

const (
	ClassCoder Class = "coder" // resident tier — the pinned hot path

	// ClassAgent is the warm tier for a SECOND coder/agent instance (Lane B —
	// Serving-plan Task 1.4, master plan §6.2). Semantics are IDENTICAL to
	// ClassVLM: IsResident()==false, IsBurst()==false, admission-gated by the
	// measured Budget().free (see Acquire's warm-tier fall-through path
	// below). ClassAgent is a DISTINCT class from ClassVLM ON PURPOSE — danger
	// zone D5: reusing ClassVLM for a second coding lane would let a future
	// genuine vision-serving workload collide with Lane B for the same
	// broker class, since the broker doesn't single-owner-enforce warm
	// classes. Do NOT fold ClassAgent back into ClassVLM.
	ClassAgent     Class = "agent"     // warm tier — 2nd coder/agent instance (Lane B, §11.4.119/D5)
	ClassVLM       Class = "vlm"       // warm tier
	ClassImage     Class = "image"     // burst tier — single-owner (§11.4.119)
	ClassVideo     Class = "video"     // burst tier — single-owner (§11.4.119)
	ClassTranslate Class = "translate" // warm tier
	ClassEmbed     Class = "embed"     // CPU-only — no GPU reservation
)

// IsBurst reports whether c is a burst class (started per-job, never co-resident
// with another burst — design §2 / §11.4.119).
func (c Class) IsBurst() bool { return c == ClassImage || c == ClassVideo }

// IsResident reports whether c is the resident (never-evicted) coder tier.
func (c Class) IsResident() bool { return c == ClassCoder }

// Lease is a granted VRAM reservation. Release is idempotent.
type Lease struct {
	ID        string // unique lease id
	Class     Class  // service class this lease was granted for
	VRAMBytes int64  // the reservation the caller declared (measured need)
	Owner     string // single-owner tag for burst classes (§11.4.119)

	broker   *broker
	released atomic.Bool
}

// Release frees the reservation held by this lease. It is idempotent — calling it
// more than once (or on a nil lease) is safe and a no-op after the first call.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	if !l.released.CompareAndSwap(false, true) {
		return
	}
	l.broker.release(l)
}

// Broker is the cross-service VRAM admission gate (design §4).
type Broker interface {
	// Acquire admits a reservation of needBytes for class, or returns an error.
	//
	//   - Resident (coder): always granted, pinned; needBytes is recorded but not
	//     counted against admission (the fleet is already loaded).
	//   - Burst (image|video): single-owner (§11.4.119) — refused with
	//     ErrBurstInUse if another burst lease is live — AND admission-gated on
	//     measured free VRAM; over-budget requests are refused with
	//     ErrBudgetExceeded (fail-closed, never an OOM).
	//   - Warm (vlm|translate) and any other GPU class: admission-gated on
	//     measured free VRAM.
	//
	// ctx cancellation aborts the nvidia-smi read; the caller queues or degrades.
	Acquire(ctx context.Context, class Class, needBytes int64) (*Lease, error)

	// Budget returns the LIVE total/used/free VRAM in bytes from nvidia-smi. On a
	// read failure it returns 0,0,0 (admission via Acquire fails closed).
	Budget() (total, used, free int64)
}

// ThermalGuard reports whether the card is within a safe thermal/power envelope
// to admit a new GPU workload (§11.4.133). Returning a non-nil error refuses the
// admission. The core ships a permissive guard; a concrete temperature/power
// probe is injected via WithThermalGuard.
type ThermalGuard func(ctx context.Context) error

type broker struct {
	mu         sync.Mutex
	read       budgetReader
	thermal    ThermalGuard
	headroom   int64
	burstLease *Lease            // the single live burst lease, if any (§11.4.119)
	warm       map[string]*Lease // live warm-tier leases keyed by lease id
	seq        uint64
}

// Option configures a Broker.
type Option func(*broker)

// WithThermalGuard injects a §11.4.133 thermal/power safety probe consulted
// before admitting a GPU workload. Without it the broker uses a permissive guard.
func WithThermalGuard(g ThermalGuard) Option {
	return func(b *broker) {
		if g != nil {
			b.thermal = g
		}
	}
}

// New returns a Broker backed by the real nvidia-smi reader.
func New(opts ...Option) Broker {
	b := newWithReader(readNvidiaSMI)
	for _, o := range opts {
		o(b)
	}
	return b
}

// newWithReader builds a broker with an injected budget reader (test seam).
func newWithReader(r budgetReader) *broker {
	return &broker{
		read:     r,
		thermal:  func(context.Context) error { return nil },
		headroom: HeadroomBytes,
		warm:     make(map[string]*Lease),
	}
}

func (b *broker) Budget() (total, used, free int64) {
	t, u, f, err := b.read(context.Background())
	if err != nil {
		return 0, 0, 0
	}
	return t, u, f
}

func (b *broker) Acquire(ctx context.Context, class Class, needBytes int64) (*Lease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Resident tier (coder): always granted, pinned. The fleet is the hot path
	// and is already resident, so it is never counted against admission.
	if class.IsResident() {
		return b.grant(class, needBytes, ""), nil
	}

	// Single-owner enforcement for burst classes (§11.4.119): no second burst
	// lease may be live while one is. Checked BEFORE the budget read so a
	// contended burst is rejected deterministically.
	if class.IsBurst() && b.burstLease != nil {
		return nil, fmt.Errorf("%w: live=%s owner=%s", ErrBurstInUse, b.burstLease.Class, b.burstLease.Owner)
	}

	// Thermal/power safety gate (§11.4.133) — permissive by default, injectable.
	if err := b.thermal(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrThermalUnsafe, err)
	}

	// Admission on MEASURED free VRAM. An unreadable budget fails CLOSED.
	_, _, free, err := b.read(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
	}
	if !admit(free, needBytes, b.headroom) {
		return nil, fmt.Errorf("%w: need=%d free=%d headroom=%d", ErrBudgetExceeded, needBytes, free, b.headroom)
	}

	lease := b.grant(class, needBytes, string(class))
	if class.IsBurst() {
		b.burstLease = lease
	} else {
		b.warm[lease.ID] = lease
	}
	return lease, nil
}

// admit is the pure admission decision and the anti-bluff core of the broker: a
// request is admitted ONLY when the measured free VRAM covers the declared need
// PLUS the safety headroom. Fail-closed. This is the function the §1.1 paired
// mutation disables to prove an over-commit would then be (wrongly) granted.
func admit(free, needBytes, headroom int64) bool {
	if needBytes < 0 {
		return false
	}
	return free >= needBytes+headroom
}

func (b *broker) grant(class Class, needBytes int64, owner string) *Lease {
	b.seq++
	return &Lease{
		ID:        fmt.Sprintf("%s-%d-%d", class, time.Now().UnixNano(), b.seq),
		Class:     class,
		VRAMBytes: needBytes,
		Owner:     owner,
		broker:    b,
	}
}

func (b *broker) release(l *Lease) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.burstLease != nil && b.burstLease.ID == l.ID {
		b.burstLease = nil
		return
	}
	delete(b.warm, l.ID)
}
