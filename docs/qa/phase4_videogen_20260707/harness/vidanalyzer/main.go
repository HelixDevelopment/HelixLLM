// Command vidanalyzer is the RED-first, self-validated anti-bluff analyzer for
// the HelixLLM Phase-4 VIDEO-generation capability (§11.4.107 / §11.4.107(10) /
// §11.4.115 / §11.4.136). Given a generated short MP4 it decides whether the
// clip is a REAL generated video with LIVE, ADVANCING frames or a DEGENERATE
// clip (a frozen single-repeated-frame / solid / static-loop clip a broken or
// un-implemented service would emit) — the load-bearing anti-bluff assertion
// that defeats "the harness returned *an* MP4 so it works" AND the §11.4.107
// stale/frozen-frame trap ("a picture showed, but it was the same frozen frame").
//
// It needs NO GPU: it decodes committed/generated MP4 fixtures via ffmpeg/ffprobe
// (present on the host), so its own self-validation (golden-good moving clip
// PASSes, every golden-bad frozen/solid clip FAILs) is REAL evidence produced
// now, before any GPU generation exists. A moving test pattern is the
// golden-good; a single repeated frame + a solid colour are the golden-bads.
//
// The oracle (multi-signal AND — a single metric is never proof, §11.4.107):
//
//	frameCount     ffprobe frame count over the clip (>= floor)
//	ptsMonotonic   demux PTS strictly increasing — an INDEPENDENT frame-advance
//	               counter (different oracle domain than pixels; a flat/repeated
//	               PTS ⇒ stuck-muxer ⇒ FAIL even if pixels look to move)
//	meanAdjDist    mean per-pixel |Δluma| between temporally-adjacent frames —
//	               the FREEZE-DETECTION oracle (perceptual distance, NOT
//	               byte-identical; byte-identity is only a zero-cost pre-filter).
//	               Frozen/looped ~ 0 (too low) ⇒ FAIL.
//	distinctFrac   fraction of adjacent frame-pairs that meaningfully differ —
//	               a frozen/static-loop clip is ~0 ⇒ FAIL (not-a-static-loop).
//	meanEntropy    mean per-frame luma Shannon entropy — a solid/blank clip is
//	               ~0 ⇒ FAIL (not-a-solid/blank clip).
//
// A clip is LIVE (REAL) iff ALL signals are satisfied.
//
// Polarity switch (§11.4.115): RED_MODE (env, default "1").
//
//	RED_MODE=1  reproduce-the-defect: `analyze <mp4>` PASSes (exit 0) iff the
//	            clip is DEGENERATE (defect present) — the state before a real
//	            service exists. An MP4 that is unexpectedly LIVE fails RED.
//	RED_MODE=0  standing regression-guard: `analyze <mp4>` PASSes (exit 0) iff
//	            the clip is a REAL live generated video (defect absent, post-fix).
//
// Subcommands:
//
//	gen-golden <good|frozen|solid> <out.mp4>   deterministic ffmpeg fixtures
//	analyze     <mp4>                           RED_MODE-polarity verdict
//	selfvalidate                                §11.4.107(10) oracle proof
//
// ffmpeg/ffprobe absent ⇒ honest SKIP-with-reason (§11.4.3), exit 3 — NEVER a
// faked PASS (a tool-absent fake-pass would be a §11.4 PASS-bluff).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ---- oracle thresholds. Calibrated on THIS harness's own deterministic ffmpeg
// fixtures (§11.4.6 — never blindly from literature). A real generated WAN/LTX
// MP4 at runtime-proof time re-calibrates these against captured outputs. ----
//
// §11.4.107(13) RECALIBRATION (2026-07-11, docs/qa/videogen_recalibration_20260711*):
// advanceDiffFloor was originally 1.5, calibrated ONLY on the synthetic
// ffmpeg `testsrc2` golden-good fixture (a high-contrast, large-block moving
// test pattern that scores mean_adj_luma_dist=4.19 at 64x64 downscale). The
// real WAN2.2 TI2V-5B clip analyzed in docs/qa/videogen_analysis_20260711T141601Z
// (docs/qa/wan2_generation_20260709T0010Z/helix_code_wan2_generation.mp4,
// 1280x704, 24fps, 121 frames, smoother cinematic AI-generated content) scored
// mean_adj_luma_dist=1.098228624131945 — genuinely live per an INDEPENDENT
// full-resolution ffmpeg `freezedetect` cross-check (zero freeze windows at
// two thresholds) and per distinct_adjacent_frac=1.000 (every single one of
// its 120 adjacent frame-pairs exceeds distinctEpsilon) — yet 1.098 < 1.5
// false-flagged it DEGENERATE. Both golden-bad fixtures (`frozen`: one real
// frame looped; `solid`: blank colour) measure mean_adj_luma_dist EXACTLY
// 0.000000... (deterministic ffmpeg re-encode of byte-identical source
// frames — no decode noise). The recalibrated floor (0.55) is set at ~50% of
// the single captured real-live sample (1.098), giving: (a) ~2x safety
// margin BELOW the known-live WAN measurement (1.098 vs floor 0.55 — a
// 0.548 absolute / 100% relative margin), and (b) an unbounded margin ABOVE
// the exact-zero measured on both golden-bad fixtures (0.55 vs 0.000 — the
// golden-bads are not "close" to the new floor by any measure). This is a
// SINGLE real-fixture (n=1) calibration — an HONEST §11.4.6 gap, not
// resolved here: as more real WAN/LTX outputs are captured, this floor
// SHOULD be revisited/tightened using a proper distribution rather than one
// sample. distinctFracLow (0.50) and entropyFloor (3.0) are UNCHANGED — both
// already comfortably separate the WAN clip (distinctFrac=1.000,
// entropy=6.789) from the golden-bads (distinctFrac=0.000, entropy 0.000 for
// `solid`) and needed no adjustment; the false-positive was isolated to the
// single meanAdjDist signal.
const (
	frameCountFloor  = 8    // sampled frames; a too-short clip is not a proof window
	advanceDiffFloor = 0.55 // mean |Δluma| adjacent frames; frozen/looped ~ 0 (was 1.5 — §11.4.107(13) recalibration, see comment above)
	advanceDiffCeil  = 80.0 // above -> per-frame pure noise, no temporal coherence
	distinctFracLow  = 0.50 // fraction of adjacent pairs that meaningfully differ
	entropyFloor     = 3.0  // mean per-frame luma entropy bits; solid/blank ~ 0
	distinctEpsilon  = 0.75 // per-pixel |Δluma| above which an adjacent pair "differs"
	analyzeEdge      = 64    // frames downscaled to analyzeEdge x analyzeEdge for the oracle
)

