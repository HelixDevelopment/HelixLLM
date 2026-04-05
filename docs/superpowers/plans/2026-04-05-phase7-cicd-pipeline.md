# Phase 7: CI/CD Pipeline & Automation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish GitHub Actions CI/CD pipelines for continuous integration, weekly security scanning, and release automation. Create root-level AGENTS.md for agent collaboration.

**Architecture:** Three separate workflow files: CI (on push/PR), Security (weekly schedule), Release (on tag). All use existing Makefile targets. Container operations use the Containers submodule indirectly via Makefile.

**Tech Stack:** GitHub Actions, Go 1.26.1, golangci-lint, govulncheck, Makefile

---

### Task 1: Create CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create .github/workflows directory**

Run: `mkdir -p .github/workflows`

- [ ] **Step 2: Create CI workflow file**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build-and-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          cache: true

      - name: Install tools
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

      - name: Check formatting
        run: |
          gofmt -l . | tee /tmp/gofmt.out
          test ! -s /tmp/gofmt.out || (echo "Files need gofmt:" && cat /tmp/gofmt.out && exit 1)

      - name: Lint
        run: make lint

      - name: Unit tests with race detection
        run: make test-unit

      - name: Coverage threshold
        run: make coverage

      - name: Vulnerability scan
        run: make scan-vuln

      - name: SAST scan
        run: make scan-sast

      - name: Build binary
        run: make build

      - name: Upload coverage
        if: github.event_name == 'push' && github.ref == 'refs/heads/main'
        uses: actions/upload-artifact@v4
        with:
          name: coverage-report
          path: coverage-unit.out
```

- [ ] **Step 3: Validate workflow syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "Valid YAML"`
Expected: "Valid YAML"

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "feat: add GitHub Actions CI pipeline with lint, test, coverage, and security scans"
```

---

### Task 2: Create security scanning workflow

**Files:**
- Create: `.github/workflows/security.yml`

- [ ] **Step 1: Create security workflow**

Create `.github/workflows/security.yml`:

```yaml
name: Security Scan

on:
  schedule:
    - cron: "0 6 * * 1"  # Every Monday at 06:00 UTC
  workflow_dispatch:       # Allow manual trigger

jobs:
  vulnerability-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          cache: true

      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Go vulnerability scan
        run: govulncheck ./...

      - name: Trivy filesystem scan
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: fs
          scan-ref: .
          severity: CRITICAL,HIGH
          exit-code: 1

  container-scan:
    runs-on: ubuntu-latest
    needs: vulnerability-scan
    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          cache: true

      - name: Build container
        run: make container

      - name: Trivy container scan
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: helixllm:dev
          severity: CRITICAL,HIGH
          exit-code: 1
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/security.yml
git commit -m "feat: add weekly security scanning workflow with govulncheck and Trivy"
```

---

### Task 3: Create release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      packages: write

    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          cache: true

      - name: Run tests
        run: make test-unit

      - name: Build binary
        run: make build

      - name: Build container
        run: make container IMAGE=helixllm TAG=${{ github.ref_name }}

      - name: Generate changelog
        id: changelog
        run: |
          PREV_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
          if [ -n "$PREV_TAG" ]; then
            echo "## Changes since $PREV_TAG" > /tmp/changelog.md
            git log ${PREV_TAG}..HEAD --pretty=format:"- %s (%h)" >> /tmp/changelog.md
          else
            echo "## Initial Release" > /tmp/changelog.md
            git log --pretty=format:"- %s (%h)" >> /tmp/changelog.md
          fi

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          body_path: /tmp/changelog.md
          files: bin/helixllm
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "feat: add release workflow with binary build, container, and changelog generation"
```

---

### Task 4: Create root-level AGENTS.md

**Files:**
- Create: `AGENTS.md`

- [ ] **Step 1: Create AGENTS.md**

Create `AGENTS.md` at project root:

```markdown
# AGENTS.md — HelixLLM Agent Collaboration Rules

This file defines constraints for automated agents working on the HelixLLM codebase.

## General Rules

- **No interactive processes** — no sudo, no password prompts, no TTY-dependent commands
- **No destructive git operations** — no force push, no hard reset, no branch deletion without explicit user request
- **Respect all CLAUDE.md files** — the root CLAUDE.md and every submodule's CLAUDE.md define build, test, and style conventions
- **Run tests after changes** — every code change must be validated with `make test-unit` at minimum
- **No breaking changes** — changes must not break existing working functionality

## Safe Parallel Changes (No Coordination Required)

- Adding new test files (`*_test.go`)
- Adding new challenge bank YAML files (`challenges/banks/**/*.yaml`)
- Adding new documentation files (`docs/**/*.md`)
- Adding new benchmark functions
- Modifying code within a single package (if no interface changes)

## Coordination Required

- **Interface changes** — modifying `brain.Provider`, `agents.Tool`, `knowledge.VectorStore`, or any shared interface
- **Config changes** — adding new environment variables to `internal/shared/config/config.go`
- **go.mod changes** — adding or removing dependencies
- **Makefile changes** — adding or modifying build/test targets
- **Submodule updates** — changing submodule references
- **API surface changes** — modifying HTTP route registrations in gateway

## Submodule AGENTS.md Files

Each of the 35 submodules under `submodules/` has its own `AGENTS.md` with package-specific constraints. Agents working on submodule code must read the relevant submodule's `AGENTS.md` before making changes.

## Test Requirements

| Change Type | Required Tests |
|-------------|---------------|
| Bug fix | Unit test reproducing the bug + fix verification |
| New feature | Unit tests + integration test if touching API surface |
| Refactor | All existing tests must pass unchanged |
| Performance | Benchmark before/after comparison |

## Commit Conventions

Follow Conventional Commits: `type(scope): description`

Types: `feat`, `fix`, `test`, `docs`, `refactor`, `perf`, `chore`
Scopes: `brain`, `gateway`, `knowledge`, `agents`, `control`, `shared`, `deps`
```

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs: add root-level AGENTS.md for agent collaboration rules"
```

---

### Task 5: Final verification

- [ ] **Step 1: Validate all workflow files**

Run: `for f in .github/workflows/*.yml; do python3 -c "import yaml; yaml.safe_load(open('$f'))" && echo "$f: valid"; done`
Expected: All files valid

- [ ] **Step 2: Verify AGENTS.md is present**

Run: `test -f AGENTS.md && echo "AGENTS.md exists"`
Expected: "AGENTS.md exists"

- [ ] **Step 3: Run full test suite**

Run: `make test-unit`
Expected: All tests PASS
