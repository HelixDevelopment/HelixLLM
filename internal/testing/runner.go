// Package testing provides a challenge runner for loading and executing
// YAML-based challenge banks against a running HelixLLM instance.
//
// # Harness integrity (CONST-035 / Article XI §11.9)
//
// This harness previously reported success while executing nothing: bank
// files whose declared schema the loader had no field for loaded as zero
// entries, steps whose declared shape the dispatcher did not recognise were
// silently marked "skipped", and a challenge with zero executed steps was
// then reported "passed". A suite that cannot fail is worse than a red one,
// so three invariants are now structural:
//
//  1. Loading is STRICT. An unknown key, an unparseable step, or an unknown
//     assertion type is a load ERROR — never a silently dropped entry.
//  2. Dispatch covers what the banks actually declare. A step the harness
//     cannot execute is reported "skipped" with a concrete reason and is
//     counted, never absorbed into a green result.
//  3. Verify reports every way a run executed nothing: no banks, no
//     challenges, no executed steps, or a challenge whose every step was
//     skipped. Callers turn a non-nil Verify error into a non-zero exit.
package testing

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Step status values.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Step kinds recognised by the dispatcher.
const (
	kindHTTP      = "http_request"
	kindBenchmark = "benchmark"
	kindChaos     = "chaos"
)

// ChallengeBank is the top-level container for a YAML challenge bank file.
//
// Two bank shapes exist in challenges/banks and BOTH are authoritative:
//
//	shape A — a flat bank with a top-level `steps:` list, where each step is
//	          an independently named, independently asserted entry;
//	shape B — a bank with a `challenges:` list, each challenge owning steps.
type ChallengeBank struct {
	Name        string          `yaml:"name"`
	Version     string          `yaml:"version"`
	Description string          `yaml:"description"`
	Platforms   []string        `yaml:"platforms"`
	Timeout     string          `yaml:"timeout"`
	Category    string          `yaml:"category"`
	Priority    string          `yaml:"priority"`
	Steps       []ChallengeStep `yaml:"steps"`
	Challenges  []Challenge     `yaml:"challenges"`

	// SourcePath records where the bank was loaded from, so a failure can
	// name the file that declared it.
	SourcePath string `yaml:"-"`
}

// Challenge describes a single named challenge with categorised steps.
type Challenge struct {
	ID          string          `yaml:"id"`
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Category    string          `yaml:"category"`
	Priority    string          `yaml:"priority"`
	Steps       []ChallengeStep `yaml:"steps"`
	Tags        []string        `yaml:"tags,omitempty"`

	// Source names the bank file this challenge came from.
	Source string `yaml:"-"`
}

// ChallengeStep is one executable action inside a challenge. It carries the
// union of every step shape the banks declare:
//
//	typed  — `type: http_request` with a `params:` payload;
//	inline — bare `method:`/`path:` (+ optional body/headers/concurrent);
//	compact— `action: "GET /path"` with an `expected:` substring.
type ChallengeStep struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	// Params is the executor-specific payload. It stays a map because the
	// benchmark / chaos executors declare open-ended knobs; the http_request
	// executor validates its own keys strictly in decodeHTTPParams.
	Params     map[string]any `yaml:"params"`
	Assertions []Assertion    `yaml:"assertions"`
	OnFailure  string         `yaml:"on_failure"`

	// Inline HTTP shape.
	Method     string            `yaml:"method"`
	Path       string            `yaml:"path"`
	Headers    map[string]string `yaml:"headers"`
	Body       any               `yaml:"body"`
	Concurrent int               `yaml:"concurrent"`
	Repeat     int               `yaml:"repeat"`
	Retry      *RetrySpec        `yaml:"retry"`

	// Compact shape.
	Action   string `yaml:"action"`
	Expected string `yaml:"expected"`

	// kind is resolved at load time by validateStep.
	kind string `yaml:"-"`
	// http is the normalised request, populated for kind == kindHTTP.
	http httpRequestSpec `yaml:"-"`
}

// RetrySpec re-runs a step until its assertions hold or attempts run out.
type RetrySpec struct {
	MaxAttempts int `yaml:"max_attempts"`
	IntervalMS  int `yaml:"interval_ms"`
}

