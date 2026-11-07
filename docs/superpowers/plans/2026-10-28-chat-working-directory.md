# Chat Working Directory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Browse a selected daemon host and run each new chat in a persistent user-selected directory across supported local coding agents.

**Architecture:** Store an optional {daemon_id, path} as the chat's requested working_directory, separately from the actual historical work_dir. Request directory listings through the existing heartbeat pending-action transport, including Redis-backed requests. Pass the selected directory into existing LocalWorkDir and ExecOptions.Cwd execution paths.

**Tech Stack:** Go, Chi, sqlc/PostgreSQL, Redis, daemon heartbeat/WebSocket hints, React, TanStack Query, Zustand, zod.

**Spec:** docs/superpowers/specs/2026-10-28-chat-working-directory-design.md

## Global Constraints

- Preserve old-client response compatibility, parse new UI responses with zod and parseWithFallback.
- Use one daemon capability: chat-working-directory-v1; absent means unsupported.
- working_directory is null/absent or {daemon_id: string, path: string}; no foreign keys or new indexes.
- Filesystem reads and OS-specific absolute-path validation run on the daemon host.
- Browse/custom-directory creation is runtime-owner-only; existing invocation and workspace membership gates remain required.
- Explicit selection overrides only execution location, preserving attached project context.
- Existing default directory/project execution behavior remains unchanged for chats without a selection.
- No real installed agent CLI executions in default checks.

## Review Focus

- A stale listing from another machine must never be selected after changing agents.
- A path containing spaces or Unicode must reach execution unchanged.
- A runtime moved to another daemon must reject the stored selection.
- Invalid/unsupported directory selections must never silently use a managed directory.
- Selecting a path in an existing conversation starts a fresh conversation; continuation preserves its path.

---

### Task 1: Persist and execute chat directory selections

