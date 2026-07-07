// Package main implements the HelixLLM Phase-3 CPU OCR capability service —
// a minimal HTTP shim over the real Tesseract 5 (OEM 1 / LSTM) engine.
//
// This is REAL infrastructure, not a test double: every byte returned comes
// from an actual `tesseract` invocation against actual pixels (§11.4.6 /
// anti-bluff). No simulated recognition, no hardcoded text, no placeholder
// confidence values.
//
// Two endpoints:
//
//	POST /v1/render   { "text": "...", "mode": "label|blank|noise", "pointsize": 48 }
//	                  -> image/png bytes, deterministically rendered via
//	                  ImageMagick `convert` inside THIS container (so the
//	                  proof harness never depends on host fonts/versions —
//	                  the documented failure mode of host-side rendering,
//	                  see submodules/helix_llm/services/ocr/README.md).
//
//	POST /v1/ocr      body = raw image bytes (any format `convert`/tesseract
//	                  reads: PNG, JPEG, ...)
//	                  -> JSON { engine, config, words[]{text,conf,left,top,
//	                  width,height,line,block}, full_text, mean_conf }
//	                  via `tesseract <img> - --oem 1 --psm 6 tsv`, parsing
//	                  the real per-word TSV confidence column.
//
//	GET  /health      -> real tesseract --version + config, proving the
//	                  engine binary is present and callable in THIS image
//	                  (§11.4.108 artifact-layer signature).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// tessConfig is the fixed Tesseract invocation mode: OEM 1 = LSTM (the
// current, most-accurate engine, Tesseract 5 default per tessdoc), PSM 6 =
// "assume a single uniform block of text" (the right default for the
// rendered-label / UI-frame use case this service targets).
const tessConfig = "--oem 1 --psm 6"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/v1/render", handleRender)
	mux.HandleFunc("/v1/ocr", handleOCR)

	addr := ":8080"
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}
	log.Printf("helix-ocr listening on %s (%s)", addr, tessConfig)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// tesseractVersion shells out to `tesseract --version` and returns the first
