# P0/P1/P2 Execution Roadmap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Execute the current P0 reliability fixes, P1 user-facing workflow improvements, and P2 platform capabilities in a dependency-safe sequence.

**Architecture:** This is a master execution roadmap, not a single feature patch. Each workstream below must get its own focused implementation plan before code changes start, because the scope spans runtime execution, squad task routing, comments/attachments, issue views, project workdirs, shared agents, and skill sync. Shared logic belongs in `packages/core/`, shared UI in `packages/views/`, pure UI in `packages/ui/`, and backend behavior in `server/`.

**Tech Stack:** Go backend with Chi/sqlc/PostgreSQL, daemon runtime backends in `server/pkg/agent` and `server/internal/daemon`, Next.js/Electron shared views via React/TanStack Query/Zustand, Vitest/RTL, Go tests, Playwright E2E where user-visible cross-surface behavior is affected.

---

## Execution Principles

- Prioritize fixes that protect the core promise: agents do work in the right place, under the right identity, and users can tell what happened.
- Ship P0 in small patches. Each P0 patch should be independently mergeable and should not wait for P1/P2 UX work.
- Do not combine backend contract changes with broad UI redesigns in the same PR.
- Keep installed-desktop compatibility: new API responses must be parsed with schema fallbacks in `packages/core/api/schemas.ts` before UI uses them.
- Any route/page shared by web and desktop must be implemented in `packages/views/`, with platform routing only in app platform layers.
- Every implementation plan must start with failing tests in the package that owns the behavior.

## Milestone Order

1. **M0: Baseline and branch hygiene**
2. **M1: P0 runtime/workdir correctness**
3. **M2: P0 squad routing observability and correctness**
4. **M3: P0 desktop freeze diagnostics**
5. **M4: P1 comment attachment and mobile composition**
6. **M5: P1 issue hierarchy views**
7. **M6: P2 project workdir policy**
8. **M7: P2 team-shared agents with per-user runtimes**
9. **M8: P2 git-backed skill sync**

---

## M0: Baseline and Branch Hygiene

**Purpose:** Avoid building on stale assumptions. The local branch is ahead of `origin/main` and includes AI skill finder/GitHub compatibility work.

**Files:**
- Read: `CLAUDE.md`
- Read: `docs/superpowers/specs/2026-05-19-ai-skill-finder-agent-draft-design.md`
- Read: `docs/superpowers/specs/2026-05-19-github-api-compat-design.md`
- Check: `git status --short --branch`

- [ ] Confirm whether the current local commits are intended to be part of this execution train.
- [ ] If implementation starts from `main`, create an isolated worktree before edits.
- [ ] For every workstream, create a short implementation plan under `docs/superpowers/plans/`.
- [ ] Before each PR-sized change, re-check open PRs so duplicate work is not started.

**Verification:**
- Run targeted checks per patch during development.
- Run `make check` before claiming any patch is complete.

---

## M1: P0 Runtime/Workdir Correctness

**Primary Issues:** `#2953`, related to `#2925`, `#2639`, `#1898`, `#1811`, `#2882`.

**Why first:** If agents run in the wrong directory, skill discovery, repo checkout, task isolation, and user trust all degrade.

**Scope:**
- Guarantee `PWD` and process cwd agree for OpenCode runs.
- Ensure OpenCode attach/server mode receives the task workdir explicitly.
- Record the effective task workdir in daemon logs and task metadata.
- Add a regression test that proves OpenCode command construction cannot resolve `--dir .` against stale `PWD`.

**Likely Files:**
- Modify: `server/pkg/agent/opencode.go`
- Modify: `server/pkg/agent/opencode_test.go`
- Possibly modify: `server/internal/daemon/execenv/runtime_config.go`
- Possibly modify: `server/internal/daemon/types.go`
- Possibly modify: `server/internal/daemon/daemon.go`

