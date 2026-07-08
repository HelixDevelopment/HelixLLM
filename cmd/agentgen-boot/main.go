// Command agentgen-boot is the on-demand boot + health harness for HelixLLM's
// Lane B — a SECOND llama.cpp coder/agent instance (Serving-plan Task 2.1
// benchmark spike, docs/research/07.2026/01_local_models_serving/
// IMPLEMENTATION_PLAN_v2.md §1(c)/Task 2.1, master plan §6.1/§6.2), co-resident
// WITH the live resident coder (:18434, never touched). Like the VLM lane,
// Lane B is a WARM tier (vrambroker.ClassAgent): once booted it STAYS UP —
// boot does NOT tear the service down. Teardown is the separate `down`
// subcommand (single-owner cleanup §11.4.119).
//
// The load-bearing discipline (mirrors cmd/visiongen-boot exactly, ClassAgent
// instead of ClassVLM per broker.go's §D5 note — a distinct class ON PURPOSE):
//
//  1. BEFORE booting the container, the Lane-B VRAM footprint MUST be admitted
//     by the vrambroker (ClassAgent, WARM/co-resident) — NEVER a raw VRAM
//     grab. Admission is gated on the MEASURED free VRAM from nvidia-smi + a
//     2 GiB headroom (broker.admit, fail-closed §11.4.6).
//     * granted  -> co-resident (coder stays live) -> boot, service stays UP.
//     * ErrBudgetExceeded -> Lane B does not fit alongside the live coder
//     RIGHT NOW; the coder-pause path is operator-gated (§11.4.122/§11.4.101).
//     This harness DOES NOT pause the live coder autonomously — it reports
//     BLOCKED and exits, leaving the coder untouched.
//     * ErrBurstInUse   -> a burst (image/video) owns the card (queue).
//     * ErrBudgetUnavailable -> nvidia-smi unreadable -> refuse fail-closed.
//  2. Boot the service on its OWN port (:18435) through the containers
//     submodule compose.Orchestrator (§11.4.76), rootless podman (§11.4.161).
//  3. Health-poll /health (containers pkg/health) until the server answers —
//     then LEAVE IT RUNNING (warm tier). NO auto-teardown in `boot`.
//  4. `down` is the explicit single-owner teardown (compose down) + it never
//     touches the coder (:18434) or any sibling lane (:18436-18443).
//
// The needBytes passed to Acquire is a CONFIG-INJECTED placeholder. The
// default (9 GiB) is sized for the plan's #1 Lane-B candidate — Mistral-
// Nemo-Instruct-2407 Q4_K_M (~6.96 GiB weights measured from the bartowski
// GGUF's Content-Length) + a modest unified KV budget (16384 ctx, 4 parallel
// slots, q8_0 KV) + activation headroom. Selecting a different Lane-B
// candidate (GLM-4.7-Flash smallest quant ~9.78 GiB, DeepSeek-Coder-V2-Lite
// Q4_K_M ~9.65 GiB — both single-slot-only per the plan's headroom math)
// REQUIRES a matching AGENTGEN_NEED_BYTES override alongside
// AGENTGEN_MODEL_GGUF — the two env vars MUST be changed together, per
// §11.4.6/§11.4.108 (never assume a bigger model fits the smaller default).
//
// Subcommands:
//
//	admit-check                     broker-only VRAM admission verdict (no boot)
//	boot   <compose-file> <project> admit -> compose up -> health poll (STAYS UP)
//	down   <compose-file> <project> single-owner teardown (compose down)
//	status <compose-file> <project> compose service status
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"digital.vasic.containers/pkg/compose"
	"digital.vasic.containers/pkg/health"

	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

const (
	service    = "agentgen"
	healthPort = "18435" // OWN port — coder :18434 + VLM :18439 + siblings untouched
)

// GiB is one gibibyte in bytes.
const GiB int64 = 1024 * 1024 * 1024

// defaultNeedBytes is the Lane-B co-resident PEAK placeholder for the DEFAULT
// provisioned model (Mistral-Nemo-Instruct-2407 Q4_K_M, ~6.96 GiB weights +
// modest 16384-ctx/4-parallel q8_0 KV + activation headroom = 9 GiB). A
// different Lane-B candidate requires overriding this alongside
// AGENTGEN_MODEL_GGUF (§11.4.6) — override with AGENTGEN_NEED_BYTES.
func needBytes() int64 {
	if v := os.Getenv("AGENTGEN_NEED_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 9 * GiB
}

// defaultModelDir is the host model cache directory (~/models — the SAME
// directory the resident coder's GGUF lives in), used to seed
// AGENTGEN_MODEL_DIR when the operator has not already set it. Never
// hardcoded into compose.agent.yml itself (§CONST-045 / §11.4.28) — injected
// here, at boot invocation, into the process environment that the containers-
// submodule orchestrator passes through to compose variable interpolation.
func defaultModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "models")
}

