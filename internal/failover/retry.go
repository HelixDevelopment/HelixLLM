package failover

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Retry errors. Each names a DIFFERENT reason a lost request was not rescued,
// so a caller — and through the LossEvent a user — learns the real one.
var (
	// ErrNoInstanceSource is returned when retry is enabled but the Runner has
	// nowhere to find an alternative. A policy that promises retry with no
	// roster to retry into is a lie told at construction time.
	ErrNoInstanceSource = errors.New("failover: an InstanceSource is required when retry is enabled (FR-049)")

	// ErrRetryDisabled: the host was lost and policy forbids automatic retry.
	// FR-049 is a MAY, so this is a legitimate configuration, not a fault.
	ErrRetryDisabled = errors.New("failover: automatic retry is disabled by policy")

	// ErrOutputAlreadyDelivered: output from the original attempt had already
	// reached the user, so FR-049 forbids re-running the request elsewhere.
	// Doing so would compose one answer from two model instances (SC-017).
	ErrOutputAlreadyDelivered = errors.New("failover: output was already delivered to the user, so the request must not be re-run on another instance (FR-049, SC-017)")

	// ErrNoEquivalentInstance: nothing equivalent was reachable elsewhere.
	ErrNoEquivalentInstance = errors.New("failover: no equivalent model instance is available on another host")

	// ErrAttemptsExhausted: the bounded attempt budget ran out. The bound is
	// what stops a fleet-wide outage turning one request into a retry storm.
	ErrAttemptsExhausted = errors.New("failover: retry attempts exhausted")
)

// RetryPolicy decides whether — and how far — a lost request may be retried.
//
// FR-049 says the System MAY retry. "MAY" is honoured literally: the zero value
// retries nothing. Silently re-running a user's request on different hardware is
// a decision an operator opts into, not a default the System assumes.
type RetryPolicy struct {
	// Enabled turns automatic retry on. Off by default.
	Enabled bool

	// MaxAttempts bounds the TOTAL number of attempts including the original.
	// Must be >= 1 when Enabled. This is the retry-storm bound: during a
	// fleet-wide outage every request would otherwise walk the entire roster,
	// multiplying load on hosts that are already failing.
	MaxAttempts int
}

// Validate reports whether the policy is usable.
func (p RetryPolicy) Validate() error {
	if !p.Enabled {
		return nil
	}
	if p.MaxAttempts < 1 {
		return fmt.Errorf("%w: MaxAttempts=%d must be >= 1 when retry is enabled", ErrInvalidConfig, p.MaxAttempts)
	}
	return nil
}

// Equivalent reports whether candidate may stand in for original.
//
// "Equivalent" is deliberately narrow: SAME MODEL IDENTITY, DIFFERENT HOST.
// Three conditions, all required:
//
//  1. Identical Fingerprint — the same family, variant and quantisation. A
//     different quantisation is a different model: it answers differently, so
//     substituting it would change WHAT answered while telling the user only
//     that the HOST changed (FR-050), which is a subtler lie than saying
//     nothing.
//  2. A different host — the point of the retry is to leave the host that went.
//  3. The candidate serves at least the original's capabilities. Identical
//     weights on a host whose runtime lacks tool-calling or vision cannot
//     complete the same request.
//
// What this deliberately does NOT do is pick a "comparable" or "similar-quality"
// model. The user chose a model; failover may move it, not replace it.
func Equivalent(original, candidate Instance) bool {
	if candidate.Fingerprint == "" || original.Fingerprint == "" {
		return false
	}
	if candidate.Fingerprint != original.Fingerprint {
		return false
	}
	if candidate.Host == "" || candidate.Host == original.Host {
		return false
	}
	have := make(map[string]struct{}, len(candidate.Capabilities))
	for _, c := range candidate.Capabilities {
		have[c] = struct{}{}
	}
	for _, need := range original.Capabilities {
		if _, ok := have[need]; !ok {
			return false
		}
	}
	return true
}

// InstanceSource offers instances that could stand in for a lost one. It is the
// seam to discovery/selection: this package decides WHETHER to move a request,
// the source knows WHERE it could go.
type InstanceSource interface {
	Equivalents(ctx context.Context, original Instance) ([]Instance, error)
}

