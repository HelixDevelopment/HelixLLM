package capability

import (
	"fmt"
	"strconv"
	"strings"
)

// Parsing for the macOS measurement path.
//
// It lives outside the darwin build tag on purpose. The exec and syscall wiring
// in measure_darwin.go cannot run anywhere but a Mac, but the part that decides
// what figure gets reported is ordinary text handling — so it is separated here
// where it can be tested from any host. That keeps the untestable surface as
// small as the platform allows, rather than letting a whole axis go unexercised
// because the CI host is the wrong OS (§11.4.81).

// darwinFeatureLeaves maps sysctl's per-capability arm64 leaves onto features.
// The leaf is present on every arm64 Mac and carries "1" or "0"; a "0" is a
// genuine negative report, not an absence.
var darwinFeatureLeaves = map[string]CPUFeature{
	"hw.optional.neon":             FeatureNEON,
	"hw.optional.AdvSIMD":          FeatureNEON,
	"hw.optional.arm.FEAT_DotProd": FeatureDotProd,
	"hw.optional.arm.FEAT_FP16":    FeatureFP16,
}

// darwinFeatureNames maps the uppercase names sysctl lists in
// machdep.cpu.features / machdep.cpu.leaf7_features on Intel Macs.
var darwinFeatureNames = map[string]CPUFeature{
	"AVX2":     FeatureAVX2,
	"AVX512F":  FeatureAVX512F,
	"AVX512BW": FeatureAVX512BW,
	"F16C":     FeatureF16C,
	"FMA":      FeatureFMA,
}

// parseDarwinFeatures reads the instruction-set features from sysctl's two
// different reporting styles: per-capability 0/1 leaves on arm64, and a
// space-separated name list on x86_64.
//
// Only what the host actually reported is returned. A leaf set to "0" yields no
// feature, and an unrecognised name is ignored rather than guessed at.
func parseDarwinFeatures(leaves map[string]string, featureList string) []CPUFeature {
	set := map[CPUFeature]struct{}{}
	for leaf, feature := range darwinFeatureLeaves {
		if strings.TrimSpace(leaves[leaf]) == "1" {
			set[feature] = struct{}{}
		}
	}
	for _, name := range strings.Fields(featureList) {
		if feature, known := darwinFeatureNames[strings.ToUpper(name)]; known {
			set[feature] = struct{}{}
		}
	}
	return sortedFeatures(set)
}

// parseVMStat reads currently-available memory from vm_stat's output.
//
// macOS publishes no single equivalent of Linux's MemAvailable, so the figure
// is composed from the page classes a new workload can genuinely claim without
// forcing anything to disk: free, inactive, speculative and purgeable. Active,
// wired and compressed pages are deliberately excluded — counting them would
// overstate what is actually spendable, which is the direction that produces an
// offer the machine cannot honour.
//
// The composition is a documented derivation from real page counts, not an
// estimate: output that does not carry a page size, or carries no page counts
// at all, is refused rather than reported as zero.
func parseVMStat(out string) (Bytes, error) {
	pageSize, err := parseVMStatPageSize(out)
	if err != nil {
		return 0, err
	}

	counted := map[string]bool{
		"Pages free":        false,
		"Pages inactive":    false,
		"Pages speculative": false,
		"Pages purgeable":   false,
	}
	var pages uint64
	sawAny := false

	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, wanted := counted[key]; !wanted {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimRight(strings.TrimSpace(value), "."), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: vm_stat %q: %v", ErrFigureUnavailable, key, err)
		}
		pages += n
		counted[key] = true
		sawAny = true
	}

	// "Pages free" is present in every vm_stat report; its absence means the
	// output was not vm_stat's at all, and a zero here would be a fabricated
	// out-of-memory reading.
	if !sawAny || !counted["Pages free"] {
		return 0, fmt.Errorf("%w: vm_stat reported no free-page count", ErrFigureUnavailable)
	}
	return Bytes(pages) * pageSize, nil
}

// parseVMStatPageSize reads the page size from vm_stat's header line, which
// reads "Mach Virtual Memory Statistics: (page size of N bytes)".
func parseVMStatPageSize(out string) (Bytes, error) {
	const marker = "page size of "
	idx := strings.Index(out, marker)
	if idx < 0 {
		return 0, fmt.Errorf("%w: vm_stat output carries no page size", ErrFigureUnavailable)
	}
	rest := out[idx+len(marker):]
	digits := rest
	if cut := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
		digits = rest[:cut]
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%w: vm_stat page size %q is not usable", ErrFigureUnavailable, digits)
	}
	return Bytes(n), nil
}
