package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	catfixtures "github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
	"github.com/HelixDevelopment/HelixLLM/internal/lifecycle"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
	"github.com/stretchr/testify/require"
)

// --- the option under test comes from a REAL selection ------------------------

// chosenOption returns an option produced by an actual selection.Select against
// a fixture host and the fixture catalogue.
//
// It is not hand-built. The whole point of this seam is that the figure the
// broker admits against is the one SELECTION recorded on the option it offered,
// so a test that invented that figure itself would prove nothing about the join.
func chosenOption(t *testing.T) selection.Option {
	t.Helper()
	res, err := selection.Select(selection.Request{
		Profile:       fixtures.DualAccelerator(),
		Entries:       catfixtures.Entries(),
		Families:      []catalogue.CapabilityFamily{catalogue.FamilyText},
		DeclaredUsage: catalogue.UsageCommercial,
		Now:           time.Now().UTC(),
	})
	require.NoError(t, err)
	fam, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok, "fixture precondition: the text family must be evaluated")
	require.NotEmpty(t, fam.Offered, "fixture precondition: the text family must offer something to admit")
	opt := fam.Offered[0]
	require.NotZero(t, opt.Cost.MemoryRequiredBytes,
		"fixture precondition: the offered option must carry a recorded memory requirement")
	return opt
}

// --- unit-test-only admission gate -------------------------------------------

// budgetAdmitter models the broker's arbitration for unit tests (CONST-050(A):
// fakes live ONLY in _test.go). The REAL arbitration is exercised by
// internal/vrambroker's own tests and, through this seam, by
// TestRealBrokerRefusesAnOverBudgetOptionThroughTheSeam below.
//
// It records every needBytes the seam asked for, so a wiring that admitted
// against anything other than the chosen option's recorded figure is visible
// rather than merely suspected.
type budgetAdmitter struct {
	mu       sync.Mutex
	total    int64
	free     int64
	headroom int64

	live     int     // reservations currently held
	peakLive int     // the most that were ever held at once
	releases int     // every Release call that reached the gate
	seen     []int64 // every needBytes the seam asked for, in order

	failWith error // when set, every Acquire refuses with this
}

func (a *budgetAdmitter) Acquire(_ context.Context, _ vrambroker.Class, needBytes int64) (runtime.Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.seen = append(a.seen, needBytes)
	if a.failWith != nil {
		return nil, a.failWith
	}
	if needBytes < 0 || a.free < needBytes+a.headroom {
		return nil, fmt.Errorf("%w: need=%d free=%d headroom=%d",
			vrambroker.ErrBudgetExceeded, needBytes, a.free, a.headroom)
	}
	a.free -= needBytes
	a.live++
	if a.live > a.peakLive {
		a.peakLive = a.live
	}
	return &fakeReservation{gate: a, need: needBytes}, nil
}

func (a *budgetAdmitter) Budget() (total, used, free int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.total, a.total - a.free, a.free
}

func (a *budgetAdmitter) snapshot() (live, peak, releases int, free int64, seen []int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live, a.peakLive, a.releases, a.free, append([]int64(nil), a.seen...)
}

// fakeReservation is deliberately NOT idempotent: a second release credits the
// budget a second time and is counted. The real *vrambroker.Lease IS idempotent,
// so a fake that was too would pass the double-release tests below even with the
// seam's own guard removed — the fake would be doing the guarding.
type fakeReservation struct {
	gate *budgetAdmitter
	need int64
}

func (r *fakeReservation) Release() {
	r.gate.mu.Lock()
	defer r.gate.mu.Unlock()
	r.gate.releases++
	r.gate.live--
	r.gate.free += r.need
}

func gib(n int64) int64 { return n * 1024 * 1024 * 1024 }

// --- 1. the admitted figure is the chosen option's, and nothing else ---------

