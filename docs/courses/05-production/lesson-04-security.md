# Lesson 4: Security Hardening

**Duration:** 25 minutes
**Prerequisites:** Lesson 1 (Containerization)
**Learning Objectives:**
- Configure TLS 1.3 with production certificates
- Set up API key and JWT authentication
- Apply rate limiting and security headers
- Run security scans and triage vulnerabilities

---

## Scene 1: TLS Configuration (5 min)

**Narration:** "HelixLLM requires TLS for all connections. There is no plaintext HTTP option. In development we use self-signed certificates, but production deployments need certificates from a trusted certificate authority."

**Demo steps:**

```bash
# Development: self-signed certificates
make certs
# This generates certs/cert.pem and certs/key.pem using:
# openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
#   -keyout certs/key.pem -out certs/cert.pem -days 365 -nodes \
#   -subj "/CN=localhost" \
#   -addext "subjectAltName=DNS:localhost,DNS:nezha.local,IP:127.0.0.1"
```

**Narration:** "The certificates use the P-256 elliptic curve, which is fast and secure. For production, replace these with certificates from Let's Encrypt or your organization's CA."

```bash
# Production: configure real certificates
HELIX_TLS_CERT=/etc/ssl/helixllm/cert.pem
HELIX_TLS_KEY=/etc/ssl/helixllm/key.pem
```

**Key points:**
- TLS 1.3 minimum -- no TLS 1.2 or lower
- Elliptic curve P-256 for performance and security
- Self-signed for development, trusted CA for production
- ALPN negotiation supports h3, h2, and http/1.1
- Rotate certificates before expiry -- `rm -rf certs/ && make certs` for dev

---

## Scene 2: Authentication (6 min)

**Narration:** "HelixLLM supports two authentication mechanisms: API key authentication and JWT token authentication. Let me configure both."

**Demo steps:**

```bash
# API Key Authentication
# Set comma-separated keys in .env
HELIX_AUTH_API_KEYS=sk-prod-key-001,sk-prod-key-002,sk-admin-key-003
```

**Narration:** "When API keys are configured, all /v1/ endpoints require a Bearer token. Internal endpoints remain open for cluster communication."

```bash
# Test with API key
curl -sk https://localhost:8443/v1/models \
  -H "Authorization: Bearer sk-prod-key-001"

# Test without API key (should return 401)
curl -sk https://localhost:8443/v1/models
```

**Expected unauthorized response:**

```json
{
  "error": {
    "message": "missing or invalid API key",
    "type": "invalid_request_error"
  }
}
```

**Narration:** "JWT authentication provides session-based access with expiring tokens."

```bash
# JWT Authentication
HELIX_AUTH_JWT_SECRET=a-strong-random-secret-at-least-32-characters
```

**Screen:** Show the authentication scope.

| Endpoint Group | Auth Required |
|---------------|---------------|
| `/v1/*` | Yes (when keys configured) |
| `/internal/*` | No (protect at network level) |

**Key points:**
- API keys: simple Bearer token authentication
- JWT: session-based with expiring tokens
- Internal endpoints have no auth -- restrict with firewall rules
- Leave `HELIX_AUTH_API_KEYS` empty for open access (development only)
- Never commit API keys to version control

---

## Scene 3: Rate Limiting and Security Headers (5 min)

**Narration:** "Rate limiting prevents abuse by limiting requests per IP address. Security headers protect against common web vulnerabilities."

**Screen:** Show the rate limiting behavior.

**Narration:** "The rate limiter uses a sliding window algorithm. When a client exceeds the limit, they receive HTTP 429 with a Retry-After header."

```bash
# Test rate limiting by sending rapid requests
for i in $(seq 1 50); do
  curl -sk -o /dev/null -w "%{http_code}\n" \
    https://localhost:8443/v1/models \
    -H "Authorization: Bearer sk-prod-key-001"
done
```

**Narration:** "Eventually you will see 429 responses, indicating the rate limit was hit."

**Screen:** Show the security headers applied.

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevents MIME type sniffing |
| `X-Frame-Options` | `DENY` | Prevents clickjacking |
| `X-XSS-Protection` | `1; mode=block` | Enables XSS filter |
| `Strict-Transport-Security` | `max-age=31536000` | Enforces HTTPS |
| `Content-Security-Policy` | `default-src 'self'` | Restricts resource loading |

```bash
# Verify security headers are present
curl -sk -I https://localhost:8443/v1/models \
  -H "Authorization: Bearer sk-prod-key-001" | grep -E "^(X-|Strict|Content-Security)"
```

**Key points:**
- Sliding window rate limiting per IP address
- HTTP 429 with Retry-After header when limit exceeded
- Redis-backed rate limiting in distributed mode for consistency
- Security headers applied to all `/v1/*` responses automatically
- Headers follow OWASP best practices

---

## Scene 4: Content Guardrails and PII Detection (4 min)

**Narration:** "The security submodule provides content guardrails that scan inputs and outputs for sensitive information."

**Screen:** Show PII detection capabilities.

| Detection | Examples |
|-----------|---------|
| Email addresses | user@example.com |
| Phone numbers | +1-555-123-4567 |
| Social Security Numbers | 123-45-6789 |
| Credit card numbers | 4111-1111-1111-1111 |

**Narration:** "PII detection runs on both inputs to the LLM and outputs from it. When PII is detected, it can be logged, redacted, or blocked depending on your configuration. This protects against accidental data leakage in LLM responses."

**Key points:**
- PII detection scans both inputs and outputs
- Configurable actions: log, redact, or block
- Protects against accidental exposure of sensitive data
- The `digital.vasic.security` submodule provides the implementation
- Essential for compliance in healthcare, finance, and government

---

## Scene 5: Security Scanning (5 min)

**Narration:** "HelixLLM includes security testing infrastructure. Challenge banks test for injection, authentication bypass, and other vulnerabilities."

**Demo steps:**

```bash
# Run security-focused challenge banks
./bin/helixllm --challenges \
  --banks-dir=challenges/banks/security/ \
  --base-url=https://localhost:8443
```

**Narration:** "The security challenge banks test for SQL injection resistance, prompt injection handling, path traversal prevention, authentication bypass attempts, PII leakage, rate limit bypass, and SSRF protection."

**Screen:** Show the hardening checklist.

```
Production Security Checklist:
[x] TLS certificates from a trusted CA
[x] Strong API keys in HELIX_AUTH_API_KEYS
[x] Strong JWT secret in HELIX_AUTH_JWT_SECRET
[x] Database and Redis passwords set
[x] /internal/* endpoints restricted at network level
[x] Podman (rootless) for container runtime
[x] JSON log format for structured audit logs
[x] PII detection rules reviewed and configured
[x] Rate limits set appropriately for expected load
[x] All submodules updated to latest versions
```

**Key points:**
- Security challenge banks in `challenges/banks/security/`
- Tests cover injection, auth bypass, PII leakage, rate limiting
- Podman is preferred over Docker for rootless containers
- Restrict `/internal/*` at the network level in production
- The `.env` file is gitignored -- never commit secrets

---

## Exercises

1. Configure API key authentication, verify that requests without the key receive 401, and test with the correct Bearer token
2. Send rapid requests to trigger rate limiting, observe the 429 responses, and read the Retry-After header to understand the cooldown period
3. Run the security challenge banks and review the results for any failed assertions that indicate potential vulnerabilities
