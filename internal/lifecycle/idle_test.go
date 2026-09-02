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

// testClock is a controllable time source. Test double — CONST-050(A) permits
// fakes ONLY in _test.go. It is race-safe because the manager reads it from
// several goroutines under -race.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(t time.Time) *testClock { return &testClock{now: t} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// recordingUnloader records which models were actually handed back to the host.
type recordingUnloader struct {
	mu     sync.Mutex
	calls  []string
	fail   map[string]error
	before func(modelID string)
}

func (u *recordingUnloader) unload(_ context.Context, modelID string) error {
	if u.before != nil {
		u.before(modelID)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err, ok := u.fail[modelID]; ok {
		return err
	}
	u.calls = append(u.calls, modelID)
	return nil
}

func (u *recordingUnloader) unloaded() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]string, len(u.calls))
	copy(out, u.calls)
	return out
}

func newTestManager(t *testing.T, cfg Config, n Notifier, clk *testClock) *Manager {
	t.Helper()
	u := &recordingUnloader{}
	m, err := New(cfg, u.unload, n, WithClock(clk.Now))
	require.NoError(t, err)
	return m
}

func newTestManagerWithUnloader(t *testing.T, cfg Config, n Notifier, clk *testClock, u *recordingUnloader) *Manager {
	t.Helper()
	m, err := New(cfg, u.unload, n, WithClock(clk.Now))
	require.NoError(t, err)
	return m
}

// TestConfig_IdlePeriodIsConfiguration is the FR-044 "configurable" half: the
// period comes from Config, and two managers with different periods genuinely
// behave differently. A hardcoded constant could not produce this difference.
func TestConfig_IdlePeriodIsConfiguration(t *testing.T) {
	const elapsed = 7 * time.Minute

	for _, tc := range []struct {
		name        string
		idleTimeout time.Duration
		wantUnload  bool
	}{
		{"short period reclaims", 5 * time.Minute, true},
		{"long period keeps it loaded", 30 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
			u := &recordingUnloader{}
			m := newTestManagerWithUnloader(t, Config{IdleTimeout: tc.idleTimeout}, &recordingNotifier{}, clk, u)
			require.NoError(t, m.Track("model-a", 1<<30, nil))

			clk.advance(elapsed)
			events, err := m.ReclaimIdle(context.Background())
			require.NoError(t, err)

			if tc.wantUnload {
				require.Len(t, events, 1)
				require.Equal(t, []string{"model-a"}, u.unloaded(),
					"FR-044: memory must actually be returned to the host")
				require.Empty(t, m.Loaded())
			} else {
				require.Empty(t, events)
				require.Empty(t, u.unloaded())
				require.Equal(t, []string{"model-a"}, m.Loaded())
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	require.ErrorIs(t, Config{}.Validate(), ErrInvalidConfig)
	require.ErrorIs(t, Config{IdleTimeout: -time.Second}.Validate(), ErrInvalidConfig)
	require.NoError(t, Config{IdleTimeout: time.Second}.Validate())

	_, err := New(Config{}, func(context.Context, string) error { return nil }, &recordingNotifier{})
	require.ErrorIs(t, err, ErrInvalidConfig, "a manager must refuse an unusable idle period")
}

func TestNew_RefusesWithoutUnloader(t *testing.T) {
	_, err := New(Config{IdleTimeout: time.Minute}, nil, &recordingNotifier{})
	require.ErrorIs(t, err, ErrNoUnloader,
		"without a real unloader the manager would only PRETEND to return memory")
}

// TestReclaimIdle_ServingResetsTheClock: serving a request makes a model
// non-idle. This is the "serving no request FOR the period" half of FR-044.
func TestReclaimIdle_ServingResetsTheClock(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: 10 * time.Minute}, &recordingNotifier{}, clk, u)
	require.NoError(t, m.Track("busy", 1<<30, nil))

	clk.advance(9 * time.Minute)
	done, err := m.BeginRequest("busy")
	require.NoError(t, err)
	clk.advance(2 * time.Minute)
	done() // last-used stamped here

	clk.advance(5 * time.Minute) // only 5 min idle since the request ended
	events, err := m.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Empty(t, events, "a model that served 5 minutes ago is not idle for 10")
	require.Empty(t, u.unloaded())

	clk.advance(6 * time.Minute) // now 11 min since it last served
	events, err = m.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, 11*time.Minute, events[0].IdleFor)
	require.Equal(t, int64(1<<30), events[0].ReclaimedBytes)
}

