// Phase-3 RAG (Retrieval-Augmented Generation) end-to-end proof harness.
//
// Composes the two ALREADY-PROVEN Phase-3 capabilities: CPU embeddings (HF
// Text Embeddings Inference, TEI, BAAI/bge-small-en-v1.5, dim 384 — see
// docs/qa/phase3_embeddings_20260706/RESULTS.md) and the live coder LLM
// (helixllm-coder, Qwen3-Coder-30B-A3B-Instruct, http://localhost:18434).
//
// It boots ITS OWN TEI container (unique compose project, port 18440 — the
// coder at :18434 and sibling Phase-3 lanes on :18435-18439 are never
// touched, §11.4.119 single-owner) via the containers submodule
// compose.Orchestrator (§11.4.76, rootless podman §11.4.161).
//
// The unfakeable proof (§11.4.6, §11.4.107(10), §11.4.115): a corpus holds
// two INVENTED facts the model cannot know from training. RED baseline asks
// the coder the same questions with NO retrieved context -> it must fail to
// produce the invented tokens. GREEN runs the full pipeline (embed -> cosine
// retrieve top-1 -> grounded prompt -> generate) -> the answer must contain
// the invented token. The retrieval ranking is real cosine over real TEI
// embeddings (never a hardcoded pick). A self-validation pass proves the
// analyzer genuinely discriminates: golden-good PASSes; three golden-bad
// variants (no-fact answer, wrong-document retrieval, empty answer) MUST
// FAIL.
//
// Subcommands:
//
//	boot-up   <compose-file> <project>                       boot TEI via containers submodule
//	boot-down <compose-file> <project>                       tear down (single-owner cleanup)
//	boot-status <compose-file> <project>                     print service status
//	embed-corpus <tei-base-url> <out.json>                   embed the whole fixture corpus
//	embed-query  <tei-base-url> <qkey> <out.json>            embed one query (qkey: q1|q2)
//	retrieve  <corpus-emb.json> <query-emb.json> <qkey> <out.json>
//	                                                          real cosine rank, all docs
//	red       <coder-base-url> <qkey> <out.json>             ask bare question, no context
//	green     <coder-base-url> <retrieval.json> <qkey> <out.json>
//	                                                          grounded prompt -> generate
//	analyze   <retrieval.json> <generation.json> <qkey>      the RAG runtime-signature verdict
//	checkretrieval <retrieval.json> <qkey>                   assert real top-1 == expected doc
//	selfvalidate <good-retrieval.json> <good-generation.json> <qkey>
//	                                                          §11.4.107(10) analyzer self-check
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"digital.vasic.containers/pkg/compose"
)

// ---- fixture corpus + queries (TEST FIXTURES, not user-facing content —
// legitimately literal here; they are the probe, not product text). Two
// documents carry an INVENTED fact the served LLM cannot know from training;
// four are topic-adjacent-or-unrelated distractors so retrieval must
// genuinely discriminate. ----

type corpusDoc struct {
	ID   string
	Text string
}

var corpus = []corpusDoc{
	{"doc_codename", "The internal codename for the 2027 HelixCode release is Borealis-9."},
	{"doc_sidecar", "The internal service-mesh sidecar used internally for HelixLLM routing is called Quillfeather-7."},
	{"doc_cat", "The cat sat on the mat."},
	{"doc_revenue", "Quarterly revenue rose four percent."},
	{"doc_coffee", "Coffee brewing techniques improve flavor extraction using controlled water temperature."},
	{"doc_ci", "The HelixCode continuous integration pipeline runs unit tests before every merge to main."},
}

type queryFixture struct {
	Text          string
	ExpectDocID   string
	ExpectToken   string
	WrongDistract string // for the golden-bad "wrong document retrieved" mutation
}

var queries = map[string]queryFixture{
	"q1": {
		Text:          "What is the internal codename for the 2027 HelixCode release?",
		ExpectDocID:   "doc_codename",
		ExpectToken:   "Borealis-9",
		WrongDistract: "doc_ci",
	},
	"q2": {
		Text:          "What is the name of the internal service-mesh sidecar used for HelixLLM routing?",
		ExpectDocID:   "doc_sidecar",
		ExpectToken:   "Quillfeather-7",
		WrongDistract: "doc_cat",
	},
}