// TestAcquireAdmitsTheChosenOptionsRecordedFigure.
//
// This is the guard the §1.1 paired mutation attacks: replace the wiring's
// figure with a static one and this test must FAIL, naming the wrong figure.
func TestAcquireAdmitsTheChosenOptionsRecordedFigure(t *testing.T) {
	opt := chosenOption(t)
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err)
	require.NotNil(t, held)
	t.Cleanup(held.Release)

	_, _, _, _, seen := gate.snapshot()
	require.Len(t, seen, 1, "the seam must ask the broker exactly once")
	require.Equal(t, int64(opt.Cost.MemoryRequiredBytes), seen[0],
		"the broker MUST be admitted against the CHOSEN OPTION's recorded requirement (%d bytes), "+
			"but it was asked to admit %d bytes — a figure that came from somewhere else",
		opt.Cost.MemoryRequiredBytes, seen[0])
	require.Equal(t, int64(opt.Cost.MemoryRequiredBytes), held.NeedBytes,
		"the held reservation must state the figure it was admitted against")
	require.Equal(t, opt.Identity, held.Option.Identity, "the hold must carry the option it was taken for")
}

// TestTheAdmittedFigureTracksTheOptionAndIsNotAConstant.
//
// Two options with different recorded requirements must produce two different
// admitted figures. A wiring that passed a constant satisfies neither the equality
// above nor the inequality here.
func TestTheAdmittedFigureTracksTheOptionAndIsNotAConstant(t *testing.T) {
	small := chosenOption(t)
	large := small
	large.Cost.MemoryRequiredBytes = small.Cost.MemoryRequiredBytes + uint64(gib(3))

	gate := &budgetAdmitter{total: gib(64), free: gib(48), headroom: gib(2)}

	h1, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, small)
	require.NoError(t, err)
	t.Cleanup(h1.Release)
	h2, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, large)
	require.NoError(t, err)
	t.Cleanup(h2.Release)

	_, _, _, _, seen := gate.snapshot()
	require.Len(t, seen, 2)
	require.NotEqual(t, seen[0], seen[1],
		"two options with different recorded requirements must be admitted against different figures; "+
			"identical figures mean the wiring is passing something static")
	require.Equal(t, int64(small.Cost.MemoryRequiredBytes), seen[0])
	require.Equal(t, int64(large.Cost.MemoryRequiredBytes), seen[1])
}

// --- 2. a broker refusal is a different fact from a selection refusal --------

// TestOverBudgetOptionIsRefusedWithTheBrokersReasonNotSelections.
//
// Selection saying "this host cannot run it" and the broker saying "this host
// cannot run it RIGHT NOW" are different facts with different remedies. The
// second may succeed in a minute, so a caller must be able to tell them apart
// without parsing prose.
func TestOverBudgetOptionIsRefusedWithTheBrokersReasonNotSelections(t *testing.T) {
	opt := chosenOption(t)
	// The host has plenty of memory — selection OFFERED this option. The card,
	// right now, does not have room for it.
	gate := &budgetAdmitter{total: gib(32), free: gib(1), headroom: gib(2)}

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.Nil(t, held, "a refused admission holds nothing")
	require.Error(t, err)

	var adm *runtime.AdmissionRefusal
	require.ErrorAs(t, err, &adm, "a broker refusal must surface as *runtime.AdmissionRefusal, got %T", err)
	require.Equal(t, runtime.AdmissionBudgetExhausted, adm.Reason)
	require.True(t, adm.Reason.Known())
	require.True(t, adm.Retryable(), "memory held by something else may be free again shortly")
	require.ErrorIs(t, err, vrambroker.ErrBudgetExceeded,
		"the broker's own sentinel must remain reachable through the refusal")
	require.Equal(t, int64(opt.Cost.MemoryRequiredBytes), adm.NeedBytes,
		"the refusal must state the figure that did not fit")
	require.Equal(t, opt.Identity, adm.Identity)

	// It is NOT a selection/runtime path refusal. Those say no path exists here
	// at all; this says the path exists and the memory is currently taken.
	var pathRefusal *runtime.Refusal
	require.False(t, errors.As(err, &pathRefusal),
		"a broker refusal must NOT masquerade as a *runtime.Refusal — the remedies differ")
}