type metrics struct {
	FrameCount   int     `json:"frame_count"`
	PtsMonotonic bool    `json:"pts_monotonic"`
	MeanAdjDist  float64 `json:"mean_adj_luma_dist"`
	DistinctFrac float64 `json:"distinct_adjacent_frac"`
	MeanEntropy  float64 `json:"mean_frame_entropy_bits"`
}

type verdict struct {
	Live    bool     `json:"live_video"`
	Reasons []string `json:"reasons"`
	Metrics metrics  `json:"metrics"`
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: vidanalyzer <gen-golden|analyze|selfvalidate> [args...]")
	}
	requireFFmpeg()
	switch os.Args[1] {
	case "gen-golden":
		cmdGenGolden()
	case "analyze":
		cmdAnalyze()
	case "selfvalidate":
		cmdSelfValidate()
	default:
		fatal("unknown subcommand: %s", os.Args[1])
	}
}

// requireFFmpeg refuses fail-closed with an honest SKIP (§11.4.3) rather than a
// fake PASS when the decode toolchain is absent.
func requireFFmpeg() {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			fmt.Printf("SKIP: %s not on PATH — video decode toolchain absent (§11.4.3, tool-absent, NOT a PASS)\n", bin)
			os.Exit(3)
		}
	}
}

// ---- ffmpeg-backed decode ----

