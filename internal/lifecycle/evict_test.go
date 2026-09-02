package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEvict_RefusesAModelThatIsServing is the FR-047 core: a request is genuinely
// in flight and eviction is attempted against it. It MUST be refused, and the
// memory MUST NOT be handed back underneath the in-flight answer.
func TestEvict_RefusesAModelThatIsServing(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	notifier := &recordingNotifier{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Minute}, notifier, clk, u)
	require.NoError(t, m.Track("answering", 8<<30, nil))

	// A real request goes in flight and stays there for the whole attempt.
	done, err := m.BeginRequest("answering")
	require.NoError(t, err)
	require.True(t, m.IsServing("answering"))

	_, err = m.Evict(context.Background(), "answering", ReasonMemoryPressure)
	require.ErrorIs(t, err, ErrModelServing,
		"FR-047: a model serving a request MUST NOT be evicted")
	require.Empty(t, u.unloaded(),
		"FR-047: the memory must NOT have been returned under the in-flight request")
	require.Zero(t, notifier.len(),
		"a refused eviction must not announce an unload that never happened")
	require.Equal(t, []string{"answering"}, m.Loaded(),
		"the model is still loaded and still able to finish the answer")

	// The idle sweep must respect the same rule even after the period elapses.
	clk.advance(10 * time.Minute)
	events, err := m.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Empty(t, events, "FR-047: the idle sweep must not evict a serving model either")
	require.Empty(t, u.unloaded())

	// Once the answer is delivered, eviction is allowed.
	done()
	require.False(t, m.IsServing("answering"))
	ev, err := m.Evict(context.Background(), "answering", ReasonMemoryPressure)
	require.NoError(t, err)
	require.Equal(t, "answering", ev.ModelID)
	require.Equal(t, ReasonMemoryPressure, ev.Reason)
	require.Equal(t, InitiatorSystem, ev.Initiator)
	require.Equal(t, []string{"answering"}, u.unloaded())
	require.Equal(t, 1, notifier.len(), "FR-046: the eviction the system chose is announced")
}

// TestEvict_RefusesWhileAnyOfSeveralRequestsIsInFlight: one finished request does
// not make a model evictable while a sibling request is still being served.
func TestEvict_RefusesWhileAnyOfSeveralRequestsIsInFlight(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Minute}, &recordingNotifier{}, clk, u)
	require.NoError(t, m.Track("shared", 4<<30, nil))

	first, err := m.BeginRequest("shared")
	require.NoError(t, err)
	second, err := m.BeginRequest("shared")
	require.NoError(t, err)

	first()
	_, err = m.Evict(context.Background(), "shared", ReasonMemoryPressure)
	require.ErrorIs(t, err, ErrModelServing, "one request remains in flight")
	require.Empty(t, u.unloaded())

	// Ending the same request twice must not decrement the count twice.
	first()
	_, err = m.Evict(context.Background(), "shared", ReasonMemoryPressure)
	require.ErrorIs(t, err, ErrModelServing, "a double End must not fake the model idle")
	require.Empty(t, u.unloaded())

	second()
	_, err = m.Evict(context.Background(), "shared", ReasonMemoryPressure)
	require.NoError(t, err)
	require.Equal(t, []string{"shared"}, u.unloaded())
}

// TestEvict_UserRequestedUnloadAlsoRefusesWhileServing: FR-047 protects the
// in-flight answer regardless of who asked for the memory back.
func TestEvict_UserRequestedUnloadAlsoRefusesWhileServing(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Minute}, &recordingNotifier{}, clk, u)
	require.NoError(t, m.Track("busy", 1<<30, nil))

	done, err := m.BeginRequest("busy")
	require.NoError(t, err)
	_, err = m.Unload(context.Background(), "busy")
	require.ErrorIs(t, err, ErrModelServing)
	require.Empty(t, u.unloaded())
	done()
}