// TestEachBrokerSentinelKeepsItsOwnReason.
func TestEachBrokerSentinelKeepsItsOwnReason(t *testing.T) {
	opt := chosenOption(t)
	cases := []struct {
		name      string
		from      error
		want      runtime.AdmissionReason
		retryable bool
	}{
		{"budget exceeded", vrambroker.ErrBudgetExceeded, runtime.AdmissionBudgetExhausted, true},
		{"burst in use", vrambroker.ErrBurstInUse, runtime.AdmissionBurstInUse, true},
		{"budget unreadable", vrambroker.ErrBudgetUnavailable, runtime.AdmissionBudgetUnreadable, false},
		{"thermally unsafe", vrambroker.ErrThermalUnsafe, runtime.AdmissionThermalUnsafe, true},
		{"unclassified", errors.New("something else entirely"), runtime.AdmissionRefused, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2), failWith: c.from}
			held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassImage, opt)
			require.Nil(t, held)
			var adm *runtime.AdmissionRefusal
			require.ErrorAs(t, err, &adm)
			require.Equal(t, c.want, adm.Reason)
			require.Equal(t, c.retryable, adm.Retryable())
			require.ErrorIs(t, err, c.from, "the underlying cause must stay reachable")
		})
	}
}

// TestAdmissionRemediesNeverCollideWithPathRefusalRemedies.
//
// Two reasons with the same remedy are one reason wearing two names. The whole
// value of keeping a broker refusal distinct from a path refusal is that it asks
// something DIFFERENT of the user, so the remedies must not overlap.
func TestAdmissionRemediesNeverCollideWithPathRefusalRemedies(t *testing.T) {
	pathRemedies := map[string]runtime.RefusalReason{}
	for _, r := range []runtime.RefusalReason{
		runtime.ReasonInsufficientResources,
		runtime.ReasonUnsupportedConfiguration,
		runtime.ReasonHostNotMeasured,
	} {
		require.NotEmpty(t, string(r.Remedy()), "every recorded path reason has a remedy")
		pathRemedies[string(r.Remedy())] = r
	}
	// Selection's own remedies are the other half of "not selection's refusal".
	for _, r := range []selection.WithheldReason{
		selection.ReasonInsufficientResources,
		selection.ReasonUnsupportedConfiguration,
		selection.ReasonExcludedByUsageTerms,
	} {
		pathRemedies[string(r.Remedy())] = runtime.RefusalReason(r)
	}

	seen := map[string]struct{}{}
	for _, a := range []runtime.AdmissionReason{
		runtime.AdmissionBudgetExhausted,
		runtime.AdmissionBurstInUse,
		runtime.AdmissionBudgetUnreadable,
		runtime.AdmissionThermalUnsafe,
		runtime.AdmissionRefused,
	} {
		require.True(t, a.Known(), "%q must be a recorded admission reason", a)
		remedy := string(a.Remedy())
		require.NotEmpty(t, remedy, "admission reason %q must ask something of the user", a)
		collided, dup := pathRemedies[remedy]
		require.False(t, dup,
			"admission reason %q asks for %q, which is also what path reason %q asks for — "+
				"the two refusal kinds would be indistinguishable in what they demand", a, remedy, collided)
		seen[remedy] = struct{}{}
	}
	require.NotEmpty(t, seen)
}

// --- 3. a granted admission really holds, and release really returns it ------

// TestGrantedAcquireHoldsTheReservation.
func TestGrantedAcquireHoldsTheReservation(t *testing.T) {
	opt := chosenOption(t)
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}
	before := gate.free

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err)

	live, _, _, free, _ := gate.snapshot()
	require.Equal(t, 1, live, "a granted acquire must leave a reservation HELD, not merely report success")
	require.Equal(t, before-int64(opt.Cost.MemoryRequiredBytes), free,
		"the held reservation must be charged against the budget")
	require.False(t, held.Released())

	held.Release()
	live, _, releases, free, _ := gate.snapshot()
	require.Zero(t, live)
	require.Equal(t, 1, releases)
	require.Equal(t, before, free, "release must return exactly what was taken")
	require.True(t, held.Released())
}

