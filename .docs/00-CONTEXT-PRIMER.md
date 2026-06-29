# Context Primer — Multica Skills 2.0 + Second Brain

**Audience**: a coding agent (or developer) who has not seen the prior conversation that produced this work. Read this file first; then pick one of the two PRDs (`01-PRD-second-brain.md` or `02-PRD-skills-2.0.md`) and execute it.

**Status**: design phase. No code has been written yet for either feature. The proposals below are blueprints, not commits.

---

## 1. What Multica is

Multica is an **open-source managed-agents platform**. A workspace contains issues (like Linear/Jira tickets), comments, and a roster of "agents" (Claude Code, Codex, custom). When a human assigns an issue to an agent, Multica's daemon picks up the task, spawns the agent CLI as a local subprocess on the developer's machine, and the agent posts back results as comments. We ARE running this very platform — this PRD is being authored by an agent inside it.

### Architecture in 1 minute

- **3 binaries**:
  - `multica-server` — central HTTP API (Go, Chi router, port 8080), the only thing that talks to Postgres. Entry: `server/cmd/server/main.go`
  - `multica` — CLI used by humans and agents. Entry: `server/cmd/multica/main.go` (Cobra)
  - `multica daemon` — subcommand of the same `multica` binary. Polls server for tasks, spawns agent CLIs locally. Entry: `server/cmd/multica/cmd_daemon.go`
- **Datastores**: Postgres (single source of truth), optional Redis (auth cache + multi-instance event relay)
- **Frontends**: Next.js web in `apps/web/`, Electron desktop in `apps/desktop/`. Both connect via REST + WebSocket (`/ws`)
- **Multi-PC**: each daemon registers a runtime row (`agent_runtime` table) on the server. Agents have `runtime_id` FK; tasks are pinned to that runtime — **no work-stealing**. Working files (`~/multica_workspaces/<ws>/<issue>/`) live on the daemon's machine.
- **Auth tiers**: `JWT` (browser cookie), `mul_*` PAT (humans), `mdt_*` daemon tokens (workspace-scoped, no user identity). Validated in `server/internal/middleware/daemon_auth.go`.

### Where the agent's prompt comes from

When the daemon claims a task, `server/internal/handler/daemon.go:ClaimTaskByRuntime` builds a `TaskContextForEnv` payload and ships it to the daemon. The daemon's `server/internal/daemon/execenv/runtime_config.go:InjectRuntimeConfig` then writes a CLAUDE.md/AGENTS.md/GEMINI.md file (depending on which agent provider) into the working directory before spawning the agent subprocess. The function `buildMetaSkillContent()` builds the markdown body. **Both features below modify this function.**

---

## 2. Where the ideas come from

We studied 4 production agent codebases. Each lives under `repos/` in this workspace if you want to read them yourself:

- `repos/openclaw/` — TS, multi-channel personal assistant; pluggable memory, sub-agent capability isolation, plugin SDK.
- `repos/hermes-agent/` — Python, Nous Research's agent with **closed learning loop**: agent autonomously creates/improves skills; a background "curator" patches/archives them during idle. Has FTS5 session search, Honcho dialectic user model, 8 swappable memory plugins.
- `repos/multica/` — the codebase you're modifying.
- `repos/paperclip/` — TS, "open-source orchestration for zero-human companies"; heartbeat-driven event loop with structured wake reasons, approval workflows, budget hard-stops, document revisions with restore.

**Key learnings that drive these PRDs:**

| Insight | Source | What we copy |
|---|---|---|
| Agents can autonomously author/improve skills with safety gates | `repos/hermes-agent/tools/skill_manager_tool.py`, `agent/curator.py` | Skills 2.0 closed loop |
| Append-only revisions with restore are the right pattern for AI-mutable content | `repos/paperclip/packages/db/src/schema/document_revisions.ts`, `services/documents.ts` | Both PRDs (versioning) |
| Provenance tracked via context-local signal so user-vs-agent writes are distinguishable | `repos/hermes-agent/tools/skill_provenance.py` (ContextVar) | Both PRDs (provenance) |
| Static regex scanner blocks dangerous content in agent-authored skills | `repos/hermes-agent/tools/skills_guard.py` | Skills 2.0 (port to Go) |
| Compact index in system prompt; full content fetched on demand | `repos/hermes-agent/agent/prompt_builder.py:712-860` (build_skills_system_prompt) | Second Brain (KB index injection) |
| Workspace-level shared markdown is **not solved** by paperclip (their `documents` table forces 1:1 with issue via `documentUq` unique constraint) | `repos/paperclip/packages/db/src/schema/issue_documents.ts:23` | Second Brain (we go N:N intentionally) |
| Multica already has dormant slots `workspace.context` (TEXT) and `issue.context_refs` (JSONB) — never read by any handler | `repos/multica/server/migrations/006_workspace_context.up.sql`, `001_init.up.sql` | Second Brain reuses both |