// line (e.g. "tesseract 5.3.0"). Real call every time — never cached as a
// hardcoded string — so a broken/absent binary surfaces immediately.
func tesseractVersion() (string, error) {
	out, err := exec.Command("tesseract", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tesseract --version: %w: %s", err, string(out))
	}
	lines := strings.SplitN(string(out), "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	ver, err := tesseractVersion()
	resp := map[string]any{
		"status": "ok",
		"engine": "tesseract",
		"config": tessConfig,
	}
	if err != nil {
		resp["status"] = "degraded"
		resp["error"] = err.Error()
	} else {
		resp["tesseract_version"] = ver
	}
	writeJSON(w, http.StatusOK, resp)
}

// renderRequest is the /v1/render body.
type renderRequest struct {
	Text      string `json:"text"`
	Mode      string `json:"mode"`      // "label" (default) | "blank" | "noise"
	PointSize int    `json:"pointsize"` // default 48
}

// handleRender deterministically produces a PNG INSIDE this container via
// ImageMagick `convert`, so the same font/version renders every fixture
// regardless of what host invoked the proof — the documented anti-pattern
// this design avoids is host-side rendering with a different font path
// silently producing an unreadable image.
func handleRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req renderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.PointSize <= 0 {
		req.PointSize = 48
	}
	mode := req.Mode
	if mode == "" {
		mode = "label"
	}

	tmpDir, err := os.MkdirTemp("", "ocr-render-*")
	if err != nil {
		http.Error(w, "tempdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)
	outPath := filepath.Join(tmpDir, "out.png")

	var args []string
	switch mode {
	case "label":
		if strings.TrimSpace(req.Text) == "" {
			http.Error(w, "text required for mode=label", http.StatusBadRequest)
			return
		}
		// label: pseudo-image auto-sizes the canvas to the rendered text
		// extent. -pointsize + a bold, universally-available font
		// (DejaVu-Sans-Bold, installed by the Containerfile) keeps this
		// deterministic across rebuilds of the SAME image.
		args = []string{
			"-background", "white",
			"-fill", "black",
			"-font", "DejaVu-Sans-Bold",
			"-pointsize", strconv.Itoa(req.PointSize),
			"-gravity", "center",
			"label:" + req.Text,
			"-bordercolor", "white",
			"-border", "24x24",
			outPath,
		}
	case "blank":
		// Pure white canvas — genuinely NO text. The golden-bad fixture
		// proving the analyzer does not hallucinate a PASS on empty input.
		args = []string{"-size", "640x200", "xc:white", outPath}
	case "noise":
		// Random per-pixel noise — genuinely NO text, structurally
		// different from "blank" (proves the analyzer rejects noise too,
		// not merely uniform-color input).
		args = []string{"-size", "640x200", "xc:", "+noise", "Random", outPath}
	default:
		http.Error(w, "unknown mode: "+mode, http.StatusBadRequest)
		return
	}

	cmd := exec.Command("convert", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("convert failed: %v: %s", err, stderr.String()), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		http.Error(w, "read rendered png: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ocrWord is one recognized word-level TSV row (level 5, conf >= 0).
type ocrWord struct {
	Text   string  `json:"text"`
	Conf   float64 `json:"conf"`
	Left   int     `json:"left"`
	Top    int     `json:"top"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Line   int     `json:"line"`
	Block  int     `json:"block"`
}

// ocrResponse is the unified /v1/ocr contract.
type ocrResponse struct {
	Engine   string    `json:"engine"`
	Config   string    `json:"config"`
	Words    []ocrWord `json:"words"`
	FullText string    `json:"full_text"`
	MeanConf float64   `json:"mean_conf"`
}

// handleOCR runs the REAL tesseract binary over the posted image bytes and
// parses its TSV output — every field in the response is derived from that
// real invocation, never fabricated.
func handleOCR(w http.ResponseWriter, r *http.Request) {
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

	tmpDir, err := os.MkdirTemp("", "ocr-in-*")
	if err != nil {
		http.Error(w, "tempdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.png")
	if err := os.WriteFile(inPath, body, 0o600); err != nil {
		http.Error(w, "write input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// `-` as the output base -> TSV written to stdout (tessdoc Command-Line-Usage).
	args := append([]string{inPath, "-"}, strings.Fields(tessConfig)...)
	args = append(args, "tsv")
	cmd := exec.Command("tesseract", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("tesseract failed: %v: %s", err, stderr.String()), http.StatusInternalServerError)
		return
	}

	words, fullText, meanConf := parseTSV(stdout.String())
	ver, _ := tesseractVersion()
	resp := ocrResponse{
		Engine:   "tesseract-" + strings.TrimPrefix(ver, "tesseract "),
		Config:   tessConfig,
		Words:    words,
		FullText: fullText,
		MeanConf: meanConf,
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseTSV parses the real `tesseract ... tsv` stdout. Columns (tessdoc):
// level page_num block_num par_num line_num word_num left top width height conf text
// Only level==5 (word) rows with conf>=0 and non-empty trimmed text are kept
// — layout-only rows (block/paragraph/line) carry conf==-1 and are skipped,
// exactly per the documented TSV contract.
func parseTSV(tsv string) (words []ocrWord, fullText string, meanConf float64) {
	lines := strings.Split(tsv, "\n")
	var textParts []string
	var confSum float64
	var confN int
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header or blank
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 12 {
			continue
		}
		level := cols[0]
		if level != "5" {
			continue
		}
		conf, err := strconv.ParseFloat(cols[10], 64)
		if err != nil || conf < 0 {
			continue
		}
		text := strings.TrimSpace(cols[11])
		if text == "" {
			continue
		}
		lineNum, _ := strconv.Atoi(cols[4])
		blockNum, _ := strconv.Atoi(cols[2])
		left, _ := strconv.Atoi(cols[6])
		top, _ := strconv.Atoi(cols[7])
		width, _ := strconv.Atoi(cols[8])
		height, _ := strconv.Atoi(cols[9])
		words = append(words, ocrWord{
			Text: text, Conf: conf,
			Left: left, Top: top, Width: width, Height: height,
			Line: lineNum, Block: blockNum,
		})
		textParts = append(textParts, text)
		confSum += conf
		confN++
	}
	fullText = strings.Join(textParts, " ")
	if confN > 0 {
		meanConf = confSum / float64(confN)
	}
	return words, fullText, meanConf
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