// TestReclaimIdle_ReleasesTheBrokerLease proves lifecycle COOPERATES with the
// existing vrambroker arbitration (§11.4.74): unloading hands the reservation
// back through the lease rather than accounting for VRAM itself.
func TestReclaimIdle_ReleasesTheBrokerLease(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	m := newTestManager(t, Config{IdleTimeout: time.Minute}, &recordingNotifier{}, clk)

	rel := &countingReleaser{}
	require.NoError(t, m.Track("leased", 3<<30, rel))

	clk.advance(2 * time.Minute)
	_, err := m.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Equal(t, int32(1), rel.count.Load(),
		"§11.4.74: the reservation goes back to the broker that granted it")
}

type countingReleaser struct{ count atomic.Int32 }

func (c *countingReleaser) Release() { c.count.Add(1) }

// TestBeginRequest_UnknownModel: serving state cannot be invented for a model
// the manager never saw loaded.
func TestBeginRequest_UnknownModel(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	m := newTestManager(t, Config{IdleTimeout: time.Minute}, &recordingNotifier{}, clk)
	_, err := m.BeginRequest("ghost")
	require.ErrorIs(t, err, ErrModelNotLoaded)
}

// TestReclaimIdle_UnloadFailureKeepsModelTracked: if the host refuses to give the
// memory back, the model is still loaded — reporting it gone would be a lie.
func TestReclaimIdle_UnloadFailureKeepsModelTracked(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	boom := errors.New("runtime refused to unload")
	u := &recordingUnloader{fail: map[string]error{"stuck": boom}}
	notifier := &recordingNotifier{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Minute}, notifier, clk, u)
	require.NoError(t, m.Track("stuck", 1<<30, nil))

	clk.advance(2 * time.Minute)
	events, err := m.ReclaimIdle(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, boom)
	require.Empty(t, events)
	require.Equal(t, []string{"stuck"}, m.Loaded(),
		"a model whose memory was not returned is still loaded")
	require.Zero(t, notifier.len(),
		"nothing was unloaded, so nothing may be announced as unloaded")

	// And it is retryable — the failed attempt did not leave it wedged.
	u.mu.Lock()
	u.fail = nil
	u.mu.Unlock()
	events, err = m.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)
}

// TestReclaimIdle_ConcurrentSweepsUnloadEachModelExactlyOnce runs the sweep from
// many goroutines at once under -race. Model bookkeeping is inherently
// concurrent; a manager that is only correct single-threaded is not correct.
func TestReclaimIdle_ConcurrentSweepsUnloadEachModelExactlyOnce(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	u := &recordingUnloader{}
	notifier := &recordingNotifier{}
	m := newTestManagerWithUnloader(t, Config{IdleTimeout: time.Minute}, notifier, clk, u)

	const models = 12
	for i := 0; i < models; i++ {
		require.NoError(t, m.Track(modelName(i), 1<<30, nil))
	}
	clk.advance(2 * time.Minute)

	const sweepers = 8
	var wg sync.WaitGroup
	var total atomic.Int32
	start := make(chan struct{})
	for i := 0; i < sweepers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			events, err := m.ReclaimIdle(context.Background())
			if err == nil {
				total.Add(int32(len(events)))
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int32(models), total.Load(),
		"each model must be reported unloaded exactly once across all concurrent sweeps")
	require.Len(t, u.unloaded(), models, "no model may be unloaded twice")
	require.Equal(t, models, notifier.len(), "exactly one announcement per real unload")
	require.Empty(t, m.Loaded())
}

func modelName(i int) string {
	return "model-" + string(rune('a'+i))
}
