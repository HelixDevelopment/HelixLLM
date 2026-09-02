package failover

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingNotifier captures what the user would be told. Test double —
// CONST-050(A) permits fakes ONLY in _test.go.
type recordingNotifier struct {
	mu      sync.Mutex
	retries []RetryEvent
	losses  []LossEvent
}

func (r *recordingNotifier) RequestRetried(ev RetryEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retries = append(r.retries, ev)
}

func (r *recordingNotifier) RequestLost(ev LossEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.losses = append(r.losses, ev)
}

func (r *recordingNotifier) retrySnapshot() []RetryEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RetryEvent, len(r.retries))
	copy(out, r.retries)
	return out
}

func (r *recordingNotifier) lossSnapshot() []LossEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LossEvent, len(r.losses))
	copy(out, r.losses)
	return out
}

// ---------------------------------------------------------------------------
// T064 / FR-050 / SC-017 / CONST-046
// ---------------------------------------------------------------------------

// TestRetryEvent_IsMachineComposed is the CONST-046 guard: the announcement
// carries a machine KEY plus DATA, never a baked English sentence. A user who
// silently got their answer from a different host than they chose has been
// misled — and a user who gets that news only in English has been misled in
// every other language.
func TestRetryEvent_IsMachineComposed(t *testing.T) {
	ev := RetryEvent{
		RequestID:      "req-42",
		ModelID:        "qwen2.5-coder-7b",
		Fingerprint:    "qwen2.5-coder-7b@Q4_K_M",
		OriginalHost:   "gpu-01.lan:11434",
		ServingHost:    "gpu-02.lan:11434",
		Reason:         ReasonHostUnreachable,
		Attempts:       2,
		DiscardedBytes: 512,
		At:             time.Unix(1_700_000_000, 0).UTC(),
	}
	require.NoError(t, ev.Validate())

	key := ev.MessageKey()
	require.Equal(t, "failover.retry.host_unreachable", key)
	require.False(t, strings.ContainsAny(key, " !?"),
		"a message KEY must not be a sentence")

	f := ev.Fields()
	// FR-050: which host ultimately served the request must be renderable.
	require.Equal(t, "gpu-02.lan:11434", f["serving_host"])
	require.Equal(t, "gpu-01.lan:11434", f["original_host"])
	require.Equal(t, "qwen2.5-coder-7b", f["model_id"])
	require.Equal(t, "req-42", f["request_id"])
	require.Equal(t, "host_unreachable", f["reason"])
	require.Equal(t, 2, f["attempts"])
	require.Equal(t, int64(512), f["discarded_bytes"])
	require.Equal(t, int64(1_700_000_000), f["at_unix"])

	// No rendered prose may leak out of this package.
	for k, v := range f {
		s, ok := v.(string)
		if !ok {
			continue
		}
		require.False(t, strings.Contains(s, " "),
			"field %q looks like prose (%q); CONST-046 forbids composing sentences here", k, s)
	}
}

// TestRetryEvent_Validate refuses an announcement that cannot do its FR-050 job.
func TestRetryEvent_Validate(t *testing.T) {
	base := RetryEvent{
		RequestID:    "req-1",
		ModelID:      "m",
		OriginalHost: "a:1",
		ServingHost:  "b:1",
		Reason:       ReasonHostUnreachable,
		Attempts:     2,
	}
	require.NoError(t, base.Validate())

	noServing := base
	noServing.ServingHost = ""
	require.ErrorIs(t, noServing.Validate(), ErrUnexplainedRetry,
		"FR-050: an announcement that cannot name the host that served is useless")

	noOriginal := base
	noOriginal.OriginalHost = ""
	require.ErrorIs(t, noOriginal.Validate(), ErrUnexplainedRetry)

	noReason := base
	noReason.Reason = ""
	require.ErrorIs(t, noReason.Validate(), ErrUnexplainedRetry)

	sameHost := base
	sameHost.ServingHost = sameHost.OriginalHost
	require.ErrorIs(t, sameHost.Validate(), ErrUnexplainedRetry,
		"an announcement claiming a retry that never left the original host is false")

	oneAttempt := base
	oneAttempt.Attempts = 1
	require.ErrorIs(t, oneAttempt.Validate(), ErrUnexplainedRetry,
		"a single attempt is not a retry")
}

// TestLossEvent_NamesTheLostHost is the SC-016 guard on the reporting side: the
// user is told which host went and why the request could not be rescued.
func TestLossEvent_NamesTheLostHost(t *testing.T) {
	ev := LossEvent{
		RequestID:      "req-9",
		ModelID:        "qwen2.5-coder-7b",
		LostHost:       "gpu-07.lan:11434",
		Reason:         ReasonHostUnreachable,
		Outcome:        OutcomeOutputAlreadyDelivered,
		Attempts:       1,
		DiscardedBytes: 0,
		At:             time.Unix(1_700_000_000, 0).UTC(),
	}
	require.NoError(t, ev.Validate())
	require.Equal(t, "failover.loss.output_already_delivered", ev.MessageKey())

	f := ev.Fields()
	require.Equal(t, "gpu-07.lan:11434", f["lost_host"])
	require.Equal(t, "output_already_delivered", f["outcome"])
	require.Equal(t, "host_unreachable", f["reason"])

	noHost := ev
	noHost.LostHost = ""
	require.ErrorIs(t, noHost.Validate(), ErrUnexplainedLoss,
		"SC-016: a failure that cannot name the lost host is the truncated-answer defect")

	noOutcome := ev
	noOutcome.Outcome = ""
	require.ErrorIs(t, noOutcome.Validate(), ErrUnexplainedLoss)
}

// TestLossOutcome_KeysAreDistinct: every reason a retry did not happen has its
// own key, so the user can be told the real one rather than a generic failure.
func TestLossOutcome_KeysAreDistinct(t *testing.T) {
	outcomes := []LossOutcome{
		OutcomeRetryDisabled,
		OutcomeOutputAlreadyDelivered,
		OutcomeNoEquivalentInstance,
		OutcomeAttemptsExhausted,
		OutcomeRetryFailed,
	}
	seen := map[string]bool{}
	for _, o := range outcomes {
		require.NotEmpty(t, string(o))
		key := LossEvent{
			RequestID: "r", ModelID: "m", LostHost: "h", Reason: ReasonHostUnreachable, Outcome: o,
		}.MessageKey()
		require.False(t, seen[key], "outcome %q produces a duplicate message key %q", o, key)
		seen[key] = true
		require.True(t, strings.HasPrefix(key, "failover.loss."))
	}
}
