# Lesson 4: CI/CD Pipeline

**Duration:** 20 minutes
**Prerequisites:** Lesson 3 (Challenge Banks)
**Learning Objectives:**
- Understand the GitHub Actions CI workflow for automated testing
- Configure security scanning as part of the build pipeline
- Set up automated release builds with container image publishing
- Implement branch protection rules for safe collaboration

---

## Scene 1: CI Pipeline Overview (4 min)

**Narration:** "HelixLLM uses GitHub Actions for continuous integration. Every push and pull request triggers a pipeline that builds the binary, runs all tests, checks coverage, lints the code, and scans for vulnerabilities. Let me walk through the workflow."

**Screen:** Show the CI pipeline stages.

```
Push / Pull Request
    |
    v
+------------------+
| 1. Build         |  go build ./cmd/helixllm
|    (1 min)       |
+------------------+
    |
    v
+------------------+
| 2. Lint          |  golangci-lint run ./...
|    (2 min)       |
+------------------+
    |
    v
+------------------+
| 3. Unit Tests    |  go test -v -race -count=1 ./internal/...
|    (3 min)       |
+------------------+
    |
    v
+------------------+
| 4. Coverage      |  Enforce 85% threshold
|    (30 sec)      |
+------------------+
    |
    v
+------------------+
| 5. Integration   |  go test -v -count=1 ./tests/integration/
|    (5 min)       |
+------------------+
    |
    v
+------------------+
| 6. Security Scan |  Vulnerability scanning
|    (2 min)       |
+------------------+
```

**Key points:**
- Every push triggers the full pipeline
- Pull requests must pass before merging
- Race detection is enabled in CI (catches concurrency bugs)
- Coverage threshold is enforced -- build fails below 85%
- The entire pipeline runs in under 15 minutes

---

## Scene 2: GitHub Actions Workflow (5 min)

**Narration:** "The CI workflow is defined in a YAML file under .github/workflows/. Let me show you the key parts."

**Screen:** Show the workflow configuration.

```yaml
# .github/workflows/ci.yml
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
      - name: Checkout with submodules
        uses: actions/checkout@v4
        with:
          submodules: recursive

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Build
        run: make build

      - name: Lint
        run: make lint

      - name: Unit Tests with Race Detection
        run: go test -v -race -count=1 -coverprofile=coverage-unit.out ./internal/...

      - name: Coverage Check
        run: make coverage

      - name: Integration Tests
        run: make test-integration

      - name: Upload Coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage-report
          path: coverage-unit.out
```

**Narration:** "The workflow checks out the repository with all submodules, sets up Go, then runs each stage sequentially. The coverage report is uploaded as an artifact for later review."

**Key points:**
- `submodules: recursive` ensures all 37 submodules are available
- Go version matches the project's go.mod
- Race detection is enabled for CI runs
- Coverage artifact is preserved for review
- Each step fails fast -- later steps do not run if earlier ones fail

---

## Scene 3: Security Scanning (4 min)

**Narration:** "Security scanning runs as a separate workflow or as an additional step in the CI pipeline. It checks for known vulnerabilities in dependencies and potential security issues in the code."

**Screen:** Show the security scanning configuration.

```yaml
# .github/workflows/security.yml
name: Security

on:
  push:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'  # Weekly on Monday

jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          submodules: recursive

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Go Vulnerability Check
        run: go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...

      - name: Dependency Audit
        run: go mod verify

      - name: Static Analysis
        run: make lint
```

**Narration:** "govulncheck checks your dependencies against the Go vulnerability database. It reports known CVEs in packages you actually use, not just packages in your dependency tree. The workflow runs on every push and also on a weekly schedule to catch newly disclosed vulnerabilities."

**Key points:**
- `govulncheck` checks against the Go vulnerability database
- `go mod verify` validates dependency integrity
- Weekly scheduled runs catch newly disclosed CVEs
- Static analysis via golangci-lint catches code-level issues
- Security scan failures should block merging

---

## Scene 4: Release Workflow (4 min)

**Narration:** "When you are ready to release, a separate workflow builds the production binary and container image."

**Screen:** Show the release workflow.

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          submodules: recursive

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Run Full Test Suite
        run: make test-all

      - name: Build Binary
        run: make build

      - name: Build Container Image
        run: make container

      - name: Push Container Image
        run: make container-push
        env:
          REGISTRY_TOKEN: ${{ secrets.REGISTRY_TOKEN }}

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: bin/helixllm
```

**Narration:** "The release workflow triggers when you push a version tag. It runs the full test suite first, then builds the binary and container image, pushes the image to a registry, and creates a GitHub release with the binary attached."

**Demo steps:**

```bash
# Create a release
git tag v1.0.0
git push origin v1.0.0
# This triggers the release workflow
```

**Key points:**
- Triggered by version tags (e.g., `v1.0.0`)
- Runs the full test suite before building
- Builds both binary and container image
- Pushes to container registry with authenticated credentials
- Creates a GitHub release with the binary as a download

---

## Scene 5: Branch Protection and PR Process (3 min)

**Narration:** "Branch protection rules prevent merging code that has not passed the pipeline."

**Screen:** Show recommended branch protection settings.

| Setting | Value | Purpose |
|---------|-------|---------|
| Require pull request | Yes | No direct pushes to main |
| Required reviewers | 1+ | Code review before merge |
| Require status checks | CI, Security | Pipeline must pass |
| Require branch up to date | Yes | Must be rebased on latest main |
| Dismiss stale reviews | Yes | Re-review after new pushes |

**Narration:** "The PR process is: create a branch, make your changes, open a pull request, wait for CI to pass, get a code review, then merge. The branch protection rules enforce this process for the main branch."

```bash
# Standard PR workflow
git checkout -b feature/my-change
# ... make changes ...
make lint
make test-unit
git add -A
git commit -m "feat: add my feature"
git push -u origin feature/my-change
# Open PR on GitHub
```

**Key points:**
- All changes go through pull requests
- CI must pass before merging
- At least one code review required
- Branch must be up to date with main
- Stale reviews are dismissed when new commits are pushed

---

## Exercises

1. Examine the `.github/workflows/` directory in the repository and trace each workflow step to understand what runs and in what order
2. Create a feature branch, make a small change (such as adding a test), push it, and open a pull request to observe the CI pipeline running
3. Set up branch protection rules on a test repository requiring status checks and pull requests, then verify that direct pushes to main are blocked
