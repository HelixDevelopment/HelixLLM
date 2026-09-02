package failover

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles. CONST-050(A): fakes live ONLY in _test.go.
// ---------------------------------------------------------------------------

// staticSource offers a fixed roster of instances. Test double.
type staticSource struct {
	mu        sync.Mutex
	instances []Instance
	err       error
	calls     int
}

func (s *staticSource) Equivalents(_ context.Context, original Instance) ([]Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]Instance, 0, len(s.instances))
	for _, c := range s.instances {
		if Equivalent(original, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// recordingDeliverer is the streaming sink: everything written to it has
// genuinely reached the user and can never be un-sent.
type recordingDeliverer struct {
	mu   sync.Mutex
	sent []byte
}

func (d *recordingDeliverer) Deliver(_ context.Context, p []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, p...)
	return nil
}

func (d *recordingDeliverer) delivered() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return string(d.sent)
}

func hostA() Instance {
	return Instance{Host: "gpu-01.lan:11434", ModelID: "qwen2.5-coder-7b",
		Fingerprint: "qwen2.5-coder-7b@Q4_K_M", Capabilities: []string{"chat", "tools"}}
}

func hostB() Instance {
	return Instance{Host: "gpu-02.lan:11434", ModelID: "qwen2.5-coder-7b",
		Fingerprint: "qwen2.5-coder-7b@Q4_K_M", Capabilities: []string{"chat", "tools", "vision"}}
}

func hostC() Instance {
	return Instance{Host: "gpu-03.lan:11434", ModelID: "qwen2.5-coder-7b",
		Fingerprint: "qwen2.5-coder-7b@Q4_K_M", Capabilities: []string{"chat", "tools"}}
}

func enabledPolicy() RetryPolicy { return RetryPolicy{Enabled: true, MaxAttempts: 3} }

func newTestRunner(t *testing.T, p RetryPolicy, src InstanceSource, n Notifier, clk *testClock, opts ...RunnerOption) *Runner {
	t.Helper()
	opts = append([]RunnerOption{WithRunnerClock(clk.Now)}, opts...)
	r, err := NewRunner(p, src, n, opts...)
	require.NoError(t, err)
	return r
}

// ---------------------------------------------------------------------------
// SC-017 — THE DOMINATING INVARIANT
// ---------------------------------------------------------------------------

// TestRun_RetryReplacesTheAnswerAndNeverSplicesIt is the SC-017 proof and the
// §1.1 paired-mutation target.
//
// The first instance produces REAL partial output and then dies. The retry
// produces a complete answer. The final answer must contain NOTHING from the
// dead instance — not a prefix, not a fragment, not a byte. A spliced answer
// would be a fluent, plausible, silently-corrupt reply: the defect that is
// nearly invisible in production and catastrophic in output quality.
func TestRun_RetryReplacesTheAnswerAndNeverSplicesIt(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB()}}
	notifier := &recordingNotifier{}
	r := newTestRunner(t, enabledPolicy(), src, notifier, clk)

	const deadPartial = "PARTIAL-FROM-DEAD-INSTANCE:the capital of France is Par"
	const liveAnswer = "The capital of France is Paris."

	var attempts []string
	run := func(_ context.Context, target Instance, out *Output) error {
		attempts = append(attempts, target.Host)
		if target.Host == hostA().Host {
			// A real partial answer was produced before the host went.
			_, err := out.Write([]byte(deadPartial))
			require.NoError(t, err)
			return NewHostLostError("req-splice", target, errors.New("no route to host"))
		}
		_, err := out.Write([]byte(liveAnswer))
		require.NoError(t, err)
		return nil
	}

	res, err := r.Run(context.Background(), Request{ID: "req-splice", Original: hostA()}, run)
	require.NoError(t, err)
	require.Equal(t, []string{hostA().Host, hostB().Host}, attempts)

	answer := string(res.Answer)

	// SC-017: exactly one model instance composed this answer.
	require.Equal(t, liveAnswer, answer,
		"SC-017: the answer must be EXACTLY the surviving instance's output")
	require.NotContains(t, answer, deadPartial,
		"SC-017: the dead instance's partial output must be discarded, not concatenated")
	require.NotContains(t, answer, "PARTIAL-FROM-DEAD-INSTANCE",
		"SC-017: no fragment of the dead instance may appear in the final answer")
	require.NotContains(t, answer, "the capital of France is Par",
		"SC-017: not even the plausible-looking prefix may survive")
	require.False(t, strings.HasPrefix(answer, deadPartial[:8]),
		"SC-017: a retry REPLACES an answer; it never splices one")
	require.Len(t, answer, len(liveAnswer),
		"SC-017: a spliced answer would be longer than any single instance produced")

	// The discarded output is accounted for rather than quietly forgotten.
	require.Equal(t, int64(len(deadPartial)), res.DiscardedBytes)
	require.Equal(t, hostB().Host, res.ServingHost)
	require.Equal(t, 2, res.Attempts)
	require.True(t, res.Retried)

	// FR-050 / SC-017: the user is told which host ultimately served.
	retries := notifier.retrySnapshot()
	require.Len(t, retries, 1)
	require.Equal(t, hostB().Host, retries[0].ServingHost)
	require.Equal(t, hostA().Host, retries[0].OriginalHost)
	require.Equal(t, int64(len(deadPartial)), retries[0].DiscardedBytes)
	require.Empty(t, notifier.lossSnapshot())
}

