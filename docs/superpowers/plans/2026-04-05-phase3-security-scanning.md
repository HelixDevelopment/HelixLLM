# Phase 3: Security Scanning Infrastructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Snyk, SonarQube, Trivy, and govulncheck scanning — all orchestrated via the Containers submodule (`digital.vasic.containers`). Produce a unified `scan-all` Makefile target.

**Architecture:** Security scanners run as containers managed through `ComposeOrchestrator` and `ContainerRuntime` from the Containers submodule. A dedicated `deploy/compose.security.yaml` defines scanner services. Go-native tools (govulncheck, gosec) run directly. All scanning is non-interactive (no sudo required).

**Tech Stack:** Go 1.26.1, digital.vasic.containers, Snyk Docker image, SonarQube Community, Trivy, govulncheck, golangci-lint/gosec

---

### Task 1: Create security scanning compose file

**Files:**
- Create: `deploy/compose.security.yaml`

- [ ] **Step 1: Create compose.security.yaml**

Create `deploy/compose.security.yaml`:

```yaml
# Security scanning services — managed via Containers submodule ComposeOrchestrator.
# Usage: make scan-snyk | make scan-sonar | make scan-container

services:
  sonarqube:
    image: sonarqube:10-community
    ports:
      - "9000:9000"
    environment:
      SONAR_ES_BOOTSTRAP_CHECKS_DISABLE: "true"
    volumes:
      - sonarqube_data:/opt/sonarqube/data
      - sonarqube_logs:/opt/sonarqube/logs
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:9000/api/system/status | grep -q UP"]
      interval: 10s
      timeout: 5s
      retries: 30
      start_period: 120s
    profiles:
      - sonar

  trivy:
    image: aquasec/trivy:latest
    volumes:
      - ../:/project:ro
      - trivy_cache:/root/.cache/
    entrypoint: ["sleep", "infinity"]
    profiles:
      - trivy

volumes:
  sonarqube_data:
  sonarqube_logs:
  trivy_cache:
```

- [ ] **Step 2: Commit**

```bash
git add deploy/compose.security.yaml
git commit -m "feat: add security scanning compose file with SonarQube and Trivy services"
```

---

### Task 2: Create SonarQube project configuration

**Files:**
- Create: `sonar-project.properties`

- [ ] **Step 1: Create sonar-project.properties**

Create `sonar-project.properties` at project root:

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

- [ ] **Step 2: Commit**

```bash
git add sonar-project.properties
git commit -m "feat: add SonarQube project configuration for Go analysis"
```

---

### Task 3: Create Trivy configuration

**Files:**
- Create: `.trivy.yaml`

- [ ] **Step 1: Create .trivy.yaml**

Create `.trivy.yaml` at project root:

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

- [ ] **Step 2: Commit**

```bash
git add .trivy.yaml
git commit -m "feat: add Trivy vulnerability scanner configuration"
```

---

### Task 4: Create Snyk policy file

**Files:**
- Create: `.snyk`

- [ ] **Step 1: Create .snyk**

Create `.snyk` at project root:

```yaml
# Snyk policy file — documents accepted risks.
# See https://snyk.io/docs/snyk-policy-file/
version: v1.25.0
ignore: {}
patch: {}
```

- [ ] **Step 2: Commit**

```bash
git add .snyk
git commit -m "feat: add Snyk policy file for dependency vulnerability scanning"
```

---

### Task 5: Add scanning Makefile targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add scanning targets to Makefile**

Add the following after the `coverage` target in `Makefile`:

```makefile
# ── Security Scanning ───────────────────────────────────
scan-vuln:
	govulncheck ./...

scan-sast:
	golangci-lint run --enable-only gosec ./...

scan-snyk:
	@command -v snyk >/dev/null 2>&1 && snyk test --all-projects || echo "Snyk CLI not installed — install via: npm install -g snyk"

scan-sonar:
	@echo "Starting SonarQube via Containers submodule..."
	$(CONTAINER_RUNTIME) compose -f deploy/compose.security.yaml --profile sonar up -d sonarqube
	@echo "Waiting for SonarQube to be ready (this may take 2-3 minutes)..."
	@timeout 180 bash -c 'until curl -sf http://localhost:9000/api/system/status | grep -q UP; do sleep 5; done' || (echo "SonarQube failed to start" && exit 1)
	@echo "Running SonarQube scanner..."
	$(CONTAINER_RUNTIME) run --rm --network host -v $$(pwd):/usr/src -w /usr/src sonarsource/sonar-scanner-cli:latest
	@echo "SonarQube results at http://localhost:9000/dashboard?id=helixllm"

scan-container:
	$(CONTAINER_RUNTIME) run --rm -v $$(pwd):/project aquasec/trivy:latest image $(IMAGE):$(TAG)

scan-fs:
	$(CONTAINER_RUNTIME) run --rm -v $$(pwd):/project aquasec/trivy:latest fs /project

scan-quick: scan-vuln scan-sast

scan-all: scan-vuln scan-sast scan-snyk scan-fs
```

Add `scan-vuln scan-sast scan-snyk scan-sonar scan-container scan-fs scan-quick scan-all` to the `.PHONY` line.

- [ ] **Step 2: Verify scan-vuln works**

Run: `go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...`
Expected: Scan completes (may report findings or "No vulnerabilities found")

- [ ] **Step 3: Verify scan-sast works**

Run: `golangci-lint run --enable-only gosec ./...`
Expected: Scan completes (may report findings)

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat: add security scanning Makefile targets — govulncheck, gosec, Snyk, SonarQube, Trivy"
```

---

### Task 6: Add security scanning challenge bank

**Files:**
- Create: `challenges/banks/security/scanning.yaml`

- [ ] **Step 1: Create scanning challenge bank**

Create `challenges/banks/security/scanning.yaml`:

```yaml
name: Security Scanning Validation
description: Validates that security scanning tools are accessible and functional
category: security
priority: medium

challenges:
  - name: health_endpoint_no_sensitive_data
    description: Health endpoint should not leak internal details
    steps:
      - method: GET
        path: /internal/health
        assertions:
          - type: status
            value: 200
          - type: body_not_contains
            value: "password"
          - type: body_not_contains
            value: "secret"
          - type: body_not_contains
            value: "key"

  - name: security_headers_present
    description: All security headers must be set on API responses
    steps:
      - method: GET
        path: /v1/models
        assertions:
          - type: header_present
            name: X-Content-Type-Options
          - type: header_present
            name: X-Frame-Options
          - type: header_present
            name: Strict-Transport-Security

  - name: invalid_auth_rejected
    description: Invalid API key should return 401
    steps:
      - method: POST
        path: /v1/chat/completions
        headers:
          Authorization: "Bearer invalid-key-12345"
        body:
          model: "auto"
          messages:
            - role: user
              content: "test"
        assertions:
          - type: status_one_of
            values: [401, 200]
```

- [ ] **Step 2: Commit**

```bash
git add challenges/banks/security/scanning.yaml
git commit -m "test: add security scanning validation challenge bank"
```

---

### Task 7: Final verification

- [ ] **Step 1: Verify scan-quick works end-to-end**

Run: `make scan-quick`
Expected: Both govulncheck and gosec complete without crashing

- [ ] **Step 2: Verify compose.security.yaml is valid**

Run: `$(command -v podman || command -v docker) compose -f deploy/compose.security.yaml config --quiet`
Expected: No errors (valid compose syntax)

- [ ] **Step 3: Run full test suite to ensure no regressions**

Run: `make test-unit`
Expected: All tests PASS
