package capability

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// T010 RED. Orchestration, and the rule that gives the whole package its point:
// a partial or failed measurement is reported as incomplete and is NEVER
// quietly completed with a default (FR-056).
//
// A fabricated figure is indistinguishable from a measured one by the time it
// reaches selection, so the only safe behaviour is to refuse.

func TestMeasure_OnThisMachineProducesAUsableProfile(t *testing.T) {
	p, err := Measure(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Measure() on the live host: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("the measured profile is not well-formed: %v", err)
	}
	if !p.MeasurementComplete {
		t.Fatalf("MeasurementComplete = false with no error returned; the two must agree")
	}
	if err := p.ValidateForSelection(); err != nil {
		t.Fatalf("ValidateForSelection() = %v on a complete measurement", err)
	}

	if p.HostIdentity == "" {
		t.Error("HostIdentity is empty; the naming scheme depends on it")
	}
	if p.CPU.LogicalCores <= 0 || p.CPU.PhysicalCores <= 0 {
		t.Errorf("CPU cores not measured: %+v", p.CPU)
	}
	if p.MemoryTotal == 0 || p.MemoryAvailable == 0 {
		t.Errorf("memory not measured: total=%d available=%d", p.MemoryTotal, p.MemoryAvailable)
	}
	if p.StorageAvailable == 0 {
		t.Error("StorageAvailable = 0 on a host that just wrote this test's temp files")
	}
	if !p.AcceleratorStateKnown() {
		t.Errorf("AcceleratorState = %v on a complete measurement", p.AcceleratorState)
	}
	if age := p.Age(time.Now()); age < 0 || age > time.Minute {
		t.Errorf("MeasuredAt is %v old; a fresh measurement must stamp itself now", age)
	}
	t.Logf("host=%q arch=%s cores=%d/%d mem=%d/%d storage=%d accel=%d",
		p.HostIdentity, p.CPU.Architecture, p.CPU.PhysicalCores, p.CPU.LogicalCores,
		p.MemoryAvailable, p.MemoryTotal, p.StorageAvailable, len(p.Accelerators))
}

func TestMeasure_HonoursAnExplicitHostIdentity(t *testing.T) {
	p, err := Measure(context.Background(), Options{HostIdentity: "declared-host"})
	if err != nil {
		t.Fatalf("Measure(): %v", err)
	}
	if p.HostIdentity != "declared-host" {
		t.Errorf("HostIdentity = %q, want the declared value", p.HostIdentity)
	}
}

func TestMeasure_FailedAxisMarksTheProfileIncompleteAndCarriesNoDefault(t *testing.T) {
	// A storage path that cannot be measured. The tempting behaviour — assume
	// "plenty of disk" and carry on — would let selection offer a model that
	// cannot be stored. Refusing is the only honest outcome.
	missing := filepath.Join(t.TempDir(), "no", "such", "place")
	p, err := Measure(context.Background(), Options{WeightsDir: missing, RequireExactStoragePath: true})

	if err == nil {
		t.Fatal("Measure() returned no error for an unmeasurable storage path")
	}
	if !errors.Is(err, ErrFigureUnavailable) && !errors.Is(err, ErrPlatformUnsupported) {
		t.Errorf("err = %v; a failure must name why the figure is missing", err)
	}
	if p.MeasurementComplete {
		t.Error("MeasurementComplete = true despite a failed axis — this is the silent-default defect")
	}
	if p.StorageAvailable != 0 {
		t.Errorf("StorageAvailable = %d after a failed storage measurement; a failure must carry no figure", p.StorageAvailable)
	}
	if !errors.Is(p.ValidateForSelection(), ErrNotMeasured) {
		t.Errorf("ValidateForSelection() = %v, want ErrNotMeasured — an incomplete profile is not a basis for selection",
			p.ValidateForSelection())
	}
	// Even incomplete, what was returned must still be structurally sound, so
	// a caller can log it and report honestly on what WAS measured.
	if err := p.Validate(); err != nil {
		t.Errorf("an honestly-incomplete profile must still be well-formed: %v", err)
	}
}

func TestMeasure_PartialResultKeepsTheFiguresItDidObtain(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	p, _ := Measure(context.Background(), Options{WeightsDir: missing, RequireExactStoragePath: true})

	// The axes that succeeded are still reported. Incomplete means "do not
	// select on this", not "discard everything".
	if p.CPU.LogicalCores <= 0 {
		t.Error("CPU measurement was discarded because an unrelated axis failed")
	}
	if p.MemoryTotal == 0 {
		t.Error("memory measurement was discarded because an unrelated axis failed")
	}
	if p.MeasuredAt.IsZero() {
		t.Error("a partial measurement carries no timestamp")
	}
}

func TestMeasure_ResolvesTheWeightsDirToItsFilesystem(t *testing.T) {
	// The directory a download will create does not exist yet, but the
	// filesystem that will hold it does. Without the exact-path requirement,
	// measurement answers about that filesystem rather than refusing.
	notYetCreated := filepath.Join(t.TempDir(), "models", "will-be-created-later")
	p, err := Measure(context.Background(), Options{WeightsDir: notYetCreated})
	if err != nil {
		t.Fatalf("Measure(): %v", err)
	}
	if !p.MeasurementComplete {
		t.Error("MeasurementComplete = false for a weights directory that does not exist yet but whose filesystem does")
	}
	if p.StorageAvailable == 0 {
		t.Error("StorageAvailable = 0 for a path whose enclosing filesystem is measurable")
	}
}

func TestMeasure_RespectsACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := Measure(ctx, Options{})
	if err == nil {
		t.Fatal("Measure() with a cancelled context returned no error")
	}
	if p.MeasurementComplete {
		t.Error("MeasurementComplete = true for a cancelled measurement")
	}
}
