// Command visiongen-boot is the on-demand boot + health harness for the
// HelixLLM VISION (VLM) service — a multimodal server (GGUF + mmproj /
// libmtmd) on the host accelerator, co-resident WITH the live resident coder
// (:18434, never touched). Unlike the image/video BURST lanes, the VLM is a
// WARM tier (vrambroker.ClassVLM): once booted it STAYS UP to answer vision
// requests — boot does NOT tear the service down. Teardown is the separate
// `down` subcommand (single-owner cleanup §11.4.119).
//
// WHICH MODEL RUNS IS MEASURED, NOT CONFIGURED (FR-056)
//
// This harness does not have a default model and cannot be told which model to
// run. Every boot measures this host, joins the measurement against the
// recorded catalogue under the declared usage, and serves an option the host
// was proven able to run. See modelchoice.go for the decision and the three
// distinct reasons a candidate can be withheld.
//
//   - VISIONGEN_MODEL_GGUF and VISIONGEN_MMPROJ are OUTPUTS of that decision,
//     written here for compose to interpolate. They are no longer inputs: a
//     value found in the environment is reported and overwritten, because a
//     configured name would defeat the measurement.
//   - VISIONGEN_MODEL_DIR remains an INPUT. It says WHERE model files live on
//     this host, never which of them runs.
//   - The VRAM figure admitted by the broker comes from the CHOSEN option's
//     recorded requirement. VISIONGEN_NEED_BYTES is no longer honoured: it
//     implied a model ("~9 GiB means the 7B"), which is the static selection
//     FR-056 forbids.
//
// The rest of the discipline is unchanged (design scratchpad/
// design_gpu_generative*.md + docs/qa/phase3_vision_20260707):
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
// Subcommands:
//
//	plan   [--pin id[:variant]]     measure + decide + report; boots nothing
//	admit-check                     broker-only VRAM admission verdict (no boot)
//	boot   <compose-file> <project> measure -> choose -> admit -> up -> health (STAYS UP)
//	down   <compose-file> <project> single-owner teardown (compose down)
//	status <compose-file> <project> compose service status
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"digital.vasic.containers/pkg/compose"
	"digital.vasic.containers/pkg/health"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

const (
	service    = "visiongen"
	healthPort = "18439" // OWN port — coder :18434 + siblings :18435-18443 untouched

	// family is the capability this lane serves. It selects WITHIN the
	// catalogue; it never names a model.
	family = catalogue.FamilyVision

	// forbidKey is the operator's forbid-list. Forbidding can only remove an
	// option the measurement offered, never add one it did not.
	forbidKey = "VISIONGEN_FORBID_MODELS"

	// modelDirKey says WHERE model files live on this host.
	modelDirKey = "VISIONGEN_MODEL_DIR"

	// weightsKey and projectorKey are compose interpolation OUTPUTS, written
	// from the measured decision.
	weightsKey   = "VISIONGEN_MODEL_GGUF"
	projectorKey = "VISIONGEN_MMPROJ"
)

// GiB is one gibibyte in bytes.
const GiB int64 = 1024 * 1024 * 1024

// defaultModelDir is the VLM model cache directory (~/models/vlm_cache), used
// to seed VISIONGEN_MODEL_DIR when the operator has not set it. It is a
// LOCATION and nothing more — which model runs is decided from the measurement,
// and this only says where that model's files are looked for. Never hardcoded
// into compose.vision.yml itself (§CONST-045 / §11.4.28).
func defaultModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "models", "vlm_cache")
}

