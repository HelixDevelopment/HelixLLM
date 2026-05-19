// runner unit tests — round-295.
//
// Exercises the parseFixture / loadFixtures helpers + the full run()
// path against a controlled fixtures directory. Race-detector clean.
//
// Unit tests are the ONLY layer where in-test mocks are permitted
// (CONST-050(A)). Here the only "fake" is an alternate
// FIXTURES_DIR pointing at t.TempDir() — the runner itself still
// exercises real i18n / orchestrator / types contracts.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(dir, name),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestParseFixture_MinimalKeys(t *testing.T) {
	body := `# comment line
locale: en
message_key: round_295_unit_test_key
message_value: "Hello {{detail}}"
expect_substr: "Hello"
template_detail_val: "unit-token"
`
	f := parseFixture(body)
	if f.locale != "en" {
		t.Errorf("locale: want en got %q", f.locale)
	}
	if f.messageKey != "round_295_unit_test_key" {
		t.Errorf("messageKey: got %q", f.messageKey)
	}
	if f.messageValue != "Hello {{detail}}" {
		t.Errorf("messageValue: got %q", f.messageValue)
	}
	if f.expectSubstr != "Hello" {
		t.Errorf("expectSubstr: got %q", f.expectSubstr)
	}
	if f.templateDetailVal != "unit-token" {
		t.Errorf("templateDetailVal: got %q", f.templateDetailVal)
	}
}

func writeAllFiveFixtures(t *testing.T, dir string) {
	t.Helper()
	writeFixture(t, dir, "en.yaml",
		`locale: en
message_key: round_295_demo_en
message_value: "Round 295 demo message: detail {{detail}}"
expect_substr: "Round 295 demo message"
template_detail_val: "english-detail-token"
`)
	writeFixture(t, dir, "de.yaml",
		`locale: de
message_key: round_295_demo_de
message_value: "Runde 295 Demo Nachricht: Detail {{detail}}"
expect_substr: "Runde 295 Demo Nachricht"
template_detail_val: "german-detail-token"
`)
	writeFixture(t, dir, "es.yaml",
		`locale: es
message_key: round_295_demo_es
message_value: "Ronda 295 mensaje: detalle {{detail}}"
expect_substr: "Ronda 295 mensaje"
template_detail_val: "spanish-detail-token"
`)
	writeFixture(t, dir, "ja.yaml",
		`locale: ja
message_key: round_295_demo_ja
message_value: "Raundo 295 demo: shosai {{detail}}"
expect_substr: "Raundo 295 demo"
template_detail_val: "japanese-detail-token"
`)
	writeFixture(t, dir, "sr.yaml",
		`locale: sr
message_key: round_295_demo_sr
message_value: "Runda 295 demo poruka: detalj {{detail}}"
expect_substr: "Runda 295 demo poruka"
template_detail_val: "serbian-detail-token"
`)
}

func TestLoadFixtures_FullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeAllFiveFixtures(t, dir)

	fixtures, err := loadFixtures(dir)
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}
	if len(fixtures) != 5 {
		t.Fatalf("expected 5 fixtures, got %d", len(fixtures))
	}

	t.Setenv("HELIXLLM_FIXTURES_DIR", dir)
	t.Setenv("HELIXLLM_MUTATE_RUNNER", "")

	var out bytes.Buffer
	code := run(&out)
	if code != 0 {
		t.Fatalf("run() exit=%d, want 0\n%s",
			code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "FAIL=0") {
		t.Errorf("expected FAIL=0 summary, got:\n%s", got)
	}
	for _, loc := range []string{"en", "de", "sr", "ja", "es"} {
		if !strings.Contains(got,
			"i18n.LoadMessages_roundtrip."+loc) {
			t.Errorf("missing per-locale invariant %q", loc)
		}
	}
	// Confirm orchestrator invariant fired.
	if !strings.Contains(got,
		"gateway.EnhanceRequest_injects_action_hint") {
		t.Errorf("missing orchestrator hint invariant: %s", got)
	}
}

func TestRun_MutationDetected(t *testing.T) {
	// Paired-mutation: with HELIXLLM_MUTATE_RUNNER=1 the runner
	// inverts invariant (4) and MUST exit non-zero — proving the
	// runner actually checks what it claims (CONST-050(A) §1.1).
	dir := t.TempDir()
	writeAllFiveFixtures(t, dir)

	t.Setenv("HELIXLLM_FIXTURES_DIR", dir)
	t.Setenv("HELIXLLM_MUTATE_RUNNER", "1")

	var out bytes.Buffer
	code := run(&out)
	if code == 0 {
		t.Fatalf("mutated runner exited 0 — mutation undetected:\n%s",
			out.String())
	}
}
