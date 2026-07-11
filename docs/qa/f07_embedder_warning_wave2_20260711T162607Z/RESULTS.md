# F07 — HELIX_EMBEDDING_PROVIDER=local non-semantic embedder transparency warning (Wave-2)

**Assignment (warn-half, safe-autonomous):** default
`HELIX_EMBEDDING_PROVIDER=local` is a NON-SEMANTIC hash embedder. Add a
startup WARNING log making this transparent (do NOT change the default —
that half is operator-gated). Prove the warning fires when the default is
active + not when a real provider is configured.

## Root cause

`internal/shared/config/config.go`: `EmbeddingProvider string
env:"HELIX_EMBEDDING_PROVIDER" default:"local"`.
`internal/knowledge/embedding_providers.go`'s `NewEmbedder` resolves
`"local"`, `"hash"`, `""`, AND any unrecognised provider name to
`knowledge.HashEmbedder` — a deterministic SHA-256-based embedder with
**zero semantic content**. Before this fix, `cmd/helixllm/main.go`
constructed this silently: an operator running the default, zero-config
path had no signal that RAG retrieval was not doing semantic search at all.

## Fix

`cmd/helixllm/main.go`: extracted the embedder-construction block (previously
inline in `main()`) into a standalone, directly-testable function
`buildEmbedder(cfg *config.HelixConfig, log logging.Logger) knowledge.Embedder`.
Behaviour is preserved byte-for-byte (same `NewEmbedder` call, same
error-fallback to `NewHashEmbedder(768)`); the only addition is: after the
embedder is finalized, a **type-assertion** on the returned value
(`embedder.(*knowledge.HashEmbedder)`) — not a string match on the config
value — triggers a `log.WithField("embedding_provider", ...).Warn(...)`
call. Using a type-assertion (rather than `cfg.Knowledge.EmbeddingProvider
== "local"`) means the warning correctly fires for EVERY path
`knowledge.NewEmbedder` can take that ends in a `HashEmbedder` — `"local"`,
`"hash"`, `""`, an unrecognised provider name, AND the pre-existing
error-fallback path — uniformly, with a single check.

The default itself is **unchanged**: `TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic`
asserts the returned embedder is still `*knowledge.HashEmbedder` for
`EmbeddingProvider="local"`.

## Real captured evidence (this session, `-count=1` fresh run)

`test_output.txt` (full transcript, `cmd/helixllm/embedder_warning_test.go`):

- `TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic` — PASS. Captured
  REAL logrus output (via `logging.NewWithOutput(..., &buf)`, a genuine
  logger writing to an in-memory buffer, not a mock):

  ```
  time="2026-07-11T21:18:28+05:00" level=warning msg="RAG embeddings are using the non-semantic HashEmbedder (HELIX_EMBEDDING_PROVIDER=local/hash, unset, or unrecognised, or embedder construction failed) — embeddings do NOT capture semantic similarity and RAG retrieval quality will be significantly degraded; set HELIX_EMBEDDING_PROVIDER to a real provider (e.g. \"openai\" or \"llama\" pointing at a real embedding-serving endpoint) for production-quality RAG" embedding_provider=local
  ```

- `TestBuildEmbedder_EmptyProvider_AlsoWarns` — PASS (empty
  `EmbeddingProvider` also warns, proving the type-assertion discriminator
  works uniformly, not just for the literal string `"local"`).
- `TestBuildEmbedder_RealProviderConfigured_NoWarning` — PASS.
  `EmbeddingProvider="openai"` with a real (non-network, construction-only)
  API key produces a genuine `*knowledge.OpenAIEmbedder` and **empty**
  captured log output (no warning). `digital.vasic.embeddings/pkg/openai.NewClient`
  does not perform any network I/O at construction (verified: only checks
  `apiKey != ""` locally), so this is a real, hermetic, deterministic,
  re-runnable (§11.4.98) construction of the production type — not a mock.
- `TestBuildEmbedder_LlamaProviderConfigured_NoWarning` — PASS.
  `EmbeddingProvider="llama"` also produces no warning (the `LlamaEmbedder`
  constructor likewise performs no network I/O at construction time).

```
=== RUN   TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic
--- PASS: TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic (0.00s)
=== RUN   TestBuildEmbedder_EmptyProvider_AlsoWarns
--- PASS: TestBuildEmbedder_EmptyProvider_AlsoWarns (0.00s)
=== RUN   TestBuildEmbedder_RealProviderConfigured_NoWarning
--- PASS: TestBuildEmbedder_RealProviderConfigured_NoWarning (0.00s)
=== RUN   TestBuildEmbedder_LlamaProviderConfigured_NoWarning
--- PASS: TestBuildEmbedder_LlamaProviderConfigured_NoWarning (0.00s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/cmd/helixllm	0.008s
```

## §1.1 load-bearing mutation (this session)

Mutated `cmd/helixllm/main.go`'s warning condition to
`if _, isHash := embedder.(*knowledge.HashEmbedder); false && isHash { // MUTATED for paired §1.1 mutation test`,
re-ran `TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic`:

```
=== RUN   TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic
    embedder_warning_test.go:46: expected a non-semantic HashEmbedder warning in captured log output, got:
--- FAIL: TestBuildEmbedder_DefaultLocalProvider_WarnsNonSemantic (0.00s)
FAIL
```

Restored the clean source (`grep -c MUTATED` → `0`), `go build ./cmd/helixllm/...`
succeeded, re-ran the full `TestBuildEmbedder_*` suite → all PASS again
(captured above). The warning check is genuinely load-bearing.

## Answer to the task's explicit proof requirement

> "Prove the warning fires when the default is active + not when a real
> provider is configured."

Proven above with real captured logrus output: fires (WARN level, correct
substrings) for `local` and empty (both are "the default is active" in
practice — `""` resolves identically to `"local"`); does NOT fire for
`openai` or `llama`. The default itself is unchanged (still resolves to
`*knowledge.HashEmbedder`) — only its visibility changed.
