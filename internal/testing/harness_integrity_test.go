package testing_test

// Standing regression guard for the harness-integrity defect (CONST-035 /
// Article XI §11.9): the challenge harness used to report SUCCESS while
// executing NOTHING.
//
// Three independent mechanisms produced that outcome, each sufficient alone:
//
//	1. runStep dispatched on step.Action; every bank writes method:/path:/
//	   assertions: and never action:, so every step fell to the default branch,
//	   was marked "skipped", and runChallenge then set the challenge "passed"
//	   because nothing had FAILED. Measured: "3 passed" against the RFC 5737
//	   discard address https://192.0.2.1:9/.
//	2. 19 of 28 bank files declare a top-level `steps:` key the Bank struct had
//	   no field for, so ~115 declared entries loaded as ZERO, silently, exit 0.
//	3. LoadBanksDir did not recurse, so the Makefile's own
//	   --banks-dir=challenges/banks/ (subdirectories only) loaded zero banks.
//
// Measured pre-fix: `make test-challenges` printed "0 passed, 0 failed,
// 0 skipped" and exited 0.
//
// POLARITY SWITCH (§11.4.115). One source, two roles:
//
//	RED_MODE=1  reproduce-and-assert-the-defect-is-PRESENT. Meaningful only on
//	            a pre-fix artifact; on the fixed artifact these FAIL, and that
//	            failure is the proof the guard has teeth.
//	RED_MODE=0  (default) standing GREEN guard: assert the defect is ABSENT.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	helixtest "github.com/HelixDevelopment/HelixLLM/internal/testing"
)

// redMode reports whether the polarity switch is set to reproduce the
// historical defect rather than guard against it.
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// banksRoot is the shipped bank corpus, relative to this package.
const banksRoot = "../../challenges/banks"

// closedTarget returns a base URL that is guaranteed to refuse connections
// immediately (a listener opened then closed), so "the harness reached the
// target" can never be confused with "the harness executed nothing".
func closedTarget(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	srv.Close()
	return url
}

func writeBank(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write bank: %v", err)
	}
}

// TestHarnessIntegrity_UnreachableTargetCannotPass is the core guard: when the
// target is unreachable, challenges MUST fail and steps MUST have been
// attempted. Under the historical defect the same corpus reported "3 passed"
// with zero steps executed.
func TestHarnessIntegrity_UnreachableTargetCannotPass(t *testing.T) {
	r := helixtest.NewRunner(closedTarget(t))
	if err := r.LoadBanksDir(filepath.Join(banksRoot, "safety")); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}
	results := r.RunAll(context.Background())
	if len(results) == 0 {
		t.Fatal("no challenges were produced from challenges/banks/safety")
	}

	passed, executed := 0, 0
	for _, res := range results {
		if res.Status == helixtest.StatusPassed {
			passed++
		}
		executed += res.Executed()
	}

	if redMode() {
		if passed == 0 || executed != 0 {
			t.Fatalf("RED_MODE=1: defect NOT reproduced (passed=%d executed=%d) — "+
				"the harness no longer passes while executing nothing", passed, executed)
		}
		t.Logf("RED_MODE=1: defect reproduced — %d passed, %d steps executed", passed, executed)
		return
	}

	if passed != 0 {
		t.Errorf("%d challenge(s) reported passed against an unreachable target", passed)
	}
	if executed == 0 {
		t.Error("zero steps executed: the harness did not attempt any request")
	}
}

// TestHarnessIntegrity_ZeroBanksIsLoadError proves an empty bank directory is
// refused rather than silently producing a green empty run.
func TestHarnessIntegrity_ZeroBanksIsLoadError(t *testing.T) {
	r := helixtest.NewRunner(closedTarget(t))
	err := r.LoadBanksDir(t.TempDir())

	if redMode() {
		if err != nil {
			t.Fatalf("RED_MODE=1: defect NOT reproduced — empty dir now errors: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("empty banks dir loaded without error: an empty run would report success")
	}
	if !strings.Contains(err.Error(), "no challenge bank files") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

// TestHarnessIntegrity_AllSkippedChallengeFailsVerify proves a challenge whose
// every step was skipped is surfaced by Verify (which the CLI turns into a
// non-zero exit) rather than counted as a pass.
func TestHarnessIntegrity_AllSkippedChallengeFailsVerify(t *testing.T) {
	dir := t.TempDir()
	writeBank(t, dir, "skipped.yaml", `
name: all-skipped
steps:
  - name: fault-injection-only
    type: chaos
    params:
      action: kill_process
      target: gateway
    assertions:
      - type: min_success_rate
        value: 0.9
    on_failure: |
      Chaos step has no executor.
`)

	r := helixtest.NewRunner(closedTarget(t))
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}
	results := r.RunAll(context.Background())
	if len(results) != 1 {
		t.Fatalf("want 1 challenge, got %d", len(results))
	}

	verifyErr := r.Verify(results)

	if redMode() {
		if results[0].Status != helixtest.StatusPassed || verifyErr != nil {
			t.Fatalf("RED_MODE=1: defect NOT reproduced (status=%s verify=%v)",
				results[0].Status, verifyErr)
		}
		return
	}

	if results[0].Status == helixtest.StatusPassed {
		t.Error("challenge with zero executed steps reported passed")
	}
	if verifyErr == nil {
		t.Fatal("Verify accepted a run in which every step was skipped")
	}
	if !strings.Contains(verifyErr.Error(), "executed 0 steps") {
		t.Errorf("Verify error does not name what did not run: %v", verifyErr)
	}
}

// TestHarnessIntegrity_TopLevelStepsBankLoadsItsEntries proves the ~115
// entries declared with a top-level `steps:` key actually load.
func TestHarnessIntegrity_TopLevelStepsBankLoadsItsEntries(t *testing.T) {
	r := helixtest.NewRunner(closedTarget(t))
	if err := r.LoadBanksDir(filepath.Join(banksRoot, "api")); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}
	n := len(r.RunAll(context.Background()))

	if redMode() {
		if n != 0 {
			t.Fatalf("RED_MODE=1: defect NOT reproduced — %d entries now load", n)
		}
		return
	}
	if n == 0 {
		t.Fatal("challenges/banks/api declares entries but produced 0 challenges")
	}
	t.Logf("challenges/banks/api produced %d challenges", n)
}

// TestHarnessIntegrity_BankDirLoadIsRecursive proves the Makefile's own
// --banks-dir=challenges/banks/ (subdirectories only) loads the whole corpus.
func TestHarnessIntegrity_BankDirLoadIsRecursive(t *testing.T) {
	r := helixtest.NewRunner(closedTarget(t))
	if err := r.LoadBanksDir(banksRoot); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}
	banks := len(r.Banks())

	if redMode() {
		if banks != 0 {
			t.Fatalf("RED_MODE=1: defect NOT reproduced — %d banks now load", banks)
		}
		return
	}
	if banks == 0 {
		t.Fatal("challenges/banks contains bank files but 0 banks loaded")
	}
	t.Logf("challenges/banks loaded %d bank files recursively", banks)
}

