# Security

HelixLLM implements multiple layers of security: authentication, rate limiting, content guardrails, PII detection, HTTP security headers, and TLS enforcement.

## TLS

All traffic is encrypted with TLS 1.3 minimum. The server requires a certificate and private key:

```bash
HELIX_TLS_CERT=./certs/cert.pem
HELIX_TLS_KEY=./certs/key.pem
```

For local development, `make certs` generates a self-signed certificate:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
  -keyout certs/key.pem -out certs/cert.pem -days 365 -nodes \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,DNS:nezha.local,IP:127.0.0.1"
```

Key properties:
- Elliptic curve P-256 (fast, secure)
- Subject Alternative Names for localhost and configured hostnames
- 365-day validity
- No passphrase (for automated startup)

For production, use certificates from a trusted CA.

## Authentication

### API Key Authentication

Configure one or more API keys:

```bash
HELIX_AUTH_API_KEYS=sk-key1,sk-key2,sk-key3
```

When set, all `/v1/*` endpoints require a Bearer token:

```
Authorization: Bearer sk-key1
```

Requests without a valid key receive HTTP 401. The middleware is in `internal/gateway/middleware/auth.go`.

Leave `HELIX_AUTH_API_KEYS` empty to disable authentication (open access).

### JWT Authentication

Configure a JWT signing secret:

```bash
HELIX_AUTH_JWT_SECRET=your-secret-key
```

When set, the system can issue and validate JWT tokens for session-based access. The `digital.vasic.auth` submodule provides the full JWT lifecycle.

### Authentication Scope

| Endpoint Group | Auth Required |
|---------------|---------------|
| `/v1/*` | Yes (when API keys configured) |
| `/internal/*` | No |

Internal endpoints are intended for cluster-internal communication and health checks. In production, restrict access to these endpoints at the network level (firewall, reverse proxy).

## Rate Limiting

The rate limiter middleware limits requests per IP using a sliding window algorithm:

```go
v1.Use(gwmw.RateLimit(opts.RateLimit))
```

When the limit is exceeded, the server returns HTTP 429 with a `Retry-After` header.

In distributed mode, rate limiting is backed by Redis (`digital.vasic.ratelimiter`) for consistency across gateway instances.

## Security Headers

The `SecurityHeaders` middleware sets protective HTTP headers on all `/v1/*` responses:

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevents MIME type sniffing |
| `X-Frame-Options` | `DENY` | Prevents clickjacking |
| `X-XSS-Protection` | `1; mode=block` | Enables XSS filter |
| `Strict-Transport-Security` | `max-age=31536000` | Enforces HTTPS |
| `Content-Security-Policy` | `default-src 'self'` | Restricts resource loading |

## PII Detection and Redaction

The `digital.vasic.security` submodule provides content guardrails:

- **PII detection:** Scans inputs and outputs for personally identifiable information (email addresses, phone numbers, SSNs, credit card numbers)
- **Redaction:** Optionally redacts detected PII before logging or forwarding
- **Content guardrails:** Configurable rules engine for blocking or transforming specific content patterns

## Input Validation

All API handlers validate inputs before processing:

- **Chat completions:** `messages` array must not be empty
- **Knowledge ingest:** `content` and `collection` must not be empty
- **Knowledge query:** `query` must not be empty
- **Agent chat:** `messages` array must not be empty
- **Cluster deploy:** `services` array must not be empty

Invalid requests return HTTP 400 with a descriptive error message.

## Request ID Correlation

Every request receives a unique ID via the `X-Request-Id` header:

- If the client provides one, it is used
- Otherwise, the server generates a UUID
- The ID is propagated through all internal calls
- Included in logs and response headers

This enables end-to-end request tracing and audit logging.

## SSH Security (Multi-Host)

Cluster communication uses SSH with these constraints:

- Key-based authentication only (no passwords)
- Ed25519 keys recommended (configured via `HELIX_SSH_KEY`)
- Each host requires the public key in `~/.ssh/authorized_keys`
- The SSH user (`HELIX_SSH_USER`) should have minimal required permissions

## Container Security

Podman is preferred over Docker for its security properties:

- **Rootless:** Containers run without root privileges
- **Daemonless:** No long-running daemon with root access
- **SELinux support:** Better mandatory access control

## Secrets Management

Sensitive values in `.env`:

| Variable | Sensitivity |
|----------|-------------|
| `HELIX_LLM_OPENAI_KEY` | High -- API key with billing access |
| `HELIX_LLM_ANTHROPIC_KEY` | High -- API key with billing access |
| `HELIX_AUTH_JWT_SECRET` | High -- token signing key |
| `HELIX_AUTH_API_KEYS` | High -- authentication credentials |
| `HELIX_DB_PASSWORD` | Medium -- database access |
| `HELIX_REDIS_PASSWORD` | Medium -- cache access |

The `.env` file is gitignored. Never commit secrets to version control.

## Security Testing

Security tests validate:

- SQL injection resistance
- Prompt injection handling
- Path traversal prevention
- Authentication bypass attempts
- PII leakage in responses
- Rate limit bypass attempts
- SSRF protection

Challenge banks for security testing are in `challenges/banks/security/`.

## Hardening Checklist

For production deployments:

- [ ] Use certificates from a trusted CA (not self-signed)
- [ ] Set strong, unique API keys in `HELIX_AUTH_API_KEYS`
- [ ] Set a strong `HELIX_AUTH_JWT_SECRET`
- [ ] Set database and Redis passwords
- [ ] Restrict `/internal/*` endpoints at the network level
- [ ] Use Podman (rootless) for container runtime
- [ ] Enable `HELIX_LOG_FORMAT=json` for structured audit logs
- [ ] Review and configure PII detection rules
- [ ] Set appropriate rate limits
- [ ] Keep all submodules updated