// httpParams is the validated form of `params:` on an http_request step.
// Any key under params outside this set is a load error.
type httpParams struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    string
}

// httpParamKeys is the closed set of keys permitted under an http_request
// step's `params:`. A typo'd key refuses to load rather than contributing a
// silently-empty request.
var httpParamKeys = map[string]bool{
	"method": true, "path": true, "headers": true, "body": true,
}

// decodeHTTPParams validates and converts an http_request params block.
func decodeHTTPParams(m map[string]any) (httpParams, error) {
	var p httpParams
	for k := range m {
		if !httpParamKeys[k] {
			return p, fmt.Errorf(
				"unknown key %q under params (known: method, path, headers, body)", k)
		}
	}
	if v, ok := m["method"]; ok {
		s, ok := v.(string)
		if !ok {
			return p, fmt.Errorf("params.method must be a string, got %T", v)
		}
		p.Method = s
	}
	if v, ok := m["path"]; ok {
		s, ok := v.(string)
		if !ok {
			return p, fmt.Errorf("params.path must be a string, got %T", v)
		}
		p.Path = s
	}
	if v, ok := m["body"]; ok {
		s, ok := v.(string)
		if !ok {
			return p, fmt.Errorf("params.body must be a string, got %T", v)
		}
		p.Body = s
	}
	if v, ok := m["headers"]; ok {
		raw, ok := v.(map[string]any)
		if !ok {
			return p, fmt.Errorf("params.headers must be a mapping, got %T", v)
		}
		p.Headers = make(map[string]string, len(raw))
		for hk, hv := range raw {
			s, ok := hv.(string)
			if !ok {
				return p, fmt.Errorf(
					"params.headers[%q] must be a string, got %T", hk, hv)
			}
			p.Headers[hk] = s
		}
	}
	return p, nil
}

// httpRequestSpec is the normalised request a step executes.
type httpRequestSpec struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
}

// Assertion is one check applied to a step's observed response.
type Assertion struct {
	Type   string   `yaml:"type"`
	Field  string   `yaml:"field"`
	Name   string   `yaml:"name"`
	Path   string   `yaml:"path"`
	Max    *float64 `yaml:"max"`
	Value  any      `yaml:"value"`
	Values []any    `yaml:"values"`

	// Expected is a scalar for most assertions and a list for one_of.
	Expected any `yaml:"expected"`

	// Benchmark / chaos-only knobs, declared so strict decoding accepts
	// the banks that use them.
	Threshold        any `yaml:"threshold"`
	ThresholdPercent any `yaml:"threshold_percent"`
	Window           any `yaml:"window"`
}

// ChallengeResult captures the full outcome of executing a challenge.
type ChallengeResult struct {
	ID       string
	Name     string
	Source   string
	Status   string // passed, failed, skipped
	Steps    []StepResult
	Error    string
	Duration time.Duration
}

// Executed reports how many of the challenge's steps actually ran.
func (c ChallengeResult) Executed() int {
	n := 0
	for _, s := range c.Steps {
		if s.Status != StatusSkipped {
			n++
		}
	}
	return n
}

// StepResult captures the outcome of a single step.
type StepResult struct {
	Name   string
	Status string
	Detail string
}

// Runner loads challenge banks and executes them against a base URL.
type Runner struct {
	banks     []ChallengeBank
	filesSeen int
	baseURL   string
	client    *http.Client
}

