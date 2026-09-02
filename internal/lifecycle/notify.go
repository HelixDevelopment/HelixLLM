package lifecycle

import (
	"errors"
	"fmt"
	"time"
)

// ErrUnexplainedUnload is returned when an unload event does not say which model
// went and why. FR-046: a model MUST NOT leave the available set unexplained, so
// an event that cannot explain itself is refused rather than emitted.
var ErrUnexplainedUnload = errors.New("lifecycle: unload event does not name the model and the reason (FR-046)")

// ErrNoNotifier is returned when a Manager is constructed without a Notifier.
// Without one a self-initiated unload would have nobody to tell — exactly the
// silent disappearance FR-046 forbids.
var ErrNoNotifier = errors.New("lifecycle: a Notifier is required — a self-initiated unload must be announced (FR-046)")

// UnloadReason is the machine-readable cause of an unload. It is a KEY, not a
// sentence: the user-facing wording is resolved from it by the presentation
// layer in the user's own language (CONST-046 — no hardcoded English here).
type UnloadReason string

const (
	// ReasonIdleTimeout: the model served no request for the configured idle
	// period, so its memory went back to the host (FR-044).
	ReasonIdleTimeout UnloadReason = "idle_timeout"

	// ReasonMemoryPressure: the model was evicted to make room for a selection
	// the host could not otherwise fit (FR-045).
	ReasonMemoryPressure UnloadReason = "memory_pressure"

	// ReasonUserRequested: the user asked for this model to be unloaded.
	ReasonUserRequested UnloadReason = "user_requested"
)

// Initiator says WHO decided the unload. The distinction is load-bearing: an
// unload the system chose on its own initiative must be announced (FR-046),
// while one the user asked for is already known to them.
type Initiator string

const (
	InitiatorSystem Initiator = "system"
	InitiatorUser   Initiator = "user"
)

// RequiresAnnouncement reports whether an unload by this initiator must be
// announced to the user.
func (i Initiator) RequiresAnnouncement() bool { return i == InitiatorSystem }

// messageKeyPrefix namespaces the locale-bundle keys this package emits.
const messageKeyPrefix = "lifecycle.unload."

// UnloadEvent is the record of one model leaving the available set. It carries
// machine keys plus data — never a rendered sentence — so the same event reads
// correctly in every language the product speaks (CONST-046).
type UnloadEvent struct {
	ModelID        string        // which model went (FR-046)
	Reason         UnloadReason  // why it went (FR-046)
	Initiator      Initiator     // who decided
	IdleFor        time.Duration // how long it had served nothing
	ReclaimedBytes int64         // how much memory went back to the host
	At             time.Time     // when
}

// MessageKey is the locale-bundle key for this event's wording.
func (e UnloadEvent) MessageKey() string { return messageKeyPrefix + string(e.Reason) }

// Fields returns the interpolation data for the message key. A renderer needs
// nothing beyond these values to compose the sentence in any language.
func (e UnloadEvent) Fields() map[string]any {
	return map[string]any{
		"model_id":         e.ModelID,
		"reason":           string(e.Reason),
		"initiator":        string(e.Initiator),
		"idle_for_seconds": int64(e.IdleFor / time.Second),
		"reclaimed_bytes":  e.ReclaimedBytes,
		"at_unix":          e.At.Unix(),
	}
}

// Validate reports whether the event can actually explain the disappearance.
func (e UnloadEvent) Validate() error {
	if e.ModelID == "" {
		return fmt.Errorf("%w: no model named", ErrUnexplainedUnload)
	}
	if e.Reason == "" {
		return fmt.Errorf("%w: model=%s has no reason", ErrUnexplainedUnload, e.ModelID)
	}
	if e.Initiator == "" {
		return fmt.Errorf("%w: model=%s has no initiator", ErrUnexplainedUnload, e.ModelID)
	}
	return nil
}

// Notifier receives unload announcements. Implementations render the event's
// MessageKey + Fields for the user; this package never renders text itself.
type Notifier interface {
	ModelUnloaded(UnloadEvent)
}

// announce forwards an event to the notifier when — and only when — the system
// took the decision. A user-requested unload is already known to the user, so
// re-announcing it would be noise, not an explanation.
func (m *Manager) announce(ev UnloadEvent) error {
	if !ev.Initiator.RequiresAnnouncement() {
		return nil
	}
	if err := ev.Validate(); err != nil {
		return err
	}
	m.notifier.ModelUnloaded(ev)
	return nil
}