**Plan Tasks:**
- [ ] Write a focused plan: `docs/superpowers/plans/2026-05-21-opencode-task-workdir.md`.
- [ ] Add tests in `server/pkg/agent/opencode_test.go` for env construction: `PWD` must equal `opts.Cwd`.
- [ ] Add tests for command args when attach/server mode custom args include relative `--dir`.
- [ ] Implement env normalization in OpenCode backend.
- [ ] If attach mode needs an explicit task directory flag, add a narrow arg builder with blocked-arg protection.
- [ ] Log effective workdir, `PWD`, and attach directory source without leaking secrets.
- [ ] Run `cd server && go test ./pkg/agent -run OpenCode`.
- [ ] Run `make test` if backend-only, then `make check` before completion.

**Acceptance:**
- OpenCode tasks use Multica task workdir for `cmd.Dir`, `PWD`, skill discovery, and repo checkout guidance.
- Existing custom args cannot override daemon-controlled workdir semantics.
- Failure mode is visible in logs rather than silent.

---

## M2: P0 Squad Routing Observability and Correctness

**Primary Issues:** `#2911`, `#2702`.

**Why second:** Squad assignment is a core differentiator, but current reports show "leader said delegated" while no worker task ran.

**Scope:**
- Add traceability from squad leader evaluation to actual worker task enqueue.
- Make "no action" decisions auditable with reason and task id.
- Add server tests for comments/assignments that should trigger squad leader work.
- Add a minimal UI/API surface only if needed to expose failure reasons.

**Likely Files:**
- Modify: `server/internal/handler/squad.go`
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/pkg/db/queries/agent.sql`
- Modify or add tests: `server/internal/handler/handler_test.go`
- Modify or add tests: `server/internal/handler/daemon_test.go`
- Possibly modify: `packages/views/squads/components/squad-detail-page.tsx`
- Possibly modify: `packages/core/agents/derive-presence.ts`

**Plan Tasks:**
- [ ] Write a focused plan: `docs/superpowers/plans/2026-05-21-squad-routing-observability.md`.
- [ ] Add server tests for squad assignment enqueue on issue creation and comment trigger.
- [ ] Add server tests for leader self-trigger guard where leader and worker are the same agent.
- [ ] Add tests that a leader "delegate" command produces worker task rows or an explicit failure state.
- [ ] Add activity details for leader evaluation: `decision`, `reason`, `task_id`, `target_agent_id`, and `enqueue_result`.
- [ ] Add structured warnings for skipped enqueue paths: no runtime, archived agent, pending duplicate, missing squad, or invalid worker.
- [ ] Run `cd server && go test ./internal/handler -run 'Squad|Leader|Task'`.
- [ ] Run `make test`, then `make check` before completion.

**Acceptance:**
- A squad leader cannot silently claim delegation succeeded if no worker task was created.
- Users and maintainers can inspect whether the issue is prompt behavior, routing logic, runtime readiness, or daemon claim failure.
- Existing anti-loop protections remain intact.

---

## M3: P0 Desktop Freeze Diagnostics

**Primary Issue:** `#2864`.

**Why diagnostic first:** The report has no reliable reproduction path. Instrumentation should land before speculative UI/process changes.

**Scope:**
- Add a desktop diagnostics export for Windows.
- Include renderer logs, main process logs, daemon status, recent task/runtime state, app version, OS, and last navigation/action breadcrumbs.
- Add a UI entry point that works even when daemon is offline.

**Likely Files:**
- Modify: `apps/desktop/src/main/*`
- Modify: `apps/desktop/src/renderer/src/*`
- Modify: `apps/desktop/src/shared/runtime-config.ts`
- Possibly modify: `packages/views/settings/*`

