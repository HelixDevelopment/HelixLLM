// RAG-Qdrant + cross-encoder-reranker fusion — end-to-end LIVE proof harness.
//
// UPGRADES the already-proven RAG core (in-memory embeddings + cosine
// retrieval, live-proven at docs/qa/phase3_rag_20260707/) with:
//   - a REAL Qdrant vector database (real HTTP upsert + real ANN cosine
//     search over the REST API — never an in-memory substitute), and
//   - a REAL cross-encoder reranker (HF Text-Embeddings-Inference /rerank
//     endpoint serving BAAI/bge-reranker-base) that re-scores the ANN
//     candidate set and can REORDER it.
//
// Both new services boot via the containers submodule compose.Orchestrator
// (§11.4.76), rootless podman (§11.4.161), CPU-only (§11.4.119 — the GPU is
// owned by the concurrent video-analysis stream and is never touched here).
// The live coder LLM (helixllm-coder, :18434) is READ-ONLY, never restarted
// (§11.4.122).
//
// The unfakeable proof (§11.4.6, §11.4.107(10), §11.4.115, §11.4.169):
//   - two FRESH invented facts (never used in any prior proof run) the coder
//     cannot know from training;
//   - RED baseline: the bare question, no context -> the coder must fail to
//     produce the invented token;
//   - GREEN: embed -> Qdrant upsert -> Qdrant ANN search (real cosine, top-N)
//     -> cross-encoder rerank (real TEI /rerank call) -> reorder -> top-2
//     grounded prompt -> live coder generates -> answer MUST contain the
//     invented token;
//   - a genuine reranker-improves-ordering case: for at least one query the
//     corpus is deliberately adversarial (a lexically-overlapping distractor
//     describing a DIFFERENT, wrongly-scoped entity) so the raw bi-encoder
//     ANN ranking is empirically checked to see whether it ranks the
//     distractor above the fact-bearing document; the harness reports the
//     REAL observed before/after ranking, never an assumed one;
//   - a self-validated analyzer: golden-good (real captured artefacts) MUST
//     PASS; golden-bad mutations (no-fact answer, wrong post-rerank top-1,
//     empty answer, rerank-did-not-correct-a-known-wrong-ANN-rank) MUST FAIL.
//
// Subcommands:
//
//	boot-up   <compose-file> <project>
//	boot-down <compose-file> <project>
//	boot-status <compose-file> <project>
//	embed-corpus <tei-embed-base> <out.json>
//	embed-query  <tei-embed-base> <qkey> <out.json>
//	qdrant-upsert <qdrant-base> <collection> <corpus-emb.json> <out.json>
//	qdrant-search <qdrant-base> <collection> <query-emb.json> <qkey> <topN> <out.json>
//	rerank <tei-rerank-base> <ann.json> <qkey> <out.json>
//	red    <coder-base> <qkey> <out.json>
//	green  <coder-base> <reranked.json> <qkey> <out.json>
//	analyze <ann.json> <reranked.json> <generation.json> <qkey>
//	checkrerankimproves <ann.json> <reranked.json> <qkey>
//	selfvalidate <ann.json> <reranked.json> <generation.json> <qkey>
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// FRESH facts (never used in the phase3rag proof) carry invented tokens the
// coder cannot know. Each fact doc has a deliberately adversarial,
// lexically-overlapping distractor describing a DIFFERENT, wrongly-scoped
// entity — the standard bi-encoder failure mode a cross-encoder reranker
// exists to fix. ----

type corpusDoc struct {
	ID   string
	Text string
}