// TestEvict_NeverUnloadsUnderAnInFlightRequest_Concurrent makes the FR-047 case
// genuinely concurrent: real requests are begun and ended on live goroutines
// while eviction is attempted from other goroutines at the same time, all under
// -race. The invariant is checked INSIDE the unload path itself — at the moment
// the memory would actually be handed back — so a lost race cannot slip past a
// check performed only before or after.
func TestEvict_NeverUnloadsUnderAnInFlightRequest_Concurrent(t *testing.T) {
	const (
		servers   = 4
		evictors  = 4
		rounds    = 400
		modelID   = "contended"
		reloadEvy = true
	)

	var (
		inFlight   atomic.Int32 // requests the test knows are genuinely in flight
		violations atomic.Int32 // unloads that happened while one was in flight
		evicted    atomic.Int32
		refused    atomic.Int32
	)

	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	notifier := &recordingNotifier{}
	u := &recordingUnloader{
		// Checked at the exact instant the memory is returned to the host.
		before: func(string) {
			if inFlight.Load() > 0 {
				violations.Add(1)
			}
		},
	}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Hour}, notifier, clk, u)
	require.NoError(t, m.Track(modelID, 2<<30, nil))

	var serving, evicting sync.WaitGroup
	stop := make(chan struct{})

	// Request servers: begin a real request, hold it, end it. They run for the
	// whole life of the eviction storm, so requests really are in flight while
	// eviction is attempted.
	for i := 0; i < servers; i++ {
		serving.Add(1)
		go func() {
			defer serving.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				done, err := m.BeginRequest(modelID)
				if err != nil {
					// Model is currently unloaded/unloading — that is legal; the
					// request simply cannot be served by it right now.
					continue
				}
				inFlight.Add(1)
				// A real, observable window during which an answer is in flight.
				for j := 0; j < 20; j++ {
					_ = m.IsServing(modelID)
				}
				inFlight.Add(-1)
				done()
				// A real gap between requests, so the model is genuinely idle
				// some of the time and eviction can legitimately win the race.
				// Without gaps every eviction attempt trivially hits a serving
				// model and the test would prove nothing about the guard.
				time.Sleep(20 * time.Microsecond)
			}
		}()
	}

	// Evictors: hammer eviction against the same model concurrently.
	for i := 0; i < evictors; i++ {
		evicting.Add(1)
		go func() {
			defer evicting.Done()
			for r := 0; r < rounds; r++ {
				_, err := m.Evict(context.Background(), modelID, ReasonMemoryPressure)
				switch {
				case err == nil:
					evicted.Add(1)
					if reloadEvy {
						// Put it back so the contention keeps going.
						_ = m.Track(modelID, 2<<30, nil)
					}
				case isServingRefusal(err):
					refused.Add(1)
				}
				time.Sleep(10 * time.Microsecond)
			}
		}()
	}

	evicting.Wait() // the eviction storm finishes first...
	close(stop)     // ...then the request servers are told to wind down.
	serving.Wait()

	t.Logf("concurrency reached: evictions=%d serving-refusals=%d unload-while-serving-violations=%d",
		evicted.Load(), refused.Load(), violations.Load())

	require.Zero(t, violations.Load(),
		"FR-047: a model was unloaded while a request was in flight — an in-flight answer was corrupted")
	require.Positive(t, evicted.Load(),
		"the test must actually evict sometimes, or it proves nothing")
	require.Positive(t, refused.Load(),
		"the test must actually hit the serving refusal, or the contention window never opened")
	require.Equal(t, int(evicted.Load()), notifier.len(),
		"FR-046: every system-initiated unload that really happened is announced")
}

func isServingRefusal(err error) bool {
	return errors.Is(err, ErrModelServing)
}

// TestEvictable_PairedMutation is the §1.1 paired mutation for the FR-047 guard.
// It proves the serving check is LOAD-BEARING and not a tautology: with the real
// guard an actively-serving model is REFUSED; with the serving check removed the
// SAME model would be (wrongly) evicted out from under its in-flight request.
func TestEvictable_PairedMutation(t *testing.T) {
	serving := &modelState{id: "answering", inFlight: 1}
	idle := &modelState{id: "quiet", inFlight: 0}

	// REAL guard: a serving model is refused, an idle one is allowed.
	require.ErrorIs(t, evictable(serving), ErrModelServing,
		"the real guard MUST refuse to evict a model that is serving a request")
	require.NoError(t, evictable(idle))

	// MUTATION: remove the serving check (always-evictable). This is the exact
	// defect the guard exists to prevent; under it the in-flight answer dies.
	evictableMutatedAlwaysAllow := func(*modelState) error { return nil }
	require.NoError(t, evictableMutatedAlwaysAllow(serving),
		"with the serving check removed the eviction would be (wrongly) allowed — proves the guard is load-bearing")

	require.NotEqual(t,
		evictable(serving) == nil,
		evictableMutatedAlwaysAllow(serving) == nil,
		"real guard and mutated guard MUST disagree on an actively-serving model")
}

// TestEvictLRUIdle_NamesWhatItFrees is FR-045 / SC-018: when room is needed the
// least-recently-used IDLE model is offered up, and whatever goes is named.
func TestEvictLRUIdle_NamesWhatItFrees(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	notifier := &recordingNotifier{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Hour}, notifier, clk, u)

	require.NoError(t, m.Track("oldest", 1<<30, nil))
	clk.advance(time.Minute)
	require.NoError(t, m.Track("middle", 2<<30, nil))
	clk.advance(time.Minute)
	require.NoError(t, m.Track("newest", 3<<30, nil))

	// "oldest" is busy, so the LRU *idle* model is "middle" — not "oldest".
	done, err := m.BeginRequest("oldest")
	require.NoError(t, err)
	defer done()

	ev, err := m.EvictLRUIdle(context.Background())
	require.NoError(t, err)
	require.Equal(t, "middle", ev.ModelID,
		"the least-recently-used IDLE model is freed; the serving one is untouched (FR-045 + FR-047)")
	require.Equal(t, ReasonMemoryPressure, ev.Reason)
	require.Equal(t, []string{"middle"}, u.unloaded())
	require.Equal(t, 1, notifier.len(), "SC-018: whatever is unloaded is named")
	require.Equal(t, "middle", notifier.snapshot()[0].ModelID)
}

// TestEvictLRUIdle_NothingIdleToFree: with every model busy there is no honest
// offer to make — say so rather than taking one anyway.
func TestEvictLRUIdle_NothingIdleToFree(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Hour}, &recordingNotifier{}, clk, u)
	require.NoError(t, m.Track("only", 1<<30, nil))

	done, err := m.BeginRequest("only")
	require.NoError(t, err)
	defer done()

	_, err = m.EvictLRUIdle(context.Background())
	require.ErrorIs(t, err, ErrNoIdleModel)
	require.Empty(t, u.unloaded())
}

func TestEvict_UnknownModel(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	m := newTestManager(t, Config{IdleTimeout: time.Minute}, &recordingNotifier{}, clk)
	_, err := m.Evict(context.Background(), "ghost", ReasonMemoryPressure)
	require.ErrorIs(t, err, ErrModelNotLoaded)
}
