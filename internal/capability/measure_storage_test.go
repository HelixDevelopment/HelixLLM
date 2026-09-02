package capability

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// T007 RED. Free storage is its own axis (research.md D2). These tests assert
// three things a memory-derived or constant implementation cannot satisfy: the
// figure tracks the real filesystem, it differs per path, and a path that
// cannot be measured produces an error rather than a zero.

func TestMeasureStorage_ReportsRealFreeSpace(t *testing.T) {
	got, err := MeasureStorage(t.TempDir())
	if err != nil {
		t.Fatalf("MeasureStorage(tempdir): %v", err)
	}
	if got == 0 {
		t.Fatal("free storage = 0 on a writable temporary directory")
	}

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("SKIP-OK: the df cross-check oracle is not available on %s", runtime.GOOS)
	}
	want, ok := freeBytesViaDfForTest(t, t.TempDir())
	if !ok {
		t.Skip("SKIP-OK: df produced no independent figure to cross-check against")
	}
	// Free space genuinely moves between the two readings, so this is a
	// same-order-of-magnitude check, not equality. A memory figure or an
	// invented constant would miss by far more than a factor of two.
	if uint64(got) > want*2 || want > uint64(got)*2 {
		t.Errorf("MeasureStorage reported %d bytes free, df reports %d — not the same filesystem", got, want)
	}
}

func TestMeasureStorage_IsPerFilesystemNotGlobal(t *testing.T) {
	// A tmpfs mount and the root filesystem have unrelated free-space figures.
	// An implementation returning one global number — or a memory figure —
	// gives the same answer for both.
	const tmpfs = "/dev/shm"
	if runtime.GOOS != "linux" {
		t.Skipf("SKIP-OK: this test needs a known second filesystem; %s has no guaranteed one at a fixed path", runtime.GOOS)
	}
	if _, err := MeasureStorage(tmpfs); err != nil {
		t.Skipf("SKIP-OK: no second filesystem at %s on this host: %v", tmpfs, err)
	}

	onTmpfs, err := MeasureStorage(tmpfs)
	if err != nil {
		t.Fatalf("MeasureStorage(%s): %v", tmpfs, err)
	}
	onRoot, err := MeasureStorage("/")
	if err != nil {
		t.Fatalf("MeasureStorage(/): %v", err)
	}
	if onTmpfs == onRoot {
		t.Errorf("both filesystems reported %d bytes free; the figure does not depend on the path", onRoot)
	}
}

func TestMeasureStorage_IsNotTheMemoryFigure(t *testing.T) {
	// The axis-confusion this guards against is real: a host can have plenty
	// of memory and almost no disk (the LowStorage fixture), so answering a
	// storage question with a memory number produces a wrong offer, not a
	// slightly-off one.
	storage, err := MeasureStorage("/")
	if err != nil {
		t.Skipf("SKIP-OK: root filesystem not measurable here: %v", err)
	}
	mem, err := MeasureMemory()
	if err != nil {
		t.Skipf("SKIP-OK: memory not measurable here: %v", err)
	}
	if storage == mem.Available || storage == mem.Total {
		t.Errorf("free storage (%d) equals a memory figure (total %d, available %d); "+
			"the two axes are not being measured separately", storage, mem.Total, mem.Available)
	}
}

func TestMeasureStorage_UnmeasurablePathErrorsRatherThanReturningZero(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "directory")
	got, err := MeasureStorage(missing)
	if err == nil {
		t.Fatalf("MeasureStorage(%q) returned %d with no error; an unmeasurable path must not read as a full disk or an empty one", missing, got)
	}
	if !errors.Is(err, ErrFigureUnavailable) && !errors.Is(err, ErrPlatformUnsupported) {
		t.Errorf("error was %v; want it to name why the figure is missing", err)
	}
	if got != 0 {
		t.Errorf("failed measurement returned %d; a failure must carry no figure", got)
	}
}

// freeBytesViaDfForTest asks df, which is an entirely different code path from
// the statfs syscall the implementation uses.
func freeBytesViaDfForTest(t *testing.T, path string) (uint64, bool) {
	t.Helper()
	out, err := exec.Command("df", "-k", "-P", path).Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, false
	}
	kib, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, false
	}
	return kib * 1024, true
}
