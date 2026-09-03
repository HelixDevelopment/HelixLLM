package vrambroker

// Admits is only usable as "what the broker would do" for as long as it IS what
// the broker does. Selection now states its device-memory reserve from
// HeadroomBytes and agrees with Admits (see internal/runtime), so if Admits ever
// drifted from Acquire the agreement would hold against the wrong answer and the
// user would meet the difference at load time.
//
// So the predicate is checked against a REAL Acquire — the same code path
// production takes, with only the nvidia-smi read replaced — across the boundary
// where the two could differ.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdmitsAgreesWithAcquire(t *testing.T) {
	const need int64 = 10 * 1024 * MiB

	// Points derived from the headroom, not written as absolute sizes, so the
	// sweep keeps straddling the boundary if the constant moves.
	frees := []int64{
		need - 1,
		need,
		need + HeadroomBytes - 1,
		need + HeadroomBytes,
		need + HeadroomBytes + 1,
		need + 2*HeadroomBytes,
	}
	for i := int64(0); i <= 16; i++ {
		if step := HeadroomBytes / 8; step > 0 {
			frees = append(frees, need+i*step)
		}
	}

	for _, free := range frees {
		if free < 0 {
			continue
		}
		total := free + 8*1024*MiB
		b := newWithReader(func(context.Context) (int64, int64, int64, error) {
			return total, total - free, free, nil
		})

		lease, err := b.Acquire(context.Background(), ClassVLM, need)
		granted := err == nil
		if lease != nil {
			lease.Release()
		}

		require.Equal(t, granted, Admits(free, need),
			"Admits and a real Acquire disagree at %d MiB free for a %d MiB request (err=%v)",
			free/MiB, need/MiB, err)
	}
}

// TestAdmitsRefusesANegativeRequest keeps the predicate's fail-closed edge
// aligned with the gate's: a nonsensical need is refused by both, not admitted
// because the arithmetic happened to come out true.
func TestAdmitsRefusesANegativeRequest(t *testing.T) {
	require.False(t, Admits(1<<40, -1), "a negative request must be refused, not admitted")
}
