# certs/ — dev-only TLS keypair

This directory holds the **development-only** self-signed TLS certificate
and private key that HelixLLM's HTTP server uses locally:

- `cert.pem` — self-signed X.509 certificate, `CN=helixllm`
- `key.pem`  — matching private key

Both files are consumed via `HELIX_TLS_CERT` / `HELIX_TLS_KEY`
(`internal/shared/config/config.go`, defaulting to `./certs/cert.pem` /
`./certs/key.pem`) and by the `make certs` Makefile target.

## These files are NOT committed

Per CONST-053 / §11.4.30 (no private keys/certs versioned) and the
project-wide anti-bluff mandate, `*.pem` / `*.key` are git-ignored. They
used to be committed by mistake and have been untracked — treat that old
pair as burned and never reuse it for anything beyond local dev.

## Regenerating

Run either of these (idempotent — skips generation if both files already
exist; pass `--force` / re-run `make certs` after `rm -f certs/*.pem` to
force a fresh pair):

```bash
./scripts/gen_dev_certs.sh
# or
make certs
```

This produces a fresh self-signed `CN=helixllm` RSA-4096 keypair valid for
~10 years. **Dev-only** — never use these certificates in production, and
never commit the regenerated files.
