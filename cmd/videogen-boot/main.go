// Command videogen-boot is the on-demand boot + health + single-owner-teardown
// harness for the HelixLLM Phase-4 GPU video-generation service (WAN 2.2 / LTX
// on the host RTX 5090). It is the ready-to-run scaffold that makes the eventual
// runtime proof a fast-follow once the operator authorizes a coder-pause window
// (§11.4.122); it does NOT itself run a GPU generation.
//
// The load-bearing discipline (design scratchpad/design_gpu_generative*.md):
//
//  1. BEFORE booting the container, the video footprint MUST be admitted by the
//     vrambroker (ClassVideo, BURST, single-owner §11.4.119) — NEVER a raw VRAM
//     grab. Admission is gated on the MEASURED free VRAM from nvidia-smi + a
//     2 GiB headroom (broker.admit, fail-closed §11.4.6). Image + video are BOTH
//     ClassX burst so the broker guarantees only ONE generative job holds VRAM.
//     * granted  -> co-resident fast path (coder stays live) -> boot.
//     * ErrBudgetExceeded -> the selected no-pause config (default WAN-5B-FP8
//     @480p ~8-10 GB peak) does not fit alongside the live coder RIGHT NOW; the
//     coder-pause path is required. This harness DOES NOT pause the live coder
//     autonomously (pausing a shipped capability is operator-gated, §11.4.122/
//     §11.4.101) — it reports BLOCKED and exits, leaving the coder untouched.
//     * ErrBurstInUse   -> another image/video burst owns the card (queue).
//     * ErrBudgetUnavailable -> nvidia-smi unreadable -> refuse fail-closed.
//  2. Boot the service on its OWN port (:18443) through the containers submodule
//     compose.Orchestrator (§11.4.76), rootless podman (§11.4.161). Distinct
//     from the coder (:18434) and the image-gen sibling (:18442).
//  3. Health-poll /health (containers pkg/health) until the shim answers.
//  4. Single-owner teardown (compose down) + Release the burst lease — the
//     coder (:18434) is never touched.
//
// The needBytes passed to Acquire is a CONFIG-INJECTED placeholder derived from
// the §11.4.150 research (WAN 2.2 TI2V-5B FP8 @480p ~8-10 GB peak, native
// offload) — it MUST be replaced by the MEASURED first-run peak on THIS card
// (torch.cuda.max_memory_allocated / nvidia-smi delta) before it is treated as
// fact (§11.4.6/§11.4.108). WAN-5B-FP8@480p sits close to the ~10.4 GiB
// co-resident ceiling, so the calibration is load-bearing. See README
// "PENDING runtime-proof".
//
// Subcommands:
//
//	admit-check                     broker-only VRAM admission verdict (no boot)
//	boot   <compose-file> <project> admit -> compose up -> health poll (no gen)
//	down   <compose-file> <project> single-owner teardown (compose down)
//	status <compose-file> <project> compose service status
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"digital.vasic.containers/pkg/compose"
	"digital.vasic.containers/pkg/health"

	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

const (
	service    = "videogen"
	healthPort = "18443" // OWN port — coder :18434 + image-gen :18442 untouched
)

// GiB is one gibibyte in bytes.
const GiB int64 = 1024 * 1024 * 1024

// needBytes is the co-resident PEAK placeholder for the default no-pause path
// (WAN 2.2 TI2V-5B FP8 @480p ~8-10 GB peak, rounded up to 10 GiB margin — it
// sits close to the ~10.4 GiB co-resident ceiling). PENDING first-run
// calibration on THIS card (§11.4.6) — override with VIDEOGEN_NEED_BYTES.
func needBytes() int64 {
	if v := os.Getenv("VIDEOGEN_NEED_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 10 * GiB
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: videogen-boot <admit-check|boot|down|status> [compose-file] [project]")
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

// admit acquires a burst (ClassVideo) lease for the video footprint, returning
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
	lease, err := broker.Acquire(ctx, vrambroker.ClassVideo, need)
	return lease, err
}

// classifyAdmit prints a human verdict for an Acquire error and returns an exit
// code (0 = admitted, non-zero = not admitted / blocked).
func classifyAdmit(err error) int {
	switch {
	case err == nil:
		fmt.Println("ADMIT-OK: video footprint admitted co-resident (coder stays live) — fast path")
		return 0
	case errors.Is(err, vrambroker.ErrBudgetExceeded):
		fmt.Println("BLOCKED: ErrBudgetExceeded — the selected no-pause config does not fit alongside the live coder right now.")
		fmt.Println("         The coder-pause path is required (operator-gated §11.4.122); this harness does NOT")
		fmt.Println("         pause the live coder autonomously. Coder untouched.")
		return 10
	case errors.Is(err, vrambroker.ErrBurstInUse):
		fmt.Println("BLOCKED: ErrBurstInUse — another image/video burst owns the card (single-owner §11.4.119). Queue.")
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

	// (1) admit BEFORE boot — the whole point (§11.4.119 / broker).
	lease, err := admit(ctx)
	if code := classifyAdmit(err); code != 0 {
		os.Exit(code)
	}
	defer lease.Release() // single-owner slot freed on exit no matter what

	// (2) boot on :18443 through the containers submodule orchestrator.
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
	fmt.Printf("UP-OK: %s videogen via containers submodule orchestrator (:%s)\n", p.Name, healthPort)

	// (3) health poll.
	ok := pollHealth(ctx, 5*time.Minute)

	// (4) single-owner teardown — coder untouched.
	dctx, dcancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer dcancel()
	if derr := orch.Down(dctx, p,
		compose.WithDownRemoveVolumes(false), // keep the HF model cache
		compose.WithDownRemoveOrphans(true),
	); derr != nil {
		fmt.Printf("WARN: compose down: %v\n", derr)
	} else {
		fmt.Printf("DOWN-OK: %s videogen torn down (single-owner cleanup, coder untouched)\n", p.Name)
	}

	if !ok {
		fmt.Println("BLOCKED: videogen service never became healthy on :" + healthPort)
		os.Exit(4)
	}
	fmt.Println("BOOT-HEALTH-OK: videogen /health answered. Generation + VRAM calibration is the")
	fmt.Println("PENDING runtime-proof step (operator-authorized coder-pause + first-run calibration).")
}

// pollHealth probes the shim /health via the containers pkg/health HTTP checker
// (§11.4.76 primitive) until it answers or the deadline passes.
func pollHealth(ctx context.Context, budget time.Duration) bool {
	target := health.HealthTarget{
		Name:    service,
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
			fmt.Printf("HEALTH-OK: videogen /health after %d polls (status=%s)\n", n, res.Details["status_code"])
			return true
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Printf("HEALTH-TIMEOUT: videogen /health did not answer within %s (%d polls)\n", budget, n)
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
		compose.WithDownRemoveVolumes(false),
		compose.WithDownRemoveOrphans(true),
	); err != nil {
		fatal("compose down: %v", err)
	}
	fmt.Printf("DOWN-OK: %s videogen (single-owner cleanup, coder untouched)\n", p.Name)
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