// TestRun_ThreeInstancesStillProduceASingleInstanceAnswer: two hosts die in
// sequence. The answer must still be exactly one instance's output — a splice
// bug that survives one retry often shows up on the second.
func TestRun_ThreeInstancesStillProduceASingleInstanceAnswer(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB(), hostC()}}
	r := newTestRunner(t, RetryPolicy{Enabled: true, MaxAttempts: 3}, src, &recordingNotifier{}, clk)

	run := func(_ context.Context, target Instance, out *Output) error {
		switch target.Host {
		case hostA().Host:
			_, _ = out.Write([]byte("AAAA"))
			return NewHostLostError("req-3", target, errors.New("gone"))
		case hostB().Host:
			_, _ = out.Write([]byte("BBBB"))
			return NewHostLostError("req-3", target, errors.New("gone too"))
		default:
			_, _ = out.Write([]byte("CCCC"))
			return nil
		}
	}

	res, err := r.Run(context.Background(), Request{ID: "req-3", Original: hostA()}, run)
	require.NoError(t, err)
	require.Equal(t, "CCCC", string(res.Answer),
		"SC-017: neither dead instance's output may appear in the answer")
	require.NotContains(t, string(res.Answer), "A")
	require.NotContains(t, string(res.Answer), "B")
	require.Equal(t, int64(8), res.DiscardedBytes)
	require.Equal(t, 3, res.Attempts)
}

// ---------------------------------------------------------------------------
// FR-049 — "ONLY when no output has yet been delivered to the user"
// ---------------------------------------------------------------------------

// TestRun_RefusesToRetryOnceOutputHasReachedTheUser: with a streaming sink,
// bytes written by the first instance are GONE — already on the user's screen.
// Re-running elsewhere would compose one answer from two instances, which is
// precisely what SC-017 forbids, so the request fails explicitly instead.
func TestRun_RefusesToRetryOnceOutputHasReachedTheUser(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB()}}
	notifier := &recordingNotifier{}
	sink := &recordingDeliverer{}
	r := newTestRunner(t, enabledPolicy(), src, notifier, clk, WithDeliverer(sink))

	var hosts []string
	run := func(_ context.Context, target Instance, out *Output) error {
		hosts = append(hosts, target.Host)
		_, _ = out.Write([]byte("streamed-to-the-user"))
		return NewHostLostError("req-streamed", target, errors.New("host vanished"))
	}

	res, err := r.Run(context.Background(), Request{ID: "req-streamed", Original: hostA()}, run)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrHostLost, "FR-048: the failure is explicit")
	require.ErrorIs(t, err, ErrOutputAlreadyDelivered)
	require.Contains(t, err.Error(), hostA().Host, "SC-016: the failure names the lost host")

	require.Equal(t, []string{hostA().Host}, hosts,
		"FR-049: no second attempt may run once output has reached the user")
	require.Zero(t, src.calls, "an ineligible retry must not even shop for an alternative")
	require.Equal(t, "streamed-to-the-user", sink.delivered())
	require.False(t, res.Retried)

	require.Empty(t, notifier.retrySnapshot())
	losses := notifier.lossSnapshot()
	require.Len(t, losses, 1)
	require.Equal(t, OutcomeOutputAlreadyDelivered, losses[0].Outcome)
	require.Equal(t, hostA().Host, losses[0].LostHost)
}