// setDefaultEnv sets key=val in the process environment ONLY if key is
// currently unset/empty, so an operator-supplied override always wins.
func setDefaultEnv(key, val string) {
	if val == "" {
		return
	}
	if os.Getenv(key) == "" {
		os.Setenv(key, val)
	}
}

// applyDefaultAgentEnv seeds the model-selection env var compose.agent.yml
// interpolates (AGENTGEN_MODEL_DIR/AGENTGEN_MODEL_GGUF) with the DEFAULT
// provisioned Mistral-Nemo-12B model, so `boot` works out-of-the-box once the
// GGUF artefact is present (§11.4.77) while remaining fully env-overridable
// for a different Lane-B candidate.
func applyDefaultAgentEnv() {
	setDefaultEnv("AGENTGEN_MODEL_DIR", defaultModelDir())
	setDefaultEnv("AGENTGEN_MODEL_GGUF", "Mistral-Nemo-Instruct-2407-Q4_K_M.gguf")
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: agentgen-boot <admit-check|boot|down|status> [compose-file] [project]")
	}
	switch os.Args[1] {
	case "admit-check":
		cmdAdmitCheck()
	case "boot":
		cmdBoot()
	case "down":
		cmdDown()
	case "status":
		cmdStatus()
	default:
		fatal("unknown subcommand: %s", os.Args[1])
	}
}

// admit acquires a WARM (ClassAgent) lease for the Lane-B footprint, returning
// the lease or a classified reason. It NEVER pauses the coder — an
// ErrBudgetExceeded is surfaced as a BLOCKED verdict (coder-pause is operator
// gated, §11.4.122).
func admit(ctx context.Context) (*vrambroker.Lease, error) {
	broker := vrambroker.New() // real nvidia-smi-backed admission (§11.4.6 fail-closed)
	need := needBytes()
	total, used, free := broker.Budget()
	fmt.Printf("VRAM budget (nvidia-smi): total=%dMiB used=%dMiB free=%dMiB need=%dMiB headroom=%dMiB\n",
		total/(1024*1024), used/(1024*1024), free/(1024*1024),
		need/(1024*1024), vrambroker.HeadroomBytes/(1024*1024))
	lease, err := broker.Acquire(ctx, vrambroker.ClassAgent, need)
	return lease, err
}

// classifyAdmit prints a human verdict for an Acquire error and returns an exit
// code (0 = admitted, non-zero = not admitted / blocked).
func classifyAdmit(err error) int {
	switch {
	case err == nil:
		fmt.Println("ADMIT-OK: Lane-B footprint admitted co-resident (coder stays live) — warm tier")
		return 0
	case errors.Is(err, vrambroker.ErrBudgetExceeded):
		fmt.Println("BLOCKED: ErrBudgetExceeded — Lane B does not fit alongside the live coder right now.")
		fmt.Println("         The coder-pause path is required (operator-gated §11.4.122); this harness does NOT")
		fmt.Println("         pause the live coder autonomously. Coder untouched.")
		return 10
	case errors.Is(err, vrambroker.ErrBurstInUse):
		fmt.Println("BLOCKED: ErrBurstInUse — an image/video burst owns the card (single-owner §11.4.119). Queue.")
		return 11
	case errors.Is(err, vrambroker.ErrBudgetUnavailable):
		fmt.Println("BLOCKED: ErrBudgetUnavailable — nvidia-smi unreadable; refusing fail-closed (§11.4.6). No boot.")
		return 12
	case errors.Is(err, vrambroker.ErrThermalUnsafe):
		fmt.Println("BLOCKED: ErrThermalUnsafe — card outside safe thermal/power envelope (§11.4.133). No boot.")
		return 13
	default:
		fmt.Printf("BLOCKED: admission failed: %v\n", err)
		return 14
	}
}

func cmdAdmitCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lease, err := admit(ctx)
	code := classifyAdmit(err)
	if lease != nil {
		// admit-check only tests the gate — release immediately (§11.4.119).
		lease.Release()
	}
	os.Exit(code)
}

func project() compose.ComposeProject {
	if len(os.Args) < 4 {
		fatal("need <compose-file> <project>")
	}
	return compose.ComposeProject{
		Name:     os.Args[3],
		File:     os.Args[2],
		Services: []string{service},
	}
}

