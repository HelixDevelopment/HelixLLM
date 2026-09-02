package failover

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles. CONST-050(A): fakes live ONLY in _test.go.
// ---------------------------------------------------------------------------

// testClock is a controllable time source so silence grace periods are
// exercised without sleeping.
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

// scriptedProbe is an independent liveness probe whose answer the test controls.
// It counts calls so a test can prove the detector did NOT probe a healthy,
// progressing request.
type scriptedProbe struct {
	mu    sync.Mutex
	err   error
	calls int
	hosts []string
}

func (p *scriptedProbe) Probe(_ context.Context, host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.hosts = append(p.hosts, host)
	return p.err
}

func (p *scriptedProbe) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *scriptedProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func testInstance() Instance {
	return Instance{
		Host:         "gpu-01.lan:11434",
		ModelID:      "qwen2.5-coder-7b",
		Fingerprint:  "qwen2.5-coder-7b@Q4_K_M",
		Capabilities: []string{"chat", "tools"},
	}
}

func newTestDetector(t *testing.T, cfg DetectConfig, probe LivenessProbe, clk *testClock) *Detector {
	t.Helper()
	d, err := NewDetector(cfg, probe, WithClock(clk.Now))
	require.NoError(t, err)
	return d
}

// ---------------------------------------------------------------------------
// T062 / FR-048 / SC-016
// ---------------------------------------------------------------------------

// TestAssess_ProgressingRequestIsAliveAndNotProbed: a request that is still
// producing output needs no corroboration at all. Probing every in-flight
// request would add load to a fleet that may already be struggling.
func TestAssess_ProgressingRequestIsAliveAndNotProbed(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	probe := &scriptedProbe{}
	d := newTestDetector(t, DetectConfig{SilenceGrace: 30 * time.Second, ProbeFailuresForLoss: 2}, probe, clk)

	w, err := d.Watch("req-1", testInstance())
	require.NoError(t, err)

	clk.advance(10 * time.Second)
	w.Progress()
	clk.advance(5 * time.Second)

	a, err := w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, VerdictAlive, a.Verdict)
	require.False(t, a.Lost())
	require.NoError(t, a.Err())
	require.Zero(t, probe.callCount(),
		"a progressing request must not trigger liveness traffic")
}

// TestAssess_TimeoutOnALiveHostIsNotLoss is the load-bearing distinction of
// T062: a stalled request whose host still answers an independent probe is
// SLOW, never LOST. Treating slowness as death causes needless retries that
// double the load on a fleet that is already struggling.
func TestAssess_TimeoutOnALiveHostIsNotLoss(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	probe := &scriptedProbe{} // probe succeeds: the host is alive, just slow
	d := newTestDetector(t, DetectConfig{SilenceGrace: 30 * time.Second, ProbeFailuresForLoss: 2}, probe, clk)

	w, err := d.Watch("req-slow", testInstance())
	require.NoError(t, err)

	// A deadline expired on the request channel. That is a TIMEOUT, not a death.
	w.TransportFailure(context.DeadlineExceeded)
	clk.advance(10 * time.Minute) // silence far beyond the grace period

	for i := 0; i < 5; i++ {
		a, err := w.Assess(context.Background())
		require.NoError(t, err)
		require.Equal(t, VerdictSlow, a.Verdict,
			"a timeout against a host that answers an independent probe is slowness, not loss")
		require.False(t, a.Lost())
		require.NoError(t, a.Err(),
			"FR-048 only fires for a host proven unreachable — never for a slow one")
		require.Zero(t, a.ProbeFailures)
	}
	require.Equal(t, 5, probe.callCount(),
		"each assessment of a stalled request corroborates out of band")
}

// TestAssess_DeclaresLossOnlyAfterIndependentCorroboration: even a hard
// transport failure is not, on its own, a death certificate. Loss requires the
// configured number of consecutive INDEPENDENT probe failures.
func TestAssess_DeclaresLossOnlyAfterIndependentCorroboration(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	probe := &scriptedProbe{}
	d := newTestDetector(t, DetectConfig{SilenceGrace: 30 * time.Second, ProbeFailuresForLoss: 3}, probe, clk)

	inst := testInstance()
	w, err := d.Watch("req-dead", inst)
	require.NoError(t, err)

	// The connection died mid-stream. Suspicious, not conclusive.
	w.TransportFailure(io.ErrUnexpectedEOF)

	probe.setErr(errors.New("dial tcp: connect: no route to host"))

	a, err := w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, VerdictSlow, a.Verdict, "one failed probe is not yet proof")
	require.Equal(t, 1, a.ProbeFailures)

	a, err = w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, VerdictSlow, a.Verdict, "two failed probes is still below the threshold")
	require.Equal(t, 2, a.ProbeFailures)

	a, err = w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, VerdictLost, a.Verdict, "the configured corroboration threshold is met")
	require.True(t, a.Lost())
	require.Equal(t, 3, a.ProbeFailures)

	// FR-048 / SC-016: the failure is explicit and NAMES the host that went.
	lossErr := a.Err()
	require.Error(t, lossErr)
	require.ErrorIs(t, lossErr, ErrHostLost)
	require.Contains(t, lossErr.Error(), inst.Host,
		"SC-016: the failure must name the lost host")

	var hle *HostLostError
	require.True(t, errors.As(lossErr, &hle))
	require.Equal(t, inst.Host, hle.Host)
	require.Equal(t, "req-dead", hle.RequestID)
}

