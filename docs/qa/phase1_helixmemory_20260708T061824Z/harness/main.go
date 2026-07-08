// Phase-1 HelixMemory bring-up proof harness.
//
// Composes THREE already-proven local building blocks into a minimal
// reference implementation of the mem0-style memory mechanism (embed ->
// persist vector+text -> embed query -> similarity search -> ground
// generation): a dedicated Postgres+pgvector store (booted via the
// containers submodule, §11.4.76/§11.4.161), a dedicated CPU TEI embeddings
// service (HF Text-Embeddings-Inference, BAAI/bge-small-en-v1.5, dim 384 —
// see docs/qa/phase3_embeddings_20260706/RESULTS.md), and the live coder LLM
// (helixllm-coder, http://localhost:18434 — read-only, never
// restarted/stopped, §11.4.122/§11.4.119).
//
// HONEST SCOPE NOTE (§11.4.6/§11.4.150): this harness does NOT install or
// invoke the upstream `mem0` Python package nor the `graphiti-core` /
// Graphiti-MCP-server codebase. It implements the SAME underlying mechanism
// those projects use (a fact is embedded and persisted; a later query is
// embedded and matched by vector similarity; the top match grounds a real
// LLM generation) using the SAME class of backing store the mem0 OSS
// self-hosted server itself uses for its vector layer (Postgres + pgvector).
// See HELIXMEMORY_PROVIDER.md for the full recommended production stack and
// the follow-on work items to wire the literal upstream packages.
//
// The unfakeable proof (§11.4.6, §11.4.107(10), §11.4.115): two "remember
// this" facts are invented for this proof, plus two topic-adjacent distractor
// facts, so retrieval must genuinely discriminate. RED baseline asks the
// coder the same recall questions with NOTHING stored/retrieved -> it must
// fail to produce the invented tokens. GREEN stores all four facts (real TEI
// embeddings, real Postgres/pgvector persistence), then for each query embeds
// it, retrieves the real top-1 match via pgvector cosine distance, grounds a
// prompt with it, and generates on the SAME live coder -> the answer must
// contain the invented token. A self-validation pass proves the analyzer
// genuinely discriminates: golden-good PASSes; three golden-bad variants
// (no-fact answer, wrong-document retrieval, empty answer) MUST FAIL.
//
// Subcommands:
//
//	boot-up   <compose-file> <project>                       boot postgres+tei via containers submodule
//	boot-down <compose-file> <project>                       tear down (single-owner cleanup)
//	boot-status <compose-file> <project>                     print service status
//	db-init   <pg-conninfo>                                  CREATE EXTENSION vector + memory_facts table
//	remember-all <tei-base-url> <pg-conninfo> <out.json>     embed + persist the whole fixture fact set
//	embed-query  <tei-base-url> <qkey> <out.json>            embed one recall query (qkey: q1|q2)
//	recall    <pg-conninfo> <query-emb.json> <qkey> <out.json>
//	                                                          real pgvector cosine rank, all facts
//	red       <coder-base-url> <qkey> <out.json>             ask bare recall question, no memory
//	green     <coder-base-url> <retrieval.json> <qkey> <out.json>
//	                                                          grounded-on-recalled-memory prompt -> generate
//	analyze   <retrieval.json> <generation.json> <qkey>      the memory runtime-signature verdict
//	checkretrieval <retrieval.json> <qkey>                   assert real top-1 == expected fact
//	selfvalidate <good-retrieval.json> <good-generation.json> <qkey>
//	                                                          §11.4.107(10) analyzer self-check
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"digital.vasic.containers/pkg/compose"
)

// ---- fixture memory facts + recall queries (TEST FIXTURES, not user-facing
// content — legitimately literal here; they are the probe, not product
// text). Two facts carry an INVENTED fact the served LLM cannot know from
// training; two are topic-adjacent-or-unrelated distractors so retrieval
// must genuinely discriminate. ----

type memoryFact struct {
	ID   string
	Text string
}

var facts = []memoryFact{
	{"mem_region", "Remember that my preferred deployment region for HelixLLM is called Emberfall-Station."},
	{"mem_alias", "Remember that my internal alias for the coder agent is Wraithloom."},
	{"mem_lunch", "Remember that I usually eat lunch at noon."},
	{"mem_color", "Remember that my favorite color is teal."},
}

type recallQuery struct {
	Text          string
	ExpectFactID  string
	ExpectToken   string
	WrongDistract string // for the golden-bad "wrong document retrieved" mutation
}

