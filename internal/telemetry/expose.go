package telemetry

import (
	"sort"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

// Exposing the measurements to users and to automated checks (FR-032).
//
// # Why two renderings and not one
//
// FR-032 names two audiences, and they want opposite things. An automated
// check verifying the Success Criteria wants EVERY figure, unrounded, under a
// name that will not change, flat enough to query, with no judgement applied —
// it is going to decide for itself what the numbers mean. A person wants the
// few figures they can act on, grouped by the model they are about, with the
// notable things surfaced (the host is swapping; this reading is old) and
// worded in their own language.
//
// One rendering serving both ends up serving neither: aggregate for the person
// and the check loses the resolution it needs; enumerate for the check and the
// person is handed a wall of gauges. So there are two, and — this is the part
// that matters — they are both derived from a single Snapshot taken at one
// instant. A user and a check reading the same moment can disagree about what
// is worth mentioning, never about what the figure is.
//
// # What each one is
//
//   - Series is the machine half: flat Samples with stable names, exact
//     values, and labels. It maps onto Prometheus, OpenMetrics or JSON without
//     this package taking a dependency on any of them.
//   - Report is the user half: message KEYS plus interpolation data, following
//     the same CONST-046 discipline as lifecycle.UnloadEvent. This package
//     never renders a sentence, so the same reading reads correctly in every
//     language the product speaks.
//
// # Absence is not zero
//
// Both renderings preserve the distinction the readings themselves make. A
// host that has not been observed is not a host with no memory left; a model
// nobody has asked anything is not a model with a latency of zero; a host
// whose swap state could not be read is not a host that is not swapping. Each
// of those is its own key and its own gauge, because the alternative is
// telling somebody something untrue about their own machine.

// Metric names for the machine rendering. They are constants because an
// automated check binds to them: renaming one silently breaks every check that
// depended on it, so a rename is a visible change here.
const (
	MetricModelHostMemoryBytes        = "helixllm_model_host_memory_bytes"
	MetricModelAcceleratorMemoryBytes = "helixllm_model_accelerator_memory_bytes"
	MetricModelRequestsTotal          = "helixllm_model_requests_total"
	MetricModelTokensTotal            = "helixllm_model_tokens_total"
	MetricModelServiceSecondsTotal    = "helixllm_model_service_seconds_total"
	MetricModelTokensPerSecond        = "helixllm_model_tokens_per_second"
	MetricModelRequestLatencySeconds  = "helixllm_model_request_latency_seconds"
	MetricModelLastRequestAgeSeconds  = "helixllm_model_last_request_age_seconds"
	MetricHostMemoryAvailableBytes    = "helixllm_host_memory_available_bytes"
	MetricHostMemoryTotalBytes        = "helixllm_host_memory_total_bytes"
	MetricHostDeviceMemoryAvailable   = "helixllm_host_accelerator_memory_available_bytes"
	MetricHostDeviceMemoryTotal       = "helixllm_host_accelerator_memory_total_bytes"
	MetricHostSwapping                = "helixllm_host_swapping"
	MetricHostSwapKnown               = "helixllm_host_swap_known"
	MetricHostSwapUsedBytes           = "helixllm_host_swap_used_bytes"
	MetricObservationAgeSeconds       = "helixllm_observation_age_seconds"
)

// Message keys for the user rendering. Each is resolved to wording by the
// presentation layer in the user's own language (CONST-046).
const (
	KeyHostHeadroom         = "telemetry.host.headroom"
	KeyHostSwapping         = "telemetry.host.swapping"
	KeyHostSwapUndetermined = "telemetry.host.swap_undetermined"
	KeyHostUnobserved       = "telemetry.host.unobserved"
	KeyModelUsage           = "telemetry.model.usage"
	KeyModelUsageUnobserved = "telemetry.model.usage_unobserved"
	KeyModelPerformance     = "telemetry.model.performance"
	KeyModelNoRequests      = "telemetry.model.no_requests"
)

// Sample is one named figure for an automated check. Value is exact and
// unrounded; At is the instant the underlying reading was taken, so a check can
// reject a figure that is too old to conclude anything from.
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
	At     time.Time
}

// Line is one thing to tell a user: a message key and the data to interpolate
// into it. It carries no rendered text — the consumer supplies the wording for
// the locale it is speaking (CONST-046).
type Line struct {
	MessageKey string
	Fields     map[string]any
}

// ModelSnapshot is everything known about one running model at one instant.
// The *Known flags separate "not observed" from "observed as zero".
type ModelSnapshot struct {
	ModelID    string
	UsageKnown bool
	Usage      ModelUsage
	PerfKnown  bool
	Perf       PerfSummary
}

// Snapshot is the single source both renderings are derived from.
type Snapshot struct {
	// At is the instant the snapshot was taken, against which every age is
	// computed.
	At time.Time
	// HostKnown says whether Host holds a reading. Read it before Host.
	HostKnown bool
	Host      HostUsage
	// Models is one entry per model known to either source, ordered by id.
	Models []ModelSnapshot
}