// TestReleaseReturnsTheBudgetSoTheNextAcquireFits.
//
// "The budget came back" is only credible if it can be spent again.
func TestReleaseReturnsTheBudgetSoTheNextAcquireFits(t *testing.T) {
	opt := chosenOption(t)
	need := int64(opt.Cost.MemoryRequiredBytes)
	// Room for exactly one of these at a time.
	gate := &budgetAdmitter{total: gib(32), free: need, headroom: 0}

	first, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err)

	blocked, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.Nil(t, blocked)
	require.ErrorIs(t, err, vrambroker.ErrBudgetExceeded, "the second must not fit while the first is held")

	first.Release()

	second, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err, "after the release the same figure must fit again")
	t.Cleanup(second.Release)
}

// --- 4. release happens exactly once, down either path -----------------------

// TestReleaseIsIdempotent.
func TestReleaseIsIdempotent(t *testing.T) {
	opt := chosenOption(t)
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}
	before := gate.free

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		held.Release()
	}

	live, _, releases, free, _ := gate.snapshot()
	require.Equal(t, 1, releases, "five Release calls must reach the gate exactly once")
	require.Zero(t, live)
	require.Equal(t, before, free, "the budget must be credited once, not five times")
}

// TestConcurrentReleaseReturnsTheBudgetExactlyOnce runs under -race.
func TestConcurrentReleaseReturnsTheBudgetExactlyOnce(t *testing.T) {
	opt := chosenOption(t)
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}
	before := gate.free

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			held.Release()
		}()
	}
	wg.Wait()

	_, _, releases, free, _ := gate.snapshot()
	require.Equal(t, 1, releases)
	require.Equal(t, before, free)
}

// TestLifecycleUnloadAndSeamReleaseCannotDoubleRelease.
//
// lifecycle already releases a model's reservation when it unloads it. The hold
// this seam produces IS what lifecycle is handed, so the two paths funnel into
// one guarded release rather than two independent ones.
func TestLifecycleUnloadAndSeamReleaseCannotDoubleRelease(t *testing.T) {
	opt := chosenOption(t)
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}
	before := gate.free

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.NoError(t, err)

	// A *Held is directly trackable by lifecycle — no adapter, no second seam.
	var _ lifecycle.Releaser = held

	clock := &fakeClock{now: time.Now().UTC()}
	mgr, err := lifecycle.New(
		lifecycle.Config{IdleTimeout: time.Minute},
		func(context.Context, string) error { return nil },
		notifierFunc(func(lifecycle.UnloadEvent) {}),
		lifecycle.WithClock(clock.Now),
	)
	require.NoError(t, err)
	require.NoError(t, mgr.Track(opt.ModelID, int64(opt.Cost.MemoryRequiredBytes), held))

	clock.advance(2 * time.Minute)
	events, err := mgr.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1, "the idle model must actually be unloaded")

	_, _, releases, free, _ := gate.snapshot()
	require.Equal(t, 1, releases, "lifecycle's unload must return the reservation exactly once")
	require.Equal(t, before, free)
	require.True(t, held.Released(), "the hold must know its reservation is gone")

	// The lane that took the hold now releases it too — a normal deferred
	// cleanup. It must not credit the budget a second time.
	held.Release()
	_, _, releases, free, _ = gate.snapshot()
	require.Equal(t, 1, releases, "a deferred release after a lifecycle unload must be a no-op")
	require.Equal(t, before, free)
}

type notifierFunc func(lifecycle.UnloadEvent)

func (f notifierFunc) ModelUnloaded(ev lifecycle.UnloadEvent) { f(ev) }

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// --- 5. concurrency: the same budget is never granted twice ------------------

