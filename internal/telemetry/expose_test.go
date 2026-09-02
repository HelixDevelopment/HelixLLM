package telemetry

import (
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

func findSample(t *testing.T, samples []Sample, name string, labels map[string]string) (Sample, bool) {
	t.Helper()
next:
	for _, s := range samples {
		if s.Name != name {
			continue
		}
		for k, v := range labels {
			if s.Labels[k] != v {
				continue next
			}
		}
		return s, true
	}
	return Sample{}, false
}

func findLine(lines []Line, key string) (Line, bool) {
	for _, l := range lines {
		if l.MessageKey == key {
			return l, true
		}
	}
	return Line{}, false
}

func populated(t *testing.T) (*Registry, *Recorder) {
	t.Helper()
	u := NewRegistry()
	if err := u.ObserveModel(validModelUsage()); err != nil {
		t.Fatalf("ObserveModel: %v", err)
	}
	if err := u.ObserveHost(validHostUsage()); err != nil {
		t.Fatalf("ObserveHost: %v", err)
	}
	p := NewRecorder()
	if err := p.RecordAt("qwen2.5-coder-7b", testTime(), 200*time.Millisecond, 100); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	return u, p
}

// FR-032: one set of measurements, two audiences. Both renderings must be
// derived from the same snapshot, so a user and an automated check can never be
// told different things about the same instant.
func TestCollect_JoinsUsageAndPerformanceForTheSameInstant(t *testing.T) {
	u, p := populated(t)
	// A model observed but not yet asked anything, and a model serving
	// requests without a usage reading yet.
	usageOnly := validModelUsage()
	usageOnly.ModelID = "usage-only"
	if err := u.ObserveModel(usageOnly); err != nil {
		t.Fatalf("ObserveModel: %v", err)
	}
	if err := p.RecordAt("perf-only", testTime(), time.Second, 10); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}

	snap := Collect(testTime(), u, p)
	if snap.At != testTime() {
		t.Errorf("snapshot instant = %s, want %s", snap.At, testTime())
	}

	want := []string{"perf-only", "qwen2.5-coder-7b", "usage-only"}
	got := make([]string, 0, len(snap.Models))
	for _, m := range snap.Models {
		got = append(got, m.ModelID)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("models are not ordered: %v", got)
	}
	if len(got) != len(want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("models = %v, want %v", got, want)
		}
	}

	for _, tc := range []struct {
		id         string
		usageKnown bool
		perfKnown  bool
	}{
		{"qwen2.5-coder-7b", true, true},
		{"usage-only", true, false},
		{"perf-only", false, true},
	} {
		var found *ModelSnapshot
		for i := range snap.Models {
			if snap.Models[i].ModelID == tc.id {
				found = &snap.Models[i]
			}
		}
		if found == nil {
			t.Fatalf("model %s missing from snapshot", tc.id)
		}
		if found.UsageKnown != tc.usageKnown {
			t.Errorf("%s: usage known = %v, want %v", tc.id, found.UsageKnown, tc.usageKnown)
		}
		if found.PerfKnown != tc.perfKnown {
			t.Errorf("%s: perf known = %v, want %v", tc.id, found.PerfKnown, tc.perfKnown)
		}
	}
}

// A caller may hold only one of the two sources. An absent source contributes
// nothing rather than a snapshot of zeroes.
func TestCollect_ToleratesAnAbsentSource(t *testing.T) {
	u, p := populated(t)

	if snap := Collect(testTime(), nil, p); snap.HostKnown {
		t.Error("host reported known with no usage registry")
	} else if len(snap.Models) != 1 || snap.Models[0].UsageKnown {
		t.Errorf("models = %+v, want one perf-only entry", snap.Models)
	}

	if snap := Collect(testTime(), u, nil); len(snap.Models) != 1 || snap.Models[0].PerfKnown {
		t.Errorf("models = %+v, want one usage-only entry", snap.Models)
	} else if !snap.HostKnown {
		t.Error("host reported unknown when the registry has a reading")
	}

	if snap := Collect(testTime(), nil, nil); snap.HostKnown || len(snap.Models) != 0 {
		t.Errorf("empty snapshot = %+v", snap)
	}
}