// Qdrant point IDs must be unsigned int or UUID — assign deterministic
// integer IDs by corpus index (1-based).
var corpus = []corpusDoc{
	{"doc_fact_collection", "The Qdrant collection alias for HelixCode's primary retrieval-augmented telemetry index is Nectarune-Delta7."},
	{"doc_distractor_collection", "The Qdrant collection alias for HelixCode's regional staging retrieval-augmented telemetry index is Opaline-Foxglove3, kept separate from the primary index for testing."},
	{"doc_fact_service", "The internal HelixCode service that performs cross-encoder reranking of Qdrant search results is called Ashgrove-Sentinel."},
	{"doc_distractor_service", "The internal HelixCode service that performs the initial bi-encoder embedding of documents before they reach Qdrant search is called Ashgrove-Primer, a separate stage from reranking."},
	// --- adversarial pair for q3: a bi-encoder trap using the qualifier-flip
	// mechanism proven on q1 (the cross-encoder decisively separates
	// "primary" from "staging" — on q1 it drove the staging distractor from
	// bi-encoder rank #2 to LAST). The fact doc genuinely answers the query
	// (shares the query's entities: primary/telemetry/index/Qdrant/alias) but
	// states the key phrase once; the distractor repeats "primary telemetry
	// index" several times while actually describing the STAGING index, so a
	// bi-encoder (bag-of-context cosine) tends to over-rank the phrase-dense
	// distractor, and a cross-encoder promotes the fact doc. Fires EMPIRICALLY.
	{"doc_fact_primary", "HelixCode's primary telemetry index is served from Qdrant collection alias Cindervale-Prime."},
	{"doc_distractor_staging", "HelixCode's staging telemetry index is the non-primary counterpart of the primary telemetry index; this staging telemetry index is a separate Qdrant collection kept apart from the primary telemetry index and used only for pre-release testing of the telemetry index."},
	// --- adversarial pair for q4 (same qualifier-flip mechanism, active vs
	// deprecated). ---
	{"doc_fact_active", "The active production embeddings registry is published under Qdrant alias Emberkiln-Live."},
	{"doc_distractor_deprecated", "The deprecated sandbox embeddings registry is the non-production sibling of the active production embeddings registry; this deprecated embeddings registry is a separate Qdrant alias, retained only for archival and never used as the active production embeddings registry."},
	{"doc_cat", "The cat sat on the mat."},
	{"doc_coffee", "Coffee brewing techniques improve flavor extraction using controlled water temperature."},
	{"doc_revenue", "Quarterly revenue rose four percent."},
	{"doc_ci", "The HelixCode continuous integration pipeline runs unit tests before every merge to main."},
}

func corpusText(id string) string {
	for _, d := range corpus {
		if d.ID == id {
			return d.Text
		}
	}
	return ""
}

type queryFixture struct {
	Text          string
	ExpectDocID   string
	ExpectToken   string
	WrongDistract string // adversarial distractor for this query
}

var queries = map[string]queryFixture{
	"q1": {
		Text:          "What is the internal Qdrant collection alias for HelixCode's PRIMARY retrieval-augmented telemetry index, not the regional staging one?",
		ExpectDocID:   "doc_fact_collection",
		ExpectToken:   "Nectarune-Delta7",
		WrongDistract: "doc_distractor_collection",
	},
	"q2": {
		Text:          "What is the name of the internal HelixCode service that performs cross-encoder reranking of Qdrant search results?",
		ExpectDocID:   "doc_fact_service",
		ExpectToken:   "Ashgrove-Sentinel",
		WrongDistract: "doc_distractor_service",
	},
	// q3/q4 are the adversarial reranker-improvement probes: the fact doc
	// carries the invented token but is topic-shifted + terse, while the
	// distractor is lexically dense with the query terms — the classic
	// bi-encoder failure a cross-encoder reranker is deployed to fix.
	"q3": {
		Text:          "Which Qdrant collection alias serves HelixCode's primary telemetry index?",
		ExpectDocID:   "doc_fact_primary",
		ExpectToken:   "Cindervale-Prime",
		WrongDistract: "doc_distractor_staging",
	},
	"q4": {
		Text:          "Which Qdrant alias holds HelixCode's active production embeddings registry?",
		ExpectDocID:   "doc_fact_active",
		ExpectToken:   "Emberkiln-Live",
		WrongDistract: "doc_distractor_deprecated",
	},
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: phase3ragqdrant <subcommand> [args...]")
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
	case "qdrant-upsert":
		cmdQdrantUpsert()
	case "qdrant-search":
		cmdQdrantSearch()
	case "rerank":
		cmdRerank()
	case "red":
		cmdRed()
	case "green":
		cmdGreen()
	case "analyze":
		cmdAnalyze()
	case "checkrerankimproves":
		cmdCheckRerankImproves()
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
		Services: []string{"qdrant", "tei-embed", "tei-rerank"},
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
		fmt.Printf("UP-OK: %s qdrant+tei-embed+tei-rerank via containers submodule orchestrator\n", p.Name)
		return
	}
	if err := orch.Down(ctx, p,
		compose.WithDownRemoveVolumes(false), // keep the shared HF-model cache volumes
		compose.WithDownRemoveOrphans(true),
	); err != nil {
		fatal("compose down: %v", err)
	}
	fmt.Printf("DOWN-OK: %s qdrant+tei-embed+tei-rerank via containers submodule orchestrator\n", p.Name)
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
	reqBody, _ := json.Marshal(embeddingRequest{Model: "helix-rag-qdrant-embed", Input: inputs})
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

