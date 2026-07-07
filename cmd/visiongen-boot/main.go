// Command visiongen-boot is the on-demand boot + health harness for the
// HelixLLM VISION (VLM) service — a Qwen2.5-VL multimodal server (GGUF +
// mmproj / libmtmd) on the host RTX 5090, co-resident WITH the live resident
// coder (:18434, never touched). Unlike the image/video BURST lanes, the VLM
// is a WARM tier (vrambroker.ClassVLM): once booted it STAYS UP to answer
// vision requests — boot does NOT tear the service down. Teardown is the
// separate `down` subcommand (single-owner cleanup §11.4.119).
//
// The load-bearing discipline (design scratchpad/design_gpu_generative*.md +
// docs/qa/phase3_vision_20260707):
//
//  1. BEFORE booting the container, the VLM VRAM footprint MUST be admitted by
//     the vrambroker (ClassVLM, WARM/co-resident) — NEVER a raw VRAM grab.
//     Admission is gated on the MEASURED free VRAM from nvidia-smi + a 2 GiB
//     headroom (broker.admit, fail-closed §11.4.6).
//     * granted  -> co-resident (coder stays live) -> boot, service stays UP.
//     * ErrBudgetExceeded -> the VLM does not fit alongside the live coder
//     RIGHT NOW; the coder-pause path is operator-gated (§11.4.122/§11.4.101).
//     This harness DOES NOT pause the live coder autonomously — it reports
//     BLOCKED and exits, leaving the coder untouched.
//     * ErrBurstInUse   -> a burst (image/video) owns the card (queue).
//     * ErrBudgetUnavailable -> nvidia-smi unreadable -> refuse fail-closed.
//  2. Boot the service on its OWN port (:18439) through the containers
//     submodule compose.Orchestrator (§11.4.76), rootless podman (§11.4.161).
//  3. Health-poll /health (containers pkg/health) until the server answers —
//     then LEAVE IT RUNNING (warm tier). NO auto-teardown in `boot`.
//  4. `down` is the explicit single-owner teardown (compose down) + it never
//     touches the coder (:18434) or any sibling lane (:18435-18443).
//
// The needBytes passed to Acquire is a CONFIG-INJECTED placeholder. The
// default (5 GiB) is sized for the DEFAULT provisioned model — the 3B Q4_K_M +
// Q8_0 mmproj measured ~4.1 GiB actual peak in the phase3 proof, +~1 GiB
// margin. Booting the larger 7B variant (once its GGUF+mmproj are downloaded,
// §11.4.122 nothing removed — see VISIONGEN_MODEL_GGUF/VISIONGEN_MMPROJ below)
// REQUIRES a matching VISIONGEN_NEED_BYTES override (~9 GiB: 7B Q4_K_M + f16
// mmproj + KV budgets peak) — the two env vars MUST be changed together, per
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
	service    = "visiongen"
	healthPort = "18439" // OWN port — coder :18434 + siblings :18435-18443 untouched
)

// GiB is one gibibyte in bytes.
const GiB int64 = 1024 * 1024 * 1024

// defaultNeedBytes is the VLM co-resident PEAK placeholder for the DEFAULT
// provisioned model (3B Q4_K_M + Q8_0 mmproj, phase3-measured ~4.1 GiB actual
// + ~1 GiB margin = 5 GiB). Booting the 7B variant requires overriding this
// alongside VISIONGEN_MODEL_GGUF/VISIONGEN_MMPROJ (§11.4.6) — override with
// VISIONGEN_NEED_BYTES.
func needBytes() int64 {
	if v := os.Getenv("VISIONGEN_NEED_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 5 * GiB
}

// defaultModelDir is the phase3-harness VLM model cache directory
// (~/models/vlm_cache), used to seed VISIONGEN_MODEL_DIR when the operator
// has not already set it. Never hardcoded into compose.vision.yml itself
// (§CONST-045 / §11.4.28) — injected here, at boot invocation, into the
// process environment that the containers-submodule orchestrator passes
// through to compose variable interpolation.
func defaultModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "models", "vlm_cache")
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