// FR-032, machine half: stable metric names an automated check can rely on, so
// the Success Criteria thresholds are verifiable on a real machine.
func TestSeries_CarriesTheFiguresAnAutomatedCheckNeeds(t *testing.T) {
	u, p := populated(t)
	now := testTime().Add(3 * time.Second)
	samples := Collect(now, u, p).Series()

	model := map[string]string{"model_id": "qwen2.5-coder-7b"}
	host := map[string]string{"host": "builder-01"}
	device := map[string]string{"model_id": "qwen2.5-coder-7b", "device": "GPU-8f3c"}

	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   float64
	}{
		// SC-013: current memory and accelerator use, host headroom.
		{"helixllm_model_host_memory_bytes", model, float64(6 * capability.GiB)},
		{"helixllm_model_accelerator_memory_bytes", device, float64(5 * capability.GiB)},
		{"helixllm_host_memory_available_bytes", host, float64(20 * capability.GiB)},
		{"helixllm_host_memory_total_bytes", host, float64(64 * capability.GiB)},
		// SC-014: per-request latency and throughput.
		{"helixllm_model_requests_total", model, 1},
		{"helixllm_model_tokens_total", model, 100},
		{"helixllm_model_service_seconds_total", model, 0.2},
		{"helixllm_model_tokens_per_second", model, 500},
		// FR-033, serving side: the age of the reading is itself readable, so
		// a check can refuse to conclude anything from a stale figure.
		{"helixllm_observation_age_seconds", map[string]string{"scope": "host", "host": "builder-01"}, 3},
		{"helixllm_observation_age_seconds", map[string]string{"scope": "model", "model_id": "qwen2.5-coder-7b"}, 3},
		{"helixllm_model_last_request_age_seconds", model, 3},
	} {
		got, ok := findSample(t, samples, tc.name, tc.labels)
		if !ok {
			t.Errorf("no sample %s%v", tc.name, tc.labels)
			continue
		}
		if math.Abs(got.Value-tc.want) > 1e-6 {
			t.Errorf("%s%v = %v, want %v", tc.name, tc.labels, got.Value, tc.want)
		}
	}

	// The distribution must survive into the machine view, quantile by
	// quantile — a single mean would defeat SC-014's whole purpose.
	for _, q := range []string{"0.5", "0.95", "0.99"} {
		labels := map[string]string{"model_id": "qwen2.5-coder-7b", "quantile": q}
		if _, ok := findSample(t, samples, "helixllm_model_request_latency_seconds", labels); !ok {
			t.Errorf("no latency quantile sample for q=%s", q)
		}
	}
}

// An automated check reading "swapping = 0" must never be looking at a host
// whose swap state could not be determined. The gauge is emitted only when the
// state is a finding; a separate gauge says whether it is one.
func TestSeries_UndeterminedSwapIsNotReportedAsNotSwapping(t *testing.T) {
	host := map[string]string{"host": "builder-01"}

	u := NewRegistry()
	unknown := validHostUsage()
	unknown.Swap = SwapUnknown
	if err := u.ObserveHost(unknown); err != nil {
		t.Fatalf("ObserveHost: %v", err)
	}
	samples := Collect(testTime(), u, nil).Series()

	if s, ok := findSample(t, samples, "helixllm_host_swapping", host); ok {
		t.Errorf("swapping gauge emitted for an undetermined state with value %v", s.Value)
	}
	known, ok := findSample(t, samples, "helixllm_host_swap_known", host)
	if !ok {
		t.Fatal("no helixllm_host_swap_known sample")
	}
	if known.Value != 0 {
		t.Errorf("swap_known = %v, want 0", known.Value)
	}

	for _, tc := range []struct {
		state SwapState
		want  float64
	}{{SwapQuiet, 0}, {SwapActive, 1}} {
		u := NewRegistry()
		h := validHostUsage()
		h.Swap = tc.state
		if err := u.ObserveHost(h); err != nil {
			t.Fatalf("ObserveHost: %v", err)
		}
		samples := Collect(testTime(), u, nil).Series()
		got, ok := findSample(t, samples, "helixllm_host_swapping", host)
		if !ok {
			t.Fatalf("no swapping gauge for determined state %v", tc.state)
		}
		if got.Value != tc.want {
			t.Errorf("swapping for %v = %v, want %v", tc.state, got.Value, tc.want)
		}
		known, _ := findSample(t, samples, "helixllm_host_swap_known", host)
		if known.Value != 1 {
			t.Errorf("swap_known for %v = %v, want 1", tc.state, known.Value)
		}
	}
}