// InstanceSourceFunc adapts a plain function to InstanceSource.
type InstanceSourceFunc func(ctx context.Context, original Instance) ([]Instance, error)

// Equivalents implements InstanceSource.
func (f InstanceSourceFunc) Equivalents(ctx context.Context, original Instance) ([]Instance, error) {
	return f(ctx, original)
}

// Deliverer is the streaming sink that carries output to the user. Anything
// written through it has LEFT the System and cannot be recalled — which is
// exactly the condition FR-049 keys on. A Runner without a Deliverer buffers
// the answer instead, so nothing is delivered until an attempt succeeds and a
// retry stays permissible.
type Deliverer interface {
	Deliver(ctx context.Context, chunk []byte) error
}

// deliveryLedger counts bytes that have irreversibly reached the user across
// every attempt of ONE request. It is the FR-049 gate.
type deliveryLedger struct {
	mu    sync.Mutex
	bytes int64
}

func (l *deliveryLedger) add(n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bytes += int64(n)
}

func (l *deliveryLedger) delivered() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bytes
}

// Output is where ONE attempt writes its answer.
//
// The scope of this type is the invariant: an Output belongs to exactly one
// attempt on exactly one instance. When a request fails over, the previous
// Output is dropped whole and the next attempt receives a NEW one, so the
// surviving answer is one instance's bytes and nothing else (SC-017).
type Output struct {
	mu  sync.Mutex
	buf []byte

	ctx       context.Context
	deliverer Deliverer
	ledger    *deliveryLedger
}

func newOutput(ctx context.Context, d Deliverer, ledger *deliveryLedger) *Output {
	return &Output{ctx: ctx, deliverer: d, ledger: ledger}
}

// Write records a chunk of this attempt's answer. When the Runner is streaming,
// the chunk is forwarded to the user immediately and counted as delivered —
// after which FR-049 no longer permits a retry.
//
// Safe for concurrent use: an executor may stream from several goroutines.
func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.deliverer != nil {
		if err := o.deliverer.Deliver(o.ctx, p); err != nil {
			return 0, fmt.Errorf("delivering output to the user: %w", err)
		}
		o.ledger.add(len(p))
	}
	o.buf = append(o.buf, p...)
	return len(p), nil
}

// Len is how many bytes this attempt produced.
func (o *Output) Len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.buf)
}

// Bytes returns a copy of this attempt's output. A copy, not the slice, so a
// discarded attempt's buffer cannot be aliased into a later answer.
func (o *Output) Bytes() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]byte, len(o.buf))
	copy(out, o.buf)
	return out
}

// Attempt runs the request against one instance, writing the answer to out. It
// MUST return an error satisfying errors.Is(err, ErrHostLost) — build one with
// NewHostLostError, or take it from Assessment.Err — when and only when the
// serving host was proven unreachable. Any other error is treated as a genuine
// answer from a live host and is never retried elsewhere.
type Attempt func(ctx context.Context, target Instance, out *Output) error

// Request is one user request being run under failover protection.
type Request struct {
	ID       string   // correlates the run with detection and with announcements
	Original Instance // the instance selection originally chose
}

// AttemptRecord is what happened on one instance.
type AttemptRecord struct {
	Host      string
	Bytes     int   // output this instance produced
	Discarded bool  // whether that output was thrown away
	Err       error // why this attempt ended, if it failed
}

// Result is the outcome of a protected run.
type Result struct {
	// Answer is the complete output of exactly ONE instance (SC-017).
	Answer []byte

	// ServingHost is the host that ultimately served the request (FR-050).
	ServingHost string

	// Attempts is how many instances were tried.
	Attempts int

	// Retried reports whether the answer came from somewhere other than the
	// originally chosen host.
	Retried bool

	// DiscardedBytes is how much partial output from lost instances was thrown
	// away rather than spliced into the answer. Accounted for, never hidden.
	DiscardedBytes int64

	// Trail is the per-instance record, for audit.
	Trail []AttemptRecord
}