// Collect joins the two sources into one picture of a single instant.
//
// A model appears if EITHER source knows it: a model that has been observed
// but not yet asked anything is as real as one serving requests without a
// usage reading yet, and hiding either would misrepresent what is running.
// Either source may be nil, in which case it contributes nothing — an absence,
// never a reading of zero.
func Collect(now time.Time, usage *Registry, perf *Recorder) Snapshot {
	snap := Snapshot{At: now}

	byModel := make(map[string]*ModelSnapshot)
	order := func(id string) *ModelSnapshot {
		if m, ok := byModel[id]; ok {
			return m
		}
		m := &ModelSnapshot{ModelID: id}
		byModel[id] = m
		return m
	}

	if usage != nil {
		us := usage.Snapshot()
		snap.HostKnown = us.HostKnown
		snap.Host = us.Host
		for _, u := range us.Models {
			m := order(u.ModelID)
			m.UsageKnown = true
			m.Usage = u
		}
	}
	if perf != nil {
		for _, s := range perf.Summaries() {
			m := order(s.ModelID)
			m.PerfKnown = true
			m.Perf = s
		}
	}

	snap.Models = make([]ModelSnapshot, 0, len(byModel))
	for _, m := range byModel {
		snap.Models = append(snap.Models, *m)
	}
	sort.Slice(snap.Models, func(i, j int) bool { return snap.Models[i].ModelID < snap.Models[j].ModelID })
	return snap
}

// Series renders the snapshot for an automated check: every figure, exact,
// under a stable name, in a deterministic order.
func (s Snapshot) Series() []Sample {
	out := make([]Sample, 0, 8+len(s.Models)*8)

	if s.HostKnown {
		host := map[string]string{"host": s.Host.HostIdentity}
		at := s.Host.ObservedAt
		out = append(out,
			Sample{MetricHostMemoryAvailableBytes, host, float64(s.Host.MemoryAvailable), at},
			Sample{MetricHostMemoryTotalBytes, host, float64(s.Host.MemoryTotal), at},
		)
		// The swapping gauge exists only when the state is a finding. A check
		// that reads 0 is therefore reading "not swapping", never "we could
		// not tell" — the absence of the gauge is the undetermined case, and
		// MetricHostSwapKnown states which of the two it is looking at.
		out = append(out, Sample{MetricHostSwapKnown, host, boolValue(s.Host.Swap.Known()), at})
		if s.Host.Swap.Known() {
			out = append(out,
				Sample{MetricHostSwapping, host, boolValue(s.Host.Swap == SwapActive), at},
				Sample{MetricHostSwapUsedBytes, host, float64(s.Host.SwapUsed), at},
			)
		}
		for _, d := range s.Host.Devices {
			labels := map[string]string{
				"host":   s.Host.HostIdentity,
				"device": string(d.Device),
				"api":    string(d.API),
			}
			out = append(out,
				Sample{MetricHostDeviceMemoryAvailable, labels, float64(d.MemoryAvailable), at},
				Sample{MetricHostDeviceMemoryTotal, labels, float64(d.MemoryTotal), at},
			)
		}
		out = append(out, Sample{
			Name:   MetricObservationAgeSeconds,
			Labels: map[string]string{"scope": "host", "host": s.Host.HostIdentity},
			Value:  s.Host.Age(s.At).Seconds(),
			At:     at,
		})
	}

	for _, m := range s.Models {
		model := map[string]string{"model_id": m.ModelID}
		if m.UsageKnown {
			at := m.Usage.ObservedAt
			out = append(out, Sample{MetricModelHostMemoryBytes, model, float64(m.Usage.HostMemoryUsed), at})
			for _, a := range m.Usage.Accelerators {
				out = append(out, Sample{
					Name: MetricModelAcceleratorMemoryBytes,
					Labels: map[string]string{
						"model_id": m.ModelID,
						"device":   string(a.Device),
						"api":      string(a.API),
					},
					Value: float64(a.MemoryUsed),
					At:    at,
				})
			}
			out = append(out, Sample{
				Name:   MetricObservationAgeSeconds,
				Labels: map[string]string{"scope": "model", "model_id": m.ModelID},
				Value:  m.Usage.Age(s.At).Seconds(),
				At:     at,
			})
		}
		if m.PerfKnown {
			at := m.Perf.Last
			out = append(out,
				Sample{MetricModelRequestsTotal, model, float64(m.Perf.Requests), at},
				Sample{MetricModelTokensTotal, model, float64(m.Perf.Tokens), at},
				Sample{MetricModelServiceSecondsTotal, model, m.Perf.ServiceTime.Seconds(), at},
				Sample{MetricModelTokensPerSecond, model, m.Perf.TokensPerSecond(), at},
				Sample{MetricModelLastRequestAgeSeconds, model, m.Perf.Age(s.At).Seconds(), at},
			)
			// The distribution is carried quantile by quantile. Collapsing it
			// to one figure here would undo the whole point of keeping it
			// (SC-014).
			for _, q := range []struct {
				label string
				value time.Duration
			}{{"0.5", m.Perf.P50}, {"0.95", m.Perf.P95}, {"0.99", m.Perf.P99}} {
				out = append(out, Sample{
					Name:   MetricModelRequestLatencySeconds,
					Labels: map[string]string{"model_id": m.ModelID, "quantile": q.label},
					Value:  q.value.Seconds(),
					At:     at,
				})
			}
		}
	}

	sortSamples(out)
	return out
}