**Plan Tasks:**
- [ ] Write a focused plan: `docs/superpowers/plans/2026-05-21-desktop-diagnostics.md`.
- [ ] Add main-process IPC for collecting app/runtime diagnostics.
- [ ] Add renderer-side safe caller with timeout and error fallback.
- [ ] Add a diagnostics action in desktop settings or daemon panel.
- [ ] Add tests for log parsing and diagnostics payload shape.
- [ ] Manually verify on macOS first; document Windows verification steps.

**Acceptance:**
- A user can produce a diagnostics bundle after restart.
- Bundle includes enough context to diagnose freeze without asking the user to hunt paths manually.
- No secrets from tokens, env vars, or MCP configs are exported.

---

## M4: P1 Comment Attachment and Mobile Composition

**Primary Issue:** `#2957`.

**Why now:** Comment/chat/issue attachment infrastructure already exists. This is a high-value user-facing improvement with limited backend risk.

**Scope:**
- Improve comment composer attachment UX.
- Support mobile file selection beyond camera/gallery where the browser allows it.
- Support removing pending attachments before submit.
- Collapse or manage huge pasted content so mobile comments remain usable.

**Likely Files:**
- Modify: `packages/views/issues/components/comment-input.tsx`
- Modify: `packages/views/issues/components/comment-input.test.tsx` or add test
- Modify: `packages/views/editor/content-editor.tsx`
- Modify: `packages/views/editor/*` if pending attachment cards need shared UI
- Modify: `packages/views/locales/en/issues.json`
- Modify: `packages/views/locales/zh-Hans/issues.json`
- Possibly modify: `packages/ui/components/common/file-upload-button.tsx`

**Plan Tasks:**
- [ ] Write a focused plan: `docs/superpowers/plans/2026-05-21-comment-attachments-mobile.md`.
- [ ] Add tests that pasted/dropped files become pending attachments and submit as `attachment_ids`.
- [ ] Add tests that removing a pending attachment removes it from submit payload and editor markdown.
- [ ] Add tests for disabled submit while upload is in flight.
- [ ] Implement pending attachment strip using existing `AttachmentCard` behavior where possible.
- [ ] Add long-content collapse affordance for read comments or composer preview only if it does not hide active editing content.
- [ ] Run `pnpm --filter @multica/views exec vitest run issues/components/comment-input.test.tsx`.
- [ ] Run `pnpm test`, then `make check` before completion.

**Acceptance:**
- Users can attach files to comments, see what is pending, remove pending attachments, and submit only referenced attachments.
- Mobile users can paste large text without the composer becoming unusable.
- Existing issue description and chat attachment paths keep working.

---

## M5: P1 Issue Hierarchy Views

**Primary Issues:** `#2951`, `#2952`.

**Why after attachments:** It is larger UI work and should not block P0 reliability.

**Scope:**
- Add a hierarchy-aware issue list mode first.
- Add swimlane board mode after the list mode proves the grouping and query model.
- Use existing `parent_issue_id` and child progress data.
- Avoid backend schema changes unless current list APIs cannot support the grouping at scale.

**Likely Files:**
- Modify: `packages/views/issues/components/issues-page.tsx`
- Modify: `packages/views/issues/components/issues-header.tsx`
- Create: `packages/views/issues/components/tree-view.tsx`
- Create: `packages/views/issues/components/swimlane-board-view.tsx`
- Modify: `packages/views/issues/components/board-view.tsx`
- Modify: `packages/core/issues/queries.ts`
- Modify: `packages/core/issues/stores/*` if view mode persists in Zustand
- Modify tests: `packages/views/issues/components/issues-page.test.tsx`

**Plan Tasks:**
- [ ] Write a focused plan: `docs/superpowers/plans/2026-05-21-issue-hierarchy-views.md`.
- [ ] Add core utility tests for grouping flat issues by `parent_issue_id`.
- [ ] Add view tests for top-level parent rows with nested children.
- [ ] Add UI state for `list`, `board`, `tree`, and later `swimlane` without duplicating server state in Zustand.
- [ ] Implement tree view with indentation, parent labels, child counts, and empty state.
- [ ] Add swimlane board after tree grouping is stable.
- [ ] Run `pnpm --filter @multica/views exec vitest run issues/components/issues-page.test.tsx`.
- [ ] Add Playwright coverage only after the view is wired into both web and desktop.