// TestRun_BufferedOutputIsNotDeliveredOutput: without a streaming sink nothing
// has reached the user yet, so a retry IS permitted. This is the other half of
// the FR-049 condition and stops the rule collapsing into "never retry".
func TestRun_BufferedOutputIsNotDeliveredOutput(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB()}}
	r := newTestRunner(t, enabledPolicy(), src, &recordingNotifier{}, clk)

	run := func(_ context.Context, target Instance, out *Output) error {
		if target.Host == hostA().Host {
			_, _ = out.Write([]byte("buffered-partial"))
			return NewHostLostError("req-buf", target, errors.New("gone"))
		}
		_, _ = out.Write([]byte("complete"))
		return nil
	}
	res, err := r.Run(context.Background(), Request{ID: "req-buf", Original: hostA()}, run)
	require.NoError(t, err)
	require.Equal(t, "complete", string(res.Answer))
	require.True(t, res.Retried)
}

// ---------------------------------------------------------------------------
// FR-049 — "MAY": policy-driven and disableable, and BOUNDED
// ---------------------------------------------------------------------------

// TestRun_RetryIsOffByDefault: FR-049 is a MAY. The zero policy retries nothing;
// automatic re-running of a user's request is opted into, never assumed.
func TestRun_RetryIsOffByDefault(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB()}}
	notifier := &recordingNotifier{}
	r := newTestRunner(t, RetryPolicy{}, src, notifier, clk)

	attempts := 0
	run := func(_ context.Context, target Instance, out *Output) error {
		attempts++
		return NewHostLostError("req-off", target, errors.New("gone"))
	}
	_, err := r.Run(context.Background(), Request{ID: "req-off", Original: hostA()}, run)
	require.ErrorIs(t, err, ErrHostLost)
	require.ErrorIs(t, err, ErrRetryDisabled)
	require.Equal(t, 1, attempts, "a disabled policy must make exactly one attempt")
	require.Zero(t, src.calls)

	losses := notifier.lossSnapshot()
	require.Len(t, losses, 1)
	require.Equal(t, OutcomeRetryDisabled, losses[0].Outcome)
	require.Equal(t, hostA().Host, losses[0].LostHost)
}

// TestRun_RetryIsBounded: a fleet-wide outage must not turn one request into an
// unbounded retry storm against hosts that are all dying.
func TestRun_RetryIsBounded(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	many := []Instance{hostB(), hostC(),
		{Host: "gpu-04:11434", ModelID: hostA().ModelID, Fingerprint: hostA().Fingerprint, Capabilities: hostA().Capabilities},
		{Host: "gpu-05:11434", ModelID: hostA().ModelID, Fingerprint: hostA().Fingerprint, Capabilities: hostA().Capabilities},
	}
	src := &staticSource{instances: many}
	notifier := &recordingNotifier{}
	r := newTestRunner(t, RetryPolicy{Enabled: true, MaxAttempts: 3}, src, notifier, clk)

	var hosts []string
	run := func(_ context.Context, target Instance, out *Output) error {
		hosts = append(hosts, target.Host)
		_, _ = out.Write([]byte("x"))
		return NewHostLostError("req-storm", target, errors.New("everything is on fire"))
	}
	_, err := r.Run(context.Background(), Request{ID: "req-storm", Original: hostA()}, run)
	require.ErrorIs(t, err, ErrHostLost)
	require.ErrorIs(t, err, ErrAttemptsExhausted)
	require.Len(t, hosts, 3, "MaxAttempts=3 means exactly three attempts, never four")
	require.Equal(t, hostA().Host, hosts[0])

	// No host is tried twice: a dead host does not get a second chance.
	seen := map[string]bool{}
	for _, h := range hosts {
		require.False(t, seen[h], "host %s was retried twice", h)
		seen[h] = true
	}
	require.Equal(t, OutcomeAttemptsExhausted, notifier.lossSnapshot()[0].Outcome)
}

