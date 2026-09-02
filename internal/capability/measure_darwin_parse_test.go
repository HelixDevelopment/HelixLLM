package capability

import "testing"

// T012 RED (parsing half). The macOS measurement path cannot execute on a Linux
// host, so its parsing — the part that actually decides what figure is reported
// — is separated out and tested here.
//
// HONEST BOUNDARY (§11.4.6): the samples below are written from the documented
// output shapes of vm_stat and sysctl. They were NOT captured from a live macOS
// machine, because none was available. These tests therefore prove the parsers
// behave correctly on that shape and refuse malformed input; they do NOT prove
// the shape matches any particular macOS release. That confirmation is an
// operator-attended gap recorded for this task.

const vmStatSample = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               50000.
Pages active:                            900000.
Pages inactive:                          300000.
Pages speculative:                        20000.
Pages throttled:                              0.
Pages wired down:                        400000.
Pages purgeable:                          10000.
"Translation faults":                 123456789.
`

func TestParseVMStat_ComputesAvailableFromReclaimablePages(t *testing.T) {
	got, err := parseVMStat(vmStatSample)
	if err != nil {
		t.Fatalf("parseVMStat: %v", err)
	}
	// free + inactive + speculative + purgeable = 380000 pages of 16 KiB.
	want := Bytes(380000) * 16 * KiB
	if got != want {
		t.Errorf("available = %d, want %d", got, want)
	}
}

func TestParseVMStat_RefusesOutputWithNoPageSize(t *testing.T) {
	if _, err := parseVMStat("Pages free: 50000.\n"); err == nil {
		t.Error("parseVMStat accepted output with no page size; the page count would be meaningless")
	}
}

func TestParseVMStat_RefusesOutputWithNoFreePages(t *testing.T) {
	if _, err := parseVMStat("Mach Virtual Memory Statistics: (page size of 16384 bytes)\n"); err == nil {
		t.Error("parseVMStat accepted output with no page counts and would have reported zero available memory as a measurement")
	}
}

func TestParseDarwinFeatures_ReadsArmFeatureFlags(t *testing.T) {
	// sysctl reports arm64 capabilities as individual 0/1 leaves.
	got := parseDarwinFeatures(map[string]string{
		"hw.optional.neon":             "1",
		"hw.optional.arm.FEAT_DotProd": "1",
		"hw.optional.arm.FEAT_FP16":    "0",
		"hw.optional.AdvSIMD_HPFPCvt":  "1",
	}, "")
	assertHasFeature(t, got, FeatureNEON)
	assertHasFeature(t, got, FeatureDotProd)
	assertLacksFeature(t, got, FeatureFP16) // reported as 0, so absent
}

func TestParseDarwinFeatures_ReadsIntelFeatureStrings(t *testing.T) {
	// sysctl reports x86 capabilities as space-separated uppercase names.
	got := parseDarwinFeatures(nil,
		"FPU VME DE SSE4.2 AVX1.0 F16C FMA AVX2 BMI2 AVX512F AVX512BW")
	for _, want := range []CPUFeature{FeatureAVX2, FeatureAVX512F, FeatureAVX512BW, FeatureF16C, FeatureFMA} {
		assertHasFeature(t, got, want)
	}
	assertLacksFeature(t, got, FeatureNEON)
}

func TestParseDarwinFeatures_ClaimsNothingItWasNotTold(t *testing.T) {
	if got := parseDarwinFeatures(nil, ""); len(got) != 0 {
		t.Errorf("parseDarwinFeatures(empty) = %v, want no features — silence is not a capability", got)
	}
}

func assertHasFeature(t *testing.T, got []CPUFeature, want CPUFeature) {
	t.Helper()
	if !(CPUProfile{Features: got}).HasFeature(want) {
		t.Errorf("feature %q missing from %v", want, got)
	}
}

func assertLacksFeature(t *testing.T, got []CPUFeature, unwanted CPUFeature) {
	t.Helper()
	if (CPUProfile{Features: got}).HasFeature(unwanted) {
		t.Errorf("feature %q claimed but not reported by the host: %v", unwanted, got)
	}
}