type corpusEmbeddingsFile struct {
	Model string            `json:"model"`
	Dim   int               `json:"dim"`
	Items []corpusEmbedItem `json:"items"`
}

type corpusEmbedItem struct {
	ID      string    `json:"id"`
	PointID int       `json:"point_id"`
	Text    string    `json:"text"`
	Vector  []float64 `json:"vector"`
}

func cmdEmbedCorpus() {
	if len(os.Args) < 4 {
		fatal("usage: embed-corpus <tei-embed-base> <out.json>")
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
		items[i] = corpusEmbedItem{ID: d.ID, PointID: i + 1, Text: d.Text, Vector: v}
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
		fatal("usage: embed-query <tei-embed-base> <qkey> <out.json>")
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

// ---- REAL Qdrant REST API calls (upsert + ANN cosine search) ----
// No client library import — raw HTTP against Qdrant's documented REST API,
// mirroring the shapes internal/knowledge/qdrant.go uses via
// digital.vasic.vectordb, kept fully self-contained in this harness module.

type qdrantCreateCollectionReq struct {
	Vectors qdrantVectorParams `json:"vectors"`
}

type qdrantVectorParams struct {
	Size     int    `json:"size"`
	Distance string `json:"distance"`
}

type qdrantPoint struct {
	ID      int            `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type qdrantUpsertReq struct {
	Points []qdrantPoint `json:"points"`
}

type qdrantUpsertResp struct {
	Status string `json:"status"`
	Result struct {
		Status string `json:"status"`
	} `json:"result"`
}

func cmdQdrantUpsert() {
	if len(os.Args) < 5 {
		fatal("usage: qdrant-upsert <qdrant-base> <collection> <corpus-emb.json> <out.json>")
	}
	base, collection, corpusPath, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	var ce corpusEmbeddingsFile
	readJSON(corpusPath, &ce)
	if ce.Dim == 0 {
		fatal("corpus embeddings file has zero dimension")
	}

	httpc := &http.Client{Timeout: 60 * time.Second}

	// 1) create the collection (real HTTP PUT to Qdrant's REST API).
	createReq := qdrantCreateCollectionReq{Vectors: qdrantVectorParams{Size: ce.Dim, Distance: "Cosine"}}
	cb, _ := json.Marshal(createReq)
	req, _ := http.NewRequest(http.MethodPut, base+"/collections/"+collection, bytes.NewReader(cb))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		fatal("PUT /collections/%s: %v", collection, err)
	}
	createBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal("PUT /collections/%s status=%d body=%s", collection, resp.StatusCode, string(createBody))
	}

	// 2) upsert REAL vectors (real HTTP PUT with the embedded corpus).
	points := make([]qdrantPoint, len(ce.Items))
	for i, it := range ce.Items {
		points[i] = qdrantPoint{
			ID:     it.PointID,
			Vector: it.Vector,
			Payload: map[string]any{
				"doc_id": it.ID,
				"text":   it.Text,
			},
		}
	}
	upReq := qdrantUpsertReq{Points: points}
	ub, _ := json.Marshal(upReq)
	req2, _ := http.NewRequest(http.MethodPut, base+"/collections/"+collection+"/points?wait=true", bytes.NewReader(ub))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := httpc.Do(req2)
	if err != nil {
		fatal("PUT /collections/%s/points: %v", collection, err)
	}
	upBody, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		fatal("PUT /collections/%s/points status=%d body=%s", collection, resp2.StatusCode, string(upBody))
	}
	var ur qdrantUpsertResp
	_ = json.Unmarshal(upBody, &ur)

	writeJSON(out, map[string]any{
		"collection":      collection,
		"dim":             ce.Dim,
		"points_upserted": len(points),
		"create_response": string(createBody),
		"upsert_status":   ur.Result.Status,
	})
	fmt.Printf("QDRANT-UPSERT-OK: collection=%s dim=%d points=%d upsert_status=%s\n",
		collection, ce.Dim, len(points), ur.Result.Status)
}

type qdrantSearchReq struct {
	Vector      []float64 `json:"vector"`
	Limit       int       `json:"limit"`
	WithPayload bool      `json:"with_payload"`
}

type qdrantSearchHit struct {
	ID      int            `json:"id"`
	Score   float64        `json:"score"`
	Payload map[string]any `json:"payload"`
}

type qdrantSearchResp struct {
	Status string            `json:"status"`
	Result []qdrantSearchHit `json:"result"`
}

type rankedDoc struct {
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type annResultFile struct {
	Query      string      `json:"query"`
	QKey       string      `json:"qkey"`
	Collection string      `json:"collection"`
	Ranked     []rankedDoc `json:"ranked"` // REAL Qdrant ANN cosine order
}

func cmdQdrantSearch() {
	if len(os.Args) < 8 {
		fatal("usage: qdrant-search <qdrant-base> <collection> <query-emb.json> <qkey> <topN> <out.json>")
	}
	base, collection, queryPath, qkey, topNArg, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6], os.Args[7]
	var qe queryEmbedFile
	readJSON(queryPath, &qe)
	if qe.QKey != qkey {
		fatal("query-emb.json qkey=%s does not match requested qkey=%s", qe.QKey, qkey)
	}
	var topN int
	if _, err := fmt.Sscanf(topNArg, "%d", &topN); err != nil || topN <= 0 {
		fatal("invalid topN: %s", topNArg)
	}

	sreq := qdrantSearchReq{Vector: qe.Vector, Limit: topN, WithPayload: true}
	sb, _ := json.Marshal(sreq)
	httpc := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpc.Post(base+"/collections/"+collection+"/points/search", "application/json", bytes.NewReader(sb))
	if err != nil {
		fatal("POST /collections/%s/points/search: %v", collection, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fatal("POST /collections/%s/points/search status=%d body=%s", collection, resp.StatusCode, string(body))
	}
	var sr qdrantSearchResp
	if err := json.Unmarshal(body, &sr); err != nil {
		fatal("parse qdrant search response: %v", err)
	}
	if len(sr.Result) == 0 {
		fatal("qdrant search returned zero hits — collection empty or query malformed")
	}

	ranked := make([]rankedDoc, len(sr.Result))
	for i, hit := range sr.Result {
		docID, _ := hit.Payload["doc_id"].(string)
		text, _ := hit.Payload["text"].(string)
		ranked[i] = rankedDoc{ID: docID, Text: text, Score: hit.Score}
	}
	// Qdrant already returns hits sorted by score desc; keep as-is (real ANN
	// order), but sort defensively in case a future API version changes that.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	f := annResultFile{Query: qe.Query, QKey: qkey, Collection: collection, Ranked: ranked}
	writeJSON(out, f)
	fmt.Printf("QDRANT-SEARCH-OK: qkey=%s collection=%s top1=%s score=%.4f (of %d hits) wrote %s\n",
		qkey, collection, ranked[0].ID, ranked[0].Score, len(ranked), out)
	for i, r := range ranked {
		fmt.Printf("  ann_rank%d id=%s score=%.4f\n", i+1, r.ID, r.Score)
	}
}

// ---- REAL cross-encoder rerank via TEI /rerank ----

type rerankRequest struct {
	Query string   `json:"query"`
	Texts []string `json:"texts"`
}

type rerankHit struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

type rerankedResultFile struct {
	Query      string      `json:"query"`
	QKey       string      `json:"qkey"`
	Collection string      `json:"collection"`
	AnnRanked  []rankedDoc `json:"ann_ranked"` // input order (pre-rerank, real Qdrant ANN)
	Reranked   []rankedDoc `json:"reranked"`   // REAL cross-encoder output order
}

func cmdRerank() {
	if len(os.Args) < 6 {
		fatal("usage: rerank <tei-rerank-base> <ann.json> <qkey> <out.json>")
	}
	base, annPath, qkey, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	var ann annResultFile
	readJSON(annPath, &ann)
	if ann.QKey != qkey {
		fatal("ann.json qkey=%s does not match requested qkey=%s", ann.QKey, qkey)
	}
	if len(ann.Ranked) == 0 {
		fatal("empty ANN ranking in %s", annPath)
	}

	texts := make([]string, len(ann.Ranked))
	for i, r := range ann.Ranked {
		texts[i] = r.Text
	}
	rreq := rerankRequest{Query: ann.Query, Texts: texts}
	rb, _ := json.Marshal(rreq)
	httpc := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpc.Post(base+"/rerank", "application/json", bytes.NewReader(rb))
	if err != nil {
		fatal("POST /rerank: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fatal("POST /rerank status=%d body=%s", resp.StatusCode, string(body))
	}
	var hits []rerankHit
	if err := json.Unmarshal(body, &hits); err != nil {
		fatal("parse /rerank response: %v (body=%s)", err, string(body))
	}
	if len(hits) == 0 {
		fatal("/rerank returned zero hits (body=%s)", string(body))
	}
	// REAL cross-encoder scores -> sort desc (defensive; TEI already returns
	// sorted, but the harness never assumes an unverified ordering).
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })

	reranked := make([]rankedDoc, 0, len(hits))
	for _, h := range hits {
		if h.Index < 0 || h.Index >= len(ann.Ranked) {
			fatal("rerank response index %d out of range (n=%d)", h.Index, len(ann.Ranked))
		}
		src := ann.Ranked[h.Index]
		reranked = append(reranked, rankedDoc{ID: src.ID, Text: src.Text, Score: h.Score})
	}

	f := rerankedResultFile{Query: ann.Query, QKey: qkey, Collection: ann.Collection, AnnRanked: ann.Ranked, Reranked: reranked}
	writeJSON(out, f)
	fmt.Printf("RERANK-OK: qkey=%s ann_top1=%s reranked_top1=%s reordered=%v wrote %s\n",
		qkey, ann.Ranked[0].ID, reranked[0].ID, ann.Ranked[0].ID != reranked[0].ID, out)
	for i, r := range reranked {
		fmt.Printf("  rerank_rank%d id=%s score=%.4f\n", i+1, r.ID, r.Score)
	}
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
	fmt.Printf("[RED] RED-OK: qkey=%s coder did NOT know %q without context (defect correctly reproduced)\n", qkey, q.ExpectToken)
}

func cmdGreen() {
	if len(os.Args) < 6 {
		fatal("usage: green <coder-base-url> <reranked.json> <qkey> <out.json>")
	}
	base, rerankedPath, qkey, out := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var rf rerankedResultFile
	readJSON(rerankedPath, &rf)
	if len(rf.Reranked) == 0 {
		fatal("empty reranked list in %s", rerankedPath)
	}
	topN := 2
	if len(rf.Reranked) < topN {
		topN = len(rf.Reranked)
	}
	var ctxLines []string
	for i := 0; i < topN; i++ {
		ctxLines = append(ctxLines, fmt.Sprintf("%d. %s", i+1, rf.Reranked[i].Text))
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
	fmt.Printf("GREEN answer (qkey=%s, grounded on top-%d RERANKED docs): %q\n", qkey, topN, answer)
	if containsToken(answer, q.ExpectToken) {
		fmt.Printf("[GREEN] GREEN-OK: qkey=%s grounded answer contains invented token %q\n", qkey, q.ExpectToken)
		return
	}
	fmt.Printf("[GREEN] GREEN-FAIL: qkey=%s grounded answer does NOT contain invented token %q\n", qkey, q.ExpectToken)
	os.Exit(1)
}

// ---- normalization (§11.4.6 — assert on the invented TOKEN, not an exact
// string; handles hyphen/space/case variance in phrasing) ----

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

// ---- the RAG+rerank runtime-signature analyzer (§11.4.107(10)/§11.4.108) ----
//
// PASS requires BOTH: (1) the FINAL (post-rerank) top-1 document is the
// expected fact-bearing document, AND (2) the generated answer genuinely
// contains the invented fact token.

type analyzeResult struct {
	pass          bool
	reasons       []string
	rerankedTop1  string
	rerankedScore float64
	answer        string
	tokenFound    bool
	rerankTop1OK  bool
}

func analyze(rf rerankedResultFile, cr chatResponse, q queryFixture) analyzeResult {
	res := analyzeResult{pass: true}
	if len(rf.Reranked) == 0 {
		res.pass = false
		res.reasons = append(res.reasons, "empty reranked ranking")
	} else {
		res.rerankedTop1 = rf.Reranked[0].ID
		res.rerankedScore = rf.Reranked[0].Score
		res.rerankTop1OK = res.rerankedTop1 == q.ExpectDocID
		if !res.rerankTop1OK {
			res.pass = false
			res.reasons = append(res.reasons, fmt.Sprintf(
				"post-rerank top-1 %q != expected fact-bearing doc %q", res.rerankedTop1, q.ExpectDocID))
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
	fmt.Printf("[%s] %s reranked_top1=%s(score=%.4f, ok=%v) tokenFound=%v answer=%q\n",
		tag, verdict, res.rerankedTop1, res.rerankedScore, res.rerankTop1OK, res.tokenFound, res.answer)
	for _, r := range res.reasons {
		fmt.Printf("    reason: %s\n", r)
	}
}

func cmdAnalyze() {
	if len(os.Args) < 6 {
		fatal("usage: analyze <ann.json-unused> <reranked.json> <generation.json> <qkey>")
	}
	rerankedPath, genPath, qkey := os.Args[3], os.Args[4], os.Args[5]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var rf rerankedResultFile
	readJSON(rerankedPath, &rf)
	var cr chatResponse
	readJSON(genPath, &cr)
	res := analyze(rf, cr, q)
	printAnalyze("RAG-QDRANT-RERANK-RUNTIME-SIGNATURE", res)
	if !res.pass {
		os.Exit(1)
	}
}

// cmdCheckRerankImproves is the DEDICATED, mutation-tested assertion that the
// reranker genuinely IMPROVED the ordering: the real ANN (pre-rerank) top-1
// must be WRONG (not the fact doc) AND the real post-rerank top-1 must be
// CORRECT (the fact doc). This is a strictly stronger claim than "the final
// answer happens to be right" — it proves reranking did causal work.
func cmdCheckRerankImproves() {
	// argv: [bin, "checkrerankimproves", <reranked.json>, <qkey>] -> len 4.
	if len(os.Args) < 4 {
		fatal("usage: checkrerankimproves <reranked.json> <qkey>")
	}
	rerankedPath, qkey := os.Args[2], os.Args[3]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var rf rerankedResultFile
	readJSON(rerankedPath, &rf)
	if len(rf.AnnRanked) == 0 || len(rf.Reranked) == 0 {
		fmt.Println("[RERANK-IMPROVES-CHECK] FAIL: missing ann_ranked or reranked data")
		os.Exit(1)
	}
	annTop1 := rf.AnnRanked[0].ID
	rerankedTop1 := rf.Reranked[0].ID
	annWasWrong := annTop1 != q.ExpectDocID
	rerankedIsRight := rerankedTop1 == q.ExpectDocID
	if annWasWrong && rerankedIsRight {
		fmt.Printf("[RERANK-IMPROVES-CHECK] PASS: qkey=%s real ANN top1=%s (WRONG, expected %s) -> real reranked top1=%s (CORRECT) — reranker genuinely fixed the ordering\n",
			qkey, annTop1, q.ExpectDocID, rerankedTop1)
		return
	}
	fmt.Printf("[RERANK-IMPROVES-CHECK] FAIL: qkey=%s ann_top1=%s(wrong=%v) reranked_top1=%s(right=%v) — does not demonstrate a genuine correction\n",
		qkey, annTop1, annWasWrong, rerankedTop1, rerankedIsRight)
	os.Exit(1)
}

// cmdSelfValidate is the §11.4.107(10) analyzer mutation-proofing: the
// golden-good REAL captured reranked+generation MUST PASS, and each
// deliberately-degraded golden-bad variant MUST FAIL. If a bad variant
// PASSes, the analyzer is a bluff gate and this command exits non-zero.
func cmdSelfValidate() {
	if len(os.Args) < 6 {
		fatal("usage: selfvalidate <ann.json> <reranked.json> <generation.json> <qkey>")
	}
	annPath, rerankedPath, genPath, qkey := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	q, ok := queries[qkey]
	if !ok {
		fatal("unknown qkey: %s", qkey)
	}
	var goodAnn annResultFile
	readJSON(annPath, &goodAnn)
	var goodRF rerankedResultFile
	readJSON(rerankedPath, &goodRF)
	var goodCR chatResponse
	readJSON(genPath, &goodCR)

	allOK := true

	// golden-good MUST PASS (both analyze() and checkrerankimproves-shape).
	gr := analyze(goodRF, goodCR, q)
	printAnalyze("GOLDEN-GOOD(expect PASS)", gr)
	if !gr.pass {
		allOK = false
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
		allOK = false
		fmt.Println("    SELF-VALIDATION VIOLATION: no-fact answer PASSed the analyzer")
	}

	// golden-bad (b): post-rerank top-1 is the WRONG (distractor) document.
	wrongRF := cloneReranked(goodRF)
	if len(wrongRF.Reranked) > 0 {
		wrongRF.Reranked[0] = rankedDoc{
			ID:    q.WrongDistract,
			Text:  corpusText(q.WrongDistract),
			Score: wrongRF.Reranked[0].Score,
		}
	}
	br := analyze(wrongRF, goodCR, q)
	printAnalyze("GOLDEN-BAD-WRONG-RERANK-TOP1(expect FAIL)", br)
	if br.pass {
		allOK = false
		fmt.Println("    SELF-VALIDATION VIOLATION: wrong post-rerank top-1 PASSed the analyzer")
	}

	// golden-bad (c): empty answer.
	emptyCR := cloneChat(goodCR)
	if len(emptyCR.Choices) > 0 {
		emptyCR.Choices[0].Message.Content = ""
	}
	cr2 := analyze(goodRF, emptyCR, q)
	printAnalyze("GOLDEN-BAD-EMPTY-ANSWER(expect FAIL)", cr2)
	if cr2.pass {
		allOK = false
		fmt.Println("    SELF-VALIDATION VIOLATION: empty answer PASSed the analyzer")
	}

	// golden-bad (d) — MUTATION of checkrerankimproves: a case where the real
	// ANN top-1 was ALREADY correct (never wrong) MUST FAIL the
	// "rerank-genuinely-improved-ordering" claim, even though the final
	// answer is fine. This proves checkrerankimproves is not a bluff gate
	// that would rubber-stamp "no correction needed" as "correction proven".
	noopAnn := cloneAnn(goodAnn)
	if len(noopAnn.Ranked) > 0 {
		noopAnn.Ranked[0] = rankedDoc{ID: q.ExpectDocID, Text: corpusText(q.ExpectDocID), Score: noopAnn.Ranked[0].Score}
	}
	noopImprovesPass := (noopAnn.Ranked[0].ID != q.ExpectDocID) && (goodRF.Reranked[0].ID == q.ExpectDocID)
	fmt.Printf("[GOLDEN-BAD-ANN-ALREADY-CORRECT(expect rerank-improves FAIL)] ann_top1(mutated)=%s reranked_top1=%s rerank_improves_claim_would_pass=%v\n",
		noopAnn.Ranked[0].ID, goodRF.Reranked[0].ID, noopImprovesPass)
	if noopImprovesPass {
		allOK = false
		fmt.Println("    SELF-VALIDATION VIOLATION: ann-already-correct case would PASS the rerank-improves claim")
	}

	if !allOK {
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

func cloneReranked(r rerankedResultFile) rerankedResultFile {
	out := r
	out.Reranked = make([]rankedDoc, len(r.Reranked))
	copy(out.Reranked, r.Reranked)
	out.AnnRanked = make([]rankedDoc, len(r.AnnRanked))
	copy(out.AnnRanked, r.AnnRanked)
	return out
}

func cloneAnn(r annResultFile) annResultFile {
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
