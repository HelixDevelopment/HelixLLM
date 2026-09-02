package failover

import (
	"errors"
	"fmt"
	"time"
)

// Notification errors.
var (
	// ErrUnexplainedRetry is returned when a retry announcement cannot do its
	// FR-050 job — name which host ultimately served the request. A request
	// answered elsewhere and reported as if nothing happened is exactly the
	// mistake FR-050 exists to prevent.
	ErrUnexplainedRetry = errors.New("failover: retry announcement does not name the host that ultimately served the request (FR-050)")

	// ErrUnexplainedLoss is returned when a loss announcement cannot name the
	// host that became unreachable. SC-016 requires an explicit failure naming
	// the lost host, never a truncated answer presented as complete.
	ErrUnexplainedLoss = errors.New("failover: loss announcement does not name the lost host (FR-048, SC-016)")

	// ErrNoNotifier is returned when a Runner is built without a Notifier.
	// Without one an automatic retry would be silent, and a silent retry means
	// the user believes their chosen host answered when another one did.
	ErrNoNotifier = errors.New("failover: a Notifier is required — an automatic retry must be told to the user (FR-050, SC-017)")
)

// messageKeyPrefix namespaces the locale-bundle keys this package emits.
const messageKeyPrefix = "failover."

// RetryReason is the machine-readable cause of a failover. It is a KEY, not a
// sentence: the user-facing wording is resolved from it by the presentation
// layer in the user's own language (CONST-046 — no hardcoded English here).
type RetryReason string

// ReasonHostUnreachable: the serving host was proven unreachable while the
// request was in flight (FR-048).
const ReasonHostUnreachable RetryReason = "host_unreachable"

// LossOutcome says why a lost request was NOT rescued. Each value is a distinct
// key so the user is told the real reason rather than a generic failure — "the
// retry was switched off" and "your answer had already started arriving" are
// different facts and lead the reader to different actions.
type LossOutcome string

const (
	// OutcomeRetryDisabled: automatic retry is not enabled by policy (FR-049
	// is a MAY, and the operator said no).
	OutcomeRetryDisabled LossOutcome = "retry_disabled"

	// OutcomeOutputAlreadyDelivered: output from the original attempt had
	// already reached the user, so FR-049 forbids re-running the request
	// elsewhere. Continuing would splice two model instances into one answer,
	// which SC-017 forbids outright.
	OutcomeOutputAlreadyDelivered LossOutcome = "output_already_delivered"

	// OutcomeNoEquivalentInstance: nothing equivalent was available on another
	// reachable host.
	OutcomeNoEquivalentInstance LossOutcome = "no_equivalent_instance"

	// OutcomeAttemptsExhausted: the bounded attempt budget ran out.
	OutcomeAttemptsExhausted LossOutcome = "attempts_exhausted"

	// OutcomeRetryFailed: a retry was made and the replacement host was lost
	// too.
	OutcomeRetryFailed LossOutcome = "retry_failed"
)

// RetryEvent is the record of a request that was automatically retried and then
// answered somewhere else. It carries machine keys plus data — never a rendered
// sentence — so the same event reads correctly in every language the product
// speaks (CONST-046).
type RetryEvent struct {
	RequestID      string      // which request
	ModelID        string      // which model answered (the SAME model — see Equivalent)
	Fingerprint    string      // the model identity both instances share
	OriginalHost   string      // the host originally chosen, which was lost
	ServingHost    string      // the host that ULTIMATELY served the request (FR-050)
	Reason         RetryReason // why the failover happened
	Attempts       int         // total attempts made, including the original
	DiscardedBytes int64       // partial output from the lost instance, thrown away (SC-017)
	At             time.Time   // when the answer was completed
}

// MessageKey is the locale-bundle key for this event's wording.
func (e RetryEvent) MessageKey() string { return messageKeyPrefix + "retry." + string(e.Reason) }

