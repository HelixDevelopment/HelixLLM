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

---

## Security Scanning

HelixLLM provides multiple scanning tools for vulnerability detection, static analysis, and container security. These are orchestrated via Makefile targets and can be integrated into CI pipelines.

### Quick Scan

```bash
make scan-quick
```

Runs the two fastest scanners in sequence:

1. **govulncheck** -- Checks Go dependencies against the Go vulnerability database
2. **gosec** -- Static analysis for Go security issues (via golangci-lint)

Use this as a pre-commit check or in fast CI feedback loops.

### Full Scan Suite

```bash
make scan-all
```

Runs the complete scanning suite:

1. **govulncheck** (`scan-vuln`) -- Dependency vulnerability check
2. **gosec** (`scan-sast`) -- Static application security testing
3. **Snyk** (`scan-snyk`) -- Third-party vulnerability database (requires Snyk CLI)
4. **Trivy filesystem** (`scan-fs`) -- Filesystem scan for vulnerabilities, misconfigurations, and secrets

### Individual Scanners

#### Vulnerability Check (govulncheck)

```bash
make scan-vuln
```

Underlying command:

```bash
govulncheck ./...
```

Scans all Go packages against the official Go vulnerability database. Reports only vulnerabilities that are actually reachable in the call graph, reducing false positives compared to dependency-only scanners.

Install: `go install golang.org/x/vuln/cmd/govulncheck@latest`

#### Static Application Security Testing (gosec)

```bash
make scan-sast
```

Underlying command:

```bash
golangci-lint run --enable-only gosec ./...
```

Runs gosec rules through golangci-lint for consistent integration with the existing lint pipeline. Detects common Go security issues including hardcoded credentials, SQL injection, weak cryptography, and insecure file permissions.

#### Snyk Dependency Scan

```bash
make scan-snyk
```

Scans all project dependencies using the Snyk vulnerability database. Requires the Snyk CLI:

```bash
npm install -g snyk
snyk auth
```

If the Snyk CLI is not installed, the target prints installation instructions and exits gracefully.

#### SonarQube Analysis

```bash
make scan-sonar
```

Starts a SonarQube instance via container compose and runs the scanner. The process:

1. Starts SonarQube via `deploy/compose.security.yaml` (sonar profile)
2. Waits up to 3 minutes for SonarQube to report UP status
3. Runs the sonar-scanner-cli container against the project
4. Results are available at `http://localhost:9000/dashboard?id=helixllm`

The scanner uses the configuration in `sonar-project.properties`:

```properties
sonar.projectKey=helixllm
sonar.projectName=HelixLLM
sonar.projectVersion=1.0

sonar.sources=internal/,pkg/,cmd/
sonar.tests=internal/,tests/
sonar.test.inclusions=**/*_test.go

sonar.go.coverage.reportPaths=coverage-unit.out
sonar.go.tests.reportPaths=test-report.json

sonar.exclusions=submodules/**,vendor/**,bin/**,certs/**

sonar.qualitygate.wait=true
```

Key configuration points:

- **Sources:** Scans `internal/`, `pkg/`, and `cmd/` directories
- **Test inclusions:** All `*_test.go` files
- **Exclusions:** Submodules, vendor, build artifacts, and certificates are excluded
- **Quality gate:** The scan waits for the quality gate result and fails if the gate is not passed
- **Coverage:** Reads from `coverage-unit.out` (run `make coverage` first for accurate results)

#### Container Image Scan (Trivy)

```bash
make scan-container
```

Underlying command:

```bash
podman run --rm -v $(pwd):/project aquasec/trivy:latest image helixllm:dev
```

Scans the built container image (`helixllm:dev`) for OS package vulnerabilities, language-specific dependency issues, and misconfigurations. Build the container first with `make container`.

#### Filesystem Scan (Trivy)

```bash
make scan-fs
```

Underlying command:

```bash
podman run --rm -v $(pwd):/project aquasec/trivy:latest fs /project
```

Scans the project filesystem for vulnerabilities in dependencies, exposed secrets, and IaC misconfigurations. Does not require a built container image.

### Trivy Configuration

Trivy is configured via `.trivy.yaml` in the project root:

```yaml
severity:
  - CRITICAL
  - HIGH
  - MEDIUM

security-checks:
  - vuln
  - config
  - secret

ignore-unfixed: true

exit-code: 1

timeout: 10m
```

Configuration details:

| Setting | Value | Purpose |
|---------|-------|---------|
| `severity` | CRITICAL, HIGH, MEDIUM | Only report vulnerabilities at these levels; LOW is ignored |
| `security-checks` | vuln, config, secret | Check for vulnerabilities, misconfigurations, and leaked secrets |
| `ignore-unfixed` | true | Suppress findings that have no available fix yet |
| `exit-code` | 1 | Return non-zero on findings, causing CI failure |
| `timeout` | 10m | Maximum scan duration before timeout |

### Snyk Policy File

The `.snyk` policy file documents accepted risks and applied patches:

```yaml
version: v1.25.0
ignore: {}
patch: {}
```

To ignore a known false positive or accepted risk:

```yaml
ignore:
  SNYK-GOLANG-EXAMPLE-12345:
    - '*':
        reason: 'False positive, not reachable in our code'
        expires: 2026-07-01T00:00:00.000Z
```

The `ignore` section suppresses specific vulnerability IDs. Each entry requires a `reason` and an `expires` date to force periodic review. The `patch` section can apply Snyk-provided patches to vulnerable dependencies.

### Vulnerability Triage Process

When a scanner reports a finding, follow this triage process:

1. **Assess reachability:** Determine whether the vulnerable code path is actually called in HelixLLM. govulncheck already does this for Go dependencies; for container vulnerabilities, manual review is needed.

2. **Check severity:** CRITICAL and HIGH findings must be addressed before the next release. MEDIUM findings should be tracked and resolved within two release cycles. LOW findings are informational.

3. **Determine resolution:**
   - **Upgrade:** If a fixed version exists, update the dependency
   - **Patch:** If a Snyk patch is available, apply it via `.snyk`
   - **Mitigate:** If no fix exists, document the mitigation (e.g., network-level controls) and add to `.snyk` ignore with expiration
   - **Accept:** For false positives or unreachable code, add to `.snyk` ignore with reason

4. **Document:** Record the triage decision in the commit message or pull request description. Include the vulnerability ID, severity, and resolution.

5. **Verify:** After resolution, re-run the relevant scanner to confirm the finding is resolved.

### Scanning in CI

The recommended CI integration runs scanners in this order:

```
make scan-quick          # Fast feedback (govulncheck + gosec)
make scan-fs             # Filesystem scan (Trivy)
make scan-container      # Container image scan (after build)
make scan-sonar          # Full quality analysis (optional, requires SonarQube)
```

The `scan-quick` target should be part of every CI run. Container scanning runs after `make container`. SonarQube analysis is typically run on merge to the main branch or as a scheduled nightly job.