// NewRunner creates a Runner that targets baseURL for HTTP challenges.
func NewRunner(baseURL string) *Runner {
	return &Runner{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Banks returns the loaded banks (read-only view, for callers that report
// what was loaded).
func (r *Runner) Banks() []ChallengeBank { return r.banks }

// TrustCACert adds the PEM certificate(s) at path to the trust anchors used
// for challenge requests, IN ADDITION to the system pool.
//
// This exists for ONE reason: the project's dev server is served over HTTPS
// with a self-signed certificate (`make certs` writes certs/cert.pem), so a
// verifying client cannot reach the target the challenge Makefile points at —
// every challenge then fails on x509 rather than on the feature under test.
//
// Certificate verification stays ON: this PINS the project's own dev
// certificate rather than disabling verification, so a MITM with any other
// certificate is still rejected. There is deliberately no "skip verification"
// option — that would make every challenge's transport-level PASS meaningless.
func (r *Runner) TrustCACert(path string) error {
	pem, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read CA cert %s: %w", path, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return fmt.Errorf("CA cert %s contains no usable PEM certificate", path)
	}
	r.client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}
	return nil
}

// -------------------------------------------------------------------------
// Loading — strict by construction
// -------------------------------------------------------------------------

// LoadBank reads and parses a single YAML bank file, appending it to the
// runner's list of banks.
//
// Decoding is STRICT: an unknown top-level, challenge, step, or assertion
// key is an error. A bank that declares no entries is an error. A bank whose
// typo'd key would otherwise contribute zero entries refuses to load and
// says so, rather than exiting green.
func (r *Runner) LoadBank(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bank %s: %w", path, err)
	}

	var bank ChallengeBank
	if err := strictUnmarshal(data, &bank); err != nil {
		return fmt.Errorf("parse bank %s: %w", path, err)
	}
	bank.SourcePath = path

	if len(bank.Steps) == 0 && len(bank.Challenges) == 0 {
		return fmt.Errorf(
			"bank %s declares no entries: it has neither a top-level `steps:` "+
				"list nor a `challenges:` list", path)
	}

	if err := validateBank(&bank); err != nil {
		return err
	}

	r.filesSeen++
	r.banks = append(r.banks, bank)
	return nil
}

// LoadBanksDir walks dir RECURSIVELY and loads every .yaml / .yml file found.
//
// Recursion matters: challenges/banks/ contains only sub-directories, so a
// non-recursive walk of it loads zero banks and reports success.
// A directory containing no bank files at all is an error.
func (r *Runner) LoadBanksDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("read banks dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("banks dir %s is not a directory", dir)
	}

	found := 0
	walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		found++
		return r.LoadBank(p)
	})
	if walkErr != nil {
		return walkErr
	}

	if found == 0 {
		return fmt.Errorf(
			"no challenge bank files (*.yaml, *.yml) found under %s: "+
				"the harness would otherwise execute nothing and report success", dir)
	}
	return nil
}

// strictUnmarshal decodes YAML with unknown-field rejection.
func strictUnmarshal(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if err == io.EOF {
			return fmt.Errorf("file is empty")
		}
		return err
	}
	return nil
}

// validateBank resolves and validates every step in a bank.
func validateBank(bank *ChallengeBank) error {
	for i := range bank.Steps {
		if err := validateStep(&bank.Steps[i]); err != nil {
			return fmt.Errorf("bank %s: step %q: %w",
				bank.SourcePath, stepLabel(bank.Steps[i], i), err)
		}
	}
	for ci := range bank.Challenges {
		ch := &bank.Challenges[ci]
		if len(ch.Steps) == 0 {
			return fmt.Errorf("bank %s: challenge %q declares no steps",
				bank.SourcePath, ch.Name)
		}
		for i := range ch.Steps {
			if err := validateStep(&ch.Steps[i]); err != nil {
				return fmt.Errorf("bank %s: challenge %q: step %q: %w",
					bank.SourcePath, ch.Name, stepLabel(ch.Steps[i], i), err)
			}
		}
	}
	return nil
}

func stepLabel(s ChallengeStep, i int) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("#%d", i+1)
}

var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodHead: true,
	http.MethodOptions: true,
}

// validateStep resolves a step's kind and rejects anything the harness
// cannot account for. A step that declares no executable action is an
// ERROR at load time — it is never silently dropped into a skip.
func validateStep(s *ChallengeStep) error {
	switch {
	case s.Action != "":
		return validateCompactStep(s)
	case s.Type != "":
		return validateTypedStep(s)
	case s.Method != "":
		s.kind = kindHTTP
		return validateInlineHTTPStep(s)
	default:
		return fmt.Errorf(
			"declares no executable action: expected one of `type:`, " +
				"`method:`, or `action:`")
	}
}

