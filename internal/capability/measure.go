package capability

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Measurement orchestration.
//
// The single rule this file exists to enforce: when any axis fails, the profile
// is marked incomplete and the missing figure stays missing (FR-056). It is
// never replaced by a plausible default, because once a default reaches
// selection nothing downstream can tell it apart from a measurement — and the
// user gets an offer for a model their machine cannot actually run.
//
// The corollary is that an incomplete profile is not discarded either. Whatever
// was genuinely measured is kept and returned, so a caller can report honestly
// on what is known while ValidateForSelection refuses to act on it.

// Options configures one measurement.
type Options struct {
	// HostIdentity overrides the machine's own name. Empty means measure it.
	HostIdentity string

	// WeightsDir is the directory whose filesystem governs whether model
	// weights can be stored. Empty means the working directory.
	WeightsDir string

	// RequireExactStoragePath refuses to fall back to the nearest existing
	// ancestor of WeightsDir.
	//
	// The default (false) is right for a directory a download will create: the
	// directory does not exist yet, but the filesystem that will hold it does,
	// and that filesystem is the honest answer. Set this when the exact path
	// must already exist for the figure to mean anything.
	RequireExactStoragePath bool
}

// Measure reads this host's current capability.
//
// It returns a profile in every case. When err is non-nil the profile carries
// whatever was successfully measured, MeasurementComplete is false, and each
// failed axis is absent rather than defaulted — join-wrapped in err so a caller
// can ask errors.Is which kind of gap it hit.
func Measure(ctx context.Context, opts Options) (HostCapabilityProfile, error) {
	p := HostCapabilityProfile{
		MeasuredAt:       time.Now().UTC(),
		AcceleratorState: AcceleratorStateUnknown,
	}
	var failures []error

	p.HostIdentity = opts.HostIdentity
	if p.HostIdentity == "" {
		name, err := os.Hostname()
		if err != nil {
			failures = append(failures, fmt.Errorf("%w: host identity: %v", ErrFigureUnavailable, err))
		}
		p.HostIdentity = name
	}

	// Cancellation is checked up front and treated as a failure of every axis
	// rather than a partial reading: a half-measured host under a dead context
	// is not something to reason about.
	if err := ctx.Err(); err != nil {
		failures = append(failures, fmt.Errorf("%w: measurement cancelled: %v", ErrFigureUnavailable, err))
		return finish(p, failures)
	}

	if cpu, err := MeasureCPU(); err != nil {
		// Keep whatever the platform did manage to read — architecture and
		// logical cores are useful in a report even when topology is not.
		p.CPU = cpu
		failures = append(failures, fmt.Errorf("cpu: %w", err))
	} else {
		p.CPU = cpu
	}

	if mem, err := MeasureMemory(); err != nil {
		p.MemoryTotal, p.MemoryAvailable = mem.Total, mem.Available
		failures = append(failures, fmt.Errorf("memory: %w", err))
	} else {
		p.MemoryTotal, p.MemoryAvailable = mem.Total, mem.Available
	}

	storagePath := opts.WeightsDir
	if !opts.RequireExactStoragePath {
		storagePath = StoragePathForWeights(opts.WeightsDir)
	}
	if free, err := MeasureStorage(storagePath); err != nil {
		// Deliberately left at zero. A guessed "there is probably room" here
		// is the difference between an honest refusal and an offer the host
		// cannot store.
		failures = append(failures, fmt.Errorf("storage: %w", err))
	} else {
		p.StorageAvailable = free
	}

	accel, err := MeasureAccelerators(ctx)
	p.AcceleratorState = accel.State
	p.Accelerators = accel.Devices
	if err != nil {
		failures = append(failures, fmt.Errorf("accelerators: %w", err))
	}
	for _, gap := range accel.Gaps {
		failures = append(failures, fmt.Errorf("%w: accelerators: %s", ErrFigureUnavailable, gap))
	}

	return finish(p, failures)
}

// finish stamps completeness and returns the joined failures.
//
// MeasurementComplete is set only when nothing failed AND the profile passes
// its own structural checks — so the flag can never claim more than the data
// supports, whichever way a future axis is added.
func finish(p HostCapabilityProfile, failures []error) (HostCapabilityProfile, error) {
	if len(failures) == 0 {
		p.MeasurementComplete = true
		if err := p.Validate(); err != nil {
			// The axes all reported success yet the result is not coherent.
			// Report it as a failure rather than shipping a profile that
			// claims to be complete while contradicting itself.
			p.MeasurementComplete = false
			return p, fmt.Errorf("%w: measurement is internally inconsistent: %v", ErrFigureUnavailable, err)
		}
		return p, nil
	}
	p.MeasurementComplete = false
	return p, errors.Join(failures...)
}
