package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	helixtest "github.com/HelixDevelopment/HelixLLM/internal/testing"
)

// runChallenges loads YAML banks from banksDir and executes challenges
// filtered by category or priority (or all if neither is set).
// Returns 0 on success, 1 if any challenge failed or banks could not be loaded.
//
// User-facing strings are resolved via the injected TranslatorAPI per
// CONST-046 (no hardcoded literals visible to end users).
func runChallenges(tr i18n.TranslatorAPI, lang, baseURL, banksDir, category, priority, caCert string) int {
	runner := helixtest.NewRunner(baseURL)

	// Optional trust anchor for the project's self-signed dev certificate
	// (`make certs`). Certificate verification stays ON — this PINS that cert,
	// it does not disable checking.
	if caCert != "" {
		if err := runner.TrustCACert(caCert); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
	}
	if err := runner.LoadBanksDir(banksDir); err != nil {
		msg := tr.T(lang, i18n.KeyHelixllmCLIFailedToLoadBanks, map[string]string{
			"detail": err.Error(),
		})
		fmt.Fprintf(os.Stderr, "%s\n", msg)
		return 1
	}

	var results []helixtest.ChallengeResult
	switch {
	case category != "":
		results = runner.RunByCategory(context.Background(), category)
	case priority != "":
		results = runner.RunByPriority(context.Background(), priority)
	default:
		results = runner.RunAll(context.Background())
	}

	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case helixtest.StatusPassed:
			passed++
		case helixtest.StatusFailed:
			failed++
			fmt.Printf("%s\n", tr.T(lang, i18n.KeyHelixllmCLIChallengeFail, map[string]string{
				"id":    r.ID,
				"error": r.Error,
			}))
		case helixtest.StatusSkipped:
			skipped++
		}
	}

	fmt.Printf("\n%s\n", tr.T(lang, i18n.KeyHelixllmCLIChallengeSummary, map[string]string{
		"passed":  strconv.Itoa(passed),
		"failed":  strconv.Itoa(failed),
		"skipped": strconv.Itoa(skipped),
	}))

	// Harness-integrity guard (CONST-035 / Article XI §11.9): a run that
	// executed nothing it claimed to is a FAILURE, not a pass. Verify names
	// every empty-run condition — no banks, no challenges, no executed
	// steps, or a challenge whose every step was skipped — and any of them
	// exits non-zero.
	if err := runner.Verify(results); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	if failed > 0 {
		return 1
	}
	return 0
}