// TestRun_NoEquivalentInstanceIsAnHonestFailure.
func TestRun_NoEquivalentInstanceIsAnHonestFailure(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	// A different quantisation is NOT the same model, so nothing here qualifies.
	src := &staticSource{instances: []Instance{
		{Host: "gpu-09:11434", ModelID: "qwen2.5-coder-7b", Fingerprint: "qwen2.5-coder-7b@Q8_0", Capabilities: []string{"chat", "tools"}},
	}}
	notifier := &recordingNotifier{}
	r := newTestRunner(t, enabledPolicy(), src, notifier, clk)

	run := func(_ context.Context, target Instance, out *Output) error {
		return NewHostLostError("req-none", target, errors.New("gone"))
	}
	_, err := r.Run(context.Background(), Request{ID: "req-none", Original: hostA()}, run)
	require.ErrorIs(t, err, ErrHostLost)
	require.ErrorIs(t, err, ErrNoEquivalentInstance)
	require.Equal(t, OutcomeNoEquivalentInstance, notifier.lossSnapshot()[0].Outcome)
}

// TestRun_NonLossErrorsAreNotRetried: failover is for a host that WENT. A model
// that returned a genuine application error must not be re-run elsewhere in the
// hope of a different answer.
func TestRun_NonLossErrorsAreNotRetried(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB()}}
	notifier := &recordingNotifier{}
	r := newTestRunner(t, enabledPolicy(), src, notifier, clk)

	boom := errors.New("context length exceeded")
	attempts := 0
	run := func(_ context.Context, target Instance, out *Output) error {
		attempts++
		return boom
	}
	_, err := r.Run(context.Background(), Request{ID: "req-app-err", Original: hostA()}, run)
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrHostLost)
	require.Equal(t, 1, attempts)
	require.Empty(t, notifier.retrySnapshot())
	require.Empty(t, notifier.lossSnapshot(),
		"an application error is not a host loss and must not be announced as one")
}

