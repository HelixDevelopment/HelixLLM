// Package testing provides a challenge runner for loading and executing
// YAML-based challenge banks against a running HelixLLM instance.
package testing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ChallengeBank is the top-level container for a YAML challenge bank file.
type ChallengeBank struct {
	Version    string      `yaml:"version"`
	Name       string      `yaml:"name"`
	Challenges []Challenge `yaml:"challenges"`
}

// Challenge describes a single named challenge with categorised steps.
type Challenge struct {
	ID       string          `yaml:"id"`
	Name     string          `yaml:"name"`
	Category string          `yaml:"category"`
	Priority string          `yaml:"priority"`
	Steps    []ChallengeStep `yaml:"steps"`
	Tags     []string        `yaml:"tags,omitempty"`
}

// ChallengeStep is one action inside a challenge.
type ChallengeStep struct {
	Name     string `yaml:"name"`
	Action   string `yaml:"action"`
	Expected string `yaml:"expected"`
}

// ChallengeResult captures the full outcome of executing a challenge.
type ChallengeResult struct {
	ID       string
	Name     string
	Status   string // passed, failed, skipped
	Steps    []StepResult
	Error    string
	Duration time.Duration
}

// StepResult captures the outcome of a single step.
type StepResult struct {
	Name   string
	Status string
	Detail string
}

// Runner loads challenge banks and executes them against a base URL.
type Runner struct {
	banks   []ChallengeBank
	baseURL string
	client  *http.Client
}

// NewRunner creates a Runner that targets baseURL for HTTP challenges.
func NewRunner(baseURL string) *Runner {
	return &Runner{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// LoadBank reads and parses a single YAML bank file, appending it to
// the runner's list of banks.
func (r *Runner) LoadBank(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bank %s: %w", path, err)
	}

	var bank ChallengeBank
	if err := yaml.Unmarshal(data, &bank); err != nil {
		return fmt.Errorf("parse bank %s: %w", path, err)
	}

	r.banks = append(r.banks, bank)
	return nil
}

// LoadBanksDir walks dir and loads every .yaml / .yml file found.
func (r *Runner) LoadBanksDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read banks dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		if err := r.LoadBank(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// RunAll executes every challenge across all loaded banks.
func (r *Runner) RunAll(ctx context.Context) []ChallengeResult {
	var results []ChallengeResult
	for _, bank := range r.banks {
		for _, ch := range bank.Challenges {
			results = append(results, r.runChallenge(ctx, ch))
		}
	}
	return results
}

// RunByCategory executes only challenges whose Category matches the given string.
func (r *Runner) RunByCategory(ctx context.Context, category string) []ChallengeResult {
	var results []ChallengeResult
	for _, bank := range r.banks {
		for _, ch := range bank.Challenges {
			if ch.Category == category {
				results = append(results, r.runChallenge(ctx, ch))
			}
		}
	}
	return results
}

// RunByPriority executes only challenges whose Priority matches the given string.
func (r *Runner) RunByPriority(ctx context.Context, priority string) []ChallengeResult {
	var results []ChallengeResult
	for _, bank := range r.banks {
		for _, ch := range bank.Challenges {
			if ch.Priority == priority {
				results = append(results, r.runChallenge(ctx, ch))
			}
		}
	}
	return results
}

// runChallenge executes all steps in a single challenge and collects results.
func (r *Runner) runChallenge(ctx context.Context, ch Challenge) ChallengeResult {
	start := time.Now()
	result := ChallengeResult{
		ID:   ch.ID,
		Name: ch.Name,
	}

	for _, step := range ch.Steps {
		if ctx.Err() != nil {
			result.Steps = append(result.Steps, StepResult{
				Name:   step.Name,
				Status: "skipped",
				Detail: "context cancelled",
			})
			continue
		}

		sr := r.runStep(ctx, step)
		result.Steps = append(result.Steps, sr)
		if sr.Status == "failed" {
			result.Status = "failed"
			result.Error = sr.Detail
			result.Duration = time.Since(start)
			return result
		}
	}

	result.Duration = time.Since(start)
	if result.Status == "" {
		result.Status = "passed"
	}
	return result
}

// runStep executes a single step. Steps whose Action starts with "POST" or
// "GET" are HTTP steps; all others are skipped.
func (r *Runner) runStep(ctx context.Context, step ChallengeStep) StepResult {
	action := strings.TrimSpace(step.Action)

	switch {
	case strings.HasPrefix(strings.ToUpper(action), "POST "):
		return r.runHTTPStep(ctx, step, http.MethodPost, strings.TrimSpace(action[5:]))
	case strings.HasPrefix(strings.ToUpper(action), "GET "):
		return r.runHTTPStep(ctx, step, http.MethodGet, strings.TrimSpace(action[4:]))
	default:
		// Non-HTTP step: mark as skipped.
		return StepResult{
			Name:   step.Name,
			Status: "skipped",
			Detail: fmt.Sprintf("unsupported action: %s", action),
		}
	}
}

// runHTTPStep performs an HTTP request and compares the body to step.Expected.
func (r *Runner) runHTTPStep(
	ctx context.Context,
	step ChallengeStep,
	method, path string,
) StepResult {
	url := r.baseURL + "/" + strings.TrimLeft(path, "/")

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return StepResult{
			Name:   step.Name,
			Status: "failed",
			Detail: fmt.Sprintf("build request: %v", err),
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return StepResult{
			Name:   step.Name,
			Status: "failed",
			Detail: fmt.Sprintf("http %s %s: %v", method, url, err),
		}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return StepResult{
			Name:   step.Name,
			Status: "failed",
			Detail: fmt.Sprintf("read response body: %v", err),
		}
	}
	body := string(bodyBytes)

	if step.Expected != "" && !strings.Contains(body, step.Expected) {
		return StepResult{
			Name:   step.Name,
			Status: "failed",
			Detail: fmt.Sprintf("expected %q in response body, got: %s", step.Expected, body),
		}
	}

	return StepResult{
		Name:   step.Name,
		Status: "passed",
		Detail: fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}
