package testing

// Assertion evaluation for http_request steps.
//
// Every assertion type the challenge banks declare on an HTTP step is
// implemented here. The registry is closed: validateAssertions rejects any
// type absent from it at LOAD time, so an unrecognised assertion can never
// silently evaluate to "true by omission".

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// httpAssertions is the closed set of assertion types valid on an HTTP step.
// The bool value is unused; membership is the contract.
var httpAssertions = map[string]bool{
	"http_status_ok":       true,
	"http_status":          true,
	"status":               true,
	"status_one_of":        true,
	"one_of":               true,
	"equals":               true,
	"not_equal":            true,
	"contains":             true,
	"body_not_contains":    true,
	"not_empty":            true,
	"empty_or_absent":      true,
	"no_5xx":               true,
	"no_5xx_except":        true,
	"header_present":       true,
	"response_time_ms":     true,
	"max_response_time_ms": true,
	"max_latency":          true,
	"response_time_p99_ms": true,
	"min_count":            true,
	"not_all_zero":         true,
	"all_match":            true,
	"none_match":           true,
	"json_path":            true,
}

// nonHTTPAssertions are declared by benchmark / chaos steps. They are
// accepted at load time but never evaluated, because those step kinds have
// no executor — the step is reported skipped and Verify surfaces it.
var nonHTTPAssertions = map[string]bool{
	"latency_p50_lte":         true,
	"latency_p95_lte":         true,
	"latency_p99_lte":         true,
	"max_latency_p99":         true,
	"min_success_rate":        true,
	"min_success_rate_at_max": true,
	"min_throughput_rps":      true,
	"record_max_rps":          true,
	"no_memory_growth":        true,
	"no_data_loss":            true,
	"recovery_within":         true,
}

func knownNonHTTPAssertion(t string) bool {
	return nonHTTPAssertions[t] || httpAssertions[t]
}

// evaluateStep applies every assertion to the collected samples.
//
// Content and status assertions must hold for EVERY sample — a step that
// fires 100 concurrent requests and asserts `status: 200` is asserting that
// all 100 returned 200. Latency assertions are computed over the sample set.
func evaluateStep(step ChallengeStep, samples []httpSample) StepResult {
	name := step.Name
	if name == "" {
		name = step.http.Method + " " + step.http.Path
	}

	if len(samples) == 0 {
		return StepResult{Name: name, Status: StatusFailed,
			Detail: "no response samples were collected"}
	}

	// A transport error is a failure, always — including when the step
	// declares no assertions at all.
	for _, s := range samples {
		if s.Err != nil {
			return StepResult{Name: name, Status: StatusFailed,
				Detail: s.Err.Error()}
		}
	}

	// Compact `action:`/`expected:` steps assert a body substring.
	if step.Action != "" && step.Expected != "" {
		for _, s := range samples {
			if !strings.Contains(s.Body, step.Expected) {
				return StepResult{Name: name, Status: StatusFailed,
					Detail: fmt.Sprintf("expected %q in response body, got: %s",
						step.Expected, truncate(s.Body, 300))}
			}
		}
	}

	for i, a := range step.Assertions {
		if err := evalAssertion(a, samples); err != nil {
			return StepResult{Name: name, Status: StatusFailed,
				Detail: fmt.Sprintf("assertion #%d (%s): %v", i+1, a.Type, err)}
		}
	}

	return StepResult{Name: name, Status: StatusPassed,
		Detail: fmt.Sprintf("HTTP %d (%d sample(s), %s)",
			samples[0].Status, len(samples), latencySummary(samples))}
}