func TestSeries_IsDeterministicallyOrdered(t *testing.T) {
	u, p := populated(t)
	snap := Collect(testTime(), u, p)
	first := snap.Series()
	for i := 0; i < 5; i++ {
		again := snap.Series()
		if len(again) != len(first) {
			t.Fatalf("series length changed: %d then %d", len(first), len(again))
		}
		for j := range first {
			if again[j].Name != first[j].Name || again[j].Value != first[j].Value {
				t.Fatalf("series order changed at %d: %v vs %v", j, first[j], again[j])
			}
		}
	}
}

// FR-032, user half. CONST-046: this package emits message keys and figures,
// never sentences — a consumer renders them in its own locale.
func TestReport_EmitsKeysAndFiguresNeverProse(t *testing.T) {
	u, p := populated(t)
	lines := Collect(testTime().Add(time.Second), u, p).Report(time.Minute)
	if len(lines) == 0 {
		t.Fatal("no report lines")
	}
	for _, l := range lines {
		if !strings.HasPrefix(l.MessageKey, "telemetry.") {
			t.Errorf("message key %q is outside this package's namespace", l.MessageKey)
		}
		if strings.ContainsAny(l.MessageKey, " .!?") && strings.Contains(l.MessageKey, " ") {
			t.Errorf("message key %q looks like a sentence", l.MessageKey)
		}
		for name, v := range l.Fields {
			switch v.(type) {
			case string, bool, int, int64, uint64, float64:
			default:
				t.Errorf("field %s of %s has type %T: a field must be a figure or an identifier, "+
					"so the consumer does the wording", name, l.MessageKey, v)
			}
		}
	}

	usage, ok := findLine(lines, "telemetry.model.usage")
	if !ok {
		t.Fatal("no model usage line")
	}
	if usage.Fields["model_id"] != "qwen2.5-coder-7b" {
		t.Errorf("model_id = %v", usage.Fields["model_id"])
	}
	if usage.Fields["host_memory_bytes"] != uint64(6*capability.GiB) {
		t.Errorf("host_memory_bytes = %v", usage.Fields["host_memory_bytes"])
	}

	perf, ok := findLine(lines, "telemetry.model.performance")
	if !ok {
		t.Fatal("no model performance line")
	}
	for _, f := range []string{"p50_ms", "p95_ms", "p99_ms", "tokens_per_second", "requests"} {
		if _, ok := perf.Fields[f]; !ok {
			t.Errorf("performance line has no %s field", f)
		}
	}

	if _, ok := findLine(lines, "telemetry.host.headroom"); !ok {
		t.Fatal("no host headroom line")
	}
}

// A figure older than the caller's tolerance must be visibly stale, not
// silently believed (FR-033, serving side).
func TestReport_MarksAFigureOlderThanTheCallersToleranceAsStale(t *testing.T) {
	u, p := populated(t)

	fresh := Collect(testTime().Add(2*time.Second), u, p).Report(30 * time.Second)
	usage, _ := findLine(fresh, "telemetry.model.usage")
	if usage.Fields["stale"] != false {
		t.Errorf("a 2s-old reading under a 30s tolerance is marked stale")
	}

	old := Collect(testTime().Add(10*time.Minute), u, p).Report(30 * time.Second)
	usage, _ = findLine(old, "telemetry.model.usage")
	if usage.Fields["stale"] != true {
		t.Errorf("a 10m-old reading under a 30s tolerance is not marked stale")
	}
	host, _ := findLine(old, "telemetry.host.headroom")
	if host.Fields["stale"] != true {
		t.Errorf("a 10m-old host reading under a 30s tolerance is not marked stale")
	}

	// A reading stamped ahead of now has an unknowable age, so it cannot be
	// shown to be current either.
	future := Collect(testTime().Add(-time.Hour), u, p).Report(30 * time.Second)
	usage, _ = findLine(future, "telemetry.model.usage")
	if usage.Fields["stale"] != true {
		t.Errorf("a reading stamped in the future is treated as current")
	}
}