**Don't reinvent.** Each PRD section labelled "Reference implementation" gives you exact file paths to read in those repos before coding.

---

## 3. The two features and how they relate

### Second Brain (`01-PRD-second-brain.md`)
A workspace-level markdown knowledge base, hierarchical by path. Humans and agents create/edit `.md` files like `clients/acme/brief.md`. The agent always sees an index in its CLAUDE.md and can read full content with `multica doc get <path>`. Versioned append-only.

### Skills 2.0 (`02-PRD-skills-2.0.md`)
Today's `skill` table has no versioning, no agent-side mutation, no curator. We add: revision history with restore, a `propose`/`patch` flow that lets agents author skills (gated by `pending_review`), a static security scanner ported from Hermes, and a curator service that auto-transitions `active→stale→archived` plus optional LLM consolidation.

### Why they share a primer

Both features need the same scaffolding:

1. **Append-only revision pattern** (revision_number, parent_revision, author_type, task_id, operation, change_summary). Implement once in `server/internal/service/revisioning.go` and reuse.
2. **Fuzzy find/replace** for agent patches. Port from `repos/hermes-agent/tools/skill_manager_tool.py:465-560` (the `_apply_patch` function — fuzzy whitespace-tolerant search). One Go implementation reused by both.
3. **Provenance helper** that turns an HTTP request into `{author_type, author_id, task_id}` based on whether the caller has `mdt_*` (daemon = `agent_*`) or `mul_*`/JWT (human). Implement in middleware once.
4. **Both modify `buildMetaSkillContent()`** in `server/internal/daemon/execenv/runtime_config.go`. To avoid PR conflicts, **do Second Brain first** (it establishes the section structure); Skills 2.0 just appends one section.

---

## 4. Multica codebase conventions

These are non-negotiable. Read `CLAUDE.md` and `CONTRIBUTING.md` in the multica repo for the full guide; here are the load-bearing ones:

- **License**: Apache 2.0 modified. No CLA, no signed commits required.
- **Local dev**: `make dev` boots Postgres in Docker, runs migrations, starts server (port 8080) + frontend (port 3000). Prerequisites: Node 20+, pnpm 10.28+, Go 1.26+, Docker.
- **Worktrees**: For features this size, use `git worktree add ../multica-feat-x -b feat/x main && cd .. && make dev`. Generates `.env.worktree` with isolated DB/ports.
- **Migrations**: Pure SQL, in `server/migrations/<NNN>_<name>.up.sql` + `.down.sql`. No generator. CI runs `migrate-up` before tests.
- **sqlc workflow**: write SQL in `server/pkg/db/queries/*.sql`, run `make sqlc` to regenerate `server/pkg/db/generated/`. Commit generated files.
- **Routing**: Chi router, all routes in `server/cmd/server/router.go`. Route conventions: single-word top-level (`/login`) or `/{noun}/{verb}` (`/workspaces/new`). **Never** hyphenated groups (`/new-workspace`) — collides with workspace slugs.
- **Handler pattern**: in `server/internal/handler/`, use `parseUUIDOrBadRequest()` for user input, never raw `parseUUID()`. Loaders like `loadSkillForUser()` abstract identity (UUID vs human-readable like `MUL-123`).
- **Auth middleware**: `Auth` for users, `DaemonAuth` for daemon. New endpoints exposed to agents must be allowed by `DaemonAuth`.
- **Conventional Commits**: `feat(skill): add propose endpoint`, `fix(daemon): handle nil context_refs`, etc.
- **PR template** asks for: issue link, change description, **thinking path**, AI tool disclosure (yes — they explicitly want to know if Claude/Copilot helped), test attestation, screenshots if UI.
- **CI** must pass: `pnpm typecheck`, `pnpm test` (Vitest), `go test ./...`, Playwright e2e.
- **Tests**: backend tests live next to handlers (`*_test.go`). E2E in `e2e/` with Playwright. New feature → at least one happy-path Go integration test + one e2e (frontend or CLI).
- **Stack details**:
  - Go 1.26+, Chi router, sqlc, pgxpool, gorilla/websocket
  - Postgres pg17 (with pgvector extension loaded but unused so far — green field for future semantic search)
  - Frontend: Next.js (web), Electron (desktop), TanStack Query, Vitest
  - Shared packages in `packages/` consumed by web + desktop

