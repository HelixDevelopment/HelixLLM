# Phase 10: Hugo Website

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete Hugo static website with all documentation, API reference, course catalog, blog/changelog, and community pages — ready to deploy.

**Architecture:** Hugo static site using the hugo-book theme (documentation-focused, clean, minimal). Content mirrors the docs/ directory structure with Hugo front matter. Mermaid diagrams rendered client-side via shortcode. Makefile targets for build and serve.

**Tech Stack:** Hugo, hugo-book theme, Markdown, Mermaid.js, HTML/CSS

---

### Task 1: Initialize Hugo site

**Files:**
- Create: `website/config.toml`
- Create: `website/go.mod` (Hugo module)

- [ ] **Step 1: Create website directory**

Run: `mkdir -p website`

- [ ] **Step 2: Initialize Hugo site**

Run: `cd website && hugo new site . --force && cd ..`

If Hugo is not installed, install it first:
Run: `go install github.com/gohugoio/hugo@latest`

- [ ] **Step 3: Create Hugo config**

Create `website/config.toml`:

```toml
baseURL = "https://helixllm.dev/"
title = "HelixLLM"
theme = "hugo-book"

enableGitInfo = true
defaultContentLanguage = "en"

[params]
  description = "Enterprise-grade distributed LLM system built in Go"
  BookToC = true
  BookSection = "docs"
  BookRepo = "https://github.com/HelixDevelopment/HelixLLM"
  BookEditPath = "edit/main/website/content"

[markup]
  [markup.goldmark]
    [markup.goldmark.renderer]
      unsafe = true
  [markup.highlight]
    style = "github"
    lineNos = false

[menu]
  [[menu.before]]
    name = "GitHub"
    url = "https://github.com/HelixDevelopment/HelixLLM"
    weight = 1
```

- [ ] **Step 4: Add hugo-book theme as git submodule**

Run: `cd website && git submodule add https://github.com/alex-shpak/hugo-book themes/hugo-book && cd ..`

Or if git submodules are preferred to be avoided for the website, use Hugo modules:

Create `website/go.mod`:
```
module github.com/HelixDevelopment/HelixLLM/website

go 1.26
```

And add to `website/config.toml`:
```toml
[module]
  [[module.imports]]
    path = "github.com/alex-shpak/hugo-book"
```

- [ ] **Step 5: Commit**

```bash
git add website/config.toml website/go.mod
git commit -m "feat: initialize Hugo website with hugo-book theme"
```

---

### Task 2: Create landing page and navigation

**Files:**
- Create: `website/content/_index.md`
- Create: `website/content/menu/index.md`

- [ ] **Step 1: Create content directory**

Run: `mkdir -p website/content/docs/user-guide website/content/docs/manual website/content/api website/content/courses website/content/blog website/content/community`

- [ ] **Step 2: Create landing page**

Create `website/content/_index.md`:

```markdown
---
title: "HelixLLM"
type: docs
---

# HelixLLM

**Enterprise-grade distributed LLM system built in Go.**

HelixLLM is a single binary that provides a complete LLM infrastructure stack — API-compatible with OpenAI and Anthropic, supporting local inference via llama.cpp, RAG pipelines, ReAct agents, and multi-host cluster deployment.

## Key Features

- **Drop-in API compatibility** — OpenAI and Anthropic clients work without modification
- **Local LLM inference** — Run models locally via llama.cpp for complete privacy
- **RAG pipeline** — Ingest documents, generate embeddings, semantic search
- **Agent system** — ReAct loop with tool calling and MCP integration
- **Multi-host distribution** — Deploy across cluster via SSH, auto-schedule workloads
- **Production-ready** — HTTP/3, TLS 1.3, rate limiting, Prometheus metrics, OTEL tracing

## Quick Start

```bash
git clone https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM
make deps && make certs && make dev
```

```bash
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Hello!"}]}'
```

## Documentation

- [Getting Started]({{< relref "/docs/user-guide/getting-started" >}})
- [API Reference]({{< relref "/docs/user-guide/api-reference" >}})
- [Architecture]({{< relref "/docs/manual/architecture" >}})
- [Video Courses]({{< relref "/courses" >}})
```

- [ ] **Step 3: Create navigation menu**

Create `website/content/menu/index.md`:

```markdown
---
headless: true
---

- **User Guide**
  - [Getting Started]({{< relref "/docs/user-guide/getting-started" >}})
  - [Configuration]({{< relref "/docs/user-guide/configuration" >}})
  - [API Reference]({{< relref "/docs/user-guide/api-reference" >}})
  - [Models]({{< relref "/docs/user-guide/models" >}})
  - [Agents]({{< relref "/docs/user-guide/agents" >}})
  - [RAG Knowledge]({{< relref "/docs/user-guide/rag-knowledge" >}})
  - [Multi-Host Setup]({{< relref "/docs/user-guide/multi-host-setup" >}})
  - [Monitoring]({{< relref "/docs/user-guide/monitoring" >}})
  - [Troubleshooting]({{< relref "/docs/user-guide/troubleshooting" >}})
- **Developer Manual**
  - [Architecture]({{< relref "/docs/manual/architecture" >}})
  - [Development]({{< relref "/docs/manual/development" >}})
  - [Testing]({{< relref "/docs/manual/testing" >}})
  - [Security]({{< relref "/docs/manual/security" >}})
  - [Operations]({{< relref "/docs/manual/operations" >}})
  - [Modules]({{< relref "/docs/manual/modules" >}})
- **[API Reference]({{< relref "/api" >}})**
- **[Video Courses]({{< relref "/courses" >}})**
- **[Blog]({{< relref "/blog" >}})**
```

