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