// TestRun_HonoursContextCancellation: a cancelled request stops retrying.
func TestRun_HonoursContextCancellation(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB(), hostC()}}
	r := newTestRunner(t, RetryPolicy{Enabled: true, MaxAttempts: 5}, src, &recordingNotifier{}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	run := func(_ context.Context, target Instance, out *Output) error {
		attempts++
		cancel()
		return NewHostLostError("req-cancel", target, errors.New("gone"))
	}
	_, err := r.Run(ctx, Request{ID: "req-cancel", Original: hostA()}, run)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

// ---------------------------------------------------------------------------
// Equivalence
// ---------------------------------------------------------------------------

// TestEquivalent defines what "equivalent model elsewhere" means: the SAME
// model identity, on a DIFFERENT host, able to serve at least what the original
// was serving.
func TestEquivalent(t *testing.T) {
	orig := hostA()

	require.True(t, Equivalent(orig, hostB()), "same weights, other host, superset of capabilities")
	require.False(t, Equivalent(orig, orig), "the same host is not somewhere else")

	sameHostOtherPort := orig
	require.False(t, Equivalent(orig, sameHostOtherPort))

	otherQuant := hostB()
	otherQuant.Fingerprint = "qwen2.5-coder-7b@Q8_0"
	require.False(t, Equivalent(orig, otherQuant),
		"a different quantisation is a different model and may answer differently")

	otherModel := hostB()
	otherModel.Fingerprint = "llama-3.1-8b@Q4_K_M"
	require.False(t, Equivalent(orig, otherModel))

	missingCap := hostB()
	missingCap.Capabilities = []string{"chat"}
	require.False(t, Equivalent(orig, missingCap),
		"identical weights on a host that cannot serve tools is not a substitute")

	blank := hostB()
	blank.Fingerprint = ""
	require.False(t, Equivalent(orig, blank), "an unidentified model is never equivalent")

	noHost := hostB()
	noHost.Host = ""
	require.False(t, Equivalent(orig, noHost), "a host that cannot be named cannot be reported (FR-050)")
}

// TestRetryPolicy_Validate.
func TestRetryPolicy_Validate(t *testing.T) {
	require.NoError(t, RetryPolicy{}.Validate(), "a disabled policy is always valid")
	require.NoError(t, RetryPolicy{Enabled: true, MaxAttempts: 1}.Validate())
	require.ErrorIs(t, RetryPolicy{Enabled: true, MaxAttempts: 0}.Validate(), ErrInvalidConfig)
	require.ErrorIs(t, RetryPolicy{Enabled: true, MaxAttempts: -1}.Validate(), ErrInvalidConfig)

	_, err := NewRunner(enabledPolicy(), &staticSource{}, nil)
	require.ErrorIs(t, err, ErrNoNotifier,
		"SC-017: an answer from another host must never be delivered silently")

	_, err = NewRunner(enabledPolicy(), nil, &recordingNotifier{})
	require.ErrorIs(t, err, ErrNoInstanceSource,
		"an enabled retry policy with nowhere to retry to would be a lie")

	// Retry disabled needs no source: it will never look for one.
	_, err = NewRunner(RetryPolicy{}, nil, &recordingNotifier{})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

// TestRun_ConcurrentRequestsEachGetASingleInstanceAnswer: many requests fail
// over at once through ONE Runner. Every answer must still come from exactly
// one instance — cross-request contamination would be the same SC-017 defect
// with a wider blast radius.
func TestRun_ConcurrentRequestsEachGetASingleInstanceAnswer(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	src := &staticSource{instances: []Instance{hostB()}}
	r := newTestRunner(t, enabledPolicy(), src, &recordingNotifier{}, clk)

	const workers = 24
	var wg sync.WaitGroup
	results := make([]string, workers)
	errs := make([]error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("req-%03d", i)
			dead := fmt.Sprintf("DEAD-%03d", i)
			live := fmt.Sprintf("LIVE-%03d", i)
			run := func(_ context.Context, target Instance, out *Output) error {
				if target.Host == hostA().Host {
					_, _ = out.Write([]byte(dead))
					return NewHostLostError(id, target, errors.New("gone"))
				}
				// Write in pieces: a splice bug is easiest to see mid-stream.
				for _, part := range []string{live[:4], live[4:]} {
					if _, err := out.Write([]byte(part)); err != nil {
						return err
					}
				}
				return nil
			}
			res, err := r.Run(context.Background(), Request{ID: id, Original: hostA()}, run)
			results[i], errs[i] = string(res.Answer), err
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, fmt.Sprintf("LIVE-%03d", i), results[i],
			"SC-017 under concurrency: request %d's answer must be exactly its own surviving instance's output", i)
		require.NotContains(t, results[i], "DEAD")
	}
}

// TestOutput_ConcurrentWritesAreSafe: an executor may stream from several
// goroutines; the buffer and the delivery ledger must not race.
func TestOutput_ConcurrentWritesAreSafe(t *testing.T) {
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	sink := &recordingDeliverer{}
	r := newTestRunner(t, RetryPolicy{}, nil, &recordingNotifier{}, clk, WithDeliverer(sink))

	run := func(_ context.Context, _ Instance, out *Output) error {
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = out.Write([]byte("ab"))
			}()
		}
		wg.Wait()
		return nil
	}
	res, err := r.Run(context.Background(), Request{ID: "req-par", Original: hostA()}, run)
	require.NoError(t, err)
	require.Len(t, res.Answer, 64)
	require.Len(t, sink.delivered(), 64)
}
