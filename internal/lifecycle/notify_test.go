package lifecycle

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingNotifier captures announcements. Test double — CONST-050(A) permits
// fakes ONLY in _test.go.
type recordingNotifier struct {
	mu     sync.Mutex
	events []UnloadEvent
}

func (r *recordingNotifier) ModelUnloaded(ev UnloadEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingNotifier) snapshot() []UnloadEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]UnloadEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recordingNotifier) len() int { return len(r.snapshot()) }

// TestUnloadEvent_IsMachineComposed is the CONST-046 guard: the announcement
// carries a machine key plus DATA, never a baked English sentence. A reader that
// renders in another language must have everything it needs from Fields().
func TestUnloadEvent_IsMachineComposed(t *testing.T) {
	ev := UnloadEvent{
		ModelID:        "qwen2.5-coder-7b",
		Reason:         ReasonIdleTimeout,
		Initiator:      InitiatorSystem,
		IdleFor:        11 * time.Minute,
		ReclaimedBytes: 6 * 1024 * 1024 * 1024,
		At:             time.Unix(1_700_000_000, 0).UTC(),
	}
	require.NoError(t, ev.Validate())

	key := ev.MessageKey()
	require.Equal(t, "lifecycle.unload.idle_timeout", key,
		"the key must be a stable machine identifier a locale bundle resolves")

	// CONST-046: no prose anywhere in the emitted key.
	require.NotContains(t, strings.ToLower(key), " ",
		"a message KEY must not be a sentence")

	f := ev.Fields()
	// FR-046: WHICH model, and WHY — both must be present as data.
	require.Equal(t, "qwen2.5-coder-7b", f["model_id"], "FR-046: the announcement must name the model")
	require.Equal(t, string(ReasonIdleTimeout), f["reason"], "FR-046: the announcement must carry the reason")
	require.Equal(t, string(InitiatorSystem), f["initiator"])
	require.Equal(t, int64(11*60), f["idle_for_seconds"])
	require.Equal(t, int64(6*1024*1024*1024), f["reclaimed_bytes"])
}

// TestUnloadEvent_Validate_RejectsUnexplained proves an event cannot claim a
// model vanished without saying which one, or why (FR-046).
func TestUnloadEvent_Validate_RejectsUnexplained(t *testing.T) {
	t.Run("no model named", func(t *testing.T) {
		ev := UnloadEvent{Reason: ReasonIdleTimeout, Initiator: InitiatorSystem}
		require.ErrorIs(t, ev.Validate(), ErrUnexplainedUnload)
	})
	t.Run("no reason given", func(t *testing.T) {
		ev := UnloadEvent{ModelID: "m", Initiator: InitiatorSystem}
		require.ErrorIs(t, ev.Validate(), ErrUnexplainedUnload)
	})
	t.Run("no initiator", func(t *testing.T) {
		ev := UnloadEvent{ModelID: "m", Reason: ReasonIdleTimeout}
		require.ErrorIs(t, ev.Validate(), ErrUnexplainedUnload)
	})
}

// TestInitiator_OnlySystemUnloadsAreAnnounced distinguishes an unload the SYSTEM
// decided (must be announced — the user did not ask for it) from one the USER
// requested (already known to them).
func TestInitiator_OnlySystemUnloadsAreAnnounced(t *testing.T) {
	require.True(t, InitiatorSystem.RequiresAnnouncement(),
		"a self-initiated unload MUST be announced (FR-046)")
	require.False(t, InitiatorUser.RequiresAnnouncement(),
		"a user-requested unload is already known to the user")
}

// TestManager_SystemUnloadIsAnnounced_UserUnloadIsNot is the end-to-end FR-046
// assertion: a model does not leave the available set unexplained.
func TestManager_SystemUnloadIsAnnounced_UserUnloadIsNot(t *testing.T) {
	notifier := &recordingNotifier{}
	clk := newTestClock(time.Unix(1_700_000_000, 0).UTC())
	m := newTestManager(t, Config{IdleTimeout: 10 * time.Minute}, notifier, clk)

	require.NoError(t, m.Track("idle-one", 4<<30, nil))
	require.NoError(t, m.Track("user-asked", 2<<30, nil))

	// SYSTEM initiative: the idle sweep.
	clk.advance(11 * time.Minute)
	events, err := m.ReclaimIdle(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 2, "both models are idle past the configured period")

	announced := notifier.snapshot()
	require.Len(t, announced, 2, "FR-046: every self-initiated unload MUST be announced")
	for _, ev := range announced {
		require.NoError(t, ev.Validate())
		require.Equal(t, InitiatorSystem, ev.Initiator)
		require.Equal(t, ReasonIdleTimeout, ev.Reason)
		require.NotEmpty(t, ev.ModelID, "FR-046: the announcement must name the model")
	}

	// USER initiative: no announcement — they asked for it.
	require.NoError(t, m.Track("user-asked", 2<<30, nil))
	ev, err := m.Unload(context.Background(), "user-asked")
	require.NoError(t, err)
	require.Equal(t, InitiatorUser, ev.Initiator)
	require.Equal(t, ReasonUserRequested, ev.Reason)
	require.Len(t, notifier.snapshot(), 2,
		"a user-requested unload must NOT be re-announced to the user who asked for it")
}

// TestNew_RefusesWithoutNotifier: without a notifier the manager could unload a
// model with nobody to tell — the exact silent disappearance FR-046 forbids.
func TestNew_RefusesWithoutNotifier(t *testing.T) {
	_, err := New(Config{IdleTimeout: time.Minute}, func(context.Context, string) error { return nil }, nil)
	require.ErrorIs(t, err, ErrNoNotifier)
}
