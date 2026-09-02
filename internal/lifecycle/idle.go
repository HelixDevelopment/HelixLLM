package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

// Lifecycle errors. Callers switch on these with errors.Is.
var (
	// ErrInvalidConfig is returned when the lifecycle policy is unusable — most
	// importantly a non-positive idle period, which would either unload every
	// model instantly or never unload anything.
	ErrInvalidConfig = errors.New("lifecycle: invalid configuration")

	// ErrNoUnloader is returned when a Manager is constructed without a real
	// unload function. Without one the manager could only PRETEND to hand memory
	// back to the host.
	ErrNoUnloader = errors.New("lifecycle: an UnloadFunc is required — memory must really be returned to the host (FR-044)")

	// ErrModelNotLoaded is returned for an operation naming a model this manager
	// is not tracking.
	ErrModelNotLoaded = errors.New("lifecycle: model is not loaded")

	// ErrModelUnloading is returned when a model is already being unloaded. A
	// second unload of the same model is refused rather than raced.
	ErrModelUnloading = errors.New("lifecycle: model is already being unloaded")

	// ErrNoIdleModel is returned when room was needed but every loaded model is
	// serving a request, so there is no honest offer to make (FR-045 + FR-047).
	ErrNoIdleModel = errors.New("lifecycle: no idle model to free — every loaded model is serving a request")
)

// Config is the lifecycle policy. Every value here is CONFIGURATION supplied by
// the caller — FR-044's idle period is deliberately NOT a constant compiled into
// this package.
type Config struct {
	// IdleTimeout is how long a model may serve no request before its memory is
	// returned to the host. Must be > 0.
	IdleTimeout time.Duration
}

// Validate reports whether the policy is usable.
func (c Config) Validate() error {
	if c.IdleTimeout <= 0 {
		return fmt.Errorf("%w: IdleTimeout=%s must be > 0", ErrInvalidConfig, c.IdleTimeout)
	}
	return nil
}

// UnloadFunc actually returns a model's memory to the host. It is the seam to
// whichever runtime holds the weights (llama.cpp server, Ollama, a remote host);
// the manager owns WHEN a model is unloaded, the runtime owns HOW.
type UnloadFunc func(ctx context.Context, modelID string) error

// Releaser releases a resource reservation. The production value is a
// *vrambroker.Lease: lifecycle COOPERATES with the existing broker arbitration
// (§11.4.74) — it hands the reservation back through the lease that granted it
// rather than accounting for VRAM itself.
type Releaser interface {
	Release()
}

// Compile-time proof that the broker's lease satisfies the seam this package
// releases through. If the broker's contract changes, this stops compiling.
var _ Releaser = (*vrambroker.Lease)(nil)

// modelState is the tracked state of one loaded model. Guarded by Manager.mu.
type modelState struct {
	id        string
	bytes     int64
	lease     Releaser
	inFlight  int  // requests currently being served
	unloading bool // claimed by an in-progress unload; no new request may begin
	lastUsed  time.Time
	loadedAt  time.Time
}

// Manager tracks loaded models, returns idle ones' memory to the host, and
// refuses to take a model away from a request it is still answering.
//
// Model serving state is inherently concurrent — requests begin and end on many
// goroutines while sweeps and evictions run on others — so every field below is
// guarded by mu, and the decision to unload a model is claimed atomically with
// the serving check that permits it.
type Manager struct {
	mu       sync.Mutex
	models   map[string]*modelState
	cfg      Config
	unload   UnloadFunc
	notifier Notifier
	clock    func() time.Time
}

// Option configures a Manager.
type Option func(*Manager)

// WithClock injects the time source. Production uses time.Now; tests drive a
// controllable clock so idle periods are exercised without sleeping.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.clock = now
		}
	}
}

// New builds a Manager. Both a real unloader and a notifier are REQUIRED: a
// manager that cannot hand memory back, or cannot say what it took, is not a
// lifecycle manager (FR-044, FR-046).
func New(cfg Config, unload UnloadFunc, notifier Notifier, opts ...Option) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if unload == nil {
		return nil, ErrNoUnloader
	}
	if notifier == nil {
		return nil, ErrNoNotifier
	}
	m := &Manager{
		models:   make(map[string]*modelState),
		cfg:      cfg,
		unload:   unload,
		notifier: notifier,
		clock:    time.Now,
	}
	for _, o := range opts {
		o(m)
	}
	return m, nil
}