**Acceptance:**
- Users can distinguish parent issues from sub-issues in dense task sets.
- Board swimlanes group children under parents without losing status columns.
- Drag/update behavior remains optimistic and workspace-scoped query keys remain keyed by `wsId`.

---

## M6: P2 Project Workdir Policy

**Primary Issues:** `#1811`, `#2882`.

**Why before shared agents:** Runtime selection and shared-agent behavior should respect where work is allowed to happen.

**Scope:**
- Add project-level workdir policy with a conservative first version.
- Support explicit `canonical_workdir` / preferred local path as metadata.
- Add prompt/runtime context so agents know the intended workdir.
- Do not claim hard filesystem enforcement until tool-level enforcement exists for each runtime.

**Likely Files:**
- Add migration under `server/migrations/`
- Modify: `server/pkg/db/queries/project.sql`
- Modify: `server/internal/handler/project.go`
- Modify: `server/internal/handler/project_resource.go`
- Modify: `server/internal/daemon/execenv/runtime_config.go`
- Modify: `packages/core/types/project.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/views/projects/components/project-detail.tsx`

**Plan Tasks:**
- [ ] Write a design doc first: `docs/superpowers/specs/2026-05-21-project-workdir-policy-design.md`.
- [ ] Write implementation plan after design approval.
- [ ] Add DB fields for policy and path metadata.
- [ ] Add API schema parsing with fallback.
- [ ] Add UI controls with clear copy: advisory mode first, enforcement later.
- [ ] Add daemon prompt context for project workdir policy.
- [ ] Add tests for API validation and prompt injection.

**Acceptance:**
- Project owners can set a preferred workdir policy.
- Agents receive the policy consistently.
- The product does not overpromise sandbox enforcement that is not technically guaranteed.

---

## M7: P2 Team-Shared Agents With Per-User Runtimes

**Primary Issue:** `#2916`.

**Why after workdir policy:** Shared agents need explicit ownership/runtime binding rules, especially when different users run the same agent definition locally.

**Scope:**
- Split agent definition from runtime binding.
- Allow a team-visible agent template/profile to resolve to the current user's runtime when the user assigns or triggers work.
- Keep permission checks clear: who can edit shared definition, who can bind their runtime, who can run it.

**Likely Files:**
- Add migrations under `server/migrations/`
- Modify: `server/pkg/db/queries/agent.sql`
- Modify: `server/pkg/db/queries/runtime.sql`
- Modify: `server/internal/handler/agent.go`
- Modify: `server/internal/handler/daemon.go`
- Modify: `packages/core/types/agent.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/views/agents/components/*`
- Modify: `packages/views/modals/create-issue.tsx`

**Plan Tasks:**
- [ ] Write a design doc first: `docs/superpowers/specs/2026-05-21-shared-agent-runtime-bindings-design.md`.
- [ ] Write implementation plan after design approval.
- [ ] Define data model: shared definition, owner/admin permissions, per-user runtime binding.
- [ ] Add backend resolution rule for task enqueue.
- [ ] Add UI for "Use my runtime" binding status.
- [ ] Add tests for access, enqueue resolution, and missing-runtime fallback.

**Acceptance:**
- A team can maintain one agent definition.
- Each user can run that shared agent on their own runtime.
- Missing binding produces a clear setup action instead of a silent queue failure.

---

## M8: P2 Git-Backed Skill Sync

**Primary Issue:** `#2917`.

**Why last:** It builds naturally on the local AI skill finder/agent draft work and should not block P0 reliability.

**Scope:**
- Persist skill origin as a git source.
- Add manual sync first; scheduled auto-sync can follow.
- Validate incoming `SKILL.md` and files before replacing active content.
- Preserve audit trail and rollback path.