// The three swap states must reach a user as three different things.
func TestReport_DistinguishesTheThreeSwapStates(t *testing.T) {
	for _, tc := range []struct {
		state   SwapState
		wantKey string
		absent  []string
	}{
		{SwapActive, "telemetry.host.swapping", []string{"telemetry.host.swap_undetermined"}},
		{SwapUnknown, "telemetry.host.swap_undetermined", []string{"telemetry.host.swapping"}},
		{SwapQuiet, "", []string{"telemetry.host.swapping", "telemetry.host.swap_undetermined"}},
	} {
		u := NewRegistry()
		h := validHostUsage()
		h.Swap = tc.state
		if tc.state == SwapActive {
			h.SwapUsed = 2 * capability.GiB
		}
		if err := u.ObserveHost(h); err != nil {
			t.Fatalf("ObserveHost: %v", err)
		}
		lines := Collect(testTime(), u, nil).Report(time.Minute)

		if tc.wantKey != "" {
			if _, ok := findLine(lines, tc.wantKey); !ok {
				t.Errorf("state %v: no %s line", tc.state, tc.wantKey)
			}
		}
		for _, key := range tc.absent {
			if _, ok := findLine(lines, key); ok {
				t.Errorf("state %v: unexpected %s line", tc.state, key)
			}
		}
		headroom, ok := findLine(lines, "telemetry.host.headroom")
		if !ok {
			t.Fatalf("state %v: no headroom line", tc.state)
		}
		if headroom.Fields["swap_state"] != tc.state.String() {
			t.Errorf("state %v: headroom swap_state = %v", tc.state, headroom.Fields["swap_state"])
		}
	}
}

// A model nobody has asked anything yet must say so, rather than appear with a
// latency of zero.
func TestReport_AModelThatHasServedNothingSaysSo(t *testing.T) {
	u := NewRegistry()
	if err := u.ObserveModel(validModelUsage()); err != nil {
		t.Fatalf("ObserveModel: %v", err)
	}
	lines := Collect(testTime(), u, NewRecorder()).Report(time.Minute)

	if _, ok := findLine(lines, "telemetry.model.no_requests"); !ok {
		t.Error("no telemetry.model.no_requests line for a model that has served nothing")
	}
	if _, ok := findLine(lines, "telemetry.model.performance"); ok {
		t.Error("a performance line was emitted for a model that has served nothing")
	}
}

// An unobserved host must be reported as unobserved, not as a host with no
// memory left.
func TestReport_AnUnobservedHostIsNotAHostWithNoHeadroom(t *testing.T) {
	lines := Collect(testTime(), NewRegistry(), NewRecorder()).Report(time.Minute)
	if _, ok := findLine(lines, "telemetry.host.headroom"); ok {
		t.Error("a headroom line was emitted with no host reading")
	}
	if _, ok := findLine(lines, "telemetry.host.unobserved"); !ok {
		t.Error("no telemetry.host.unobserved line")
	}
}

// Both audiences read the same snapshot, so the same fact must carry the same
// value in both renderings.
func TestBothRenderingsAgreeOnTheSameFact(t *testing.T) {
	u, p := populated(t)
	snap := Collect(testTime().Add(time.Second), u, p)

	samples := snap.Series()
	lines := snap.Report(time.Minute)

	rate, ok := findSample(t, samples, "helixllm_model_tokens_per_second",
		map[string]string{"model_id": "qwen2.5-coder-7b"})
	if !ok {
		t.Fatal("no tokens_per_second sample")
	}
	perf, ok := findLine(lines, "telemetry.model.performance")
	if !ok {
		t.Fatal("no performance line")
	}
	reported, ok := perf.Fields["tokens_per_second"].(float64)
	if !ok {
		t.Fatalf("tokens_per_second field is %T", perf.Fields["tokens_per_second"])
	}
	if math.Abs(reported-rate.Value) > 1e-9 {
		t.Errorf("user sees %v tokens/s, an automated check sees %v", reported, rate.Value)
	}

	mem, _ := findSample(t, samples, "helixllm_model_host_memory_bytes",
		map[string]string{"model_id": "qwen2.5-coder-7b"})
	usage, _ := findLine(lines, "telemetry.model.usage")
	if float64(usage.Fields["host_memory_bytes"].(uint64)) != mem.Value {
		t.Errorf("user sees %v bytes, an automated check sees %v",
			usage.Fields["host_memory_bytes"], mem.Value)
	}
}
