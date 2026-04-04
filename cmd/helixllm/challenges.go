package main

import (
	"context"
	"fmt"
	"os"

	helixtest "github.com/HelixDevelopment/HelixLLM/internal/testing"
)

// runChallenges loads YAML banks from banksDir and executes challenges
// filtered by category or priority (or all if neither is set).
// Returns 0 on success, 1 if any challenge failed or banks could not be loaded.
func runChallenges(baseURL, banksDir, category, priority string) int {
	runner := helixtest.NewRunner(baseURL)
	if err := runner.LoadBanksDir(banksDir); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load banks: %v\n", err)
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
			fmt.Printf("FAIL: %s - %s\n", r.ID, r.Error)
		case "skipped":
			skipped++
		}
	}

	fmt.Printf("\n%d passed, %d failed, %d skipped\n", passed, failed, skipped)
	if failed > 0 {
		return 1
	}
	return 0
}