// Track registers a model as loaded and holding bytes of host memory. lease is
// the reservation to release when the model is unloaded (a *vrambroker.Lease in
// production); it may be nil for a model that holds no brokered reservation.
// Tracking an already-tracked model refreshes its last-used stamp.
func (m *Manager) Track(modelID string, bytes int64, lease Releaser) error {
	if modelID == "" {
		return fmt.Errorf("%w: model id is empty", ErrInvalidConfig)
	}
	now := m.clock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.models[modelID]; ok {
		st.lastUsed = now
		return nil
	}
	m.models[modelID] = &modelState{
		id:       modelID,
		bytes:    bytes,
		lease:    lease,
		lastUsed: now,
		loadedAt: now,
	}
	return nil
}

// BeginRequest marks the start of a request served by modelID and returns the
// function that marks its end. While the returned function has not been called
// the model is SERVING and cannot be evicted (FR-047). The returned function is
// idempotent — calling it twice does not fake the model idle.
func (m *Manager) BeginRequest(modelID string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.models[modelID]
	if !ok {
		return nil, fmt.Errorf("%w: model=%s", ErrModelNotLoaded, modelID)
	}
	if st.unloading {
		return nil, fmt.Errorf("%w: model=%s", ErrModelUnloading, modelID)
	}
	st.inFlight++
	st.lastUsed = m.clock()

	var once sync.Once
	return func() {
		once.Do(func() { m.endRequest(modelID) })
	}, nil
}

func (m *Manager) endRequest(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.models[modelID]
	if !ok {
		return
	}
	if st.inFlight > 0 {
		st.inFlight--
	}
	st.lastUsed = m.clock()
}

// IsServing reports whether modelID is currently answering at least one request.
func (m *Manager) IsServing(modelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.models[modelID]
	return ok && st.inFlight > 0
}

// Loaded returns the ids of every tracked model, sorted for determinism.
func (m *Manager) Loaded() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.models))
	for id := range m.models {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ReclaimIdle returns to the host the memory of every model that has served no
// request for the configured idle period (FR-044), and announces each unload
// (FR-046). A model currently serving a request is never taken (FR-047).
//
// Errors from individual unloads are joined and returned; models whose memory
// was NOT actually released stay tracked and are retried on the next sweep.
func (m *Manager) ReclaimIdle(ctx context.Context) ([]UnloadEvent, error) {
	now := m.clock()
	claimed := m.claimIdle(now)

	var (
		events []UnloadEvent
		errs   []error
	)
	for _, st := range claimed {
		ev, err := m.completeUnload(ctx, st, ReasonIdleTimeout, InitiatorSystem, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		events = append(events, ev)
	}
	return events, errors.Join(errs...)
}

// claimIdle atomically selects every idle-past-the-period model and marks it as
// unloading, so concurrent sweeps cannot claim the same model twice and no new
// request can begin on a model already on its way out.
func (m *Manager) claimIdle(now time.Time) []*modelState {
	m.mu.Lock()
	defer m.mu.Unlock()

	var claimed []*modelState
	for _, st := range m.models {
		if st.unloading {
			continue
		}
		// FR-047: never take a model away from a request it is answering, no
		// matter how long ago it last STARTED being idle.
		if err := evictable(st); err != nil {
			continue
		}
		if now.Sub(st.lastUsed) < m.cfg.IdleTimeout {
			continue
		}
		st.unloading = true
		claimed = append(claimed, st)
	}
	sort.Slice(claimed, func(i, j int) bool { return claimed[i].id < claimed[j].id })
	return claimed
}

// completeUnload performs the actual hand-back for a model already claimed
// (st.unloading == true). It calls the unloader WITHOUT holding the mutex, then
// drops the model and releases its broker reservation on success. On failure the
// claim is released so the model stays tracked and remains retryable.
func (m *Manager) completeUnload(
	ctx context.Context,
	st *modelState,
	reason UnloadReason,
	initiator Initiator,
	now time.Time,
) (UnloadEvent, error) {
	if err := m.unload(ctx, st.id); err != nil {
		m.mu.Lock()
		st.unloading = false
		m.mu.Unlock()
		return UnloadEvent{}, fmt.Errorf("returning memory of model %s to the host: %w", st.id, err)
	}

	m.mu.Lock()
	delete(m.models, st.id)
	lease := st.lease
	m.mu.Unlock()

	// §11.4.74: the reservation goes back to the broker that granted it.
	if lease != nil {
		lease.Release()
	}

	ev := UnloadEvent{
		ModelID:        st.id,
		Reason:         reason,
		Initiator:      initiator,
		IdleFor:        now.Sub(st.lastUsed),
		ReclaimedBytes: st.bytes,
		At:             now,
	}
	if err := m.announce(ev); err != nil {
		return ev, fmt.Errorf("announcing unload of model %s: %w", st.id, err)
	}
	return ev, nil
}