// applyDefaultVisionEnv seeds the three model-selection env vars compose.
// vision.yml interpolates (VISIONGEN_MODEL_DIR/VISIONGEN_MODEL_GGUF/
// VISIONGEN_MMPROJ) with the DEFAULT provisioned 3B model, so `boot` works
// out-of-the-box on a fresh clone (§11.4.77) while remaining fully
// env-overridable for the 7B variant once downloaded (§11.4.122).
func applyDefaultVisionEnv() {
	setDefaultEnv("VISIONGEN_MODEL_DIR", defaultModelDir())
	setDefaultEnv("VISIONGEN_MODEL_GGUF", "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf")
	setDefaultEnv("VISIONGEN_MMPROJ", "mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf")
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: visiongen-boot <admit-check|boot|down|status> [compose-file] [project]")
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

// admit acquires a WARM (ClassVLM) lease for the VLM footprint, returning the
// lease or a classified reason. It NEVER pauses the coder — an
// ErrBudgetExceeded is surfaced as a BLOCKED verdict (coder-pause is operator
// gated, §11.4.122).
func admit(ctx context.Context) (*vrambroker.Lease, error) {
	broker := vrambroker.New() // real nvidia-smi-backed admission (§11.4.6 fail-closed)
	need := needBytes()
	total, used, free := broker.Budget()
	fmt.Printf("VRAM budget (nvidia-smi): total=%dMiB used=%dMiB free=%dMiB need=%dMiB headroom=%dMiB\n",
		total/(1024*1024), used/(1024*1024), free/(1024*1024),
		need/(1024*1024), vrambroker.HeadroomBytes/(1024*1024))
	lease, err := broker.Acquire(ctx, vrambroker.ClassVLM, need)
	return lease, err
}

// classifyAdmit prints a human verdict for an Acquire error and returns an exit
// code (0 = admitted, non-zero = not admitted / blocked).
func classifyAdmit(err error) int {
	switch {
	case err == nil:
		fmt.Println("ADMIT-OK: VLM footprint admitted co-resident (coder stays live) — warm tier")
		return 0
	case errors.Is(err, vrambroker.ErrBudgetExceeded):
		fmt.Println("BLOCKED: ErrBudgetExceeded — the VLM does not fit alongside the live coder right now.")
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
		Services: []string{"visiongen"},
	}
}

func cmdBoot() {
	p := project()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// (0) seed default model-selection env vars (§CONST-045/§11.4.28 — never a
	// hardcoded path inside compose.vision.yml itself) so `boot` works
	// out-of-the-box on the provisioned 3B model.
	applyDefaultVisionEnv()

	// Fail loud (§11.4.6) if the model dir could not be resolved: an empty
	// VISIONGEN_MODEL_DIR makes compose interpolate the bind mount as
	// `:/models:ro` (malformed host path → cryptic podman error). Surface an
	// actionable message instead — happens only if os.UserHomeDir() errored
	// AND the operator did not set VISIONGEN_MODEL_DIR.
	if os.Getenv("VISIONGEN_MODEL_DIR") == "" {
		fatal("VISIONGEN_MODEL_DIR is unset and no default could be derived " +
			"(home directory unavailable) — set VISIONGEN_MODEL_DIR to the VLM model cache path")
	}

	// (1) admit BEFORE boot — the whole point (broker / §11.4.6 fail-closed).
	lease, err := admit(ctx)
	if code := classifyAdmit(err); code != 0 {
		os.Exit(code)
	}
	// The warm-tier lease is tied to THIS process; the container keeps holding
	// the VRAM after we exit. Release on exit so the in-process accounting slot
	// is freed — the running VLM container is independent of this process.
	defer lease.Release()

	// (2) boot on :18439 through the containers submodule orchestrator.
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
	fmt.Printf("UP-OK: %s visiongen via containers submodule orchestrator (:%s)\n", p.Name, healthPort)

	// (3) health poll — then LEAVE IT RUNNING (warm tier). NO auto-teardown:
	// the VLM must stay UP to serve vision requests. Teardown is `down`.
	ok := pollHealth(ctx, 5*time.Minute)
	if !ok {
		fmt.Println("BLOCKED: visiongen service never became healthy on :" + healthPort)
		os.Exit(4)
	}
	fmt.Println("BOOT-HEALTH-OK: visiongen /health answered. VLM stays UP (warm tier, coder untouched).")
	fmt.Println("Run `visiongen-boot down <compose-file> <project>` for single-owner teardown.")
}

// pollHealth probes the server /health via the containers pkg/health HTTP
// checker (§11.4.76 primitive) until it answers or the deadline passes.
func pollHealth(ctx context.Context, budget time.Duration) bool {
	target := health.HealthTarget{
		Name:    "visiongen",
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
			fmt.Printf("HEALTH-OK: visiongen /health after %d polls (status=%s)\n", n, res.Details["status_code"])
			return true
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Printf("HEALTH-TIMEOUT: visiongen /health did not answer within %s (%d polls)\n", budget, n)
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
	fmt.Printf("DOWN-OK: %s visiongen (single-owner cleanup, coder untouched)\n", p.Name)
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
