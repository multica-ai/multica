# Platform Extension Squad Runtime Implementation Plan

> **Follow-up:** 本计划已经完成的导入时固定 Runtime 基线保持不变。通用 Runtime Pool、调用时分配与 Session Affinity 的增量实施使用 `docs/superpowers/plans/2026-08-09-runtime-pool-session-affinity.md`。

> **Execution mode:** Subagent-Driven Development in the current session. Every feature task uses red-green-refactor, then spec-compliance and code-quality review.

**Goal:** Import a Platform Extension into Multica as native Agents, Skills, and a Squad; automatically bind it to an online idle `platform-agent-cli` Runtime; execute a real Multica task through the bundled CLI with imported Agent, Skill, and Command context.

**Architecture:** The independent Go CLI owns Extension compilation, Codex-compatible app-server behavior, runtime bootstrap, and mock/HTTP adapters. Multica owns immutable release import, idle runtime allocation, native resource creation, task-side context materialization, and the Extensions UI. Existing fixed `runtime_id`, Daemon claim, session resume, and Native Squad delegation remain unchanged.

**Primary spec:** `docs/superpowers/specs/2026-08-08-platform-extension-squad-runtime-design.md`

## Task 1: Independent CLI repository and Extension compiler

**Files**

- Initialize Git in `/Users/zxx/Documents/技术学习/platform-agent-cli`.
- Create `internal/extension/model.go`, `compiler.go`, and `compiler_test.go`.
- Add `testdata/extensions/research-team.source.json` and golden bundle.
- Update `go.mod` to a production module path.

**TDD cycle**

1. Write failing tests for valid compilation, stable digest, leader validation, duplicate names, missing/root-duplicate `SKILL.md`, unsafe paths, tool suffix rejection, flow/runtime classification, stable flow order, and bundle digest validation.
2. Run `/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/extension -count=1`; confirm RED.
3. Implement the smallest standard-library compiler. Preserve Command name, description, content, and metadata. Hash canonical JSON without the digest field.
4. Run `gofmt` and the package tests; confirm GREEN.
5. Commit in the CLI repo as `feat: compile platform extensions`.

## Task 2: CLI management commands and platform API client

**Files**

- Create `internal/platformapi/client.go` and tests.
- Create `internal/command/command.go` and tests.
- Modify `cmd/platform-agent-cli/main.go` and tests.

**TDD cycle**

1. Write failing contract tests for `extension compile|validate`, `agent list|get|create|update|run`, `skill list|get|upload|download`, `extension list|get`, and `config show`.
2. Cover stdin/file input, JSON output, HTTP method/path, bearer auth, timeouts, non-2xx errors, exit codes, and token redaction.
3. Implement a small dispatcher. Mutating commands consume `--input <file|->`; reads accept ids. `mock` returns deterministic fixtures; `http` requires endpoint/token.
4. Verify with `go test ./cmd/platform-agent-cli ./internal/command ./internal/platformapi -count=1`.
5. Commit as `feat: add platform management commands`.

## Task 3: Runtime context loader and execution adapters

**Files**

- Create `internal/runtimecontext/model.go`, `loader.go`, and tests.
- Create `internal/runtimeadapter/adapter.go`, `mock.go`, `http.go`, and tests.

**TDD cycle**

1. Write failing tests for real `AGENTS.md`, `.platform-agent/context.json`, recursive `.agent_context/skills`, safe symlink/path handling, missing context, command conflicts, and Multica environment identity.
2. Write failing adapter tests for context-aware mock output, HTTP JSON/NDJSON events, cancellation, authorization errors, timeouts, and secret redaction.
3. Load immutable context once per Thread. Never read `CODEX_HOME`.
4. Implement `POST /v1/runtime/execute`. The mock output must prove Extension key/version, source Agent key, Skill count, Command count, and input.
5. Verify with `go test ./internal/runtimecontext ./internal/runtimeadapter -count=1`.
6. Commit as `feat: load platform runtime context`.

## Task 4: Concurrent and cancellable app-server

**Files**

- Rewrite `internal/appserver/server.go` and its tests; split state/protocol helpers when useful.
- Wire adapters in `cmd/platform-agent-cli/main.go`.

**TDD cycle**