// TestConcurrentAcquireOfTheSameBudgetGrantsOnce runs under -race.
func TestConcurrentAcquireOfTheSameBudgetGrantsOnce(t *testing.T) {
	opt := chosenOption(t)
	need := int64(opt.Cost.MemoryRequiredBytes)
	// Room for exactly one.
	gate := &budgetAdmitter{total: gib(32), free: need, headroom: 0}

	const racers = 8
	var (
		granted atomic.Int64
		refused atomic.Int64
		mu      sync.Mutex
		holds   []*runtime.Held
		wg      sync.WaitGroup
		start   = make(chan struct{})
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
			if err != nil {
				refused.Add(1)
				if held != nil {
					t.Error("a refused acquire must not return a hold")
				}
				if !errors.Is(err, vrambroker.ErrBudgetExceeded) {
					t.Errorf("refusal must carry the budget reason, got %v", err)
				}
				return
			}
			granted.Add(1)
			mu.Lock()
			holds = append(holds, held)
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), granted.Load(), "exactly one racer may hold a budget that fits one")
	require.Equal(t, int64(racers-1), refused.Load())

	live, peak, _, _, seen := gate.snapshot()
	require.Equal(t, 1, peak, "the budget must never have been granted twice at once")
	require.Equal(t, 1, live)
	require.Len(t, seen, racers, "every racer must have asked the gate")
	for _, s := range seen {
		require.Equal(t, need, s, "every request must carry the chosen option's figure")
	}

	for _, h := range holds {
		h.Release()
	}
	live, _, _, free, _ := gate.snapshot()
	require.Zero(t, live)
	require.Equal(t, need, free, "the budget must be whole again")
}

// --- 6. a refusal leaves nothing behind --------------------------------------

// TestRefusedAcquireLeavesNoPartialState.
func TestRefusedAcquireLeavesNoPartialState(t *testing.T) {
	opt := chosenOption(t)
	for _, cause := range []error{
		vrambroker.ErrBudgetExceeded,
		vrambroker.ErrBurstInUse,
		vrambroker.ErrBudgetUnavailable,
		vrambroker.ErrThermalUnsafe,
	} {
		t.Run(cause.Error()[:24], func(t *testing.T) {
			gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2), failWith: cause}
			before := gate.free

			held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVideo, opt)
			require.Error(t, err)
			require.Nil(t, held, "a refusal must return no hold at all — not an empty one")

			live, peak, releases, free, _ := gate.snapshot()
			require.Zero(t, live, "a refusal must hold nothing")
			require.Zero(t, peak, "a refusal must never have held anything, even briefly")
			require.Zero(t, releases, "there is nothing to release after a refusal")
			require.Equal(t, before, free, "a refusal must not move the budget")
		})
	}
}

// --- 7. the seam refuses what it cannot honestly admit -----------------------

// TestOptionWithNoRecordedRequirementIsRefusedBeforeTheBroker.
//
// A zero requirement is not "free" — it is a figure that was never recorded, and
// admitting against it would let anything past a gate that admits on size.
func TestOptionWithNoRecordedRequirementIsRefusedBeforeTheBroker(t *testing.T) {
	opt := chosenOption(t)
	opt.Cost.MemoryRequiredBytes = 0
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}

	held, err := runtime.Acquire(context.Background(), gate, vrambroker.ClassVLM, opt)
	require.Nil(t, held)
	require.ErrorIs(t, err, runtime.ErrNoRecordedRequirement)

	_, _, _, _, seen := gate.snapshot()
	require.Empty(t, seen, "the broker must never be asked to admit an unrecorded figure")
}

// TestNilAdmitterIsRefused.
func TestNilAdmitterIsRefused(t *testing.T) {
	held, err := runtime.Acquire(context.Background(), nil, vrambroker.ClassVLM, chosenOption(t))
	require.Nil(t, held)
	require.ErrorIs(t, err, runtime.ErrNoAdmitter)
}