// evalAssertion returns nil when the assertion holds, else an error naming
// what was expected and what was observed.
func evalAssertion(a Assertion, samples []httpSample) error {
	switch a.Type {

	case "response_time_ms", "max_response_time_ms", "max_latency":
		limit, ok := numericOf(a.Max, a.Value, a.Expected)
		if !ok {
			return fmt.Errorf("no numeric limit given (`max:` or `expected:`)")
		}
		worst := maxLatencyMS(samples)
		if worst > limit {
			return fmt.Errorf("slowest sample %.1fms exceeds limit %.1fms", worst, limit)
		}
		return nil

	case "response_time_p99_ms":
		limit, ok := numericOf(a.Max, a.Value, a.Expected)
		if !ok {
			return fmt.Errorf("no numeric limit given (`max:`)")
		}
		p99 := percentileMS(samples, 0.99)
		if p99 > limit {
			return fmt.Errorf("p99 %.1fms exceeds limit %.1fms (n=%d)",
				p99, limit, len(samples))
		}
		return nil

	case "header_present":
		key := a.Name
		if key == "" {
			key = a.Field
		}
		if key == "" {
			return fmt.Errorf("no header name given (`name:`)")
		}
		return forEachSample(samples, func(s httpSample) error {
			if s.Headers.Get(key) == "" {
				return fmt.Errorf("response header %q is absent or empty", key)
			}
			return nil
		})

	case "json_path":
		if a.Path == "" {
			return fmt.Errorf("no `path:` given")
		}
		field := "body." + strings.TrimPrefix(strings.TrimPrefix(a.Path, "$"), ".")
		return forEachSample(samples, func(s httpSample) error {
			got, ok := resolveField(field, s)
			if !ok {
				return fmt.Errorf("path %s not present in response", a.Path)
			}
			if a.Value == nil {
				return nil
			}
			if !scalarEqual(got, a.Value) {
				return fmt.Errorf("path %s = %v, want %v", a.Path, got, a.Value)
			}
			return nil
		})
	}

	// Remaining assertions are per-sample and mostly field-scoped.
	return forEachSample(samples, func(s httpSample) error {
		switch a.Type {

		case "http_status_ok":
			if s.Status < 200 || s.Status > 299 {
				return fmt.Errorf("HTTP %d is not 2xx; body: %s",
					s.Status, truncate(s.Body, 300))
			}
			return nil

		case "status", "http_status":
			want, ok := numericOf(a.Value, a.Expected)
			if !ok {
				return fmt.Errorf("no expected status given")
			}
			if float64(s.Status) != want {
				return fmt.Errorf("HTTP %d, want %d; body: %s",
					s.Status, int(want), truncate(s.Body, 300))
			}
			return nil

		case "status_one_of":
			allowed := listOf(a.Values, a.Expected)
			for _, v := range allowed {
				if fmt.Sprint(s.Status) == scalarString(v) {
					return nil
				}
			}
			return fmt.Errorf("HTTP %d is not one of %v; body: %s",
				s.Status, allowed, truncate(s.Body, 300))

		case "no_5xx":
			if s.Status >= 500 {
				return fmt.Errorf("HTTP %d is a server error; body: %s",
					s.Status, truncate(s.Body, 300))
			}
			return nil

		case "no_5xx_except":
			if s.Status < 500 {
				return nil
			}
			for _, v := range listOf(a.Values, a.Expected) {
				if fmt.Sprint(s.Status) == scalarString(v) {
					return nil
				}
			}
			return fmt.Errorf("HTTP %d is a server error not in the allowed set %v",
				s.Status, listOf(a.Values, a.Expected))

		case "body_not_contains":
			needle := scalarString(firstNonNil(a.Value, a.Expected))
			if needle == "" {
				return fmt.Errorf("no needle given (`value:`)")
			}
			hay := s.Body
			if a.Field != "" {
				got, ok := resolveField(a.Field, s)
				if !ok {
					return nil // absent field cannot contain the needle
				}
				hay = stringify(got)
			}
			if strings.Contains(hay, needle) {
				return fmt.Errorf("response contains forbidden text %q", needle)
			}
			return nil
		}

		// Field-scoped assertions from here down.
		if a.Field == "" {
			return fmt.Errorf("no `field:` given")
		}
		got, present := resolveField(a.Field, s)

		switch a.Type {

		case "not_empty":
			if !present || isEmptyValue(got) {
				return fmt.Errorf("field %s is absent or empty; body: %s",
					a.Field, truncate(s.Body, 300))
			}
			return nil

		case "empty_or_absent":
			if !present || isEmptyValue(got) {
				return nil
			}
			return fmt.Errorf("field %s should be empty or absent, got %v",
				a.Field, truncate(stringify(got), 200))

		case "contains":
			if !present {
				return fmt.Errorf("field %s is absent; body: %s",
					a.Field, truncate(s.Body, 300))
			}
			needle := scalarString(firstNonNil(a.Expected, a.Value))
			if !strings.Contains(stringify(got), needle) {
				return fmt.Errorf("field %s = %q, want it to contain %q",
					a.Field, truncate(stringify(got), 200), needle)
			}
			return nil

		case "equals":
			if !present {
				return fmt.Errorf("field %s is absent", a.Field)
			}
			want := firstNonNil(a.Value, a.Expected)
			if !scalarEqual(got, want) {
				return fmt.Errorf("field %s = %v, want %v", a.Field, got, want)
			}
			return nil

		case "not_equal":
			if !present {
				return nil
			}
			want := firstNonNil(a.Expected, a.Value)
			if scalarEqual(got, want) {
				return fmt.Errorf("field %s = %v, which is the forbidden value",
					a.Field, got)
			}
			return nil

		case "one_of":
			if !present {
				return fmt.Errorf("field %s is absent", a.Field)
			}
			allowed := listOf(a.Values, a.Expected)
			for _, v := range allowed {
				if scalarEqual(got, v) {
					return nil
				}
			}
			return fmt.Errorf("field %s = %v, want one of %v", a.Field, got, allowed)

		case "min_count":
			if !present {
				return fmt.Errorf("field %s is absent", a.Field)
			}
			want, ok := numericOf(a.Expected, a.Value)
			if !ok {
				return fmt.Errorf("no minimum given")
			}
			n := lengthOf(got)
			if n < 0 {
				return fmt.Errorf("field %s is not countable", a.Field)
			}
			if float64(n) < want {
				return fmt.Errorf("field %s has %d element(s), want >= %d",
					a.Field, n, int(want))
			}
			return nil

		case "not_all_zero":
			if !present {
				return fmt.Errorf("field %s is absent", a.Field)
			}
			arr, ok := got.([]any)
			if !ok || len(arr) == 0 {
				return fmt.Errorf("field %s is not a non-empty array", a.Field)
			}
			for _, v := range arr {
				if f, ok := toFloat(v); ok && f != 0 {
					return nil
				}
			}
			return fmt.Errorf("field %s contains only zero values (%d element(s))",
				a.Field, len(arr))

		case "all_match", "none_match":
			if !present {
				return fmt.Errorf("field %s is absent", a.Field)
			}
			arr, ok := got.([]any)
			if !ok {
				arr = []any{got}
			}
			if len(arr) == 0 {
				return fmt.Errorf("field %s selected no elements", a.Field)
			}
			want := firstNonNil(a.Value, a.Expected)
			for _, v := range arr {
				eq := scalarEqual(v, want)
				if a.Type == "all_match" && !eq {
					return fmt.Errorf("field %s has element %v, want every element to be %v",
						a.Field, v, want)
				}
				if a.Type == "none_match" && eq {
					return fmt.Errorf("field %s has forbidden element %v", a.Field, want)
				}
			}
			return nil
		}

		return fmt.Errorf("internal error: assertion type %q has no evaluator", a.Type)
	})
}