func cmdBoot() {
	p := project()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// (0) seed default model-selection env vars (§CONST-045/§11.4.28 — never a
	// hardcoded path inside compose.agent.yml itself) so `boot` works
	// out-of-the-box on the provisioned Mistral-Nemo-12B model.
	applyDefaultAgentEnv()

	// Fail loud (§11.4.6) if the model dir could not be resolved: an empty
	// AGENTGEN_MODEL_DIR makes compose interpolate the bind mount as
	// `:/models:ro` (malformed host path -> cryptic podman error). Surface an
	// actionable message instead — happens only if os.UserHomeDir() errored
	// AND the operator did not set AGENTGEN_MODEL_DIR.
	if os.Getenv("AGENTGEN_MODEL_DIR") == "" {
		fatal("AGENTGEN_MODEL_DIR is unset and no default could be derived " +
			"(home directory unavailable) — set AGENTGEN_MODEL_DIR to the model cache path")
	}

	// (1) admit BEFORE boot — the whole point (broker / §11.4.6 fail-closed).
	lease, err := admit(ctx)
	if code := classifyAdmit(err); code != 0 {
		os.Exit(code)
	}
	// The warm-tier lease is tied to THIS process; the container keeps holding
	// the VRAM after we exit. Release on exit so the in-process accounting slot
	// is freed — the running Lane-B container is independent of this process.
	defer lease.Release()

	// (2) boot on :18435 through the containers submodule orchestrator.
	orch, oerr := compose.NewDefaultOrchestrator(".", nil)
	if oerr != nil {
		fatal("orchestrator: %v", oerr)
	}
	if err := orch.Up(ctx, p,
		compose.WithUpDetach(true),
		compose.WithRemoveOrphans(true),
	); err != nil {
		fatal("compose up: %v", err)
	}
	fmt.Printf("UP-OK: %s agentgen via containers submodule orchestrator (:%s)\n", p.Name, healthPort)

	// (3) health poll — then LEAVE IT RUNNING (warm tier). NO auto-teardown:
	// Lane B must stay UP to serve agent requests. Teardown is `down`.
	ok := pollHealth(ctx, 5*time.Minute)
	if !ok {
		fmt.Println("BLOCKED: agentgen service never became healthy on :" + healthPort)
		os.Exit(4)
	}
	fmt.Println("BOOT-HEALTH-OK: agentgen /health answered. Lane B stays UP (warm tier, coder untouched).")
	fmt.Println("Run `agentgen-boot down <compose-file> <project>` for single-owner teardown.")
}

// pollHealth probes the server /health via the containers pkg/health HTTP
// checker (§11.4.76 primitive) until it answers or the deadline passes.
func pollHealth(ctx context.Context, budget time.Duration) bool {
	target := health.HealthTarget{
		Name:    "agentgen",
		URL:     "http://localhost:" + healthPort + "/health",
		Type:    health.HealthHTTP,
		Timeout: 5 * time.Second,
	}
	deadline := time.Now().Add(budget)
	n := 0
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}
		n++
		res := health.CheckHTTP(ctx, target)
		if res.Healthy {
			fmt.Printf("HEALTH-OK: agentgen /health after %d polls (status=%s)\n", n, res.Details["status_code"])
			return true
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Printf("HEALTH-TIMEOUT: agentgen /health did not answer within %s (%d polls)\n", budget, n)
	return false
}

func cmdDown() {
	p := project()
	orch, err := compose.NewDefaultOrchestrator(".", nil)
	if err != nil {
		fatal("orchestrator: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := orch.Down(ctx, p,
		compose.WithDownRemoveVolumes(false), // keep the GGUF model cache
		compose.WithDownRemoveOrphans(true),
	); err != nil {
		fatal("compose down: %v", err)
	}
	fmt.Printf("DOWN-OK: %s agentgen (single-owner cleanup, coder untouched)\n", p.Name)
}

func cmdStatus() {
	p := project()
	orch, err := compose.NewDefaultOrchestrator(".", nil)
	if err != nil {
		fatal("orchestrator: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sts, err := orch.Status(ctx, p)
	if err != nil {
		fatal("status: %v", err)
	}
	if len(sts) == 0 {
		fmt.Println("(no services reported)")
		return
	}
	for _, s := range sts {
		fmt.Printf("%s state=%s health=%s ports=%v exit=%d\n",
			s.Name, s.State, s.Health, s.Ports, s.ExitCode)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(2)
}