// TestHarnessIntegrity_EveryShippedBankLoads proves the whole shipped corpus
// parses under STRICT decoding — no file silently contributes zero entries.
func TestHarnessIntegrity_EveryShippedBankLoads(t *testing.T) {
	r := helixtest.NewRunner(closedTarget(t))
	if err := r.LoadBanksDir(banksRoot); err != nil {
		t.Fatalf("a shipped bank failed strict loading: %v", err)
	}
	for _, b := range r.Banks() {
		if len(b.Steps) == 0 && len(b.Challenges) == 0 {
			t.Errorf("bank %s loaded with zero entries", b.SourcePath)
		}
	}
}

// TestHarnessIntegrity_UnknownKeyIsLoadError proves a typo'd key refuses to
// load instead of contributing zero entries and exiting 0.
func TestHarnessIntegrity_UnknownKeyIsLoadError(t *testing.T) {
	cases := map[string]string{
		"unknown top-level key": `
name: typo-bank
stepz:
  - name: s
    type: http_request
    params: {method: GET, path: /health}
`,
		"unknown params key": `
name: typo-params
steps:
  - name: s
    type: http_request
    params: {method: GET, path: /health, bdoy: "{}"}
`,
		"unknown assertion type": `
name: typo-assertion
steps:
  - name: s
    type: http_request
    params: {method: GET, path: /health}
    assertions:
      - type: definitely_not_a_real_assertion
        field: body
`,
		"step with no executable action": `
name: no-action
steps:
  - name: s
    assertions:
      - type: http_status_ok
`,
		"unsupported http method": `
name: bad-method
challenges:
  - name: c
    steps:
      - name: s
        action: EXEC echo hello
        expected: ""
`,
	}

	for name, bank := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeBank(t, dir, "bank.yaml", bank)
			r := helixtest.NewRunner(closedTarget(t))
			err := r.LoadBanksDir(dir)

			if redMode() {
				if err != nil {
					t.Fatalf("RED_MODE=1: defect NOT reproduced — now rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("malformed bank loaded without error")
			}
			t.Logf("refused as required: %v", err)
		})
	}
}

// TestHarnessIntegrity_AssertionsActuallyEvaluate proves the evaluator both
// passes a satisfied assertion and fails an unsatisfied one against a real
// HTTP response — an evaluator that never fails is the same class of bluff.
func TestHarnessIntegrity_AssertionsActuallyEvaluate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m1","owned_by":"llamacpp"}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeBank(t, dir, "good.yaml", `
name: satisfied
steps:
  - name: list-models
    type: http_request
    params: {method: GET, path: /v1/models}
    assertions:
      - type: http_status_ok
      - type: contains
        field: body.object
        expected: "list"
      - type: not_empty
        field: body.data
      - type: all_match
        field: body.data[*].owned_by
        value: llamacpp
      - type: header_present
        name: Content-Type
`)
	writeBank(t, dir, "bad.yaml", `
name: unsatisfied
steps:
  - name: wrong-owner
    type: http_request
    params: {method: GET, path: /v1/models}
    assertions:
      - type: all_match
        field: body.data[*].owned_by
        value: openai
`)

	r := helixtest.NewRunner(srv.URL)
	if err := r.LoadBanksDir(dir); err != nil {
		t.Fatalf("LoadBanksDir: %v", err)
	}
	results := r.RunAll(context.Background())
	if len(results) != 2 {
		t.Fatalf("want 2 challenges, got %d", len(results))
	}

	byName := map[string]helixtest.ChallengeResult{}
	for _, res := range results {
		byName[res.Name] = res
	}
	if got := byName["list-models"]; got.Status != helixtest.StatusPassed {
		t.Errorf("satisfied assertions did not pass: status=%s error=%s", got.Status, got.Error)
	}
	if got := byName["wrong-owner"]; got.Status != helixtest.StatusFailed {
		t.Errorf("unsatisfied assertion did not fail: status=%s", got.Status)
	}
}