func forEachSample(samples []httpSample, fn func(httpSample) error) error {
	for i, s := range samples {
		if err := fn(s); err != nil {
			if len(samples) > 1 {
				return fmt.Errorf("sample %d/%d: %w", i+1, len(samples), err)
			}
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Field resolution: `status`, `body`, `headers.X`, `body.a.b[0].c`,
// `body.data[*].owned_by`
// -------------------------------------------------------------------------

func resolveField(field string, s httpSample) (any, bool) {
	switch {
	case field == "status":
		return s.Status, true
	case field == "body":
		return s.Body, true
	case strings.HasPrefix(field, "headers."):
		key := strings.TrimPrefix(field, "headers.")
		v := s.Headers.Get(key)
		return v, v != ""
	case field == "headers":
		return s.Headers, true
	case strings.HasPrefix(field, "body."):
		var doc any
		if err := json.Unmarshal([]byte(s.Body), &doc); err != nil {
			return nil, false
		}
		return walkPath(doc, strings.TrimPrefix(field, "body."))
	default:
		return nil, false
	}
}

// walkPath resolves a dotted path with optional [n] / [*] indexing.
func walkPath(cur any, path string) (any, bool) {
	for _, seg := range splitPath(path) {
		key, idx, star, err := parseSegment(seg)
		if err != nil {
			return nil, false
		}
		if key != "" {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			cur, ok = m[key]
			if !ok {
				return nil, false
			}
		}
		if star {
			arr, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			cur = arr
			// The remainder of the path applies to each element.
			rest := remainderAfter(path, seg)
			if rest == "" {
				return arr, true
			}
			var out []any
			for _, el := range arr {
				v, ok := walkPath(el, rest)
				if !ok {
					return nil, false
				}
				out = append(out, v)
			}
			return out, true
		}
		if idx >= 0 {
			arr, ok := cur.([]any)
			if !ok || idx >= len(arr) {
				return nil, false
			}
			cur = arr[idx]
		}
	}
	return cur, true
}

// remainderAfter returns the portion of path following the given segment.
func remainderAfter(path, seg string) string {
	segs := splitPath(path)
	for i, s := range segs {
		if s == seg {
			return strings.Join(segs[i+1:], ".")
		}
	}
	return ""
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// parseSegment splits `choices[0]` into ("choices", 0, false, nil) and
// `data[*]` into ("data", -1, true, nil).
func parseSegment(seg string) (key string, idx int, star bool, err error) {
	idx = -1
	open := strings.IndexByte(seg, '[')
	if open < 0 {
		return seg, -1, false, nil
	}
	if !strings.HasSuffix(seg, "]") {
		return "", -1, false, fmt.Errorf("malformed path segment %q", seg)
	}
	key = seg[:open]
	inner := seg[open+1 : len(seg)-1]
	if inner == "*" {
		return key, -1, true, nil
	}
	n, convErr := strconv.Atoi(inner)
	if convErr != nil {
		return "", -1, false, fmt.Errorf("malformed index in %q", seg)
	}
	return key, n, false, nil
}

// -------------------------------------------------------------------------
// Value helpers
// -------------------------------------------------------------------------

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func listOf(vals []any, expected any) []any {
	if len(vals) > 0 {
		return vals
	}
	if arr, ok := expected.([]any); ok {
		return arr
	}
	if expected != nil {
		return []any{expected}
	}
	return nil
}

// numericOf returns the first argument convertible to a float64.
func numericOf(vals ...any) (float64, bool) {
	for _, v := range vals {
		switch t := v.(type) {
		case nil:
			continue
		case *float64:
			if t != nil {
				return *t, true
			}
		default:
			if f, ok := toFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// scalarString renders a scalar the way the banks write it, so a YAML int
// 200 and a JSON string "200" compare equal.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func scalarEqual(got, want any) bool {
	return scalarString(got) == scalarString(want)
}

// stringify renders a resolved field for substring matching. Composite
// values are rendered as JSON so `contains` works against objects too.
func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any, []any:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
	}
	return scalarString(v)
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}

func lengthOf(v any) int {
	switch t := v.(type) {
	case []any:
		return len(t)
	case map[string]any:
		return len(t)
	case string:
		return len(t)
	default:
		return -1
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// -------------------------------------------------------------------------
// Latency statistics
// -------------------------------------------------------------------------

func maxLatencyMS(samples []httpSample) float64 {
	worst := 0.0
	for _, s := range samples {
		if ms := msOf(s.Elapsed); ms > worst {
			worst = ms
		}
	}
	return worst
}

func percentileMS(samples []httpSample, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	vals := make([]float64, len(samples))
	for i, s := range samples {
		vals[i] = msOf(s.Elapsed)
	}
	sort.Float64s(vals)
	idx := int(math.Ceil(p*float64(len(vals)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

func msOf(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func latencySummary(samples []httpSample) string {
	if len(samples) == 1 {
		return fmt.Sprintf("%.1fms", msOf(samples[0].Elapsed))
	}
	return fmt.Sprintf("max %.1fms, p99 %.1fms",
		maxLatencyMS(samples), percentileMS(samples, 0.99))
}