// ptsList returns the demux presentation timestamps (seconds) of every video
// frame, via ffprobe. Order is decode order; strict monotonic-increase is the
// independent frame-advance signal.
func ptsList(mp4 string) ([]float64, error) {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "frame=pts_time",
		"-of", "csv=p=0", mp4).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe pts: %w", err)
	}
	var pts []float64
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			continue
		}
		if v, e := strconv.ParseFloat(s, 64); e == nil {
			pts = append(pts, v)
		}
	}
	return pts, nil
}

// extractFrames decodes the clip to analyzeEdge-square PNG frames in a temp dir
// and returns their downsampled luma matrices (row-major, one per frame).
func extractFrames(mp4 string) ([][]float64, error) {
	dir, err := os.MkdirTemp("", "vidanalyzer_frames_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	pattern := filepath.Join(dir, "f_%05d.png")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error",
		"-i", mp4,
		"-vf", fmt.Sprintf("scale=%d:%d", analyzeEdge, analyzeEdge),
		"-vsync", "0",
		pattern)
	if out, e := cmd.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("ffmpeg extract: %v: %s", e, string(out))
	}

	names, _ := filepath.Glob(filepath.Join(dir, "f_*.png"))
	sort.Strings(names)
	var frames [][]float64
	for _, n := range names {
		lm, e := lumaMatrix(n)
		if e != nil {
			return nil, e
		}
		frames = append(frames, lm)
	}
	return frames, nil
}

func lumaMatrix(pngPath string) ([]float64, error) {
	f, err := os.Open(pngPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	m := make([]float64, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			m = append(m, (float64(r>>8)+float64(g>>8)+float64(bl>>8))/3.0)
		}
	}
	return m, nil
}

// ---- the oracle ----

func analyzeMP4(mp4 string) (verdict, error) {
	v := verdict{}

	pts, err := ptsList(mp4)
	if err != nil {
		return v, err
	}
	frames, err := extractFrames(mp4)
	if err != nil {
		return v, err
	}
	v.Metrics.FrameCount = len(frames)

	// (2) independent frame-advance counter: PTS strictly increasing.
	mono := len(pts) >= 2
	for i := 1; i < len(pts); i++ {
		if pts[i] <= pts[i-1] {
			mono = false
			break
		}
	}
	v.Metrics.PtsMonotonic = mono

	// (3)/(4) freeze-detection + not-a-static-loop over adjacent frames.
	var distSum float64
	var pairs, distinct int
	for i := 1; i < len(frames); i++ {
		d := meanAbsDiff(frames[i-1], frames[i])
		distSum += d
		pairs++
		if d > distinctEpsilon {
			distinct++
		}
	}
	if pairs > 0 {
		v.Metrics.MeanAdjDist = distSum / float64(pairs)
		v.Metrics.DistinctFrac = float64(distinct) / float64(pairs)
	}

	// (5) per-frame not-solid: mean luma entropy across frames.
	var entSum float64
	for _, fr := range frames {
		entSum += lumaEntropy(fr)
	}
	if len(frames) > 0 {
		v.Metrics.MeanEntropy = entSum / float64(len(frames))
	}

	// multi-signal AND — every check must hold for LIVE.
	live := true
	if v.Metrics.FrameCount < frameCountFloor {
		live = false
		v.Reasons = append(v.Reasons, fmt.Sprintf("frame count %d < floor %d (clip too short to prove liveness)", v.Metrics.FrameCount, frameCountFloor))
	}
	if !v.Metrics.PtsMonotonic {
		live = false
		v.Reasons = append(v.Reasons, "demux PTS not strictly increasing (stuck/duplicated frame-advance counter)")
	}
	if v.Metrics.MeanAdjDist < advanceDiffFloor {
		live = false
		v.Reasons = append(v.Reasons, fmt.Sprintf("mean adjacent-frame diff %.3f < floor %.1f (frozen / single-repeated-frame / static loop)", v.Metrics.MeanAdjDist, advanceDiffFloor))
	}
	if v.Metrics.MeanAdjDist > advanceDiffCeil {
		live = false
		v.Reasons = append(v.Reasons, fmt.Sprintf("mean adjacent-frame diff %.3f > ceil %.1f (per-frame pure noise, no temporal coherence)", v.Metrics.MeanAdjDist, advanceDiffCeil))
	}
	if v.Metrics.DistinctFrac < distinctFracLow {
		live = false
		v.Reasons = append(v.Reasons, fmt.Sprintf("distinct adjacent-frame fraction %.3f < floor %.2f (too many identical frames = static loop)", v.Metrics.DistinctFrac, distinctFracLow))
	}
	if v.Metrics.MeanEntropy < entropyFloor {
		live = false
		v.Reasons = append(v.Reasons, fmt.Sprintf("mean frame entropy %.3f < floor %.1f (solid/blank frames)", v.Metrics.MeanEntropy, entropyFloor))
	}
	if live {
		v.Reasons = append(v.Reasons, "all liveness signals satisfied (real live generated video)")
	}
	v.Live = live
	return v, nil
}

