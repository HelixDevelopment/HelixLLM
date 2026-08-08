# Erratum — commit `e68fcdc` message misstates the pre-fix state (HXC-235)

**Date:** 2026-08-08
**Applies to:** commit `e68fcdc` — *"fix(hxc-235): report the real embedding mode instead of a hardcoded lie"*
**Scope of the error:** the commit **message** only. The **code change is correct** and needs no revision.
**Why a separate file:** `e68fcdc` is published on all four remotes. Force-push and history
rewrite are forbidden without exception (§11.4.113), so a published message cannot be amended.
The correction is recorded here instead, and is discoverable by anyone who greps the SHA.

---

## What the message claimed

Two passages assert the field already existed before the fix:

> "…the gateway half **declared the field and never** assigned it…"

> "WHAT WAS MISSING: `pkg/api/openai.go:161` **DECLARED** `SemanticEmbeddings` on the
> response struct… the field was **present, plausible, and wrong, which is worse than
> absent**: a caller…"

The argument built on that premise — that a silently-false field is more dangerous than a
missing one — is sound as a general principle. It just does not describe what happened here.

## What was actually true

`SemanticEmbeddings` did not exist **anywhere** in the pre-fix tree. Re-derived 2026-08-08
against the fix commit's own parent:

```
$ git rev-parse e68fcdc^
8990df4d…

$ git grep -c 'SemanticEmbeddings' 8990df4d -- '*.go' | wc -l
0
```

Zero files. The field was **created by `e68fcdc` itself** — the diffstat shows
`pkg/api/openai.go | 10 +` (ten insertions, no deletions), which is the declaration being
added, not a pre-existing declaration being wired up.

## Why the distinction matters

The two states are different defects with different severities, and the message asserted the
worse one:

| | Pre-fix reality (absent) | What the message claimed (declared-but-false) |
|---|---|---|
| Caller sees | no `semantic_embeddings` key | `"semantic_embeddings": false`, always |
| Caller can | detect the gap — key missing | be actively misled — key present, plausible, wrong |
| Severity | missing capability | **silent-lie**, the worse class |

So the message overstated the pre-fix severity. Anyone reading `e68fcdc` to understand the
defect's history — a future §11.4.114 regression isolation, a §11.4.150 research pass, a
reopen investigation — would start from a false premise about what the API used to emit.

## What is unaffected

- **The fix is correct.** `internal/gateway/openai.go:641` assigns
  `knowledge.IsSemanticEmbedder(embedder)` on the real-embedding path and `:673` assigns
  `false` on the zero-vector fallback — each the literal truth about the numbers that path
  produced.
- **The guards are unaffected.** `internal/knowledge/hxc235_semantic_signal_test.go` and
  `internal/gateway/hxc235_embedding_semantic_signal_test.go` assert runtime behaviour, not
  the commit narrative. Their `RED_MODE` polarity is unchanged.
- **No re-verification is owed.** The deployed behaviour was verified end-to-end after this
  fix landed; that evidence stands. This erratum corrects a historical claim, nothing live.

## Root cause of the error

I described the pre-fix state from the shape of the post-fix file — reading a declaration next
to a correct assignment and inferring the declaration predated the assignment — instead of
checking the parent tree. That is the §11.4.6 no-guessing failure in its most ordinary form: a
plausible reconstruction asserted as fact without the one command that would have settled it.
The check costs one `git grep` against `<fix>^` and is now part of how I write any commit
message that characterises a "before" state.