var queries = map[string]recallQuery{
	"q1": {
		Text:          "What is my preferred deployment region for HelixLLM?",
		ExpectFactID:  "mem_region",
		ExpectToken:   "Emberfall-Station",
		WrongDistract: "mem_lunch",
	},
	"q2": {
		Text:          "What is my internal alias for the coder agent?",
		ExpectFactID:  "mem_alias",
		ExpectToken:   "Wraithloom",
		WrongDistract: "mem_color",
	},
}

func factText(id string) string {
	for _, f := range facts {
		if f.ID == id {
			return f.Text
		}
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: phase1helixmemory <subcommand> [args...]")
	}
	switch os.Args[1] {
	case "boot-up":
		cmdBoot(true)
	case "boot-down":
		cmdBoot(false)
	case "boot-status":
		cmdStatus()
	case "db-init":
		cmdDBInit()
	case "remember-all":
		cmdRememberAll()
	case "embed-query":
		cmdEmbedQuery()
	case "recall":
		cmdRecall()
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
		Services: []string{"pg-helixmemory", "tei-helixmemory"},
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
		fmt.Printf("UP-OK: %s pg-helixmemory+tei-helixmemory via containers submodule orchestrator\n", p.Name)
		return
	}
	if err := orch.Down(ctx, p,
		compose.WithDownRemoveVolumes(false), // keep the shared HF-model cache
		compose.WithDownRemoveOrphans(true),
	); err != nil {
		fatal("compose down: %v", err)
	}
	fmt.Printf("DOWN-OK: %s pg-helixmemory+tei-helixmemory via containers submodule orchestrator\n", p.Name)
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

// ---- Postgres/pgvector via the host `psql` client (no new Go module
// dependency — mirrors the RAG harness's zero-bloat-dependency spirit,
// §11.4.28 decoupling). SQL is written to a temp file and run via
// `psql -f`, never interpolated through a shell, to avoid quoting hazards. ----

func runPSQL(conninfo string, sql string) string {
	f, err := os.CreateTemp("", "phase1hm-*.sql")
	if err != nil {
		fatal("create temp sql file: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(sql); err != nil {
		fatal("write temp sql file: %v", err)
	}
	f.Close()
	cmd := exec.Command("psql", conninfo, "-v", "ON_ERROR_STOP=1", "-t", "-A", "-F", "\x01", "-f", f.Name())
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		fatal("psql failed: %v\nstderr: %s\nsql: %s", err, errOut.String(), sql)
	}
	return out.String()
}

func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func vectorLiteral(v []float64) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(x, 'f', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func cmdDBInit() {
	if len(os.Args) < 3 {
		fatal("usage: db-init <pg-conninfo>")
	}
	conninfo := os.Args[2]
	sql := `CREATE EXTENSION IF NOT EXISTS vector;
DROP TABLE IF EXISTS memory_facts;
CREATE TABLE memory_facts (
  fact_id    TEXT PRIMARY KEY,
  text       TEXT NOT NULL,
  embedding  vector(384) NOT NULL
);
`
	out := runPSQL(conninfo, sql)
	fmt.Printf("DB-INIT-OK: extension+table ready\n%s\n", out)
}

// ---- OpenAI-compatible /v1/embeddings shapes (TEI) — identical shape to
// the Phase-3 RAG harness's proven TEI client. ----

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
	reqBody, _ := json.Marshal(embeddingRequest{Model: "helix-memory-embed", Input: inputs})
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

// cmdRememberAll embeds + persists the WHOLE fixture fact set into Postgres
// in one pass — the "store" half of the memory mechanism.
func cmdRememberAll() {
	if len(os.Args) < 5 {
		fatal("usage: remember-all <tei-base-url> <pg-conninfo> <out.json>")
	}
	teiBase, conninfo, out := os.Args[2], os.Args[3], os.Args[4]
	texts := make([]string, len(facts))
	for i, f := range facts {
		texts[i] = f.Text
	}
	r := doEmbed(teiBase, texts)
	if len(r.Data) != len(facts) {
		fatal("expected %d fact vectors, got %d", len(facts), len(r.Data))
	}
	byIdx := map[int][]float64{}
	for _, d := range r.Data {
		byIdx[d.Index] = d.Embedding
	}
	var sql strings.Builder
	dim := 0
	for i, f := range facts {
		v := byIdx[i]
		if len(v) == 0 {
			fatal("missing embedding for fact index %d (%s)", i, f.ID)
		}
		dim = len(v)
		sql.WriteString(fmt.Sprintf(
			"INSERT INTO memory_facts (fact_id, text, embedding) VALUES ('%s', '%s', '%s'::vector);\n",
			sqlEscape(f.ID), sqlEscape(f.Text), vectorLiteral(v)))
	}
	runPSQL(conninfo, sql.String())

	type rememberedFact struct {
		ID  string `json:"id"`
		Dim int    `json:"dim"`
	}
	type rememberAllFile struct {
		Model  string           `json:"model"`
		Dim    int              `json:"dim"`
		Remembered []rememberedFact `json:"remembered"`
	}
	rf := rememberAllFile{Model: r.Model, Dim: dim}
	for _, f := range facts {
		rf.Remembered = append(rf.Remembered, rememberedFact{ID: f.ID, Dim: dim})
	}
	writeJSON(out, rf)
	fmt.Printf("REMEMBER-ALL-OK: model=%s dim=%d facts=%d persisted to Postgres wrote %s\n",
		r.Model, dim, len(facts), out)
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

// ---- pgvector retrieval (real cosine distance via Postgres, the "recall" ----
// half of the memory mechanism) ----

type rankedFact struct {
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type retrievalFile struct {
	Query  string       `json:"query"`
	QKey   string       `json:"qkey"`
	Ranked []rankedFact `json:"ranked"`
}

func cmdRecall() {
	if len(os.Args) < 6 {
		fatal("usage: recall <pg-conninfo> <query-emb.json> <qkey> <out.json>")
	}
	conninfo, queryPath, qkey, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	var qe queryEmbedFile
	readJSON(queryPath, &qe)
	if qe.QKey != qkey {
		fatal("query-emb.json qkey=%s does not match requested qkey=%s", qe.QKey, qkey)
	}
	lit := vectorLiteral(qe.Vector)
	sql := fmt.Sprintf(
		"SELECT fact_id, text, 1 - (embedding <=> '%s'::vector) FROM memory_facts ORDER BY embedding <=> '%s'::vector;\n",
		lit, lit)
	rawOut := runPSQL(conninfo, sql)

	var ranked []rankedFact
	for _, line := range strings.Split(strings.TrimRight(rawOut, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x01")
		if len(parts) != 3 {
			fatal("unexpected psql row shape: %q", line)
		}
		score, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if err != nil {
			fatal("parse score from psql row %q: %v", line, err)
		}
		ranked = append(ranked, rankedFact{ID: parts[0], Text: parts[1], Score: score})
	}
	// Real pgvector-computed ranking is ALREADY in the SQL's ORDER BY — this
	// sort is a redundant, defensive assertion the client-side order agrees
	// with the DB's own cosine-distance order (never a hardcoded pick).
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	if len(ranked) == 0 {
		fatal("empty retrieval — no rows returned from memory_facts")
	}
	f := retrievalFile{Query: qe.Query, QKey: qkey, Ranked: ranked}
	writeJSON(out, f)
	fmt.Printf("RECALL-OK: qkey=%s top1=%s score=%.4f (of %d facts) wrote %s\n",
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
	if top1.ID == q.ExpectFactID {
		fmt.Printf("[RETRIEVAL-CHECK] PASS: qkey=%s real-pgvector top1=%s (score=%.4f) == expected %s\n",
			qkey, top1.ID, top1.Score, q.ExpectFactID)
		return
	}
	fmt.Printf("[RETRIEVAL-CHECK] FAIL: qkey=%s real-pgvector top1=%s (score=%.4f) != expected %s\n",
		qkey, top1.ID, top1.Score, q.ExpectFactID)
	os.Exit(1)
}

// ---- LLM chat completions (the coder, live at :18434 — read-only, never
// restarted/stopped, §11.4.122/§11.4.119) — identical shape to the Phase-3
// RAG harness's proven client. ----

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
	// RED baseline: the SAME recall question, NOTHING stored/retrieved. The
	// coder must NOT know the invented fact (§11.4.115 RED-baseline-on-the-
	// broken scenario: "broken" here = "no memory recall performed").
	cr := doChat(base, []chatMessage{
		{Role: "user", Content: q.Text + " Answer in one short sentence."},
	})
	writeJSON(out, cr)
	answer := ""
	if len(cr.Choices) > 0 {
		answer = cr.Choices[0].Message.Content
	}
	fmt.Printf("RED answer (qkey=%s, no memory): %q\n", qkey, answer)
	if containsToken(answer, q.ExpectToken) {
		fmt.Printf("[RED] RED-VIOLATION: qkey=%s coder produced the invented token %q WITHOUT memory recall — the fact is not genuinely invented/unknown, memory proof is invalidated for this query\n", qkey, q.ExpectToken)
		os.Exit(1)
	}
	fmt.Printf("[RED] RED-OK: qkey=%s coder did NOT know %q without memory recall (defect correctly reproduced — it has no way to answer)\n", qkey, q.ExpectToken)
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
	// Ground with the top-1 RECALLED memory (mem0-style: single strongest
	// matching stored fact), not a stuffed multi-doc context.
	top1 := rf.Ranked[0]
	system := "You are a helpful assistant with access to the user's stored memory. Answer the user's question " +
		"using ONLY the RECALLED MEMORY below. Quote the exact term or name from the recalled memory in your " +
		"answer. If the recalled memory does not contain the answer, say \"I don't know.\" Do not use any " +
		"information beyond the recalled memory."
	user := "RECALLED MEMORY:\n" + top1.Text + "\n\nQUESTION: " + q.Text

	cr := doChat(base, []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
	writeJSON(out, cr)
	answer := ""
	if len(cr.Choices) > 0 {
		answer = cr.Choices[0].Message.Content
	}
	fmt.Printf("GREEN answer (qkey=%s, grounded on recalled memory %s): %q\n", qkey, top1.ID, answer)
	if containsToken(answer, q.ExpectToken) {
		fmt.Printf("[GREEN] GREEN-OK: qkey=%s memory-grounded answer contains invented token %q\n", qkey, q.ExpectToken)
		return
	}
	fmt.Printf("[GREEN] GREEN-FAIL: qkey=%s memory-grounded answer does NOT contain invented token %q\n", qkey, q.ExpectToken)
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

// ---- the memory runtime-signature analyzer (§11.4.107(10) / §11.4.108) ----
//
// PASS requires BOTH: (1) the retriever's real top-1 fact is the expected
// fact-bearing memory, AND (2) the generated answer genuinely contains the
// invented fact token. Either condition failing means the memory pipeline
// did not genuinely ground the generation in the recalled fact.

type analyzeResult struct {
	pass        bool
	reasons     []string
	top1ID      string
	top1Score   float64
	answer      string
	tokenFound  bool
	retrievalOK bool
}

func analyze(rf retrievalFile, cr chatResponse, q recallQuery) analyzeResult {
	res := analyzeResult{pass: true}
	if len(rf.Ranked) == 0 {
		res.pass = false
		res.reasons = append(res.reasons, "empty retrieval ranking")
	} else {
		res.top1ID = rf.Ranked[0].ID
		res.top1Score = rf.Ranked[0].Score
		res.retrievalOK = res.top1ID == q.ExpectFactID
		if !res.retrievalOK {
			res.pass = false
			res.reasons = append(res.reasons, fmt.Sprintf(
				"recall top-1 %q != expected fact-bearing memory %q", res.top1ID, q.ExpectFactID))
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
	printAnalyze("MEMORY-RUNTIME-SIGNATURE", res)
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
		noFactCR.Choices[0].Message.Content = "I don't know based on the given memory."
	}
	ar := analyze(goodRF, noFactCR, q)
	printAnalyze("GOLDEN-BAD-NO-FACT(expect FAIL)", ar)
	if ar.pass {
		ok2 = false
		fmt.Println("    SELF-VALIDATION VIOLATION: no-fact answer PASSed the analyzer")
	}

	// golden-bad (b): recall returns the WRONG (irrelevant) memory.
	wrongRF := cloneRetrieval(goodRF)
	if len(wrongRF.Ranked) > 0 {
		wrongRF.Ranked[0] = rankedFact{
			ID:    q.WrongDistract,
			Text:  factText(q.WrongDistract),
			Score: wrongRF.Ranked[0].Score, // same score shape, wrong identity
		}
	}
	br := analyze(wrongRF, goodCR, q)
	printAnalyze("GOLDEN-BAD-WRONG-RECALL(expect FAIL)", br)
	if br.pass {
		ok2 = false
		fmt.Println("    SELF-VALIDATION VIOLATION: wrong-memory recall PASSed the analyzer")
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
	out.Ranked = make([]rankedFact, len(r.Ranked))
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