// Runner executes a request under failover protection: it detects a lost host
// through the error the attempt returns, decides whether FR-049 permits a
// retry, moves the request to an equivalent instance, and tells the user what
// happened (FR-050).
//
// Every field is immutable after construction, and all per-run state is local
// to Run, so one Runner serves any number of concurrent requests.
type Runner struct {
	policy    RetryPolicy
	source    InstanceSource
	notifier  Notifier
	deliverer Deliverer
	clock     func() time.Time
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// WithRunnerClock injects the time source used to stamp announcements.
func WithRunnerClock(now func() time.Time) RunnerOption {
	return func(r *Runner) {
		if now != nil {
			r.clock = now
		}
	}
}

// WithDeliverer makes the Runner STREAM: each chunk reaches the user as it is
// produced. That is a real product decision with a real cost — once a chunk has
// been delivered, FR-049 forbids retrying the request elsewhere, so a streaming
// request that loses its host fails rather than silently restarting.
func WithDeliverer(d Deliverer) RunnerOption {
	return func(r *Runner) { r.deliverer = d }
}

// NewRunner builds a Runner. A Notifier is always required: an answer produced
// somewhere other than the host the user chose must never arrive silently
// (FR-050, SC-017). An InstanceSource is required only when retry is enabled.
func NewRunner(policy RetryPolicy, source InstanceSource, notifier Notifier, opts ...RunnerOption) (*Runner, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if notifier == nil {
		return nil, ErrNoNotifier
	}
	if policy.Enabled && source == nil {
		return nil, ErrNoInstanceSource
	}
	r := &Runner{policy: policy, source: source, notifier: notifier, clock: time.Now}
	for _, o := range opts {
		o(r)
	}
	return r, nil
}

// Run executes req, failing over to an equivalent instance when — and only
// when — the serving host is lost AND FR-049 permits it.
//
// THE INVARIANT: each attempt writes into its OWN Output, and the answer
// returned is one attempt's bytes. A retry REPLACES the answer; it never
// splices one. Partial output from a lost instance is discarded and counted,
// never concatenated with what the replacement produces (SC-017).
func (r *Runner) Run(ctx context.Context, req Request, attempt Attempt) (Result, error) {
	if attempt == nil {
		return Result{}, fmt.Errorf("%w: attempt function is nil", ErrInvalidConfig)
	}
	if req.Original.Host == "" {
		return Result{}, fmt.Errorf("%w: request %s has no original host", ErrInvalidConfig, req.ID)
	}

	ledger := &deliveryLedger{}
	tried := map[string]bool{}
	target := req.Original

	var (
		res  Result
		lost *HostLostError
	)

	for {
		// SC-017 ANCHOR — and the §1.1 paired-mutation target.
		//
		// A FRESH Output per attempt is what makes "a retry replaces an answer"
		// true in code rather than in prose. Reusing the previous attempt's
		// Output here — or seeding this one with its bytes — would let a dead
		// instance's partial output be concatenated with the replacement's,
		// producing one fluent, plausible, silently-corrupt answer composed of
		// two model instances. TestRun_RetryReplacesTheAnswerAndNeverSplicesIt
		// detects exactly that.
		out := newOutput(ctx, r.deliverer, ledger)

		tried[target.Host] = true
		res.Attempts++
		err := attempt(ctx, target, out)

		if err == nil {
			// This attempt's bytes, alone, are the answer.
			res.Answer = out.Bytes()
			res.ServingHost = target.Host
			res.Retried = target.Host != req.Original.Host
			res.Trail = append(res.Trail, AttemptRecord{Host: target.Host, Bytes: out.Len()})
			if res.Retried {
				// FR-050: the user learns which host ultimately served them.
				if nerr := r.announceRetry(r.retryEvent(req, target, res)); nerr != nil {
					return res, fmt.Errorf("announcing retry of request %s: %w", req.ID, nerr)
				}
			}
			return res, nil
		}

		res.Trail = append(res.Trail, AttemptRecord{
			Host: target.Host, Bytes: out.Len(), Discarded: true, Err: err,
		})

		// An error that is not a host loss is a genuine answer from a live
		// host — a context-length refusal, a bad request. Re-running it
		// elsewhere would only buy a different wording of the same failure.
		var hle *HostLostError
		if !errors.As(err, &hle) {
			if errors.Is(err, ErrHostLost) {
				hle = NewHostLostError(req.ID, target, err)
			} else {
				return res, err
			}
		}
		lost = hle
		// The lost instance's partial output is thrown away, not spliced.
		res.DiscardedBytes += int64(out.Len())

		outcome, retryErr := r.mayRetry(ctx, req, ledger, res.Attempts)
		if retryErr != nil {
			return res, r.terminate(req, lost, res, outcome, retryErr)
		}

		next, pickErr := r.pick(ctx, req.Original, tried)
		if pickErr != nil {
			return res, r.terminate(req, lost, res, OutcomeNoEquivalentInstance, pickErr)
		}
		target = next
	}
}

// mayRetry applies the FR-049 gates in the order that keeps the cheapest and
// most consequential check first. It returns the LossOutcome to report and the
// error to wrap when a retry is not permitted.
func (r *Runner) mayRetry(ctx context.Context, req Request, ledger *deliveryLedger, attempts int) (LossOutcome, error) {
	if err := ctx.Err(); err != nil {
		return OutcomeAttemptsExhausted, err
	}
	if !r.policy.Enabled {
		return OutcomeRetryDisabled, ErrRetryDisabled
	}
	// FR-049: "ONLY when no output from the original attempt has yet been
	// delivered to the user." Once a byte is on the user's screen, the answer
	// has begun; finishing it from another instance would splice two models
	// into one reply (SC-017), so the request fails explicitly instead.
	if n := ledger.delivered(); n > 0 {
		return OutcomeOutputAlreadyDelivered, fmt.Errorf("%w: %d byte(s) already delivered for request %s",
			ErrOutputAlreadyDelivered, n, req.ID)
	}
	if attempts >= r.policy.MaxAttempts {
		return OutcomeAttemptsExhausted, fmt.Errorf("%w: %d of %d attempt(s) made for request %s",
			ErrAttemptsExhausted, attempts, r.policy.MaxAttempts, req.ID)
	}
	return "", nil
}

// pick chooses an equivalent instance on a host not already tried. Equivalence
// is re-checked here even though the source was asked for equivalents: this
// package owns the definition and does not delegate the guarantee.
func (r *Runner) pick(ctx context.Context, original Instance, tried map[string]bool) (Instance, error) {
	candidates, err := r.source.Equivalents(ctx, original)
	if err != nil {
		return Instance{}, fmt.Errorf("%w: %v", ErrNoEquivalentInstance, err)
	}
	for _, c := range candidates {
		if tried[c.Host] {
			// A host already tried does not get a second chance in one run.
			continue
		}
		if !Equivalent(original, c) {
			continue
		}
		return c, nil
	}
	return Instance{}, fmt.Errorf("%w: model=%s fingerprint=%s tried=%d host(s)",
		ErrNoEquivalentInstance, original.ModelID, original.Fingerprint, len(tried))
}

// terminate reports the FR-048 failure to the user and returns the error the
// caller sees. The returned error always satisfies errors.Is(err, ErrHostLost)
// and always names the lost host (SC-016), with the specific reason no retry
// rescued it joined onto it.
func (r *Runner) terminate(req Request, lost *HostLostError, res Result, outcome LossOutcome, cause error) error {
	ev := LossEvent{
		RequestID:      req.ID,
		ModelID:        req.Original.ModelID,
		LostHost:       lost.Host,
		Reason:         ReasonHostUnreachable,
		Outcome:        outcome,
		Attempts:       res.Attempts,
		DiscardedBytes: res.DiscardedBytes,
		At:             r.clock(),
	}
	if err := r.announceLoss(ev); err != nil {
		return errors.Join(lost, cause, fmt.Errorf("announcing loss of request %s: %w", req.ID, err))
	}
	return errors.Join(lost, cause)
}

func (r *Runner) retryEvent(req Request, serving Instance, res Result) RetryEvent {
	return RetryEvent{
		RequestID:      req.ID,
		ModelID:        serving.ModelID,
		Fingerprint:    serving.Fingerprint,
		OriginalHost:   req.Original.Host,
		ServingHost:    serving.Host,
		Reason:         ReasonHostUnreachable,
		Attempts:       res.Attempts,
		DiscardedBytes: res.DiscardedBytes,
		At:             r.clock(),
	}
}
