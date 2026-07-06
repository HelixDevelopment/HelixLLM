//go:build integration

// Package vrambroker integration test — REAL nvidia-smi + REAL live coder.
// CONST-050: no fakes here; drives the actual card and the actual coder HTTP
// endpoint. Run: go test -tags=integration -v ./internal/vrambroker/...
//
// Runtime signature (§11.4.108): with the coder resident on the 32 GB card,
//  1. Budget() returns the real nvidia-smi numbers (used>0, total≈32 GiB).
//  2. Acquire("coder") grants while a REAL /v1/chat/completions keeps answering.
//  3. An over-budget burst is REFUSED with ErrBudgetExceeded (no OOM); coder
//     still answers afterwards.
//  4. A second concurrent burst is refused with ErrBurstInUse.
package vrambroker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"testing"
	"time"

	vb "github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
	"github.com/stretchr/testify/require"
)

const coderBase = "http://localhost:18434"

func requireEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("SKIP-OK: nvidia-smi not on PATH — GPU-hardware test, hardware_not_present")
	}
	if !coderReachable() {
		t.Skip("SKIP-OK: coder not reachable at " + coderBase + " — live-service test, feature_disabled_by_config")
	}
}

func coderReachable() bool {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(coderBase + "/v1/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// coderModelID returns the first model id the live coder advertises.
func coderModelID(t *testing.T) string {
	t.Helper()
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(coderBase + "/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.Data, "coder advertises at least one model")
	return body.Data[0].ID
}

// coderComplete makes a REAL, TINY chat completion against the live coder and
// returns the assistant text. Small max_tokens keeps it non-perturbing — it does
// NOT load any new model; the coder is already resident.
func coderComplete(t *testing.T, model string) string {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": "Reply with only the number 4."}},
		"max_tokens":  16,
		"temperature": 0,
		"stream":      false,
	})
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Post(coderBase+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	require.NoError(t, err, "real completion request to live coder")
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "coder HTTP status; body=%s", string(raw))
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	require.NoError(t, json.Unmarshal(raw, &out), "decode completion; body=%s", string(raw))
	require.NotEmpty(t, out.Choices, "coder returned at least one choice; body=%s", string(raw))
	return out.Choices[0].Message.Content
}

func TestIntegration_Budget_RealNvidiaSMI(t *testing.T) {
	requireEnv(t)
	b := vb.New()
	total, used, free := b.Budget()

	t.Logf("Budget() live: total=%d MiB used=%d MiB free=%d MiB",
		total/vb.MiB, used/vb.MiB, free/vb.MiB)

	require.Positive(t, used, "coder is resident, so used VRAM MUST be > 0")
	require.GreaterOrEqual(t, total/vb.MiB, int64(31000), "RTX 5090 total ≈ 32 GB")
	require.LessOrEqual(t, total/vb.MiB, int64(34000))
	// free == total - used within a small slack (nvidia-smi reserved rounding).
	require.InDelta(t, float64(total-used), float64(free), float64(512*vb.MiB),
		"free should equal total-used within slack")
}

func TestIntegration_Coder_StaysLive_WhileLeasesMove(t *testing.T) {
	requireEnv(t)
	b := vb.New()
	model := coderModelID(t)

	// Coder answers BEFORE any broker activity.
	before := coderComplete(t, model)
	require.NotEmpty(t, before)
	t.Logf("coder completion (before): %q", before)

	// Acquire the resident coder lease (always granted, pinned).
	coderLease, err := b.Acquire(context.Background(), vb.ClassCoder, 19*1024*vb.MiB)
	require.NoError(t, err)
	require.NotNil(t, coderLease)

	// While the coder lease is held, an OVER-BUDGET burst is REFUSED (no OOM).
	_, _, free := b.Budget()
	over := free + 40*1024*vb.MiB // clearly exceeds free VRAM
	burst, err := b.Acquire(context.Background(), vb.ClassImage, over)
	require.Nil(t, burst, "over-budget burst MUST NOT be granted")
	require.ErrorIs(t, err, vb.ErrBudgetExceeded, "over-budget burst refused fail-closed")
	t.Logf("over-budget burst refused as expected: %v", err)

	// Coder is STILL live after the refusal — the broker never touched it.
	after := coderComplete(t, model)
	require.NotEmpty(t, after)
	t.Logf("coder completion (after refusal): %q", after)

	coderLease.Release()
}

func TestIntegration_Burst_SingleOwner_RealBudget(t *testing.T) {
	requireEnv(t)
	b := vb.New()
	ctx := context.Background()

	// A tiny burst that fits within the real free budget (bookkeeping only — the
	// core broker reserves admission, it does not allocate VRAM itself).
	first, err := b.Acquire(ctx, vb.ClassImage, 64*vb.MiB)
	require.NoError(t, err, "small burst fits real free budget")
	require.NotNil(t, first)

	// Second concurrent burst -> refused single-owner (§11.4.119).
	second, err := b.Acquire(ctx, vb.ClassVideo, 64*vb.MiB)
	require.Nil(t, second)
	require.ErrorIs(t, err, vb.ErrBurstInUse)

	// Release, then a new burst is admitted.
	first.Release()
	third, err := b.Acquire(ctx, vb.ClassImage, 64*vb.MiB)
	require.NoError(t, err)
	require.NotNil(t, third)
	third.Release()
}

func TestIntegration_OverBudget_NeverOOM_CoderSurvives(t *testing.T) {
	requireEnv(t)
	b := vb.New()
	model := coderModelID(t)

	// Hammer the admission gate with many over-budget requests; every one MUST be
	// refused, none may allocate, and the coder MUST survive.
	_, _, free := b.Budget()
	for i := 0; i < 25; i++ {
		l, err := b.Acquire(context.Background(), vb.ClassVideo, free+int64(i+1)*10*1024*vb.MiB)
		require.Nil(t, l)
		require.True(t, errors.Is(err, vb.ErrBudgetExceeded))
	}
	require.NotEmpty(t, coderComplete(t, model), "coder still answers after 25 over-budget refusals")
}