func validateCompactStep(s *ChallengeStep) error {
	s.kind = kindHTTP
	parts := strings.Fields(strings.TrimSpace(s.Action))
	if len(parts) != 2 {
		return fmt.Errorf(
			"action %q is not in `METHOD /path` form", s.Action)
	}
	method := strings.ToUpper(parts[0])
	if !httpMethods[method] {
		return fmt.Errorf("action %q uses unsupported HTTP method %q",
			s.Action, parts[0])
	}
	s.http = httpRequestSpec{Method: method, Path: parts[1]}
	return validateAssertions(s)
}

func validateTypedStep(s *ChallengeStep) error {
	switch s.Type {
	case kindHTTP:
		s.kind = kindHTTP
		if s.Params == nil {
			return fmt.Errorf("type http_request requires `params:`")
		}
		p, err := decodeHTTPParams(s.Params)
		if err != nil {
			return fmt.Errorf("params: %w", err)
		}
		if p.Method == "" || p.Path == "" {
			return fmt.Errorf("params must set both `method:` and `path:`")
		}
		method := strings.ToUpper(p.Method)
		if !httpMethods[method] {
			return fmt.Errorf("unsupported HTTP method %q", p.Method)
		}
		s.http = httpRequestSpec{
			Method:  method,
			Path:    p.Path,
			Headers: p.Headers,
			Body:    []byte(p.Body),
		}
	case kindBenchmark, kindChaos:
		// Recognised, but this harness ships no executor for load
		// generation or fault injection. The step is reported skipped
		// with a reason and Verify surfaces it — never absorbed green.
		s.kind = s.Type
	default:
		return fmt.Errorf("unknown step type %q (known: %s, %s, %s)",
			s.Type, kindHTTP, kindBenchmark, kindChaos)
	}
	return validateAssertions(s)
}

func validateInlineHTTPStep(s *ChallengeStep) error {
	method := strings.ToUpper(s.Method)
	if !httpMethods[method] {
		return fmt.Errorf("unsupported HTTP method %q", s.Method)
	}
	if s.Path == "" {
		return fmt.Errorf("declares `method:` but no `path:`")
	}
	var body []byte
	if s.Body != nil {
		// Inline bodies are YAML maps; the wire format is JSON.
		b, err := json.Marshal(normalizeYAML(s.Body))
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
		body = b
	}
	headers := s.Headers
	if body != nil {
		if headers == nil {
			headers = map[string]string{}
		}
		if _, ok := headers["Content-Type"]; !ok {
			headers["Content-Type"] = "application/json"
		}
	}
	s.http = httpRequestSpec{
		Method: method, Path: s.Path, Headers: headers, Body: body,
	}
	return validateAssertions(s)
}

// validateAssertions rejects assertion types the evaluator does not
// implement. An unknown assertion type must never evaluate to "true by
// omission".
func validateAssertions(s *ChallengeStep) error {
	for i, a := range s.Assertions {
		if a.Type == "" {
			return fmt.Errorf("assertion #%d has no `type:`", i+1)
		}
		if s.kind == kindHTTP {
			if _, ok := httpAssertions[a.Type]; !ok {
				return fmt.Errorf(
					"assertion #%d: unknown assertion type %q for an "+
						"http_request step", i+1, a.Type)
			}
			continue
		}
		// benchmark / chaos assertions are declared but unevaluated
		// because the step itself has no executor.
		if !knownNonHTTPAssertion(a.Type) {
			return fmt.Errorf("assertion #%d: unknown assertion type %q",
				i+1, a.Type)
		}
	}
	return nil
}

// normalizeYAML converts map[any]any (yaml.v3 can still produce it for
// non-string keys) into JSON-encodable shapes.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeYAML(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeYAML(val)
		}
		return out
	default:
		return v
	}
}

// -------------------------------------------------------------------------
// Normalisation — both bank shapes collapse to a flat challenge list
// -------------------------------------------------------------------------