**Likely Files:**
- Modify: `server/internal/handler/skill.go`
- Modify: `server/pkg/db/queries/skill.sql`
- Add migration under `server/migrations/`
- Possibly add package: `server/internal/skillsync/`
- Modify: `packages/core/types/skill.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/views/skills/components/*`
- Coordinate with local files from current branch:
  - `server/internal/skillindex/index.json`
  - `packages/views/skills/components/skill-finder-dialog.tsx`
  - `packages/views/inbox/components/skill-find-result.tsx`

**Plan Tasks:**
- [ ] Write a design doc first: `docs/superpowers/specs/2026-05-21-git-backed-skill-sync-design.md`.
- [ ] Write implementation plan after design approval.
- [ ] Add origin fields and sync status.
- [ ] Add manual sync endpoint.
- [ ] Add UI action and result state.
- [ ] Add tests for valid sync, invalid skill rollback, and permission checks.

**Acceptance:**
- A workspace skill can be linked to a git URL/path.
- User can trigger sync and see success/failure.
- Bad upstream content cannot corrupt the currently installed skill.

---

## Cross-Cutting Testing Matrix

- **Backend reliability:** `make test`
- **Frontend type safety:** `pnpm typecheck`
- **Frontend unit tests:** `pnpm test`
- **P0 final gate:** `make check`
- **P1 UI final gate:** targeted Vitest plus Playwright smoke for web and desktop route wiring when applicable
- **Desktop diagnostics:** manual smoke on macOS plus documented Windows verification checklist

## Recommended PR Breakdown

1. `fix(opencode): preserve Multica task workdir`
2. `fix(squads): trace leader delegation outcomes`
3. `feat(desktop): export diagnostics bundle`
4. `feat(comments): improve attachment composer`
5. `feat(issues): add hierarchy list mode`
6. `feat(issues): add swimlane board mode`
7. `feat(projects): add advisory workdir policy`
8. `feat(agents): add shared definition runtime bindings`
9. `feat(skills): add git-backed manual sync`

## Risk Register

- **P0 OpenCode:** Custom args may already rely on current behavior. Mitigate by blocking only daemon-owned workdir controls and logging clear migration hints.
- **P0 Squads:** Prompt wording and routing logic can be confused. Mitigate with server-side evidence: evaluation result and actual task enqueue outcome must be separate.
- **Desktop diagnostics:** Logs can leak secrets. Mitigate with denylist redaction for tokens, auth headers, env values, and MCP config.
- **Comment attachments:** Pending attachment deletion can race with upload completion. Mitigate by tracking upload ids and disabling submit while upload is active.
- **Issue hierarchy:** Board drag/drop can corrupt parent grouping if updates are too broad. Mitigate with focused update payloads and tests for parent/status changes.
- **Project workdir policy:** Advisory mode may be mistaken for enforcement. Mitigate with precise UI copy and docs.
- **Shared agents:** Runtime resolution can create surprising ownership semantics. Mitigate with explicit binding UI and backend permission tests.
- **Skill sync:** Remote git sources may be unavailable or malicious. Mitigate with manual sync, validation before activation, and rollback.

## Self-Review

- **Spec coverage:** The roadmap covers all selected P0, P1, and P2 items from the triage: runtime workdir, squad routing, desktop freeze diagnostics, comment attachments, issue hierarchy, project workdir policy, shared agents, and skill sync.
- **Scope split:** The work is intentionally decomposed into separate implementation plans because it spans independent subsystems.
- **Dependency order:** P0 reliability precedes user-facing P1 UX; project workdir policy precedes shared-agent runtime binding; skill sync follows current AI skill finder work.
- **Boundary rules:** Shared state remains in `packages/core/`, shared UI in `packages/views/`, app-specific APIs in platform layers, and server state stays in React Query.