**Files:** server/migrations/*chat_working_directory*, server/pkg/db/queries/chat.sql, generated sqlc files, server/internal/handler/chat.go, new chat_working_directory.go/tests, daemon.go claim construction, agent.go response, server/internal/daemon/types.go, local_directory.go/tests.

**Interfaces:** Produce optional ChatSessionResponse.WorkingDirectory and AgentTaskResponse.WorkingDirectory with protocol.ChatWorkingDirectory {DaemonID, Path}. New session input accepts the same object. Consume chat-working-directory-v1 metadata capability from Task 2.

- [x] Add failing handler tests for persistence, owner/machine/capability validation and follow-up claims; daemon tests for explicit-directory precedence and wrong-machine rejection.
- [x] Run narrow Go tests and confirm the missing behavior.
- [x] Add an idempotent reversible nullable JSONB column migration, update CreateChatSession query, and run make sqlc.
- [x] Validate selected runtime and create input; include selection in all chat responses and each task claim, with claim-time capability/machine checks.
- [x] Resolve selected directories before project resources through the existing local-directory preparation and cleanup behavior.
- [x] Run narrow tests and compile affected packages; preserve unchanged behavior for no selection.

### Task 2: Daemon directory browsing

**Files:** server/pkg/protocol/messages.go, daemon client.go/daemon.go heartbeat sections, handler.go, new runtime_directories.go/store/tests, server/cmd/server/router.go.

**Interfaces:** Own protocol.ChatWorkingDirectory and DaemonCapabilityChatWorkingDirectoryV1 = chat-working-directory-v1. POST /api/runtimes/{runtimeId}/directories with {path: string} creates a request; GET /api/runtimes/{runtimeId}/directories/{requestId} polls. Request fields: id,runtime_id,status,path,result,error; statuses pending/running/completed/failed/timeout. Result: {path,parent_path: string|null,entries: [{name,path}],selectable: boolean}; only child directories. Empty input lists daemon home. Daemon result POST /api/daemon/runtimes/{runtimeId}/directories/{requestId}/result. Reuse heartbeat pending-action hints and shared store patterns.

- [x] Add failing filesystem tests for home/default, children, symlinks, unreadable/nonexistent/file/relative paths, Unicode/spaces; request-store tests and owner authorization tests.
- [x] Run narrow Go tests and confirm missing behavior.
- [x] Implement read-only directory listing and selection eligibility with existing local-directory validation.
- [x] Add bounded ephemeral request stores (in-memory default, Redis production), owner/capability checks, and result identity checks.
- [x] Wire capability, pending heartbeat action, hint, client and router endpoints without launching an agent provider.
- [x] Run filesystem/store tests and compile handlers; ensure new queue probes do not add unbounded empty-heartbeat work. Database-backed handler execution remains pending below.

### Task 3: Shared Web/Desktop chat selection

**Files:** packages/core/types/chat.ts/index.ts, api schemas/client/tests, chat store/mutations, new directory query helpers, packages/views/chat components/controller/chat-page.tsx/chat-window.tsx, locale chat.json files.

**Interfaces:** Consume Task 2 directory API and Task 1 working_directory. API input/response object matches {daemon_id,path}. Query keys include workspace and runtime. Draft selection lives in core chat store; persisted session selection comes from Query data.

- [x] Add failing parsing tests for absent/malformed selections and listing results; a meaningful component test for choosing a directory and sending it in chat creation.
- [x] Run targeted Vitest tests and confirm missing behavior.
- [x] Parse create-chat responses as well as read/list responses; add optional working_directory to shared types and create mutation.
- [x] Add remote directory dialog with path entry, parent/child browsing, loading/error states and selection eligibility; use existing Button/Dialog contracts and semantic tokens.
- [x] Wire both chat surfaces; clear stale draft paths on runtime/agent/workspace changes, start fresh on directory change in existing conversation, and display selected directory concisely.
- [x] Translate new strings in all shared locales and run targeted Vitest, typecheck and lint.

### Task 4: Integration review and verification

**Files:** tests/docs from preceding tasks and this plan.

- [x] Review the complete diff with an independent agent; fix material findings with regression coverage.
- [x] Run relevant Go tests, frontend tests/typecheck/lint, migration checks, and git diff --check.
- [x] Update user docs for the new remote conversation-directory flow.
- [x] Report implemented behavior and actual verification, including environment-limited checks.

## Delivery verification

Implementation and independent cross-review are complete. The results below distinguish executed checks from database-dependent checks that remain pending.

- Core complete test suite: 180 files, 2402 tests passed.
- Shared chat suite: 29 files, 345 tests passed.
- Daemon directory assignment, browsing, heartbeat and legacy local-directory regressions: passed with the agent-CLI guard.
- User-directory preservation and Codex multi-turn session-store regressions: passed with the agent-CLI guard.
- Actual-source handler validation harness: six tests passed.
- Actual-source memory/Redis request-store harness: six tests plus two timeout subtests passed, no skips; dedicated ephemeral Redis was shut down.
- Migration lint tests: passed. sqlc generation: passed.
- Server router, metrics and protocol packages: compiled.
- Core/views typecheck and lint: passed; existing lint warnings remain.
- Database-backed handler persistence/claim tests, migration application and browser/E2E checks: not run. The checkout-managed DATABASE_URL is rejected by the existing PostgreSQL on port5432 (password authentication failed). Its credentials and service were left untouched.
- Managed test environment stopped; temporary compose override and never-started test container removed. Database volumes preserved.

Review corrections verified by regression tests: project context changes preserve the selected execution directory; explicit path bytes including trailing spaces are retained; chats without a project validate their selected directory before preparing an environment; unsupported or moved runtimes cannot silently run in a fallback directory.

### Checks required before merge

- [ ] Run database-backed handler persistence, directory endpoint, and claim tests against a managed database with valid credentials.
- [ ] Apply and roll back migration 595 against that database.
- [ ] Run browser/E2E checks and attach screenshots of the directory selector.