1. Add failing tests for initialize, thread start/resume/name, bootstrap failure, immediate Turn acceptance, ordered events, deltas, one final answer, one terminal notification, same-Thread conflict, independent Threads, interrupt during a blocked adapter, EOF wait, parse errors, 8 MiB scanner bound, and concurrent stdout writes.
2. Implement a locked JSONL writer, Thread registry, per-Thread active Turn, `context.CancelFunc`, WaitGroup, and exactly-once terminal guard.
3. Verify with `go test -race ./internal/appserver ./cmd/platform-agent-cli -count=1`.
4. Commit as `feat: execute context-aware app-server turns`.

## Task 5: Multica Extension schema, compiler, and persistence

**Files**

- Add migrations `265_platform_extension_release.*` and `266_platform_extension_release_identity_index.*`.
- Add `server/pkg/db/queries/platform_extension.sql`; regenerate `server/pkg/db/generated` with `cd server && sqlc generate`.
- Add `server/internal/handler/platform_extension_contract.go`, tests, and shared source/golden fixtures.

**TDD cycle**

1. Mirror the CLI compiler tests in Go and require exact golden Bundle parity.
2. Add the release table without new foreign keys. Create `(workspace_id, extension_key, version)` as a separate single-statement concurrent unique-index migration.
3. Add typed get/list/insert queries.
4. Implement server normalization and digest verification with untouched Command metadata.
5. Verify handler contract and migration lint tests.
6. Commit as `feat(server): define platform extension releases`.

## Task 6: Atomic import and idle Runtime allocation

**Files**

- Extend `server/pkg/db/queries/platform_extension.sql` and regenerate sqlc.
- Add `server/internal/handler/platform_extension.go` and integration tests.
- Register routes in `server/cmd/server/router.go`.
- Reuse transaction-bound Agent, Skill, permission, and Squad helpers; change helper visibility only when required.

**TDD cycle**

1. Add failing DB-backed tests for eligible online/alive/idle selection; wrong provider, offline, stale, busy, and private-not-owned filtering; deterministic order; concurrent `SKIP LOCKED`; and 409 when no candidate exists.
2. Add failing conversion tests for two Agents, two Skills, four bindings, one Squad, correct leader/member roles, flow-only Squad Instructions, runtime-only Command Bundle, idempotent repeat, immutable version conflict, list/detail, and full rollback.
3. Resolve alive Runtime ids through the existing liveness store, then lock and re-check the chosen row in the import transaction.
4. Create Release, Skills, Agents, all-to-all bindings, Squad, and resource mapping atomically.
5. Register `GET /api/extensions`, `POST /api/extensions/import`, and `GET /api/extensions/{id}`.
6. Verify focused handler tests and commit as `feat(server): import extensions as native squads`.

## Task 7: Daemon runtime sidecar materialization

**Files**

- Add `server/internal/daemon/execenv/platform_agent_context.go` and tests.
- Modify `execenv/execenv.go`, `context.go`, and `daemon.go` plumbing; keep the existing raw `runtime_config` claim wire and add a regression test for it.

**TDD cycle**

1. Add failing tests asserting only Platform Provider gets `.platform-agent/context.json`; invalid/missing config fails closed; file mode/JSON are correct; user paths are not clobbered; reuse replaces daemon-owned content; cleanup uses the sidecar manifest; other providers are unchanged.
2. Keep the existing raw runtime config wire field. Extract and validate only `runtime_config.platform_agent` for this Provider.
3. Pass typed context to `TaskContextForEnv` and materialize it using existing sidecar ownership rules.
4. Verify `go test ./internal/daemon/execenv ./internal/daemon -run 'PlatformAgent|PlatformCLI' -count=1`.
5. Commit as `feat(daemon): materialize platform runtime context`.

## Task 8: Core Extension API

**Files**

- Add `packages/core/extensions/{index,types,schemas,queries,mutations}.ts` and tests.
- Extend the shared `ApiClient` and its tests so Extension requests retain the existing auth, CSRF, workspace, client-identity, fallback-parse, and `ApiError` behavior.
- Export the package from `packages/core/package.json`.

**TDD cycle**

1. Add failing Zod/API tests for list, detail, import, idempotent result, structured errors, malformed responses, and 5 MiB client limit.
2. Implement stable React Query keys and import invalidation for Extensions, Agents, Skills, Squads, and Runtimes. Do not add Zustand server state.
3. Verify `corepack pnpm --filter @multica/core test -- extensions` and typecheck.
4. Commit as `feat(core): add platform extension API`.