// challenges flattens every loaded bank into executable challenges.
//
// A shape-A bank's top-level steps are independently named and independently
// asserted, so each one becomes its own challenge; that is what makes the
// per-entry pass/fail count honest.
func (r *Runner) challenges() []Challenge {
	var out []Challenge
	for _, bank := range r.banks {
		cat := bank.Category
		if cat == "" {
			cat = filepath.Base(filepath.Dir(bank.SourcePath))
		}
		for _, st := range bank.Steps {
			out = append(out, Challenge{
				ID:       bank.Name + "/" + st.Name,
				Name:     st.Name,
				Category: cat,
				Priority: bank.Priority,
				Steps:    []ChallengeStep{st},
				Source:   bank.SourcePath,
			})
		}
		for _, ch := range bank.Challenges {
			if ch.ID == "" {
				ch.ID = bank.Name + "/" + ch.Name
			}
			if ch.Category == "" {
				ch.Category = cat
			}
			if ch.Priority == "" {
				ch.Priority = bank.Priority
			}
			ch.Source = bank.SourcePath
			out = append(out, ch)
		}
	}
	return out
}

// RunAll executes every challenge across all loaded banks.
func (r *Runner) RunAll(ctx context.Context) []ChallengeResult {
	var results []ChallengeResult
	for _, ch := range r.challenges() {
		results = append(results, r.runChallenge(ctx, ch))
	}
	return results
}

// RunByCategory executes only challenges whose Category matches.
func (r *Runner) RunByCategory(ctx context.Context, category string) []ChallengeResult {
	var results []ChallengeResult
	for _, ch := range r.challenges() {
		if ch.Category == category {
			results = append(results, r.runChallenge(ctx, ch))
		}
	}
	return results
}

// RunByPriority executes only challenges whose Priority matches.
func (r *Runner) RunByPriority(ctx context.Context, priority string) []ChallengeResult {
	var results []ChallengeResult
	for _, ch := range r.challenges() {
		if ch.Priority == priority {
			results = append(results, r.runChallenge(ctx, ch))
		}
	}
	return results
}

// -------------------------------------------------------------------------
// Verify — the guard that makes "green while doing nothing" impossible
// -------------------------------------------------------------------------

