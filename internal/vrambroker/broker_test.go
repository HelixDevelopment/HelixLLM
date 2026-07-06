package vrambroker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeReader is a unit-test-only budget source (CONST-050(A): fakes ONLY in
// *_test.go). The integration proof uses the REAL nvidia-smi.
func fakeReader(total, used, free int64, err error) budgetReader {
	return func(context.Context) (int64, int64, int64, error) { return total, used, free, err }
}

// gib returns n gibibytes in bytes.
func gib(n int64) int64 { return n * 1024 * MiB }

func TestAdmit_TruthTable(t *testing.T) {
	hr := HeadroomBytes // 2 GiB
	cases := []struct {
		name          string
		free, need    int64
		wantAdmitted  bool
	}{
		{"fits with room", gib(12), gib(8), true},
		{"exact fit (need+headroom == free)", gib(10), gib(8), true},
		{"one byte over budget", gib(10), gib(8) + 1, false},
		{"far over budget (30GiB into 12GiB free)", gib(12), gib(30), false},
		{"zero need still needs headroom, fits", gib(3), 0, true},
		{"zero need, only headroom free, fits", hr, 0, true},
		{"zero need, below headroom, refused", hr - 1, 0, false},
		{"negative need refused", gib(12), -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.wantAdmitted, admit(c.free, c.need, hr))
		})
	}
}

func TestAcquire_Coder_AlwaysGranted_EvenWithZeroFree(t *testing.T) {
	// Coder is the pinned resident hot path: granted even when the card reports
	// zero free VRAM (it is already loaded, never counted against admission).
	b := newWithReader(fakeReader(gib(32), gib(32), 0, nil))
	l, err := b.Acquire(context.Background(), ClassCoder, gib(19))
	require.NoError(t, err)
	require.NotNil(t, l)
	require.Equal(t, ClassCoder, l.Class)
	l.Release()
}

func TestAcquire_Burst_SingleOwner(t *testing.T) {
	// 12 GiB free — small bursts fit; the SECOND concurrent burst is refused.
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	ctx := context.Background()

	first, err := b.Acquire(ctx, ClassImage, gib(1))
	require.NoError(t, err)
	require.NotNil(t, first)

	// Second burst (different class, still burst) while one is live -> refused.
	second, err := b.Acquire(ctx, ClassVideo, gib(1))
	require.Nil(t, second)
	require.ErrorIs(t, err, ErrBurstInUse)

	// Release the first, then a new burst is admitted (single-owner freed).
	first.Release()
	third, err := b.Acquire(ctx, ClassImage, gib(1))
	require.NoError(t, err)
	require.NotNil(t, third)
	third.Release()
}

func TestAcquire_Burst_OverBudget_Refused_FailClosed(t *testing.T) {
	// 12 GiB free; a 30 GiB burst MUST be refused (never OOM), fail-closed.
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	l, err := b.Acquire(context.Background(), ClassImage, gib(30))
	require.Nil(t, l)
	require.ErrorIs(t, err, ErrBudgetExceeded)
}

func TestAcquire_Warm_AdmissionGated(t *testing.T) {
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	ctx := context.Background()

	// Warm class within budget -> granted; warm is NOT single-owner so two warm
	// leases can coexist as long as budget allows.
	w1, err := b.Acquire(ctx, ClassVLM, gib(3))
	require.NoError(t, err)
	require.NotNil(t, w1)
	w2, err := b.Acquire(ctx, ClassTranslate, gib(3))
	require.NoError(t, err)
	require.NotNil(t, w2)

	// A warm request over budget is refused.
	over, err := b.Acquire(ctx, ClassVLM, gib(40))
	require.Nil(t, over)
	require.ErrorIs(t, err, ErrBudgetExceeded)

	w1.Release()
	w2.Release()
}

func TestAcquire_BudgetUnavailable_FailsClosed(t *testing.T) {
	// nvidia-smi read failure -> admission refused, never an implicit grant.
	b := newWithReader(fakeReader(0, 0, 0, errors.New("nvidia-smi: command not found")))
	l, err := b.Acquire(context.Background(), ClassImage, gib(1))
	require.Nil(t, l)
	require.ErrorIs(t, err, ErrBudgetUnavailable)
}

func TestAcquire_ThermalGuard_Refuses(t *testing.T) {
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	b.thermal = func(context.Context) error { return errors.New("GPU 91C > 85C limit") }
	l, err := b.Acquire(context.Background(), ClassImage, gib(1))
	require.Nil(t, l)
	require.ErrorIs(t, err, ErrThermalUnsafe)
}

func TestLease_Release_Idempotent(t *testing.T) {
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	l, err := b.Acquire(context.Background(), ClassImage, gib(1))
	require.NoError(t, err)

	// Release twice + on a nil lease -> no panic, and the burst slot is freed
	// exactly once so a new burst can be acquired.
	l.Release()
	l.Release()
	var nilLease *Lease
	nilLease.Release()

	next, err := b.Acquire(context.Background(), ClassVideo, gib(1))
	require.NoError(t, err)
	require.NotNil(t, next)
	next.Release()
}

func TestBudget_ReturnsParsedBytes(t *testing.T) {
	b := newWithReader(fakeReader(gib(32), gib(19), gib(13), nil))
	total, used, free := b.Budget()
	require.Equal(t, gib(32), total)
	require.Equal(t, gib(19), used)
	require.Equal(t, gib(13), free)
}

func TestBudget_ReadError_ReturnsZeros(t *testing.T) {
	b := newWithReader(fakeReader(0, 0, 0, errors.New("boom")))
	total, used, free := b.Budget()
	require.Zero(t, total)
	require.Zero(t, used)
	require.Zero(t, free)
}

func TestParseSMICSV(t *testing.T) {
	// Real nvidia-smi shape observed on the target card (MiB, nounits).
	total, used, free, err := parseSMICSV("32607, 19432, 12689\n")
	require.NoError(t, err)
	require.Equal(t, int64(32607)*MiB, total)
	require.Equal(t, int64(19432)*MiB, used)
	require.Equal(t, int64(12689)*MiB, free)

	// Multi-GPU output: first row is used.
	total, _, _, err = parseSMICSV("32607, 19432, 12689\n24564, 100, 24464\n")
	require.NoError(t, err)
	require.Equal(t, int64(32607)*MiB, total)

	_, _, _, err = parseSMICSV("")
	require.Error(t, err)
	_, _, _, err = parseSMICSV("garbage line without commas")
	require.Error(t, err)
	_, _, _, err = parseSMICSV("32607, notanumber, 12689")
	require.Error(t, err)
}

func TestAcquire_ConcurrentBurst_ExactlyOneWinner(t *testing.T) {
	// Race N goroutines to acquire a burst; the single-owner invariant means
	// exactly ONE succeeds, the rest get ErrBurstInUse (mutex-guarded).
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	const n = 32
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
		refused int
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			l, err := b.Acquire(context.Background(), ClassImage, gib(1))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				granted++
				require.NotNil(t, l)
			} else {
				refused++
				require.ErrorIs(t, err, ErrBurstInUse)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, granted, "exactly one burst wins the single-owner slot")
	require.Equal(t, n-1, refused)
}

// Ensure New() satisfies the Broker interface (compile-time contract).
var _ Broker = New()

func Example_admissionMath() {
	fmt.Println(admit(gib(12), gib(8), HeadroomBytes))
	fmt.Println(admit(gib(12), gib(30), HeadroomBytes))
	// Output:
	// true
	// false
}