- [ ] **Step 4: Commit**

```bash
git add website/content/
git commit -m "feat: add website landing page and navigation structure"
```

---

### Task 3: Create Mermaid shortcode

**Files:**
- Create: `website/layouts/shortcodes/mermaid.html`

- [ ] **Step 1: Create layouts directory**

Run: `mkdir -p website/layouts/shortcodes`

- [ ] **Step 2: Create mermaid shortcode**

Create `website/layouts/shortcodes/mermaid.html`:

```html
<div class="mermaid">
{{ .Inner }}
</div>
{{ if not (.Page.Scratch.Get "mermaidLoaded") }}
{{ .Page.Scratch.Set "mermaidLoaded" true }}
<script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
<script>mermaid.initialize({startOnLoad:true, theme:'neutral'});</script>
{{ end }}
```

- [ ] **Step 3: Commit**

```bash
git add website/layouts/
git commit -m "feat: add Mermaid diagram shortcode for Hugo website"
```

---

### Task 4: Migrate documentation to Hugo content

**Files:**
- Create: `website/content/docs/user-guide/*.md` (9 files)
- Create: `website/content/docs/manual/*.md` (6 files)

- [ ] **Step 1: Create migration script**

For each file in `docs/user-guide/` and `docs/manual/`, create a Hugo-compatible version by prepending front matter. The content remains the same — only front matter is added.

Example front matter for `getting-started.md`:
```yaml
---
title: "Getting Started"
weight: 1
bookToC: true
---
```

Create a simple script to migrate all docs:

```bash
# Migrate user-guide
for f in docs/user-guide/*.md; do
    name=$(basename "$f" .md)
    weight=$(echo "$name" | grep -c "getting-started\|configuration\|api-reference\|models\|agents\|rag-knowledge\|multi-host-setup\|monitoring\|troubleshooting" || true)
    title=$(head -1 "$f" | sed 's/^# //')
    mkdir -p website/content/docs/user-guide
    echo "---" > "website/content/docs/user-guide/$(basename $f)"
    echo "title: \"$title\"" >> "website/content/docs/user-guide/$(basename $f)"
    echo "weight: 1" >> "website/content/docs/user-guide/$(basename $f)"
    echo "---" >> "website/content/docs/user-guide/$(basename $f)"
    echo "" >> "website/content/docs/user-guide/$(basename $f)"
    # Skip the first line (title) since Hugo generates it from front matter
    tail -n +2 "$f" >> "website/content/docs/user-guide/$(basename $f)"
done

# Migrate manual
for f in docs/manual/*.md; do
    title=$(head -1 "$f" | sed 's/^# //')
    mkdir -p website/content/docs/manual
    echo "---" > "website/content/docs/manual/$(basename $f)"
    echo "title: \"$title\"" >> "website/content/docs/manual/$(basename $f)"
    echo "weight: 1" >> "website/content/docs/manual/$(basename $f)"
    echo "---" >> "website/content/docs/manual/$(basename $f)"
    echo "" >> "website/content/docs/manual/$(basename $f)"
    tail -n +2 "$f" >> "website/content/docs/manual/$(basename $f)"
done
```

- [ ] **Step 2: Create section index files**

Create `website/content/docs/user-guide/_index.md`:
```markdown
---
title: "User Guide"
weight: 1
bookCollapseSection: false
---

# User Guide

End-user documentation for installing, configuring, and using HelixLLM.
```

Create `website/content/docs/manual/_index.md`:
```markdown
---
title: "Developer Manual"
weight: 2
bookCollapseSection: false
---

# Developer Manual

Technical documentation for developers and operators working with HelixLLM.
```

- [ ] **Step 3: Commit**

```bash
git add website/content/docs/
git commit -m "feat: migrate all documentation to Hugo content with front matter"
```

---

### Task 5: Create API reference, courses, blog, and community pages

**Files:**
- Create: `website/content/api/_index.md`
- Create: `website/content/courses/_index.md`
- Create: `website/content/blog/_index.md`
- Create: `website/content/blog/changelog.md`
- Create: `website/content/community/_index.md`

- [ ] **Step 1: Create API reference page**

Create `website/content/api/_index.md`:

```markdown
---
title: "API Reference"
weight: 3
bookToC: true
---

# API Reference

HelixLLM provides OpenAI-compatible and Anthropic-compatible REST APIs.

## Base URL

```
https://localhost:8443
```

## Authentication

Include your API key in the Authorization header:
```
Authorization: Bearer your-api-key
```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/chat/completions` | Chat completion (OpenAI-compatible) |
| POST | `/v1/completions` | Text completion (OpenAI-compatible) |
| GET | `/v1/models` | List available models |
| POST | `/v1/embeddings` | Generate embeddings |
| POST | `/v1/messages` | Chat (Anthropic-compatible) |
| POST | `/v1/agents/chat` | Agent chat with tool calling |
| GET | `/v1/agents/tools` | List available agent tools |
| GET | `/internal/health` | Health check |
| GET | `/internal/metrics` | Prometheus metrics |

See [API Reference Guide]({{< relref "/docs/user-guide/api-reference" >}}) for complete endpoint documentation with examples.
```

- [ ] **Step 2: Create courses index**

Create `website/content/courses/_index.md`:

```markdown
---
title: "Video Courses"
weight: 4
---

# Video Courses

Comprehensive video course scripts covering every aspect of HelixLLM.

{{< hint info >}}
These are detailed lesson scripts with code examples and demo steps. Video recordings coming soon.
{{< /hint >}}

| Course | Lessons | Duration |
|--------|---------|----------|
| Getting Started | 5 | ~95 min |
| API Deep Dive | 4 | ~90 min |
| RAG Pipeline | 4 | ~105 min |
| Agent System | 4 | ~100 min |
| Production Deployment | 5 | ~135 min |
| Development & Testing | 4 | ~100 min |

**Total: 26 lessons, ~10.4 hours**
```

- [ ] **Step 3: Create blog with changelog**

Create `website/content/blog/_index.md`:

```markdown
---
title: "Blog"
weight: 5
---

# Blog

Release notes, technical articles, and project updates.
```

Create `website/content/blog/changelog.md`:

```markdown
---
title: "Changelog"
weight: 1
---

# Changelog

All notable changes to HelixLLM are documented here.

For the full commit history, see the [GitHub repository](https://github.com/HelixDevelopment/HelixLLM/commits/main).
```

- [ ] **Step 4: Create community page**

Create `website/content/community/_index.md`:

```markdown
---
title: "Community"
weight: 6
---

# Community

## Contributing

1. Fork the repository
2. Create a feature branch
3. Follow conventions in CLAUDE.md and AGENTS.md
4. Write tests (unit + integration at minimum)
5. Submit a pull request

See [Development Guide]({{< relref "/docs/manual/development" >}}) for detailed setup instructions.

## Links

- [GitHub Repository](https://github.com/HelixDevelopment/HelixLLM)
- [Issue Tracker](https://github.com/HelixDevelopment/HelixLLM/issues)
- [GitLab Mirror](https://gitlab.com/helixdevelopment1/helixllm)
```

- [ ] **Step 5: Commit**

```bash
git add website/content/
git commit -m "feat: add API reference, courses, blog, and community website pages"
```

---

### Task 6: Add Makefile targets for website

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add website targets**

Add to `Makefile` after the Development section:

```makefile
# ── Website ─────────────────────────────────────────────
website:
	cd website && hugo --minify

website-serve:
	cd website && hugo server --bind 0.0.0.0 --port 1313

website-clean:
	rm -rf website/public/
```

Add `website website-serve website-clean` to the `.PHONY` line.

- [ ] **Step 2: Commit**

```bash
git add Makefile
git commit -m "feat: add website Makefile targets for Hugo build and serve"
```

---

### Task 7: Create GitHub Pages deployment workflow

**Files:**
- Create: `.github/workflows/website.yml`

- [ ] **Step 1: Create website deployment workflow**

Create `.github/workflows/website.yml`:

```yaml
name: Deploy Website

on:
  push:
    branches: [main]
    paths:
      - "website/**"
      - "docs/**"
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive
          fetch-depth: 0

      - uses: peaceiris/actions-hugo@v3
        with:
          hugo-version: "latest"
          extended: true

      - name: Build website
        run: cd website && hugo --minify

      - uses: actions/upload-pages-artifact@v3
        with:
          path: website/public

  deploy:
    needs: build
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/deploy-pages@v4
        id: deployment
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/website.yml
git commit -m "feat: add GitHub Pages deployment workflow for website"
```

---

### Task 8: Final verification

- [ ] **Step 1: Verify Hugo build works**

Run: `cd website && hugo --minify 2>&1 | tail -5 && cd ..`
Expected: Build succeeds with page count output

- [ ] **Step 2: Count content pages**

Run: `find website/content -name "*.md" | wc -l`
Expected: 20+ content pages

- [ ] **Step 3: Verify no broken links in navigation**

Run: `cd website && hugo --minify 2>&1 | grep -i "error\|warn" | head -10 && cd ..`
Expected: No errors (warnings about missing pages are acceptable at this stage)

- [ ] **Step 4: Run full test suite to ensure no regressions**

Run: `make test-unit`
Expected: All tests PASS (website changes should not affect Go code)
