package lifecycle

import (
	"context"
	"errors"
	"fmt"
)

// ErrModelServing is returned when an unload is attempted against a model that
// is currently answering a request. FR-047: the System MUST NOT evict a model
// that is currently serving a request — taking its memory mid-answer corrupts
// the reply the user is waiting for.
var ErrModelServing = errors.New("lifecycle: model is currently serving a request and must not be evicted (FR-047)")

// evictable reports whether a model may be taken out from under its user right
// now. It is the FR-047 guard and the single point where in-flight work vetoes
// an unload — every path that unloads a model (idle sweep, memory-pressure
// eviction, user-requested unload) consults it while holding Manager.mu, so the
// serving check and the claim that follows it are atomic with respect to a
// request beginning on another goroutine.
//
// This is the §1.1 paired-mutation target: removing the in-flight check here
// makes an actively-serving model evictable, which TestEvictable_PairedMutation
// and TestEvict_RefusesAModelThatIsServing both detect.
func evictable(st *modelState) error {
	if st.inFlight > 0 {
		return fmt.Errorf("%w: model=%s in_flight=%d", ErrModelServing, st.id, st.inFlight)
	}
	return nil
}

// claimForUnload atomically verifies that modelID may be unloaded and marks it
// as unloading. Holding the mutex across both steps is what makes the FR-047
// guarantee real: between the serving check and the claim, no request can begin.
func (m *Manager) claimForUnload(modelID string) (*modelState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.models[modelID]
	if !ok {
		return nil, fmt.Errorf("%w: model=%s", ErrModelNotLoaded, modelID)
	}
	if st.unloading {
		return nil, fmt.Errorf("%w: model=%s", ErrModelUnloading, modelID)
	}
	if err := evictable(st); err != nil {
		return nil, err
	}
	st.unloading = true
	return st, nil
}

// Evict returns a model's memory to the host on the SYSTEM's initiative — for
// example to make room for a selection the host cannot otherwise fit. It is
// refused with ErrModelServing while the model is answering a request (FR-047),
// and every successful eviction is announced (FR-046).
func (m *Manager) Evict(ctx context.Context, modelID string, reason UnloadReason) (UnloadEvent, error) {
	st, err := m.claimForUnload(modelID)
	if err != nil {
		return UnloadEvent{}, err
	}
	return m.completeUnload(ctx, st, reason, InitiatorSystem, m.clock())
}

// Unload returns a model's memory to the host at the USER's request. It obeys
// the same FR-047 guarantee — an in-flight answer is never sacrificed — but is
// not announced, because the user who asked already knows.
func (m *Manager) Unload(ctx context.Context, modelID string) (UnloadEvent, error) {
	st, err := m.claimForUnload(modelID)
	if err != nil {
		return UnloadEvent{}, err
	}
	return m.completeUnload(ctx, st, ReasonUserRequested, InitiatorUser, m.clock())
}

// EvictLRUIdle frees the least-recently-used IDLE model so a new selection has
// room (FR-045), and names what it took (FR-046 / SC-018). Models that are
// serving a request are not candidates (FR-047); when every loaded model is
// busy it returns ErrNoIdleModel rather than taking one anyway.
func (m *Manager) EvictLRUIdle(ctx context.Context) (UnloadEvent, error) {
	st, err := m.claimLRUIdle()
	if err != nil {
		return UnloadEvent{}, err
	}
	return m.completeUnload(ctx, st, ReasonMemoryPressure, InitiatorSystem, m.clock())
}

// claimLRUIdle atomically picks the least-recently-used evictable model and
// marks it as unloading.
func (m *Manager) claimLRUIdle() (*modelState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var best *modelState
	for _, st := range m.models {
		if st.unloading {
			continue
		}
		if err := evictable(st); err != nil {
			continue
		}
		switch {
		case best == nil:
			best = st
		case st.lastUsed.Before(best.lastUsed):
			best = st
		case st.lastUsed.Equal(best.lastUsed) && st.id < best.id:
			// Deterministic tie-break so the choice never depends on map order.
			best = st
		}
	}
	if best == nil {
		return nil, ErrNoIdleModel
	}
	best.unloading = true
	return best, nil
}
