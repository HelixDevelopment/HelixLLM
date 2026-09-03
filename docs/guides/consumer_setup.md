# Pointing your tools at a HelixLLM instance

Per-tool setup for **Claude Toolkit**, **HelixCode** and **OpenCode**.

Read this first, because it is the thing that will bite you:

> **The `/v1` suffix is not the same for every tool.**
> **OpenCode** needs `/v1` on its `baseURL`. **HelixCode** must **not** have it.
> Both are fed from the same bare origin, and each export adds what its own client requires.
> If you hand-edit either one, you will get `/v1/v1` on one side or a 404 on the other.

Details in [The `/v1` asymmetry](#the-v1-asymmetry).

Background: [adaptive model serving](./adaptive_model_serving.md) ·
[FAQ](./adaptive_model_serving_faq.md). Sample identifiers and digests below are illustrative; the
*shapes* are what the code produces.

---

## How you get your configuration

Ask the running instance for it. Both artefacts are served by the gateway you already point your
tools at, under the same `/v1` group, the same API key and the same rate limit:

```sh
# The artefact, plus the roster and the withheld options with their reasons.
curl -s http://gpu-01.local:8080/v1/config/helixcode
curl -s http://gpu-01.local:8080/v1/config/opencode

# Your own file, with the managed section added or replaced and nothing else touched.
curl -s --data-binary @"$HOME/.env"            http://gpu-01.local:8080/v1/config/helixcode/merge
curl -s --data-binary @"$HOME/.config/opencode/opencode.json" \
     -H 'Content-Type: application/json'       http://gpu-01.local:8080/v1/config/opencode/merge
```

The endpoint you call **is** the endpoint written into the configuration — the address the request
arrived on, upgraded to `https` when a terminating proxy says so. That is deliberate: the
identifiers published here are ours, and only this gateway maps one back to the model name a
provider answers to, so pointing a consumer straight at the llama.cpp instance behind it would
publish identifiers nothing there can resolve.

**Neither endpoint writes to your files.** The `GET` returns the artefact; the `merge` `POST` takes
your current file content in the request body and returns the merged content. What lands on disk
stays your decision.

If this gateway fronts **more than one** serving host, `GET /v1/config/…` answers `400` and names
them — each export describes exactly one host, so pick one with `?host=gpu-01`. Choosing for you
would hand you one host's models under a configuration that never mentions which host.

### What the endpoint builds for you

Internally both exports take one `naming.Instance`, which the endpoint assembles from the live model
listing:

```go
inst := naming.Instance{
    Host:    "gpu-01",                  // lower-case; must match every offer's Identity.Host
    BaseURL: "http://gpu-01.local:8080", // bare origin, NO API version path
    Healthy: true,
    Offers: []naming.Offer{
        {Identity: id, Available: true},
        {Identity: other, Available: false, Reason: "model-unavailable"},
    },
}
```

`BaseURL` is deliberately version-free. Consumers disagree about whether a version segment belongs
there, so each export appends what its own client requires rather than you guessing.

### What a base URL may not contain

`safeEndpoint` refuses, rather than strips:

- userinfo (`https://user:token@host`), a query string, or a fragment — the three places a discovery
  secret rides along in a URL, and these files are committed and sourced;
- whitespace, control characters, and the shell-significant characters `" ' \` $ \` — one of the two
  artefacts is an env file a shell sources;
- any scheme that is not `http` or `https`, and any URL naming no host.

They are refused rather than silently dropped because dropping half of your endpoint would point the
consumer somewhere you did not ask for. The error never quotes the URL back — an error message is a
place secrets leak too.

### Unhealthy instances export nothing usable

If `Healthy` is false, **every** offer is withheld regardless of its own `Available` flag. A consumer
listing a model from a stopped instance would present it as selectable. Each withheld entry carries a
reason: the instance's own if it gave one, otherwise `instance-unreachable`; for an available-false
offer on a healthy instance, its own reason or `model-unavailable`. There is never an empty reason.

### Neither export writes to your files

Both return the artefact. You apply it. Each ships a `Merge*` function that takes your current file
content and returns the updated content — you decide when it lands on disk.

---

## Claude Toolkit

**There is no config-file exporter for the Claude Toolkit.** What exists is the identifier scheme it
constrains, and the toolkit reads its model list off the wire.

### Setup

1. Point the toolkit at your HelixLLM instance's OpenAI-compatible endpoint (its provider base URL /
   endpoint setting, per the toolkit's own configuration).
2. `GET /v1/models` on that instance returns the identifiers to use:

```json
{
  "object": "list",
  "data": [
    {
      "id": "helixllm-gpu-01-llama3-8b-3f2a9c1d4e5b",
      "object": "model",
      "owned_by": "<provider>",
      "model_identity": "helixllm/gpu-01/llama3:8b",
      "host": "gpu-01",
      "availability": "serving"
    }
  ]
}
```

3. Use `id` wherever the toolkit wants a model name. Requests naming it are translated back to the
   name the serving provider actually answers to before routing — so the identifier works as a model
   name, not merely as a label.

`model_identity` is the human-readable value `helixllm/<host>/<model>[:<variant>]`. Read it, do not
type it: it contains `/` and `:`, which the toolkit rejects (see below). `host` is carried separately
so you do not have to parse the identity to recover it.

Only models actually being served appear in `/v1/models`. Remote vendor models keep their upstream
id and carry no `model_identity` or `host` — that distinction is the whole point of the identity.

### Why the identifier looks like that

The toolkit applies two independent checks to a value used as both an alias name and a provider id:

```
alias name       ^[a-zA-Z][a-zA-Z0-9_-]*$
provider id      [A-Za-z0-9._-] only, non-empty
```

The second is a shell-injection guard — the provider id is interpolated into an alias body that is
re-parsed on invocation. Neither is relaxed to fit a model name. The derived identifier is their
**intersection**: `[A-Za-z0-9_-]`, opening with a letter, capped at 64 bytes. (`.` is excluded even
though the provider-id guard permits it, because the alias rule does not.)

So an identifier is `helixllm-<readable>-<digest>`:

- the readable part is a lossy rendering of host, model and variant — recognisable in a config file;
- the digest is 12 hex characters of SHA-256 **over the full canonical identity**, which is what
  keeps `llama3:8b` and `llama3-8b` from collapsing into one entry once the readable part has been
  flattened. Only the readable part is trimmed to meet the length cap; the digest never is.

Derivation is deterministic, so these names are stable in your configuration across releases.

### The host segment names a machine

`<host>` is the machine serving the model. When the backend's base URL is a **loopback or wildcard**
address — `127.0.0.1`, `localhost`, `::1`, `0.0.0.0`, which is the ordinary case, since
`HELIX_LLM_LOCAL_RPC_HOST` defaults to `localhost` and the embedded llama-server path rewrites it to
`127.0.0.1` — the identity uses **this machine's own name** instead. Such an address names no
machine, and it is the same string everywhere: publishing it verbatim meant two gateways on two
different hosts published identical identities and identical ids, and the Claude Toolkit
de-duplicates by id (`group_by(.provider_id) | map(.[0])`), so one host's models silently replaced
the other's.

A base URL that already names a real machine (`gpu-01.lan`, `10.0.0.7`) is used exactly as it is.
Both spellings of loopback resolve to the same machine name, so flipping
`HELIX_LLM_LOCAL_RPC_HOST` between `localhost` and `127.0.0.1` no longer re-mints your identifiers.

If a derived identifier ever collided with a different identity, that option is withheld with reason
`identifier-conflict` rather than silently replacing the other one.

**Migration — identifiers minted before the loopback change.** The move from the loopback literal to
the machine name re-minted every locally-served identifier once. A configuration still holding an old
`helixllm-127-0-0-1-…` (or `helixllm-localhost-…`) id names a model this gateway no longer publishes:
such a request **fails** rather than being answered by a different model. Re-run
discovery — `GET /v1/models` — and replace the ids in your configuration with the ones it returns.

The status is **404**, and the body names the migration:

```
HTTP=404 {"error":{"message":"this model identifier is no longer published: the identifiers
for locally served models changed when the serving host was renamed; re-fetch the current
ones from /v1/models and update your configuration", ...}}
```

404 rather than 503 because these two host renderings are an *exactly known* set — this gateway
published them and has permanently stopped — so the name is gone for good, not absent for a
while. 503 means "retry with backoff", and a correct client obeying it against a name that can
never resolve retries forever.

**The 404 requires both halves.** The host rendering alone is not enough, because a real machine
can be called `localhost.lan` or `localhost-2`, and its perfectly current identifiers open with
`helixllm-localhost-` too. So a name is reported permanently gone only when its host segment is a
retired rendering **and** no host this gateway is currently publishing under accounts for it. If
you are serving from a machine whose name renders that way, its own ids stay live and a model
merely missing from its list answers **503**, like any other host.

Every OTHER unresolvable identifier still answers **503**, and that is deliberate: an id carries a
digest, so for any host segment outside the retired set the gateway genuinely cannot tell a
re-minted name from a machine that is rebooting — and telling a client to stop retrying a
restarting host would be the worse error.

---

## HelixCode

### What the export produces

```sh
curl -s http://gpu-01.local:8080/v1/config/helixcode | jq -r .env_file
```

The `env_file` field is an environment-file fragment:

```sh
# >>> helixllm managed block
# helixllm/gpu-01
# helixllm-gpu-01-llama3-8b-3f2a9c1d4e5b = helixllm/gpu-01/llama3:8b
# withheld: helixllm/gpu-01/other-model = model-unavailable
HELIX_LLM_LOCAL_OPENAI_ENDPOINT="http://gpu-01.local:8080"
# <<< helixllm managed block
```

One variable, `HELIX_LLM_LOCAL_OPENAI_ENDPOINT`. That is the single thing HelixCode's live HelixLLM
route reads from configuration: a request naming provider `helixllm` or `local` is routed to a
handler that builds an OpenAI-compatible client from that variable and passes the request's own
`model` field through verbatim.

**The model roster travels as comments**, because that route has no configuration slot for a model
list — you name one identifier per request in the `model` field. The response's `models` array gives you the same list
as data: `identifier`, the `identity` it stands for, and the `wire_model` name the instance itself
answers to. `withheld` lists the options deliberately not offered, each with its reason.

The value is quoted because the file is sourced by a shell; `safeEndpoint` has already refused every
character that could escape the quoting.

### Which identifier, and why it is the strict one

The export publishes **Claude Toolkit identifiers**, even though HelixCode itself imposes no
character restriction on `model` (it copies the string into the upstream request body untouched).

The constraint is on the far side of the wire: HelixLLM maps a published identifier back to the model
name its providers answer to, and that lookup is keyed on the Claude Toolkit ruleset. An identifier
derived under any other ruleset would arrive unresolvable and fall through to whichever provider the
router reached last — a silent misroute, not an error. That ruleset is also the strictest in play, so
this is a self-restriction: nothing is relaxed to fit a name.

### Applying it

```sh
curl -s --data-binary @"$HOME/.env" \
     http://gpu-01.local:8080/v1/config/helixcode/merge > /tmp/env.merged
# read /tmp/env.merged, then move it into place yourself
```

- Replaces the managed block if it is there, appends it if it is not, and leaves every other line
  exactly as it was.
- Re-running is a no-op — the block is delimited, so the second run replaces what the first wrote.
- **Refuses** (`409`) if your file assigns `HELIX_LLM_LOCAL_OPENAI_ENDPOINT` *outside* the managed
  block, naming the line numbers. Which assignment wins depends on read order, so neither silently
  winning nor silently losing would be honest. Resolve it and re-run.
- Refuses if the managed block is not closed.

It returns the merged text; it never writes to your file.

### Note on `config.yaml`

`helix_code/config/config.yaml` declares `helix-llm` and `helix-debate` provider types. No Go code
loads them — `internal/config.LLMConfig` has no `providers` field. That block is dead configuration
and this export deliberately does not target it; building against it would produce an artefact that
changes nothing.

---

## OpenCode

### What the export produces

```sh
curl -s http://gpu-01.local:8080/v1/config/opencode | jq .document
```

The `document` field is a JSON fragment carrying just this instance's provider entry:

```json
{
  "provider": {
    "helixllm-gpu-01-8c1f0b2a7d34": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "helixllm/gpu-01",
      "options": {
        "baseURL": "http://gpu-01.local:8080/v1"
      },
      "models": {
        "helixllm-gpu-01-llama3-8b-3f2a9c1d4e5b": {
          "id": "llama3:8b",
          "name": "helixllm/gpu-01/llama3:8b"
        }
      }
    }
  }
}
```

Reading it:

- **`npm`** is what OpenCode dispatches on. `@ai-sdk/openai-compatible` is the adapter that
  configures an OpenAI-compatible client.
- **The provider key** (the response's `provider_id`) is derived from the *host*, not from a model. One instance
  is one provider entry, because `options.baseURL` is per-entry — two hosts are two entries, not two
  models under one.
- **The model key** is a derived, charset-safe identifier. The **`id` field is the wire model** —
  the name the instance actually answers to, which may contain characters the key may not. OpenCode
  resolves the wire name as `id ?? key`, so the key is only a fallback.
- **`name`** carries the human-readable identity as a displayed value.
- You reference a model as `<provider-key>/<model-key>`.

`options` has no `apiKey` field, and the Go type that renders it cannot express one — no credential
can reach the document through this path.

**Withheld options are absent from the document entirely**, not listed as unavailable. An entry under
`models` shows up in OpenCode's picker as selectable. The response's `withheld` array still reports
them, each with its reason, so you can show them somewhere they are not selectable.

Marshalling is deterministic (Go sorts map keys), so the same instance always produces the same
bytes.

### The ruleset

OpenCode's own schema places **no** pattern constraint on either key and documents no length limit.
The ruleset used here is therefore tighter than anything OpenCode enforces — a self-restriction,
never a relaxation:

`[A-Za-z0-9._-]`, `-` as separator, no leading-letter requirement (the `helixllm` prefix supplies one
anyway), no length cap.

One exclusion is load-bearing rather than conservative: **`/` is forbidden in a key.** OpenCode
parses a model reference by splitting on the *first* `/`, so a key containing one silently re-points
the reference. (Model *ids* legitimately contain `/` — that is fine, they are values.)

### Applying it

```sh
curl -s --data-binary @"$HOME/.config/opencode/opencode.json" \
     -H 'Content-Type: application/json' \
     http://gpu-01.local:8080/v1/config/opencode/merge > /tmp/opencode.merged.json
# read /tmp/opencode.merged.json, then move it into place yourself
```

- Additive by key: providers you configured yourself are copied through byte-for-byte, as is every
  other top-level key.
- **Replaces our own entry wholesale** rather than merging field-wise — a model that stopped being
  offered must disappear, and a field-wise merge would leave it behind.
- Re-running is a no-op.
- Fails (`409`) if your existing file does not parse, or if `provider` is not an object.

It returns the merged document; it never writes to your file.

---

## The `/v1` asymmetry

Both exports start from the same bare `Instance.BaseURL` and end up with different values on
purpose:

| Tool | Written value | Why |
|---|---|---|
| **OpenCode** | `<origin>/v1` | Its `@ai-sdk/openai-compatible` adapter builds requests as `` `${baseURL}/chat/completions` `` — it appends the path itself and adds **no** version segment. Without `/v1` in `baseURL`, requests go to `<origin>/chat/completions`. |
| **HelixCode** | `<origin>` — **no** `/v1` | The variable is documented base-URL-only: HelixCode's OpenAI-compatible client already carries `/v1` in its own endpoint defaults. Adding one here produces `/v1/v1`. |

The exports handle this for you. It matters when you hand-edit, copy one value into the other tool,
or write these variables by some other route.

OpenCode's export is idempotent about it: if you supplied an origin that already ends in `/v1`, it is
not doubled. HelixCode's export has no such tolerance — give it the bare origin.

---

## Troubleshooting

**A model I expect is missing from a consumer's config.** Check the response's `withheld` array. An option is written
only when the instance is healthy *and* the offer is available. `instance-unreachable` means the
instance was not healthy; `model-unavailable` means the instance was up but not serving that model.

**`ErrHostMismatch` on export.** An offer's `Identity.Host` does not equal the instance's `Host`.
Exporting it would point the consumer at the wrong machine. Hosts are lower-cased and trimmed —
`Instance.Host` must already be normalised, and `NewIdentity` normalises the identity side.

**`ErrForeignAssignment` merging the HelixCode env file.** Your file sets
`HELIX_LLM_LOCAL_OPENAI_ENDPOINT` outside the managed block, with or without `export`. Remove or
comment out that assignment and re-run.

**`ErrUnsafeEndpoint`.** The base URL carries a credential, a query, a fragment, whitespace, a
shell-significant character, or a non-HTTP scheme. Pass a plain origin; carry credentials by whatever
mechanism your instance uses, never in the URL.

**Requests with a `helixllm-…` id are landing on the wrong provider.** The identifier must be one
derived under the Claude Toolkit ruleset — that is the only ruleset the server-side resolver is keyed
on. Take the id from `/v1/models` rather than constructing one.