---

## 5. Order of operations (recommended)

If you're going to do both features, the recommended PR sequence (from prior planning):

```
A1 → A2 → A3 → (A4 in parallel) → B1 → B2 → A5 → B3 → B4 → B5 → A6 → B6 → B7
```

Where the `A*` prefix means Second Brain PRs and `B*` means Skills 2.0 PRs. Each PRD lists its own PRs as a phased rollout. Doing Second Brain first establishes the revision pattern with lower complexity (no agent mutation in early phases), so Skills 2.0 can reuse the validated helpers.

If you're only going to do one, **either is independently viable** — the shared scaffolding gets built inside the first one chosen.

---

## 6. What you need to read in the reference repos

Before writing code, skim these files. They're the load-bearing references:

**For Second Brain (1–2 hour skim):**
- `repos/paperclip/packages/db/src/schema/documents.ts` — schema for documents table
- `repos/paperclip/packages/db/src/schema/document_revisions.ts` — revision table shape
- `repos/paperclip/packages/db/src/schema/issue_documents.ts` — note the `documentUq` constraint that forces 1:1 (we're going to break this pattern intentionally with N:N)
- `repos/paperclip/server/src/services/documents.ts` — upsert + restore logic; `baseRevisionId` conflict detection
- `repos/paperclip/ui/src/components/IssueDocumentsSection.tsx` — markdown editor with autosave + diff (port to multica)
- `repos/hermes-agent/agent/prompt_builder.py:712-860` (`build_skills_system_prompt`) — the lazy index pattern
- `repos/multica/server/migrations/006_workspace_context.up.sql` — the dormant `workspace.context` slot we're going to use
- `repos/multica/server/internal/daemon/execenv/runtime_config.go` (`buildMetaSkillContent`) — where to inject the KB index

**For Skills 2.0 (2–3 hour skim):**
- `repos/hermes-agent/tools/skill_manager_tool.py` (full file, ~945 lines) — every action; especially `_apply_patch` (lines 465–560) for fuzzy patching
- `repos/hermes-agent/agent/curator.py` (~800 lines) — two-phase curator, lifecycle states
- `repos/hermes-agent/tools/skill_provenance.py` — ContextVar-based provenance signal
- `repos/hermes-agent/tools/skills_guard.py` — regex-based threat scanner (port to Go)
- `repos/hermes-agent/agent/curator_backup.py` — Hermes's tar.gz snapshot pattern (we replace with append-only DB revisions, but understand what they did)
- `repos/multica/server/internal/handler/skill.go` — current skill handler (you're extending it)
- `repos/multica/server/migrations/008_structured_skills.up.sql` — current skill schema
- `repos/multica/server/internal/service/task.go:1152-1182` (`LoadAgentSkills`) — how skills reach the agent today

---

## 7. Glossary

- **Workspace** — top-level tenant container (think "GitHub org" or "Linear workspace")
- **Issue** — work item (Linear-like)
- **Agent** — a configured CLI runtime (Claude Code, Codex, custom) bound to one workspace
- **Runtime** — a daemon registration; one runtime = one daemon process on one machine
- **Task** — a queued unit of work, always tied to an issue + agent + runtime
- **Skill** — reusable knowledge attached to an agent; today a markdown blob + optional files
- **CLAUDE.md / AGENTS.md** — written by `InjectRuntimeConfig()` at task start; the agent's "system prompt prelude"
- **Daemon token (`mdt_*`)** — workspace-scoped token used by daemon and agent subprocesses
- **PAT (`mul_*`)** — personal access token used by humans
- **Pending review** — a lifecycle state we're introducing for agent-authored content awaiting human/curator approval

---

## 8. Style notes for the PRDs

- **All file paths in the PRDs are RELATIVE to the multica repo root** (e.g., `server/migrations/...`).
- **All "Reference impl: X" links in the PRDs are RELATIVE to the workspace root** (e.g., `repos/hermes-agent/...`) so you can read them directly.
- **SQL is Postgres-flavored** (uses `JSONB`, `TEXT[]`, `gin_trgm_ops`, `to_tsvector`).
- **Go code snippets are illustrative**; treat them as pseudocode. Prefer the project's existing patterns (sqlc-generated types, pgtype.UUID, etc.) when actually writing.
- **CLI examples assume the user has run `multica login` and is in a workspace**. New CLI subcommands should follow Cobra conventions used in `server/cmd/multica/cmd_*.go`.

---

You're ready. Pick a PRD and start. If anything is ambiguous, the "Open questions" section at the end of each PRD lists what to clarify with the project owner before coding.