// Fields returns the interpolation data for the message key. A renderer needs
// nothing beyond these values to compose the sentence in any language.
func (e RetryEvent) Fields() map[string]any {
	return map[string]any{
		"request_id":      e.RequestID,
		"model_id":        e.ModelID,
		"fingerprint":     e.Fingerprint,
		"original_host":   e.OriginalHost,
		"serving_host":    e.ServingHost,
		"reason":          string(e.Reason),
		"attempts":        e.Attempts,
		"discarded_bytes": e.DiscardedBytes,
		"at_unix":         e.At.Unix(),
	}
}

// Validate reports whether the announcement can actually tell the user what
// happened to their request.
func (e RetryEvent) Validate() error {
	if e.OriginalHost == "" {
		return fmt.Errorf("%w: request=%s names no original host", ErrUnexplainedRetry, e.RequestID)
	}
	if e.ServingHost == "" {
		return fmt.Errorf("%w: request=%s names no serving host", ErrUnexplainedRetry, e.RequestID)
	}
	if e.ServingHost == e.OriginalHost {
		return fmt.Errorf("%w: request=%s claims a retry that never left host %s",
			ErrUnexplainedRetry, e.RequestID, e.OriginalHost)
	}
	if e.Reason == "" {
		return fmt.Errorf("%w: request=%s has no reason", ErrUnexplainedRetry, e.RequestID)
	}
	if e.Attempts < 2 {
		return fmt.Errorf("%w: request=%s reports %d attempt(s) — a single attempt is not a retry",
			ErrUnexplainedRetry, e.RequestID, e.Attempts)
	}
	return nil
}

// LossEvent is the record of a request whose serving host went and which was
// NOT rescued. It is the explicit failure SC-016 demands, in the same
// machine-key-plus-data shape as RetryEvent.
type LossEvent struct {
	RequestID      string      // which request
	ModelID        string      // which model was answering
	LostHost       string      // the host that became unreachable (SC-016)
	Reason         RetryReason // why the host was declared lost
	Outcome        LossOutcome // why no retry rescued it
	Attempts       int         // total attempts made
	DiscardedBytes int64       // partial output thrown away rather than presented as complete
	At             time.Time   // when
}

// MessageKey is the locale-bundle key for this event's wording.
func (e LossEvent) MessageKey() string { return messageKeyPrefix + "loss." + string(e.Outcome) }

// Fields returns the interpolation data for the message key.
func (e LossEvent) Fields() map[string]any {
	return map[string]any{
		"request_id":      e.RequestID,
		"model_id":        e.ModelID,
		"lost_host":       e.LostHost,
		"reason":          string(e.Reason),
		"outcome":         string(e.Outcome),
		"attempts":        e.Attempts,
		"discarded_bytes": e.DiscardedBytes,
		"at_unix":         e.At.Unix(),
	}
}

// Validate reports whether the failure can actually explain itself.
func (e LossEvent) Validate() error {
	if e.LostHost == "" {
		return fmt.Errorf("%w: request=%s names no host", ErrUnexplainedLoss, e.RequestID)
	}
	if e.Outcome == "" {
		return fmt.Errorf("%w: request=%s has no outcome", ErrUnexplainedLoss, e.RequestID)
	}
	if e.Reason == "" {
		return fmt.Errorf("%w: request=%s has no reason", ErrUnexplainedLoss, e.RequestID)
	}
	return nil
}

// Notifier receives failover announcements. Implementations render each event's
// MessageKey + Fields for the user; this package never renders text itself.
type Notifier interface {
	// RequestRetried is called when a request was automatically retried and
	// answered on another host (FR-050).
	RequestRetried(RetryEvent)

	// RequestLost is called when a serving host was lost and the request was
	// not rescued (FR-048, SC-016).
	RequestLost(LossEvent)
}

// announceRetry validates and emits a retry announcement. An event that cannot
// explain itself is refused rather than emitted: a malformed announcement is
// worse than none, because it looks like the user was told.
func (r *Runner) announceRetry(ev RetryEvent) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	r.notifier.RequestRetried(ev)
	return nil
}

// announceLoss validates and emits a loss announcement (FR-048, SC-016).
func (r *Runner) announceLoss(ev LossEvent) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	r.notifier.RequestLost(ev)
	return nil
}