func meanAbsDiff(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var s float64
	for i := 0; i < n; i++ {
		s += math.Abs(a[i] - b[i])
	}
	return s / float64(n)
}

func lumaEntropy(fr []float64) float64 {
	var hist [256]int64
	for _, l := range fr {
		hist[int(l+0.5)&0xff]++
	}
	total := float64(len(fr))
	if total == 0 {
		return 0
	}
	var ent float64
	for _, c := range hist {
		if c == 0 {
			continue
		}
		p := float64(c) / total
		ent -= p * math.Log2(p)
	}
	return ent
}

// ---- fixtures (deterministic ffmpeg lavfi sources) ----

func cmdGenGolden() {
	if len(os.Args) < 4 {
		fatal("usage: gen-golden <good|frozen|solid> <out.mp4>")
	}
	kind, out := os.Args[2], os.Args[3]
	if err := makeFixture(kind, out); err != nil {
		fatal("gen-golden %s: %v", kind, err)
	}
	fmt.Printf("GEN-OK: kind=%s wrote %s\n", kind, out)
}

// makeFixture synthesizes a deterministic MP4 with ffmpeg lavfi:
//
//	good   -> testsrc2 moving test pattern (real advancing frames)
//	solid  -> a single solid colour (no motion, no content)
//	frozen -> ONE testsrc2 frame looped (structured but NOT advancing — the
//	          "single repeated frame" golden-bad the freeze oracle must reject)
func makeFixture(kind, out string) error {
	switch kind {
	case "good":
		return run("ffmpeg", "-nostdin", "-v", "error", "-y",
			"-f", "lavfi", "-i", "testsrc2=size=256x256:rate=10",
			"-t", "2", "-pix_fmt", "yuv420p", out)
	case "solid":
		return run("ffmpeg", "-nostdin", "-v", "error", "-y",
			"-f", "lavfi", "-i", "color=c=blue:size=256x256:rate=10",
			"-t", "2", "-pix_fmt", "yuv420p", out)
	case "frozen":
		dir, err := os.MkdirTemp("", "vidanalyzer_frozen_")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		frame := filepath.Join(dir, "frame.png")
		// grab a single structured frame ...
		if err := run("ffmpeg", "-nostdin", "-v", "error", "-y",
			"-f", "lavfi", "-i", "testsrc2=size=256x256",
			"-frames:v", "1", frame); err != nil {
			return err
		}
		// ... then loop that ONE frame into a 2 s clip (no motion).
		return run("ffmpeg", "-nostdin", "-v", "error", "-y",
			"-loop", "1", "-i", frame,
			"-t", "2", "-r", "10", "-pix_fmt", "yuv420p", out)
	default:
		return fmt.Errorf("unknown fixture kind %q (want good|frozen|solid)", kind)
	}
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, string(out))
	}
	return nil
}

