#!/usr/bin/env bash
# scripts/gen_dev_certs.sh — regenerate the HelixLLM DEV-ONLY self-signed TLS
# keypair.
#
# Why this exists (CONST-053 / §11.4.30 + §11.4.77 regeneration mandate):
#   certs/{cert.pem,key.pem} used to be committed to git. They are private
#   key material and MUST NOT be versioned. Once excluded via .gitignore,
#   the codebase must still be able to reproduce an equivalent artifact
#   out of the box — that is what this script does.
#
# What it produces:
#   certs/cert.pem — self-signed X.509 certificate, CN=helixllm
#   certs/key.pem  — matching RSA-4096 private key
#   Both match the paths HELIX_TLS_CERT / HELIX_TLS_KEY default to in
#   internal/shared/config/config.go ("./certs/cert.pem" / "./certs/key.pem")
#   and the `make certs` Makefile target's output location.
#
# Usage:
#   ./scripts/gen_dev_certs.sh          # generate only if missing (idempotent)
#   ./scripts/gen_dev_certs.sh --force  # always regenerate
#
# These are DEV-ONLY self-signed certificates. Never use them in production;
# never commit the regenerated files (see .gitignore).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CERT_DIR="${REPO_ROOT}/certs"
CERT_FILE="${CERT_DIR}/cert.pem"
KEY_FILE="${CERT_DIR}/key.pem"
CN="helixllm"
DAYS="${HELIX_LLM_DEV_CERT_DAYS:-3650}"
FORCE=0

for arg in "$@"; do
  case "$arg" in
    --force|-f) FORCE=1 ;;
    -h|--help)
      sed -n '2,25p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *)
      echo "gen_dev_certs.sh: unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

if ! command -v openssl >/dev/null 2>&1; then
  echo "gen_dev_certs.sh: openssl not found on PATH — cannot generate dev certs" >&2
  exit 1
fi

mkdir -p "${CERT_DIR}"

if [ "${FORCE}" -eq 0 ] && [ -f "${CERT_FILE}" ] && [ -f "${KEY_FILE}" ]; then
  echo "gen_dev_certs.sh: ${CERT_FILE} and ${KEY_FILE} already present — skipping (use --force to regenerate)"
  exit 0
fi

echo "gen_dev_certs.sh: generating self-signed dev TLS keypair (CN=${CN}, ${DAYS} days) ..."
openssl req -x509 -newkey rsa:4096 \
  -keyout "${KEY_FILE}" -out "${CERT_FILE}" \
  -days "${DAYS}" -nodes \
  -subj "/CN=${CN}" 2>/dev/null

chmod 600 "${KEY_FILE}"
chmod 644 "${CERT_FILE}"

echo "gen_dev_certs.sh: done"
echo "  cert: ${CERT_FILE}"
echo "  key:  ${KEY_FILE}"
openssl x509 -in "${CERT_FILE}" -noout -subject -dates