// modelDir resolves where this host keeps VLM weights.
func modelDir() string {
	if dir := os.Getenv(modelDirKey); dir != "" {
		return dir
	}
	dir := defaultModelDir()
	if dir != "" {
		os.Setenv(modelDirKey, dir)
	}
	return dir
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: visiongen-boot <plan|admit-check|boot|down|status> [compose-file] [project] [--pin id[:variant]]")
	}
	switch os.Args[1] {
	case "plan":
		cmdPlan()
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

// chooseModel measures the host, decides which model this host can serve, and
// locates that model's artefacts.
//
// The order is the whole point: measure, then choose, then look for the chosen
// model's files. Nothing here can start a model the measurement did not offer,
// and when no offered model's weights are present on this host it refuses
// rather than falling back to whatever happens to be in the directory.
func chooseModel(ctx context.Context, dir string) (choice, error) {
	pin, err := parsePin(os.Args[1:])
	if err != nil {
		return choice{}, exitErr(exitNoOptionOffered, "CANNOT-CHOOSE: %v", err)
	}

	offered, loaded, profile, purpose, err := decide(ctx, family, dir, pin, forbidKey)
	if err != nil {
		return choice{}, err
	}

	var missing []string
	// offered arrives ordered cheapest-admissible-first from selection, and is
	// taken in that order: the first option this runtime can actually serve wins.
	// The order is not re-decided here — one rule, in one place, so the lanes
	// cannot drift apart from each other or from the Python gate.
	for _, option := range offered {
		weights, projector, locErr := locateWeights(dir, option)
		if locErr != nil {
			missing = append(missing, fmt.Sprintf("%s (%v)", option.Identity, locErr))
			continue
		}
		entry, _ := entryFor(loaded, option)
		return choice{
			Option:        option,
			Entry:         entry,
			Profile:       profile,
			Usage:         purpose,
			WeightsFile:   weights,
			ProjectorFile: projector,
		}, nil
	}

	return choice{}, exitErr(exitWeightsNotPresent,
		"CANNOT-CHOOSE: this host was measured and can serve %d %s model(s), but none of their weights "+
			"are present in %s:\n  %s\n"+
			"  No model is started: booting some other file that happens to be in that directory would be a "+
			"model nobody chose.\n  Remedy: obtain the weights for one of the options above, or point %s at "+
			"the directory that holds them.",
		len(offered), family, dir, joinLines(missing), modelDirKey)
}

// reportChoice prints the decision and the measurement it rests on.
func reportChoice(c choice) {
	fmt.Printf("CHOSEN %s — decided from the measured host %q, not from configuration.\n",
		c.Option.Identity, c.Option.HostIdentity)
	fmt.Printf("  requires: memory=%dMiB storage=%dMiB accelerator=%t\n",
		c.Option.Cost.MemoryRequiredBytes/(1024*1024),
		c.Option.Cost.StorageRequiredBytes/(1024*1024),
		c.Option.Cost.RequiresAccelerator)
	fmt.Printf("  leaves:   memory=%dMiB (%.1f%% of total) storage=%dMiB\n",
		c.Option.Headroom.MemoryRemainingBytes/(1024*1024),
		c.Option.Headroom.MemoryRemainingFraction*100,
		c.Option.Headroom.StorageRemainingBytes/(1024*1024))
	fmt.Printf("  licence:  %s permits the declared usage %q\n", c.Option.Terms.LicenseID, c.Usage)
	fmt.Printf("  weights:  %s\n  projector: %s\n", c.WeightsFile, c.ProjectorFile)
}

// applyChoice writes the decision into the environment compose interpolates.
//
// These variables are OUTPUTS. A value already present in the environment named
// a model that no measurement chose, so it is reported and overwritten rather
// than honoured.
func applyChoice(c choice) {
	for key, value := range map[string]string{
		weightsKey:   c.WeightsFile,
		projectorKey: c.ProjectorFile,
	} {
		if existing := os.Getenv(key); existing != "" && existing != value {
			fmt.Printf("IGNORED-CONFIG: %s=%q named a model that no measurement chose; "+
				"overwritten with the measured choice %q (FR-056).\n", key, existing, value)
		}
		os.Setenv(key, value)
	}
	if legacy := os.Getenv("VISIONGEN_NEED_BYTES"); legacy != "" {
		fmt.Printf("IGNORED-CONFIG: VISIONGEN_NEED_BYTES=%q is no longer honoured — it implied a model. "+
			"The admitted figure comes from the chosen option's recorded requirement (FR-056).\n", legacy)
	}
}

// needBytesFor is the VRAM footprint to admit for the chosen option: its
// recorded memory requirement. The broker adds its own headroom on top.
func needBytesFor(c choice) int64 {
	return int64(c.Option.Cost.MemoryRequiredBytes)
}

// cmdPlan measures, decides and reports — and boots nothing. It is the honest
// way to see which model this host would serve, and why the others were not
// offered, without touching the card.
func cmdPlan() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := modelDir()
	if dir == "" {
		fatal("%s is unset and no default could be derived (home directory unavailable) — "+
			"set %s to the VLM model cache path", modelDirKey, modelDirKey)
	}
	c, err := chooseModel(ctx, dir)
	if err != nil {
		fmt.Println(err)
		os.Exit(exitCodeFor(err))
	}
	reportChoice(c)
	fmt.Printf("PLAN-OK: this host would serve %s (nothing was booted).\n", c.Option.Identity)
}