// Report renders the snapshot for a person: what is running, whether the host
// is comfortable, and which readings are too old to rely on.
//
// maxAge is the caller's own tolerance for how old a reading may be before it
// is worth flagging. It is a parameter rather than a constant because the
// right answer depends on how often the caller samples; the policy governing
// whether a HOST MEASUREMENT is current enough to justify a refusal is a
// different question, and lives in capability.FreshnessPolicy.
func (s Snapshot) Report(maxAge time.Duration) []Line {
	var out []Line

	if !s.HostKnown {
		out = append(out, Line{MessageKey: KeyHostUnobserved, Fields: map[string]any{}})
	} else {
		age := s.Host.Age(s.At)
		out = append(out, Line{
			MessageKey: KeyHostHeadroom,
			Fields: map[string]any{
				"host_id":                 s.Host.HostIdentity,
				"memory_available_bytes":  uint64(s.Host.MemoryAvailable),
				"memory_total_bytes":      uint64(s.Host.MemoryTotal),
				"accelerator_count":       len(s.Host.Devices),
				"swap_state":              s.Host.Swap.String(),
				"observation_age_seconds": age.Seconds(),
				"stale":                   isStale(age, maxAge),
			},
		})
		// Swapping and "we could not tell" are each worth raising on their
		// own; a host that is comfortably not swapping is not news, and its
		// state is already in the headroom line above.
		switch s.Host.Swap {
		case SwapActive:
			out = append(out, Line{
				MessageKey: KeyHostSwapping,
				Fields: map[string]any{
					"host_id":         s.Host.HostIdentity,
					"swap_used_bytes": uint64(s.Host.SwapUsed),
				},
			})
		case SwapUnknown:
			out = append(out, Line{
				MessageKey: KeyHostSwapUndetermined,
				Fields:     map[string]any{"host_id": s.Host.HostIdentity},
			})
		}
	}

	for _, m := range s.Models {
		if m.UsageKnown {
			age := m.Usage.Age(s.At)
			out = append(out, Line{
				MessageKey: KeyModelUsage,
				Fields: map[string]any{
					"model_id":                 m.ModelID,
					"host_memory_bytes":        uint64(m.Usage.HostMemoryUsed),
					"accelerator_count":        len(m.Usage.Accelerators),
					"accelerator_memory_bytes": uint64(m.Usage.AcceleratorMemoryUsed()),
					"observation_age_seconds":  age.Seconds(),
					"stale":                    isStale(age, maxAge),
				},
			})
		} else {
			out = append(out, Line{
				MessageKey: KeyModelUsageUnobserved,
				Fields:     map[string]any{"model_id": m.ModelID},
			})
		}

		if !m.PerfKnown {
			out = append(out, Line{
				MessageKey: KeyModelNoRequests,
				Fields:     map[string]any{"model_id": m.ModelID},
			})
			continue
		}
		age := m.Perf.Age(s.At)
		out = append(out, Line{
			MessageKey: KeyModelPerformance,
			Fields: map[string]any{
				"model_id": m.ModelID,
				"requests": m.Perf.Requests,
				"tokens":   m.Perf.Tokens,
				// A person judges a model by what a slow request costs, so the
				// tail is reported beside the typical case, never averaged
				// into it (SC-014).
				"p50_ms":                  millis(m.Perf.P50),
				"p95_ms":                  millis(m.Perf.P95),
				"p99_ms":                  millis(m.Perf.P99),
				"max_ms":                  millis(m.Perf.Max),
				"tokens_per_second":       m.Perf.TokensPerSecond(),
				"service_seconds":         m.Perf.ServiceTime.Seconds(),
				"observation_age_seconds": age.Seconds(),
				"stale":                   isStale(age, maxAge),
			},
		})
	}
	return out
}

// isStale reports whether a reading of this age may still be relied on. A
// negative age means the reading is stamped ahead of the snapshot: its true age
// is unknowable, so it cannot be shown to be current either.
func isStale(age, maxAge time.Duration) bool { return age < 0 || age > maxAge }

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// sortSamples puts the series in a total order so two renderings of the same
// snapshot are byte-identical — a check diffing successive scrapes should see
// only figures change, never ordering.
func sortSamples(s []Sample) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Name != s[j].Name {
			return s[i].Name < s[j].Name
		}
		return labelKey(s[i].Labels) < labelKey(s[j].Labels)
	})
}

func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for k := range labels {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]byte, 0, 32)
	for _, k := range names {
		out = append(out, k...)
		out = append(out, '=')
		out = append(out, labels[k]...)
		out = append(out, ',')
	}
	return string(out)
}

// Compile-time reminder that this package speaks capability's vocabulary for
// resource quantities rather than a parallel one of its own.
var _ = capability.Bytes(0)