// ---- analyze (RED_MODE polarity, §11.4.115) ----

func redMode() bool {
	v := os.Getenv("RED_MODE")
	return v == "" || v == "1" // default reproduce-the-defect
}

func cmdAnalyze() {
	if len(os.Args) < 3 {
		fatal("usage: analyze <mp4>")
	}
	v, err := analyzeMP4(os.Args[2])
	if err != nil {
		fatal("analyze %s: %v", os.Args[2], err)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))

	if redMode() {
		// RED_MODE=1: expect a DEGENERATE clip (defect present, no real service).
		if !v.Live {
			fmt.Println("[RED_MODE=1] RED-OK: clip is a DEGENERATE video (defect reproduced — not a real live generated video)")
			return
		}
		fmt.Println("[RED_MODE=1] RED-VIOLATION: clip is a REAL live generated video but a defect was expected (a real service must exist)")
		os.Exit(1)
	}
	// RED_MODE=0: standing guard — expect a REAL live video (defect absent).
	if v.Live {
		fmt.Println("[RED_MODE=0] GREEN-OK: clip is a REAL live generated video (defect absent)")
		return
	}
	fmt.Println("[RED_MODE=0] GREEN-FAIL: clip is a DEGENERATE video — generation did not produce a real live video")
	os.Exit(1)
}

// ---- selfvalidate (§11.4.107(10)) — runs NOW, no GPU. Golden-good MUST be
// classified LIVE; every golden-bad MUST be classified DEGENERATE. If any bad
// fixture is classified LIVE, the oracle is a bluff gate and this exits non-zero.

func cmdSelfValidate() {
	dir, err := os.MkdirTemp("", "vidanalyzer_sv_")
	if err != nil {
		fatal("mktemp: %v", err)
	}
	defer os.RemoveAll(dir)

	ok := true

	goodPath := filepath.Join(dir, "good.mp4")
	if e := makeFixture("good", goodPath); e != nil {
		fatal("make good fixture: %v", e)
	}
	good, e := analyzeMP4(goodPath)
	if e != nil {
		fatal("analyze good: %v", e)
	}
	printSV("GOLDEN-GOOD good (expect LIVE)", good)
	if !good.Live {
		ok = false
		fmt.Println("    SELF-VALIDATION VIOLATION: golden-good was NOT classified live")
	}

	for _, kind := range []string{"frozen", "solid"} {
		p := filepath.Join(dir, kind+".mp4")
		if e := makeFixture(kind, p); e != nil {
			fatal("make %s fixture: %v", kind, e)
		}
		bad, e := analyzeMP4(p)
		if e != nil {
			fatal("analyze %s: %v", kind, e)
		}
		printSV(fmt.Sprintf("GOLDEN-BAD %s (expect DEGENERATE)", kind), bad)
		if bad.Live {
			ok = false
			fmt.Printf("    SELF-VALIDATION VIOLATION: golden-bad %q PASSed as a live video (bluff gate)\n", kind)
		}
	}

	if !ok {
		fmt.Println("[SELF-VALIDATION] FAIL")
		os.Exit(1)
	}
	fmt.Println("[SELF-VALIDATION] PASS: oracle classifies golden-good LIVE and all golden-bad DEGENERATE")
}

func printSV(tag string, v verdict) {
	label := "DEGENERATE"
	if v.Live {
		label = "LIVE"
	}
	fmt.Printf("[%s] -> %s  frames=%d ptsMono=%t adjDist=%.2f distinctFrac=%.3f entropy=%.2f\n",
		tag, label, v.Metrics.FrameCount, v.Metrics.PtsMonotonic,
		v.Metrics.MeanAdjDist, v.Metrics.DistinctFrac, v.Metrics.MeanEntropy)
	for _, r := range v.Reasons {
		fmt.Printf("    reason: %s\n", r)
	}
}

// ---- helpers ----

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(2)
}