## Task 9: Shared Extensions UI and routes

**Files**

- Add `packages/views/extensions` page and tests.
- Add en, zh-Hans, ja, and ko `extensions.json`; update locale registries/types and package export.
- Add `extensions()` to core paths/page registry, route icon mapping, sidebar Configure group, and related tests.
- Reserve the `extensions` workspace slug and add `layout.nav.extensions` in all four existing locale files.
- Add Desktop React Router route and Web Next.js page.

**TDD cycle**

1. Add failing tests for navigation registry, sidebar link, file upload, JSON parsing, size rejection, loading/success/error, Runtime/Squad/Agent/Skill links, empty state, 409 guidance, and query invalidation.
2. Implement the shared production page with a compact Release list and latest import mapping.
3. Verify Views/Desktop tests plus Core/Views/Desktop/Web typechecks.
4. Commit as `feat(views): add extension import workflow`.

## Task 10: Bundle the CLI and cross-process integration

**Files**

- Raise Platform CLI minimum version in `server/pkg/agent/version.go` and update tests.
- Extend `server/pkg/agent/platform_cli_integration_test.go`.
- Rebuild the external CLI into `apps/desktop/resources/bin/platform-agent-cli` using the existing bundling script.
- Make Desktop-owned Daemons default the bundled Runtime to Mock for this acceptance phase without overriding an explicit HTTP mode; pass the dev binary/mode through Turbo.
- Add the external CLI six-target release/checksum build and teach Desktop smoke/release workflows to fetch a pinned artifact set before strict staging.
- Harden staging and packaged-content tests to verify absolute regular executables, checksum, version, and target architecture.
- Add `scripts/platform-extension-runtime-smoke.sh` if a reusable live harness is needed.

**TDD cycle**

1. Add a failing real-binary test that creates `AGENTS.md`, two Skills, and `.platform-agent/context.json`, executes through `agent.New("platform-agent-cli")`, and asserts context-aware output.
2. Build with the pinned Go toolchain and verify `--version` and architecture.
3. Run `MULTICA_PLATFORM_AGENT_CLI_PATH=/absolute/path/to/platform-agent-cli MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent -run 'PlatformCLIIntegration|PlatformAgent' -count=1` and Desktop bundling tests.
4. Commit CLI docs as `docs: document multica runtime integration`.
5. Commit Multica changes as `test(runtime): cover imported extension execution`.

## Task 11: Real Multica end-to-end acceptance

**Files**

- Add `docs/superpowers/specs/2026-08-08-platform-extension-squad-runtime-acceptance.md`.
- Update the CLI README and Multica implementation/use documentation with actual commands and outputs.

**Acceptance cycle**

1. Start the local server and Desktop Daemon with the bundled CLI; verify provider exactly `platform-agent-cli` and status Online.
2. Import a dedicated real two-Agent/two-Skill/flow+runtime-Command fixture through the authenticated Import API used by the UI.
3. Verify one Release, one Squad, two Agents, two Skills, four bindings, correct leader/members, flow instructions, runtime Command sidecar source, and the same allocated Runtime on every Agent.
4. Create/assign a real task to the imported Leader and wait for Daemon execution.
5. Assert task completed and output includes Extension key/version, Agent key, `skills=2`, `commands=1`, and input.
6. Run Go tests/vet for agent, daemon, execenv, handler, and migrations; run Core, Views, and Desktop tests/typechecks plus Web typecheck.
7. Record exact ids, outputs, suite counts, artifact paths, and any environment-only limitation.
8. Commit as `docs: record platform extension runtime acceptance`.

## Task 12: Final reviews and handoff

1. Run a spec-compliance review against every requirement and non-goal in the primary Spec; fix omissions.
2. Run a code-quality review covering transaction boundaries, auth, liveness fallback, path safety, race handling, output protocol, query invalidation, and unrelated worktree changes; fix issues.
3. Follow verification-before-completion and re-run focused plus live end-to-end checks from fresh binaries.
4. Hand off clickable Spec/plan/acceptance paths, both repository paths and commit histories, exact Multica changes, CLI architecture and commands, cooperation flow, build/start/import/execute instructions, and verified results.