// Verify inspects a completed run and returns a non-nil error naming every
// way in which the harness executed nothing. Callers MUST turn a non-nil
// result into a non-zero exit: an empty run is a harness failure, not a pass.
func (r *Runner) Verify(results []ChallengeResult) error {
	var problems []string

	if len(r.banks) == 0 {
		problems = append(problems,
			"no challenge banks were loaded — nothing could run")
	}

	declared := len(r.challenges())
	if len(r.banks) > 0 && declared == 0 {
		problems = append(problems, fmt.Sprintf(
			"%d bank(s) loaded but they declare 0 challenges", len(r.banks)))
	}

	if len(results) == 0 {
		problems = append(problems,
			"0 challenges were selected to run — no results were produced")
	}

	executed := 0
	for _, res := range results {
		executed += res.Executed()
	}
	if len(results) > 0 && executed == 0 {
		problems = append(problems, fmt.Sprintf(
			"0 of the steps across %d challenge(s) actually executed — "+
				"every step was skipped", len(results)))
	}

	for _, res := range results {
		if res.Executed() == 0 {
			reason := "challenge declares no steps"
			if len(res.Steps) > 0 {
				reason = firstSkipReason(res.Steps)
			}
			problems = append(problems, fmt.Sprintf(
				"challenge %q (%s) executed 0 steps: %s",
				res.ID, res.Source, reason))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems[min(1, len(problems)):])
	return fmt.Errorf("challenge harness executed nothing it claimed to:\n  - %s",
		strings.Join(problems, "\n  - "))
}

func firstSkipReason(steps []StepResult) string {
	for _, s := range steps {
		if s.Status == StatusSkipped {
			return s.Detail
		}
	}
	return "no step produced a result"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// -------------------------------------------------------------------------
// Execution
// -------------------------------------------------------------------------

// runChallenge executes all steps in a single challenge and collects results.
//
// Status rules — note that "no step failed" is NOT sufficient for a pass:
//
//	any step failed        -> failed
//	>=1 step executed      -> passed
//	every step skipped     -> skipped   (Verify turns this into an exit code)
func (r *Runner) runChallenge(ctx context.Context, ch Challenge) ChallengeResult {
	start := time.Now()
	result := ChallengeResult{ID: ch.ID, Name: ch.Name, Source: ch.Source}

	for _, step := range ch.Steps {
		if ctx.Err() != nil {
			result.Steps = append(result.Steps, StepResult{
				Name:   step.Name,
				Status: StatusSkipped,
				Detail: "context cancelled before this step ran",
			})
			continue
		}

		sr := r.runStep(ctx, step)
		result.Steps = append(result.Steps, sr)
		if sr.Status == StatusFailed {
			result.Status = StatusFailed
			result.Error = sr.Detail
			if step.OnFailure != "" {
				result.Error += " | " + strings.TrimSpace(step.OnFailure)
			}
			result.Duration = time.Since(start)
			return result
		}
	}

	result.Duration = time.Since(start)
	if result.Status == "" {
		if result.Executed() > 0 {
			result.Status = StatusPassed
		} else {
			result.Status = StatusSkipped
		}
	}
	return result
}

// runStep executes a single step according to the kind resolved at load time.
func (r *Runner) runStep(ctx context.Context, step ChallengeStep) StepResult {
	switch step.kind {
	case kindHTTP:
		return r.runHTTPStep(ctx, step)
	case kindBenchmark, kindChaos:
		return StepResult{
			Name:   step.Name,
			Status: StatusSkipped,
			Detail: fmt.Sprintf(
				"no executor implemented for step type %q "+
					"(load generation / fault injection is not part of this harness)",
				step.kind),
		}
	default:
		// Unreachable: validateStep rejects unresolved kinds at load time.
		return StepResult{
			Name:   step.Name,
			Status: StatusFailed,
			Detail: fmt.Sprintf("internal error: step kind %q was never resolved", step.kind),
		}
	}
}

// httpSample is one observed response.
type httpSample struct {
	Status  int
	Headers http.Header
	Body    string
	Elapsed time.Duration
	Err     error
}

// runHTTPStep performs the step's request(s) and evaluates every assertion.
func (r *Runner) runHTTPStep(ctx context.Context, step ChallengeStep) StepResult {
	attempts := 1
	interval := time.Duration(0)
	if step.Retry != nil && step.Retry.MaxAttempts > 1 {
		attempts = step.Retry.MaxAttempts
		interval = time.Duration(step.Retry.IntervalMS) * time.Millisecond
	}

	var last StepResult
	for attempt := 1; attempt <= attempts; attempt++ {
		samples := r.collectSamples(ctx, step)
		last = evaluateStep(step, samples)
		if last.Status == StatusPassed || attempt == attempts {
			return last
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(interval):
		}
	}
	return last
}

// collectSamples issues the step's request(s), honouring repeat/concurrent.
func (r *Runner) collectSamples(ctx context.Context, step ChallengeStep) []httpSample {
	n := 1
	if step.Repeat > 1 {
		n = step.Repeat
	}
	if step.Concurrent > 1 {
		n = step.Concurrent
	}

	samples := make([]httpSample, n)
	if step.Concurrent > 1 {
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				samples[i] = r.doRequest(ctx, step.http)
			}(i)
		}
		wg.Wait()
		return samples
	}

	for i := 0; i < n; i++ {
		samples[i] = r.doRequest(ctx, step.http)
	}
	return samples
}

// doRequest performs one real HTTP request against the configured base URL.
func (r *Runner) doRequest(ctx context.Context, spec httpRequestSpec) httpSample {
	url := r.baseURL + "/" + strings.TrimLeft(spec.Path, "/")

	var body io.Reader
	if len(spec.Body) > 0 {
		body = bytes.NewReader(spec.Body)
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, spec.Method, url, body)
	if err != nil {
		return httpSample{Elapsed: time.Since(start),
			Err: fmt.Errorf("build request %s %s: %w", spec.Method, url, err)}
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return httpSample{Elapsed: time.Since(start),
			Err: fmt.Errorf("http %s %s: %w", spec.Method, url, err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		return httpSample{Status: resp.StatusCode, Headers: resp.Header,
			Elapsed: elapsed, Err: fmt.Errorf("read response body: %w", err)}
	}
	return httpSample{
		Status: resp.StatusCode, Headers: resp.Header,
		Body: string(raw), Elapsed: elapsed,
	}
}
