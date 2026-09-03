#!/usr/bin/env bash
# RED/GREEN reproduction for JWT authentication in HelixLLM (§11.4.115).
#
# Usage: repro.sh <path-to-helixllm-binary> [port]
#
# Boots the binary twice, on a scratch port, against a scratch config, and
# drives the real HTTPS surface with curl. Tokens are minted here with openssl
# — NOT with the server's own library — so a PASS proves standards interop
# rather than self-consistency.
#
# RED_MODE=1 (reproduce the defect on a PRE-FIX binary):
#   A: HELIX_AUTH_JWT_SECRET set, no API keys -> unauthenticated GET /v1/models
#      returns 200. The secret the security manual told operators to set
#      protects nothing.
#   B: HELIX_AUTH_JWT_SECRET + HELIX_AUTH_API_KEYS set -> a validly-signed
#      JWT is REJECTED with 401. The documented credential is not accepted.
#
# RED_MODE=0 (default, standing guard on a FIXED binary):
#   A: unauthenticated -> 401.  B: valid JWT -> 200.
set -uo pipefail

BIN="${1:?usage: repro.sh <binary> [port]}"
PORT="${2:-18443}"
RED_MODE="${RED_MODE:-0}"
SECRET="repro-only-hs256-signing-key-32b+"   # 33 bytes, never a deployed value
APIKEY="repro-only-api-key"
BASE="https://127.0.0.1:${PORT}"
# TLS material: the repo's dev cert by default (SANs localhost/127.0.0.1),
# overridable so the script runs against any checkout or binary location.
CERTDIR="${HELIX_REPRO_CERTDIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)/certs}"
fail=0

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

mint() { # mint <secret> <iss> <aud> <exp-offset-seconds> [alg]
  local secret="$1" iss="$2" aud="$3" off="$4" alg="${5:-HS256}"
  local now hdr pay si sig
  now=$(date +%s)
  hdr=$(printf '{"alg":"%s","typ":"JWT"}' "$alg" | b64url)
  pay=$(printf '{"iss":"%s","aud":"%s","sub":"repro","iat":%d,"exp":%d}' \
        "$iss" "$aud" "$now" "$((now + off))" | b64url)
  si="${hdr}.${pay}"
  if [ "$alg" = "none" ]; then printf '%s.' "$si"; return; fi
  sig=$(printf '%s' "$si" | openssl dgst -sha256 -hmac "$secret" -binary | b64url)
  printf '%s.%s' "$si" "$sig"
}

boot() { # boot <api-keys> <jwt-secret>
  HELIX_MODE=gateway HELIX_HOST=127.0.0.1 HELIX_PORT="$PORT" \
  HELIX_TLS_CERT="$CERTDIR/cert.pem" HELIX_TLS_KEY="$CERTDIR/key.pem" \
  HELIX_AUTH_API_KEYS="$1" HELIX_AUTH_JWT_SECRET="$2" \
  HELIX_LLM_LOCAL_RPC_PORT=1 HELIX_REDIS_HOST="" \
  "$BIN" >/tmp/repro_server_$$.log 2>&1 &
  SRV=$!
  for _ in $(seq 1 60); do
    curl -sk -o /dev/null "$BASE/internal/health" 2>/dev/null && return 0
    kill -0 "$SRV" 2>/dev/null || { echo "server died; log:"; tail -20 /tmp/repro_server_$$.log; return 1; }
    sleep 0.5
  done
  echo "server did not come up; log:"; tail -20 /tmp/repro_server_$$.log; return 1
}
stop() { [ -n "${SRV:-}" ] && kill "$SRV" 2>/dev/null; wait "$SRV" 2>/dev/null; SRV=""; }
trap stop EXIT

code() { curl -sk -o /dev/null -w '%{http_code}' "$@"; }
check() { # check <label> <got> <want>
  if [ "$2" = "$3" ]; then echo "  PASS  $1: HTTP $2 (expected $3)";
  else echo "  FAIL  $1: HTTP $2 (expected $3)"; fail=1; fi
}

echo "== HelixLLM JWT auth reproduction =="
echo "binary : $BIN"
echo "RED_MODE=$RED_MODE  ($([ "$RED_MODE" = 1 ] && echo 'asserting the DEFECT IS PRESENT' || echo 'asserting the defect is ABSENT'))"
echo

echo "-- Case A: JWT secret configured, HELIX_AUTH_API_KEYS empty --"
boot "" "$SECRET" || exit 1
a_unauth=$(code "$BASE/v1/models")
a_jwt=$(code -H "Authorization: Bearer $(mint "$SECRET" helixllm helixllm 3600)" "$BASE/v1/models")
stop
if [ "$RED_MODE" = 1 ]; then
  check "unauthenticated /v1/models (secret set => still wide open)" "$a_unauth" 200
else
  check "unauthenticated /v1/models rejected" "$a_unauth" 401
  check "valid JWT accepted"                  "$a_jwt"    200
fi
echo

echo "-- Case B: JWT secret AND API keys configured --"
boot "$APIKEY" "$SECRET" || exit 1
b_key=$(code -H "Authorization: Bearer $APIKEY" "$BASE/v1/models")
b_jwt=$(code -H "Authorization: Bearer $(mint "$SECRET" helixllm helixllm 3600)" "$BASE/v1/models")
b_unauth=$(code "$BASE/v1/models")
b_wrongsig=$(code  -H "Authorization: Bearer $(mint "wrong-secret-wrong-secret-wrong32" helixllm helixllm 3600)" "$BASE/v1/models")
b_expired=$(code   -H "Authorization: Bearer $(mint "$SECRET" helixllm helixllm -60)" "$BASE/v1/models")
b_algnone=$(code   -H "Authorization: Bearer $(mint "$SECRET" helixllm helixllm 3600 none)" "$BASE/v1/models")
b_wrongaud=$(code  -H "Authorization: Bearer $(mint "$SECRET" helixllm somebodyelse 3600)" "$BASE/v1/models")
b_wrongiss=$(code  -H "Authorization: Bearer $(mint "$SECRET" somebodyelse helixllm 3600)" "$BASE/v1/models")
stop
check "configured API key still accepted" "$b_key" 200
check "unauthenticated rejected"          "$b_unauth" 401
if [ "$RED_MODE" = 1 ]; then
  check "valid JWT (documented credential) is REJECTED" "$b_jwt" 401
else
  check "valid JWT accepted"                  "$b_jwt"      200
  echo "  -- negative security cases (all must be 401) --"
  check "wrong signature rejected"            "$b_wrongsig" 401
  check "expired token rejected"              "$b_expired"  401
  check "alg=none rejected"                   "$b_algnone"  401
  check "wrong audience rejected"             "$b_wrongaud" 401
  check "wrong issuer rejected"               "$b_wrongiss" 401
fi
echo
[ "$fail" = 0 ] && echo "RESULT: all assertions held" || echo "RESULT: assertions FAILED"
exit "$fail"
