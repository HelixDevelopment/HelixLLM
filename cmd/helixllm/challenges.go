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
func runChallenges(tr i18n.TranslatorAPI, lang, baseURL, banksDir, category, priority string) int {
	runner := helixtest.NewRunner(baseURL)
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
		case "passed":
			passed++
		case "failed":
			failed++
			fmt.Printf("%s\n", tr.T(lang, i18n.KeyHelixllmCLIChallengeFail, map[string]string{
				"id":    r.ID,
				"error": r.Error,
			}))
		case "skipped":
			skipped++
		}
	}

	fmt.Printf("\n%s\n", tr.T(lang, i18n.KeyHelixllmCLIChallengeSummary, map[string]string{
		"passed":  strconv.Itoa(passed),
		"failed":  strconv.Itoa(failed),
		"skipped": strconv.Itoa(skipped),
	}))
	if failed > 0 {
		return 1
	}
	return 0
}