// TestClasslessAcquireIsRefused: the broker's answer depends on which class is
// asking, so an unnamed class has no answer.
func TestClasslessAcquireIsRefused(t *testing.T) {
	gate := &budgetAdmitter{total: gib(32), free: gib(24), headroom: gib(2)}
	held, err := runtime.Acquire(context.Background(), gate, vrambroker.Class(""), chosenOption(t))
	require.Nil(t, held)
	require.ErrorIs(t, err, runtime.ErrNoClass)

	_, _, _, _, seen := gate.snapshot()
	require.Empty(t, seen)
}

// TestReleaseOnNilHoldIsSafe — a deferred release of a hold that was never
// granted must not panic the lane that deferred it.
func TestReleaseOnNilHoldIsSafe(t *testing.T) {
	var held *runtime.Held
	require.NotPanics(t, held.Release)
	require.True(t, held.Released())
}

// --- 8. the real broker, through this seam -----------------------------------

// TestRealBrokerRefusesAnOverBudgetOptionThroughTheSeam drives the REAL
// nvidia-smi-backed broker (no fake) and proves the refusal a caller sees is the
// broker's, carried by this seam, with nothing held afterwards.
//
// It is non-perturbing: an over-budget request allocates nothing, and the only
// device access is nvidia-smi's read-only query (§11.4.133).
func TestRealBrokerRefusesAnOverBudgetOptionThroughTheSeam(t *testing.T) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("SKIP-OK: nvidia-smi not on PATH — this proof needs the real card, hardware_not_present")
	}

	opt := chosenOption(t)
	// A recorded requirement no card has. The figure still travels the same
	// path: it is read off the option, never assumed.
	opt.Cost.MemoryRequiredBytes = uint64(gib(900))

	broker := vrambroker.New()
	adm := runtime.BrokerAdmitter(broker)
	total, used, free := broker.Budget()
	t.Logf("live nvidia-smi budget: total=%dMiB used=%dMiB free=%dMiB", total>>20, used>>20, free>>20)

	held, err := runtime.Acquire(context.Background(), adm, vrambroker.ClassVLM, opt)
	require.Nil(t, held, "an over-budget option must hold nothing on the real card")
	require.Error(t, err)

	var refusal *runtime.AdmissionRefusal
	require.ErrorAs(t, err, &refusal)
	if errors.Is(err, vrambroker.ErrBudgetUnavailable) {
		t.Skip("SKIP-OK: nvidia-smi present but unreadable here — the budget could not be measured, " +
			"so the refusal is fail-closed rather than over-budget (hardware_not_present)")
	}
	require.Equal(t, runtime.AdmissionBudgetExhausted, refusal.Reason)
	require.ErrorIs(t, err, vrambroker.ErrBudgetExceeded)
	require.Equal(t, int64(gib(900)), refusal.NeedBytes)
	t.Logf("real refusal: %v", err)
}

// TestRealBrokerGrantsAndReleasesThroughTheSeam takes a genuinely small
// reservation on the real card and gives it straight back.
func TestRealBrokerGrantsAndReleasesThroughTheSeam(t *testing.T) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("SKIP-OK: nvidia-smi not on PATH — this proof needs the real card, hardware_not_present")
	}

	broker := vrambroker.New()
	adm := runtime.BrokerAdmitter(broker)
	_, _, free := broker.Budget()
	if free < gib(3) {
		t.Skipf("SKIP-OK: only %dMiB free on the card — not enough above the broker's headroom to take "+
			"even a token reservation without disturbing what is resident (hardware_not_present)", free>>20)
	}

	opt := chosenOption(t)
	opt.Cost.MemoryRequiredBytes = uint64(64 * 1024 * 1024) // 64 MiB: a token hold

	held, err := runtime.Acquire(context.Background(), adm, vrambroker.ClassVLM, opt)
	require.NoError(t, err)
	require.NotNil(t, held)
	require.Equal(t, int64(64*1024*1024), held.NeedBytes)
	require.False(t, held.Released())

	held.Release()
	require.True(t, held.Released())
	held.Release() // idempotent against the real lease too
	require.True(t, held.Released())
}