// TestAssess_SuccessfulProbeResetsCorroboration: a host that blips and comes
// back is not gone. Failures must be CONSECUTIVE.
func TestAssess_SuccessfulProbeResetsCorroboration(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	probe := &scriptedProbe{}
	d := newTestDetector(t, DetectConfig{SilenceGrace: time.Second, ProbeFailuresForLoss: 2}, probe, clk)

	w, err := d.Watch("req-blip", testInstance())
	require.NoError(t, err)
	clk.advance(time.Minute)

	probe.setErr(errors.New("i/o timeout"))
	a, err := w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, a.ProbeFailures)

	probe.setErr(nil) // the host answers again
	a, err = w.Assess(context.Background())
	require.NoError(t, err)
	require.Zero(t, a.ProbeFailures, "a reachable host resets the corroboration count")
	require.Equal(t, VerdictSlow, a.Verdict, "still stalled, but demonstrably alive")

	probe.setErr(errors.New("i/o timeout"))
	a, err = w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, a.ProbeFailures,
		"the earlier failure must not be carried across a successful probe")
	require.NotEqual(t, VerdictLost, a.Verdict)
}

// TestAssess_StalledWithinGraceIsNotProbed: a brief gap between tokens is
// normal. Only silence beyond the grace period earns a probe.
func TestAssess_StalledWithinGraceIsNotProbed(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	probe := &scriptedProbe{}
	d := newTestDetector(t, DetectConfig{SilenceGrace: 30 * time.Second, ProbeFailuresForLoss: 1}, probe, clk)

	w, err := d.Watch("req-gap", testInstance())
	require.NoError(t, err)

	clk.advance(29 * time.Second)
	a, err := w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, VerdictAlive, a.Verdict)
	require.Zero(t, probe.callCount())

	clk.advance(2 * time.Second)
	probe.setErr(errors.New("host down"))
	a, err = w.Assess(context.Background())
	require.NoError(t, err)
	require.Equal(t, VerdictLost, a.Verdict)
	require.Equal(t, 1, probe.callCount())
}

// TestDetector_UnknownAndClosedRequests: assessing a request the detector is not
// watching is an error, not a silent "alive".
func TestDetector_UnknownAndClosedRequests(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	d := newTestDetector(t, DetectConfig{SilenceGrace: time.Second, ProbeFailuresForLoss: 1}, &scriptedProbe{}, clk)

	_, err := d.Assess(context.Background(), "never-seen")
	require.ErrorIs(t, err, ErrUnknownRequest)

	w, err := d.Watch("req-x", testInstance())
	require.NoError(t, err)
	_, err = d.Watch("req-x", testInstance())
	require.ErrorIs(t, err, ErrDuplicateRequest,
		"two in-flight watches for one request id would double-count corroboration")

	w.Close()
	_, err = w.Assess(context.Background())
	require.ErrorIs(t, err, ErrUnknownRequest)
	w.Close() // idempotent
}

// TestDetectConfig_Validate refuses a policy that cannot work.
func TestDetectConfig_Validate(t *testing.T) {
	require.ErrorIs(t, DetectConfig{SilenceGrace: 0, ProbeFailuresForLoss: 1}.Validate(), ErrInvalidConfig)
	require.ErrorIs(t, DetectConfig{SilenceGrace: -time.Second, ProbeFailuresForLoss: 1}.Validate(), ErrInvalidConfig)
	require.ErrorIs(t, DetectConfig{SilenceGrace: time.Second, ProbeFailuresForLoss: 0}.Validate(), ErrInvalidConfig)
	require.NoError(t, DetectConfig{SilenceGrace: time.Second, ProbeFailuresForLoss: 1}.Validate())

	_, err := NewDetector(DetectConfig{SilenceGrace: time.Second, ProbeFailuresForLoss: 1}, nil)
	require.ErrorIs(t, err, ErrNoLivenessProbe,
		"without an independent probe the detector could only GUESS that a host died")
}

// TestDetector_ConcurrentAssessIsRaceFree drives many watches and assessments
// across goroutines. A failover package that is only correct single-threaded is
// not correct: requests begin, stall and die on many goroutines at once.
func TestDetector_ConcurrentAssessIsRaceFree(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	probe := &scriptedProbe{}
	probe.setErr(errors.New("unreachable"))
	d := newTestDetector(t, DetectConfig{SilenceGrace: time.Nanosecond, ProbeFailuresForLoss: 4}, probe, clk)

	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		id := "req-" + string(rune('a'+i))
		w, err := d.Watch(id, testInstance())
		require.NoError(t, err)
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				w.Progress()
				w.TransportFailure(io.ErrUnexpectedEOF)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := w.Assess(context.Background()); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// The interleaving above is deliberately unconstrained, so the count a watch
	// has reached by now is not predictable — asserting a specific one would be
	// a flaky test, not a stronger one (§11.4.50). What MUST hold is that the
	// counter was not corrupted by the concurrency: with the writers finished,
	// the host is a settled suspect and the probe always fails, so exactly
	// ProbeFailuresForLoss further consecutive corroborations reach the
	// threshold. Anything else means an increment was lost or double-counted.
	var a Assessment
	for i := 0; i < 4; i++ {
		var err error
		a, err = d.Assess(context.Background(), "req-a")
		require.NoError(t, err)
	}
	require.Equal(t, VerdictLost, a.Verdict)
	require.GreaterOrEqual(t, a.ProbeFailures, 4)
	require.True(t, a.Lost())
	require.ErrorIs(t, a.Err(), ErrHostLost)
}
