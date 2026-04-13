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

## Fallback Chain Coordination

The `FallbackChain` (`internal/fallback/`) sits between the Gateway and all Brain providers. Agents and gateway handlers must be aware of the following rules when dispatching or modifying completion requests:

- **Agents dispatch through the chain, not directly to Brain.** All completion calls from the agents layer go via `gateway.Completer`, which resolves to the `FallbackChain` at runtime. Do not import or call `brain.Provider` implementations directly from agents code.
- **Rate limiting is transparent.** `RateLimitTracker` and reactive 429 failover handle provider rotation automatically. Agents do not need to implement retry logic for rate limits — if a provider is exhausted the chain silently moves to the next one. Do not add per-provider retry loops in agent code.
- **Circuit breaker state is per-provider and global.** A provider tripped open by one request type (e.g. a long-context call) will also be skipped for all other concurrent requests until the half-open probe succeeds. When writing integration tests that mock provider failures, account for this: failing a provider 3 times in test will open its breaker for 2 minutes.
- **Memory sync happens automatically for high-importance memories.** `MemoryAdapter` forwards any memory with `importance >= 0.7` to HelixMemory asynchronously after each successful completion. Agents must set the `Importance` field on memories they create; the adapter handles the rest. Low-importance memories (< 0.7) remain session-local and are not forwarded.
- **Local llama.cpp is the guaranteed fallback.** If every cloud provider in the chain is unavailable (all circuit breakers open, all rate limits exhausted), the request is served by the local llama.cpp fleet. Responses will be slower but the chain will never return a hard error solely due to cloud provider unavailability. Agents may observe higher latency under these conditions — this is expected behavior, not a bug.
- **Coordination required for fallback chain changes.** Modifying `internal/fallback/` interfaces or adding/removing providers from the chain requires coordination (see "Coordination Required" above). The chain order is determined by `ScorerBridge` at runtime — hardcoding provider order in tests or agents is forbidden.

## Commit Conventions

Follow Conventional Commits: `type(scope): description`

Types: `feat`, `fix`, `test`, `docs`, `refactor`, `perf`, `chore`
Scopes: `brain`, `fallback`, `gateway`, `knowledge`, `agents`, `control`, `shared`, `deps`
