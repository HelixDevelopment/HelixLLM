# JWT authentication — RED → GREEN evidence

Captured 2026-09-03 for the implementation of `HELIX_AUTH_JWT_SECRET`
(the field SECURITY-5 found the security manual documenting for a
capability that did not exist).

## What is here

| File | What it is |
|---|---|
| `repro.sh` | The reproduction. Boots a real HelixLLM binary on a scratch port over real TLS and drives `/v1/models` with `curl`. `RED_MODE=1` asserts the defect is present, `RED_MODE=0` asserts it is absent. Re-runnable against any binary. |
| `RED_prefix_8f7c38d.txt` | `RED_MODE=1` against a binary built from **pre-fix** HEAD `8f7c38d`. |
| `GREEN_fixed.txt` | `RED_MODE=0` against the **fixed** binary. |

Tokens in `repro.sh` are minted with `openssl dgst -sha256 -hmac`, NOT with the
server's own JWT library. A GREEN therefore proves standards interop with an
independently-produced token rather than the server agreeing with itself.

## The two defects reproduced on the pre-fix artifact

```
-- Case A: JWT secret configured, HELIX_AUTH_API_KEYS empty --
  PASS  unauthenticated /v1/models (secret set => still wide open): HTTP 200

-- Case B: JWT secret AND API keys configured --
  PASS  valid JWT (documented credential) is REJECTED: HTTP 401
```

Case A is the one that made the old documentation dangerous: an operator sets
the secret the hardening checklist asks for, and the server keeps answering
everyone. Case B is the other half: the credential the manual described was not
accepted by anything.

## After the fix

```
-- Case A --
  PASS  unauthenticated /v1/models rejected: HTTP 401
  PASS  valid JWT accepted: HTTP 200
-- Case B --
  PASS  configured API key still accepted: HTTP 200
  PASS  unauthenticated rejected: HTTP 401
  PASS  valid JWT accepted: HTTP 200
  -- negative security cases (all must be 401) --
  PASS  wrong signature rejected: HTTP 401
  PASS  expired token rejected: HTTP 401
  PASS  alg=none rejected: HTTP 401
  PASS  wrong audience rejected: HTTP 401
  PASS  wrong issuer rejected: HTTP 401
```

## Compatibility — the live :8443 configuration is untouched

`:8443` runs with BOTH auth variables empty, and the Claude Toolkit's provider
verification plus helix_code call `GET /v1/models` there with no credential.
The **fixed** binary was booted on a scratch port in that exact configuration:

```
  /v1/models             -> HTTP 200
  /v1/hardware           -> HTTP 200
  /internal/health       -> HTTP 503   (unauthenticated + reachable; 503 is the
                                        real dependency report in a scratch env
                                        with no Redis/llama.cpp — not an auth
                                        rejection)
  /metrics               -> HTTP 200

level=warning msg="AUTHENTICATION IS NOT CONFIGURED"
  accepted_credentials="NONE — every /v1 and /internal route is open to any
  client that can reach this port" api_keys_configured=false jwt_enabled=false
level=warning msg="auth: set HELIX_AUTH_API_KEYS and/or HELIX_AUTH_JWT_SECRET
  to require a credential; this server binds all interfaces by default, so an
  open posture is reachable from the whole network segment, not just localhost"
```

The running `:8443` service was NOT restarted for any of this; every boot above
used a scratch port and a scratch config.

## How to re-run

```bash
go build -o /tmp/helixllm ./cmd/helixllm
docs/qa/jwt_auth_20260903/repro.sh /tmp/helixllm 18444     # GREEN (default)
RED_MODE=1 docs/qa/jwt_auth_20260903/repro.sh /tmp/old 18444  # RED, pre-fix binary
```

The secrets and keys in `repro.sh` are reproduction-only literals. They are not
any deployment's values.
