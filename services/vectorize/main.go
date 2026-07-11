// Package main implements the HelixLLM vectorization capability — raster
// image -> SVG via the REAL vtracer CLI (visioncortex VTracer, Rust, CPU
// raster-tracing), the DEFAULT vector-graphics engine per
// docs/research/07.2026/02_vision_generative/CAPABILITIES_MASTER_PLAN_v2.md
// (P3-T4', "Vectorize (pixel→SVG)"). StarVector-8B is an OPTIONAL, narrower
// icon/logo/diagram-only GPU tier per its own model card and is NOT wired
// here — see README "StarVector tier status".
//
// This is REAL infrastructure, not a test double: every SVG returned comes
// from an actual `vtracer` invocation tracing actual pixels (§11.4.6 /
// anti-bluff). No simulated tracing, no hardcoded paths, no placeholder SVG.
//
// Two endpoints:
//
//	POST /v1/vectorize  body = raw raster image bytes (PNG/JPEG/GIF/BMP/...)
//	                    query ?preset=bw|poster|photo (optional — omit to
//	                    use vtracer's own general-purpose defaults)
//	                    -> JSON {engine, preset, source_format, width,
//	                    height, svg} via `vtracer -i <in> -o <out.svg>
//	                    [--preset X]`.
//
//	POST /v1/rasterize  body = raw SVG bytes (Content-Type image/svg+xml)
//	                    query ?w=&h= (optional explicit pixel size — the
//	                    fidelity harness passes the ORIGINAL raster's own
//	                    width/height so the re-rasterized PNG is
//	                    pixel-dimension-exact against the source, enabling a
//	                    direct per-pixel structural comparison)
//	                    -> image/png bytes, rasterized INSIDE this container
//	                    via `rsvg-convert` — so the fidelity harness never
//	                    depends on host SVG-renderer version/behaviour (the
//	                    same server-side-rendering principle the sibling OCR
//	                    service documents for /v1/render).
//
//	GET  /health        -> real `vtracer --version` + `rsvg-convert
//	                    --version`, proving both engine binaries are present
//	                    and callable in THIS image (§11.4.108 artifact-layer
//	                    signature).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/v1/vectorize", handleVectorize)
	mux.HandleFunc("/v1/rasterize", handleRasterize)

	addr := ":8080"
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
	}
	log.Printf("helix-vectorize listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// vtracerVersion shells out to `vtracer --version` and returns the trimmed
// first line (e.g. "visioncortex VTracer 0.6.5"). Real call every time —
// never cached as a hardcoded string — so a broken/absent binary surfaces
// immediately.
func vtracerVersion() (string, error) {
	out, err := exec.Command("vtracer", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("vtracer --version: %w: %s", err, string(out))
	}
	lines := strings.SplitN(string(out), "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

// rsvgVersion shells out to `rsvg-convert --version` and returns the
// trimmed first line.
func rsvgVersion() (string, error) {
	out, err := exec.Command("rsvg-convert", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rsvg-convert --version: %w: %s", err, string(out))
	}
	lines := strings.SplitN(string(out), "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status": "ok",
		"engine": "vtracer",
	}
	if ver, err := vtracerVersion(); err != nil {
		resp["status"] = "degraded"
		resp["vtracer_error"] = err.Error()
	} else {
		resp["vtracer_version"] = ver
	}
	if ver, err := rsvgVersion(); err != nil {
		resp["status"] = "degraded"
		resp["rsvg_convert_error"] = err.Error()
	} else {
		resp["rsvg_convert_version"] = ver
	}
	writeJSON(w, http.StatusOK, resp)
}

// vectorizeResponse is the unified /v1/vectorize contract.
type vectorizeResponse struct {
	Engine       string `json:"engine"`
	Preset       string `json:"preset"`
	SourceFormat string `json:"source_format"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SVG          string `json:"svg"`
}

// extForContentType maps a sniffed MIME type (http.DetectContentType) to a
// file extension the `image` crate's format loader (used internally by
// vtracer) recognizes. Defaults to .png for any undetected/binary type —
// vtracer/`image` sniffs actual file content, not just the extension, but a
// recognizable extension is required for the loader's format dispatch.
func extForContentType(ct string) string {
	switch {
	case strings.HasPrefix(ct, "image/png"):
		return ".png"
	case strings.HasPrefix(ct, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(ct, "image/gif"):
		return ".gif"
	case strings.HasPrefix(ct, "image/bmp"), strings.HasPrefix(ct, "image/x-ms-bmp"):
		return ".bmp"
	default:
		return ".png"
	}
}

// handleVectorize runs the REAL vtracer binary over the posted raster image
// bytes and returns the produced SVG — every byte of the `svg` field is the
// verbatim vtracer output, never fabricated or templated.
func handleVectorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty image body", http.StatusBadRequest)
		return
	}

	preset := r.URL.Query().Get("preset")
	switch preset {
	case "", "bw", "poster", "photo":
	default:
		http.Error(w, "unknown preset: "+preset+" (want bw|poster|photo|<empty>)", http.StatusBadRequest)
		return
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		http.Error(w, "decode image (unsupported/corrupt raster): "+err.Error(), http.StatusBadRequest)
		return
	}

	tmpDir, err := os.MkdirTemp("", "vectorize-in-*")
	if err != nil {
		http.Error(w, "tempdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	inPath := filepath.Join(tmpDir, "in"+extForContentType(http.DetectContentType(body)))
	outPath := filepath.Join(tmpDir, "out.svg")
	if err := os.WriteFile(inPath, body, 0o600); err != nil {
		http.Error(w, "write input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	args := []string{"-i", inPath, "-o", outPath}
	if preset != "" {
		args = append(args, "--preset", preset)
	}
	cmd := exec.Command("vtracer", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("vtracer failed: %v: stdout=%s stderr=%s", err, stdout.String(), stderr.String()), http.StatusInternalServerError)
		return
	}

	svgBytes, err := os.ReadFile(outPath)
	if err != nil {
		http.Error(w, "read output svg: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(svgBytes) == 0 {
		http.Error(w, "vtracer produced an empty SVG file", http.StatusInternalServerError)
		return
	}

	ver, verErr := vtracerVersion()
	if verErr != nil {
		ver = "unknown"
	}
	resp := vectorizeResponse{
		Engine:       "vtracer-" + strings.TrimPrefix(ver, "visioncortex VTracer "),
		Preset:       preset,
		SourceFormat: format,
		Width:        cfg.Width,
		Height:       cfg.Height,
		SVG:          string(svgBytes),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRasterize runs the REAL rsvg-convert binary over the posted SVG
// bytes and returns the rasterized PNG. Always executed INSIDE this
// container so the fidelity harness's re-rasterize step never depends on
// the calling host's own SVG-renderer version — mirrors the sibling OCR
// service's server-side /v1/render rationale (see README).
func handleRasterize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty svg body", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	widthS, heightS := q.Get("w"), q.Get("h")

	tmpDir, err := os.MkdirTemp("", "rasterize-*")
	if err != nil {
		http.Error(w, "tempdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	inPath := filepath.Join(tmpDir, "in.svg")
	outPath := filepath.Join(tmpDir, "out.png")
	if err := os.WriteFile(inPath, body, 0o600); err != nil {
		http.Error(w, "write input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var args []string
	if widthS != "" {
		if _, err := strconv.Atoi(widthS); err != nil {
			http.Error(w, "bad w query param: "+err.Error(), http.StatusBadRequest)
			return
		}
		args = append(args, "-w", widthS)
	}
	if heightS != "" {
		if _, err := strconv.Atoi(heightS); err != nil {
			http.Error(w, "bad h query param: "+err.Error(), http.StatusBadRequest)
			return
		}
		args = append(args, "-h", heightS)
	}
	// -a keeps rsvg-convert from distorting aspect ratio when only one of
	// -w/-h is given; harmless when both (or neither) are given.
	args = append(args, "-a", "-o", outPath, inPath)

	cmd := exec.Command("rsvg-convert", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("rsvg-convert failed: %v: stderr=%s", err, stderr.String()), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		http.Error(w, "read rasterized png: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