func corpusText(id string) string {
	for _, d := range corpus {
		if d.ID == id {
			return d.Text
		}
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: phase3rag <subcommand> [args...]")
	}
	switch os.Args[1] {
	case "boot-up":
		cmdBoot(true)
	case "boot-down":
		cmdBoot(false)
	case "boot-status":
		cmdStatus()
	case "embed-corpus":
		cmdEmbedCorpus()
	case "embed-query":
		cmdEmbedQuery()
	case "retrieve":
		cmdRetrieve()
	case "red":
		cmdRed()
	case "green":
		cmdGreen()
	case "analyze":
		cmdAnalyze()
	case "checkretrieval":
		cmdCheckRetrieval()
	case "selfvalidate":
		cmdSelfValidate()
	default:
		fatal("unknown subcommand: %s", os.Args[1])
	}
}

// ---- container boot via the containers submodule (§11.4.76) ----

func project() compose.ComposeProject {
	if len(os.Args) < 4 {
		fatal("need <compose-file> <project>")
	}
	return compose.ComposeProject{
		Name:     os.Args[3],
		File:     os.Args[2],
		Services: []string{"tei-rag"},
	}
}

func cmdBoot(up bool) {
	orch, err := compose.NewDefaultOrchestrator(".", nil)
	if err != nil {
		fatal("orchestrator: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p := project()
	if up {
		if err := orch.Up(ctx, p,
			compose.WithUpDetach(true),
			compose.WithRemoveOrphans(true),
		); err != nil {
			fatal("compose up: %v", err)
		}
		fmt.Printf("UP-OK: %s tei-rag via containers submodule orchestrator\n", p.Name)
		return
	}
	if err := orch.Down(ctx, p,
		compose.WithDownRemoveVolumes(false), // keep the shared HF-model cache
		compose.WithDownRemoveOrphans(true),
	); err != nil {
		fatal("compose down: %v", err)
	}
	fmt.Printf("DOWN-OK: %s tei-rag via containers submodule orchestrator\n", p.Name)
}

func cmdStatus() {
	orch, err := compose.NewDefaultOrchestrator(".", nil)
	if err != nil {
		fatal("orchestrator: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sts, err := orch.Status(ctx, project())
	if err != nil {
		fatal("status: %v", err)
	}
	for _, s := range sts {
		fmt.Printf("%s state=%s health=%s ports=%v exit=%d\n",
			s.Name, s.State, s.Health, s.Ports, s.ExitCode)
	}
	if len(sts) == 0 {
		fmt.Println("(no services reported)")
	}
}

// ---- OpenAI-compatible /v1/embeddings shapes (TEI) ----

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type embeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
}

func doEmbed(base string, inputs []string) embeddingResponse {
	reqBody, _ := json.Marshal(embeddingRequest{Model: "helix-rag-embed", Input: inputs})
	httpc := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpc.Post(base+"/v1/embeddings", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fatal("POST /v1/embeddings: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fatal("POST /v1/embeddings status=%d body=%s", resp.StatusCode, string(body))
	}
	var r embeddingResponse
	if err := json.Unmarshal(body, &r); err != nil {
		fatal("parse embeddings response: %v", err)
	}
	return r
}

// corpusEmbeddingsFile is the on-disk artefact carrying REAL, TEI-served
// vectors for the whole fixture corpus (§11.4.69 evidence).
type corpusEmbeddingsFile struct {
	Model string            `json:"model"`
	Dim   int               `json:"dim"`
	Items []corpusEmbedItem `json:"items"`
}

type corpusEmbedItem struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	Vector []float64 `json:"vector"`
}

func cmdEmbedCorpus() {
	if len(os.Args) < 4 {
		fatal("usage: embed-corpus <tei-base-url> <out.json>")
	}
	base, out := os.Args[2], os.Args[3]
	texts := make([]string, len(corpus))
	for i, d := range corpus {
		texts[i] = d.Text
	}
	r := doEmbed(base, texts)
	if len(r.Data) != len(corpus) {
		fatal("expected %d corpus vectors, got %d", len(corpus), len(r.Data))
	}
	byIdx := map[int][]float64{}
	for _, d := range r.Data {
		byIdx[d.Index] = d.Embedding
	}
	dim := 0
	items := make([]corpusEmbedItem, len(corpus))
	for i, d := range corpus {
		v := byIdx[i]
		if len(v) == 0 {
			fatal("missing embedding for corpus doc index %d (%s)", i, d.ID)
		}
		dim = len(v)
		items[i] = corpusEmbedItem{ID: d.ID, Text: d.Text, Vector: v}
	}
	f := corpusEmbeddingsFile{Model: r.Model, Dim: dim, Items: items}
	writeJSON(out, f)
	fmt.Printf("EMBED-CORPUS-OK: model=%s dim=%d docs=%d wrote %s\n", r.Model, dim, len(items), out)
}

type queryEmbedFile struct {
	Model  string    `json:"model"`
	QKey   string    `json:"qkey"`
	Query  string    `json:"query"`
	Vector []float64 `json:"vector"`
}

func cmdEmbedQuery() {
	if len(os.Args) < 5 {
		fatal("usage: embed-query <tei-base-url> <qkey> <out.json>")
	}
	base, qkey, out := os.Args[2], os.Args[3], os.Args[4]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	r := doEmbed(base, []string{q.Text})
	if len(r.Data) < 1 || len(r.Data[0].Embedding) == 0 {
		fatal("no embedding returned for query %s", qkey)
	}
	f := queryEmbedFile{Model: r.Model, QKey: qkey, Query: q.Text, Vector: r.Data[0].Embedding}
	writeJSON(out, f)
	fmt.Printf("EMBED-QUERY-OK: qkey=%s model=%s dim=%d wrote %s\n", qkey, r.Model, len(r.Data[0].Embedding), out)
}

// ---- cosine retrieval (pure, deterministic — the retriever) ----

func l2norm(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x * x
	}
	return math.Sqrt(s)
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	na, nb := l2norm(a), l2norm(b)
	if na == 0 || nb == 0 {
		return math.NaN()
	}
	return dot / (na * nb)
}

type rankedDoc struct {
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type retrievalFile struct {
	Query  string      `json:"query"`
	QKey   string      `json:"qkey"`
	Ranked []rankedDoc `json:"ranked"`
}

func cmdRetrieve() {
	if len(os.Args) < 6 {
		fatal("usage: retrieve <corpus-emb.json> <query-emb.json> <qkey> <out.json>")
	}
	corpusPath, queryPath, qkey, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	var ce corpusEmbeddingsFile
	readJSON(corpusPath, &ce)
	var qe queryEmbedFile
	readJSON(queryPath, &qe)
	if qe.QKey != qkey {
		fatal("query-emb.json qkey=%s does not match requested qkey=%s", qe.QKey, qkey)
	}

	ranked := make([]rankedDoc, len(ce.Items))
	for i, it := range ce.Items {
		ranked[i] = rankedDoc{ID: it.ID, Text: it.Text, Score: cosine(qe.Vector, it.Vector)}
	}
	// REAL cosine ranking over REAL TEI embeddings — not a hardcoded pick.
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	f := retrievalFile{Query: qe.Query, QKey: qkey, Ranked: ranked}
	writeJSON(out, f)
	fmt.Printf("RETRIEVE-OK: qkey=%s top1=%s score=%.4f (of %d docs) wrote %s\n",
		qkey, ranked[0].ID, ranked[0].Score, len(ranked), out)
	for i, r := range ranked {
		fmt.Printf("  rank%d id=%s score=%.4f\n", i+1, r.ID, r.Score)
	}
}

func cmdCheckRetrieval() {
	if len(os.Args) < 4 {
		fatal("usage: checkretrieval <retrieval.json> <qkey>")
	}
	path, qkey := os.Args[2], os.Args[3]
	var rf retrievalFile
	readJSON(path, &rf)
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	if len(rf.Ranked) == 0 {
		fmt.Println("[RETRIEVAL-CHECK] FAIL: empty ranking")
		os.Exit(1)
	}
	top1 := rf.Ranked[0]
	if top1.ID == q.ExpectDocID {
		fmt.Printf("[RETRIEVAL-CHECK] PASS: qkey=%s real-cosine top1=%s (score=%.4f) == expected %s\n",
			qkey, top1.ID, top1.Score, q.ExpectDocID)
		return
	}
	fmt.Printf("[RETRIEVAL-CHECK] FAIL: qkey=%s real-cosine top1=%s (score=%.4f) != expected %s\n",
		qkey, top1.ID, top1.Score, q.ExpectDocID)
	os.Exit(1)
}

// ---- LLM chat completions (the coder, live at :18434 — read-only, never
// restarted/stopped, §11.4.122/§11.4.119) ----

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Model   string       `json:"model"`
	ID      string       `json:"id"`
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// coderModel reads the served model id from the LIVE coder itself (never a
// hardcoded guess — §11.4.6 / CONST-046).
func coderModel(base string) string {
	httpc := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpc.Get(base + "/v1/models")
	if err != nil {
		fatal("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fatal("GET /v1/models status=%d body=%s", resp.StatusCode, string(body))
	}
	var mr modelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		fatal("parse /v1/models: %v", err)
	}
	if len(mr.Data) == 0 {
		fatal("coder /v1/models returned zero models")
	}
	return mr.Data[0].ID
}

func doChat(base string, messages []chatMessage) chatResponse {
	model := coderModel(base)
	reqBody, _ := json.Marshal(chatRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   100,
		Temperature: 0,
	})
	httpc := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpc.Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fatal("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fatal("POST /v1/chat/completions status=%d body=%s", resp.StatusCode, string(body))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		fatal("parse chat response: %v", err)
	}
	return cr
}

func cmdRed() {
	if len(os.Args) < 5 {
		fatal("usage: red <coder-base-url> <qkey> <out.json>")
	}
	base, qkey, out := os.Args[2], os.Args[3], os.Args[4]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	// RED baseline: the SAME question, NO retrieved context. The coder must
	// NOT know the invented fact (§11.4.115 RED-baseline-on-the-broken
	// scenario: "broken" here = "no grounding provided").
	cr := doChat(base, []chatMessage{
		{Role: "user", Content: q.Text + " Answer in one short sentence."},
	})
	writeJSON(out, cr)
	answer := ""
	if len(cr.Choices) > 0 {
		answer = cr.Choices[0].Message.Content
	}
	fmt.Printf("RED answer (qkey=%s, no context): %q\n", qkey, answer)
	if containsToken(answer, q.ExpectToken) {
		fmt.Printf("[RED] RED-VIOLATION: qkey=%s coder produced the invented token %q WITHOUT context — the fact is not genuinely invented/unknown, RAG proof is invalidated for this query\n", qkey, q.ExpectToken)
		os.Exit(1)
	}
	fmt.Printf("[RED] RED-OK: qkey=%s coder did NOT know %q without context (defect correctly reproduced — it has no way to answer)\n", qkey, q.ExpectToken)
}

func cmdGreen() {
	if len(os.Args) < 6 {
		fatal("usage: green <coder-base-url> <retrieval.json> <qkey> <out.json>")
	}
	base, retrievalPath, qkey, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var rf retrievalFile
	readJSON(retrievalPath, &rf)
	if len(rf.Ranked) == 0 {
		fatal("empty retrieval ranking in %s", retrievalPath)
	}
	// Stuff the top-2 retrieved docs into the grounded prompt context.
	topN := 2
	if len(rf.Ranked) < topN {
		topN = len(rf.Ranked)
	}
	var ctxLines []string
	for i := 0; i < topN; i++ {
		ctxLines = append(ctxLines, fmt.Sprintf("%d. %s", i+1, rf.Ranked[i].Text))
	}
	system := "You are a helpful assistant. Answer the user's question using ONLY the information " +
		"in the CONTEXT below. Quote the exact term or name from the context in your answer. " +
		"If the context does not contain the answer, say \"I don't know.\" Do not use any information " +
		"beyond the given context."
	user := "CONTEXT:\n" + strings.Join(ctxLines, "\n") + "\n\nQUESTION: " + q.Text

	cr := doChat(base, []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
	writeJSON(out, cr)
	answer := ""
	if len(cr.Choices) > 0 {
		answer = cr.Choices[0].Message.Content
	}
	fmt.Printf("GREEN answer (qkey=%s, grounded on top-%d retrieved docs): %q\n", qkey, topN, answer)
	if containsToken(answer, q.ExpectToken) {
		fmt.Printf("[GREEN] GREEN-OK: qkey=%s grounded answer contains invented token %q\n", qkey, q.ExpectToken)
		return
	}
	fmt.Printf("[GREEN] GREEN-FAIL: qkey=%s grounded answer does NOT contain invented token %q\n", qkey, q.ExpectToken)
	os.Exit(1)
}

// ---- normalization (handles hyphen/space/case variance in phrasing,
// §11.4.6 — assert on the invented TOKEN, not an exact string) ----

func normalizeToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsToken(haystack, needle string) bool {
	if strings.TrimSpace(needle) == "" {
		return false
	}
	return strings.Contains(normalizeToken(haystack), normalizeToken(needle))
}

// ---- the RAG runtime-signature analyzer (§11.4.107(10) / §11.4.108) ----
//
// PASS requires BOTH: (1) the retriever's real top-1 document is the
// expected fact-bearing document, AND (2) the generated answer genuinely
// contains the invented fact token. Either condition failing means the RAG
// pipeline did not genuinely ground the generation in the retrieved fact.

type analyzeResult struct {
	pass        bool
	reasons     []string
	top1ID      string
	top1Score   float64
	answer      string
	tokenFound  bool
	retrievalOK bool
}

func analyze(rf retrievalFile, cr chatResponse, q queryFixture) analyzeResult {
	res := analyzeResult{pass: true}
	if len(rf.Ranked) == 0 {
		res.pass = false
		res.reasons = append(res.reasons, "empty retrieval ranking")
	} else {
		res.top1ID = rf.Ranked[0].ID
		res.top1Score = rf.Ranked[0].Score
		res.retrievalOK = res.top1ID == q.ExpectDocID
		if !res.retrievalOK {
			res.pass = false
			res.reasons = append(res.reasons, fmt.Sprintf(
				"retrieval top-1 %q != expected fact-bearing doc %q", res.top1ID, q.ExpectDocID))
		}
	}
	if len(cr.Choices) > 0 {
		res.answer = cr.Choices[0].Message.Content
	}
	if strings.TrimSpace(res.answer) == "" {
		res.pass = false
		res.reasons = append(res.reasons, "empty generated answer")
	}
	res.tokenFound = containsToken(res.answer, q.ExpectToken)
	if !res.tokenFound {
		res.pass = false
		res.reasons = append(res.reasons, fmt.Sprintf(
			"answer does not contain invented token %q", q.ExpectToken))
	}
	return res
}

func printAnalyze(tag string, res analyzeResult) {
	verdict := "PASS"
	if !res.pass {
		verdict = "FAIL"
	}
	fmt.Printf("[%s] %s top1=%s(score=%.4f, ok=%v) tokenFound=%v answer=%q\n",
		tag, verdict, res.top1ID, res.top1Score, res.retrievalOK, res.tokenFound, res.answer)
	for _, r := range res.reasons {
		fmt.Printf("    reason: %s\n", r)
	}
}

func cmdAnalyze() {
	if len(os.Args) < 5 {
		fatal("usage: analyze <retrieval.json> <generation.json> <qkey>")
	}
	retrievalPath, genPath, qkey := os.Args[2], os.Args[3], os.Args[4]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var rf retrievalFile
	readJSON(retrievalPath, &rf)
	var cr chatResponse
	readJSON(genPath, &cr)
	res := analyze(rf, cr, q)
	printAnalyze("RAG-RUNTIME-SIGNATURE", res)
	if !res.pass {
		os.Exit(1)
	}
}

// cmdSelfValidate is the §11.4.107(10) analyzer mutation-proofing: the
// golden-good REAL captured retrieval+generation MUST PASS, and each
// deliberately-degraded golden-bad variant MUST FAIL. If a bad variant
// PASSes, the analyzer is a bluff gate and this command exits non-zero.
func cmdSelfValidate() {
	if len(os.Args) < 5 {
		fatal("usage: selfvalidate <good-retrieval.json> <good-generation.json> <qkey>")
	}
	retrievalPath, genPath, qkey := os.Args[2], os.Args[3], os.Args[4]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var goodRF retrievalFile
	readJSON(retrievalPath, &goodRF)
	var goodCR chatResponse
	readJSON(genPath, &goodCR)

	ok2 := true

	// golden-good MUST PASS.
	gr := analyze(goodRF, goodCR, q)
	printAnalyze("GOLDEN-GOOD(expect PASS)", gr)
	if !gr.pass {
		ok2 = false
		fmt.Println("    SELF-VALIDATION VIOLATION: golden-good did not PASS")
	}

	// golden-bad (a): answer WITHOUT the fact.
	noFactCR := cloneChat(goodCR)
	if len(noFactCR.Choices) > 0 {
		noFactCR.Choices[0].Message.Content = "I don't know based on the given context."
	}
	ar := analyze(goodRF, noFactCR, q)
	printAnalyze("GOLDEN-BAD-NO-FACT(expect FAIL)", ar)
	if ar.pass {
		ok2 = false
		fmt.Println("    SELF-VALIDATION VIOLATION: no-fact answer PASSed the analyzer")
	}

	// golden-bad (b): retrieval returns the WRONG (irrelevant) document.
	wrongRF := cloneRetrieval(goodRF)
	if len(wrongRF.Ranked) > 0 {
		wrongRF.Ranked[0] = rankedDoc{
			ID:    q.WrongDistract,
			Text:  corpusText(q.WrongDistract),
			Score: wrongRF.Ranked[0].Score, // same score shape, wrong identity
		}
	}
	br := analyze(wrongRF, goodCR, q)
	printAnalyze("GOLDEN-BAD-WRONG-RETRIEVAL(expect FAIL)", br)
	if br.pass {
		ok2 = false
		fmt.Println("    SELF-VALIDATION VIOLATION: wrong-document retrieval PASSed the analyzer")
	}

	// golden-bad (c): empty answer.
	emptyCR := cloneChat(goodCR)
	if len(emptyCR.Choices) > 0 {
		emptyCR.Choices[0].Message.Content = ""
	}
	cr := analyze(goodRF, emptyCR, q)
	printAnalyze("GOLDEN-BAD-EMPTY-ANSWER(expect FAIL)", cr)
	if cr.pass {
		ok2 = false
		fmt.Println("    SELF-VALIDATION VIOLATION: empty answer PASSed the analyzer")
	}

	if !ok2 {
		fmt.Println("[SELF-VALIDATION] FAIL")
		os.Exit(1)
	}
	fmt.Println("[SELF-VALIDATION] PASS: analyzer PASSes golden-good and FAILs all golden-bad fixtures")
}

func cloneChat(r chatResponse) chatResponse {
	out := r
	out.Choices = make([]chatChoice, len(r.Choices))
	copy(out.Choices, r.Choices)
	return out
}

func cloneRetrieval(r retrievalFile) retrievalFile {
	out := r
	out.Ranked = make([]rankedDoc, len(r.Ranked))
	copy(out.Ranked, r.Ranked)
	return out
}

// ---- small JSON helpers ----

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fatal("write %s: %v", path, err)
	}
}

func readJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		fatal("parse %s: %v", path, err)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(2)
}
