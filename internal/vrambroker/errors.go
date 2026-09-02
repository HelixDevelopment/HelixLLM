package vrambroker

import "errors"

// Broker admission errors. Callers switch on these (errors.Is) to decide whether
// to queue, degrade (e.g. route to a cloud provider), or surface a 503 — never to
// force an allocation that would OOM the card.
var (
	// ErrBudgetExceeded is returned when the requested reservation would exceed
	// the measured free VRAM (plus the safety headroom). The request is REFUSED,
	// fail-closed — the broker never grants an over-budget lease and never OOMs.
	ErrBudgetExceeded = errors.New("vrambroker: request exceeds available VRAM budget (refused, fail-closed to avoid OOM)")

	// ErrBurstInUse is returned when a burst-class (image|video) Acquire is
	// attempted while another burst lease is already live. Burst classes are
	// single-owner per §11.4.119.
	ErrBurstInUse = errors.New("vrambroker: a burst lease is already live (single-owner §11.4.119)")

	// ErrBudgetUnavailable is returned when the live VRAM budget cannot be read
	// (nvidia-smi failed). Admission fails CLOSED — an unknown budget is treated
	// as no budget, never an implicit grant (§11.4.6 no-guessing).
	ErrBudgetUnavailable = errors.New("vrambroker: VRAM budget unavailable (nvidia-smi read failed) — refusing fail-closed")

	// ErrThermalUnsafe is returned when the card is outside its safe
	// thermal/power envelope for admitting a new GPU workload (§11.4.133). The
	// core ships with a permissive guard; the concrete temperature/power probe is
	// injected via WithThermalGuard.
	ErrThermalUnsafe = errors.New("vrambroker: card outside safe thermal/power envelope (refused, §11.4.133)")

	// ErrDeviceAmbiguous is returned when several GPUs are present and none was
	// named. The broker refuses rather than reading whichever card enumerated
	// first: a budget attributed to a device the caller never chose says nothing
	// about where the work will actually land (§11.4.111, §11.4.6).
	ErrDeviceAmbiguous = errors.New("vrambroker: several accelerators measured and none named — refusing to bind the budget to an enumeration position")

	// ErrDeviceNotFound is returned when the configured device identity is not
	// among the measured devices. Admitting against some other card would be the
	// exact substitution the identity exists to prevent, so this fails CLOSED.
	ErrDeviceNotFound = errors.New("vrambroker: the named accelerator is not among the measured devices — refusing fail-closed")
)
