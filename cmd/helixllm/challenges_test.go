package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// fakeTranslator satisfies i18n.TranslatorAPI and returns a sentinel
// containing the key + a JSON-ish dump of vars, so tests can assert
// the call site uses tr.T(...) instead of a hardcoded literal.
//
// CONST-050(A): mocks PERMITTED in unit tests only — this file ends
// in _test.go and lives outside any non-unit build-tag scope.
type fakeTranslator struct {
	calls []fakeCall
}

type fakeCall struct {
	lang string
	key  string
	vars map[string]string
}

func (f *fakeTranslator) T(lang, key string, vars ...map[string]string) string {
	var v map[string]string
	if len(vars) > 0 {
		v = vars[0]
	}
	f.calls = append(f.calls, fakeCall{lang: lang, key: key, vars: v})
	if v == nil {
		return fmt.Sprintf("<TRANSLATED:%s>", key)
	}
	return fmt.Sprintf("<TRANSLATED:%s:%v>", key, v)
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// what was written. Each subtest runs sequentially → no race risk.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// CONST-046 round-95: when runChallenges is invoked with a banksDir
// that does not resolve, it MUST emit the translated message — NOT the
// raw hardcoded English literal. The fake translator confirms the call
// site reached i18n.TranslatorAPI.T(...) with the expected key.
func TestRunChallenges_FailedToLoadBanks_UsesTranslator(t *testing.T) {
	ft := &fakeTranslator{}
	stderr := captureStderr(t, func() {
		rc := runChallenges(ft, "en", "https://localhost:1", "/nonexistent/banks/dir/xyz123", "", "", "")
		if rc != 1 {
			t.Fatalf("runChallenges return code = %d, want 1 (bank-load failure)", rc)
		}
	})

	// Anti-bluff: the raw English literal must NOT appear verbatim — it
	// must reach the user through the translator. If it appears bare,
	// the call site was not migrated.
	if strings.Contains(stderr, `"failed to load banks:`) {
		t.Errorf("stderr contains bare-literal sentinel; migration not effective: %q", stderr)
	}

	if !strings.Contains(stderr, "<TRANSLATED:") {
		t.Errorf("stderr does NOT contain translator sentinel — call site did not invoke i18n.T(): %q", stderr)
	}
	if !strings.Contains(stderr, i18n.KeyHelixllmCLIFailedToLoadBanks) {
		t.Errorf("stderr missing translation key %q; got %q",
			i18n.KeyHelixllmCLIFailedToLoadBanks, stderr)
	}

	// At least one Translator.T(...) invocation MUST have happened with
	// the expected key + lang.
	matched := false
	for _, c := range ft.calls {
		if c.key == i18n.KeyHelixllmCLIFailedToLoadBanks && c.lang == "en" {
			if _, ok := c.vars["detail"]; !ok {
				t.Errorf("Translator.T() called without 'detail' var; got vars=%v", c.vars)
			}
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("no Translator.T(en, %q, {detail:...}) call recorded; got %d calls: %+v",
			i18n.KeyHelixllmCLIFailedToLoadBanks, len(ft.calls), ft.calls)
	}
}

// CONST-046 round-95: resolveCLILang must follow POSIX env precedence
// and fall back to "en" — never return an empty string (the upstream
// Translator's fallback chain assumes non-empty default-language tag).
func TestResolveCLILang_FallbackChain(t *testing.T) {
	saved := map[string]string{
		"LC_ALL": os.Getenv("LC_ALL"),
		"LANG":   os.Getenv("LANG"),
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})

	cases := []struct {
		name string
		lc   string
		lang string
		want string
	}{
		{"both_unset_defaults_to_en", "", "", "en"},
		{"lang_only_de", "", "de_DE.UTF-8", "de"},
		{"lc_all_overrides_lang", "fr_FR.UTF-8", "de_DE.UTF-8", "fr"},
		{"single_char_lang_falls_back_to_en", "", "x", "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Unsetenv("LC_ALL")
			_ = os.Unsetenv("LANG")
			if tc.lc != "" {
				_ = os.Setenv("LC_ALL", tc.lc)
			}
			if tc.lang != "" {
				_ = os.Setenv("LANG", tc.lang)
			}
			got := resolveCLILang()
			if got != tc.want {
				t.Errorf("resolveCLILang() with LC_ALL=%q LANG=%q = %q, want %q",
					tc.lc, tc.lang, got, tc.want)
			}
		})
	}
}