// admit acquires a WARM (ClassVLM) lease for the chosen model's footprint,
// returning the lease or a classified reason. It NEVER pauses the coder — an
// ErrBudgetExceeded is surfaced as a BLOCKED verdict (coder-pause is operator
// gated, §11.4.122).
func admit(ctx context.Context, need int64) (*vrambroker.Lease, error) {
	broker := vrambroker.New() // real nvidia-smi-backed admission (§11.4.6 fail-closed)
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

// cmdAdmitCheck tests the admission gate for the model this host would actually
// serve. It measures first, because the footprint to admit is the chosen
// model's — there is no fixed figure to test against.
func cmdAdmitCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := modelDir()
	if dir == "" {
		fatal("%s is unset and no default could be derived (home directory unavailable) — "+
			"set %s to the VLM model cache path", modelDirKey, modelDirKey)
	}
	c, err := chooseModel(ctx, dir)
	if err != nil {
		fmt.Println(err)
		os.Exit(exitCodeFor(err))
	}
	reportChoice(c)

	lease, aerr := admit(ctx, needBytesFor(c))
	code := classifyAdmit(aerr)
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

	// (0) WHERE the weights live. Fail loud (§11.4.6) if it cannot be resolved:
	// an empty VISIONGEN_MODEL_DIR makes compose interpolate the bind mount as
	// `:/models:ro` (malformed host path -> cryptic podman error).
	dir := modelDir()
	if dir == "" {
		fatal("%s is unset and no default could be derived (home directory unavailable) — "+
			"set %s to the VLM model cache path", modelDirKey, modelDirKey)
	}

	// (1) WHICH model — measured, never configured. A refusal here exits
	// non-zero with the reason; no default model stands in for a measurement.
	c, err := chooseModel(ctx, dir)
	if err != nil {
		fmt.Println(err)
		os.Exit(exitCodeFor(err))
	}
	reportChoice(c)
	applyChoice(c)

	// (2) admit the CHOSEN model's footprint BEFORE boot (broker / §11.4.6
	// fail-closed).
	lease, aerr := admit(ctx, needBytesFor(c))
	if code := classifyAdmit(aerr); code != 0 {
		os.Exit(code)
	}
	// The warm-tier lease is tied to THIS process; the container keeps holding
	// the VRAM after we exit. Release on exit so the in-process accounting slot
	// is freed — the running VLM container is independent of this process.
	defer lease.Release()

	// (3) boot on :18439 through the containers submodule orchestrator.
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
	fmt.Printf("UP-OK: %s visiongen via containers submodule orchestrator (:%s) serving %s\n",
		p.Name, healthPort, c.Option.Identity)

	// (4) health poll — then LEAVE IT RUNNING (warm tier). NO auto-teardown:
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

// joinLines renders a list one per indented line, for refusals that name every
// option they considered.
func joinLines(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += "\n  "
		}
		out += s
	}
	return out
}
