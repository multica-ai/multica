# Workspace Copy extension (TECH-3582)

Non-destructive **copy** of workspace contents into another workspace. Copy, not
move: every operation creates NEW rows in the target and never deletes/mutates
the source. Source stays a full read-only safety net.

Survivor decided for Firtal: **Firtal Web** is the target; **Firtal Technologies**
is copied from and kept as archive.

## Mechanics (proven)

- Each copied entity gets a fresh UUID and (for issues) the next issue number in
  the target workspace (so TECH-1234 → FIR-<next>). The source keeps its number.
- `cerebro_workspace_copy_map` records source→target per run: idempotency,
  internal-reference rewriting, and the forwarding pointer for old bookmarks.
- `agent.runtime_id` is now nullable so an agent can be copied in "parked" until
  its owner re-pairs a runtime in the target via the guide.

Migration: `server/migrations/9083_cerebro_workspace_copy.{up,down}.sql`.

## Build checklist

Foundation (this increment — landed & verified):
- [x] Migration 9083 (runtime_id nullable + copy map). Verified on a DB in a
      rollback transaction.
- [x] Copy engine core: `CopyIssue` (issue + comments + reactions + labels with
      name-mapping, new id, FIR number, mapping, idempotent). `CopyAgent` (parked:
      runtime_id null, all settings carried, name-conflict guard). `go build` +
      `go vet` green; SQL verified non-destructive against real data (source
      untouched; comments/labels copied; agent instructions carried, source runtime intact).

Per-item copy (next increments — one slice each, mirror `CopyIssue`/`CopyAgent`):
- [x] Issue threading + project/parent links + attachments: comment `parent_id`
      reply threads rebuilt; issue `project_id` + `parent_issue_id` remapped to the
      copied counterparts inline, with `RelinkIssueRelations` healing copies done in
      any order (child-before-parent, issue-before-project); issue- and
      comment-level attachments copied (blob url shared). Verified against real pg
      (`TestCopyIssue_Threads_Project_Parent_Attachments`,
      `TestRelinkIssueRelations_OrderIndependent`).
- [ ] Issue children still TODO: dependencies, PR links, wakeups, tasks/runs.
- [x] Project: row + members + nesting (parent remapped). `CopyProject`. (sprints, repo resources, issue-linking still TODO)
- [x] Chat: session + messages, agent remapped (parked, runtime null). `CopyChat`. (attachments TODO)
- [x] Autopilot: row + triggers (copied DISABLED, no webhook token), assignee/project remapped. `CopyAutopilot`. (runs history TODO)
- [x] Channel/DM: copied via `CopyIssue` (kind passthrough). DM is now a
      first-class pick in the console type dropdown (TECH-3732).
- [x] Internal references rewritten (TECH-3732): the relink post-pass also runs
      `RewriteInternalReferences`, which rewrites mention-link UUIDs
      (`mention://issue/<src>` → `<tgt>`) and bare identifier tokens
      (`TECH-123` → `FIR-<n>`) inside copied descriptions + comments, so text
      references resolve to the copies. Idempotent; source untouched. Verified
      (`TestRewriteInternalReferences`).
- [x] Cascade copy (TECH-3732): `CopyIssueCascade` (issue + all open descendant
      sub-issues) and `CopyProjectCascade` (project + all its open issues +
      their open descendants), each healing links + references once at the end.
      Done/cancelled descendants excluded (the picked root is always copied).
      Console exposes it as an "Include everything underneath" checkbox. Verified
      (`TestCopyIssueCascade`, `TestCopyProjectCascade`).
- [x] Agent extras (TECH-3742): skill bindings (`agent_skill` remapped to the
      copied skills); name-conflict policy (skip / overwrite / keep_both,
      `conflict.go`). Wakeups + reminders now travel per issue (below).
- [x] Per-issue extras (TECH-3742): still-pending `cerebro_agent_wakeup` (agent /
      issue / origin-comment remapped, reset to clean pending) and
      `cerebro_issue_date_reminder` travel inside `CopyIssue` (`copy_perissue.go`).
- [x] Notes (TECH-3742): an artifact's `cerebro_note` extension + versions /
      comments (threads rebuilt) / references / shares travel with it
      (`copyNoteExtensionTx` in `copy_artifact.go`).
- [x] Cross-cutting reference rewrite (TECH-3742): `RewriteInternalReferences`
      now also rewrites agent instructions, skill bodies, autopilot descriptions
      and chat messages (`references.go`).

One-time bulk (Settings — `CopyFoundation`, `copy_foundation.go` + `copy_roles.go`):
- [x] Labels (workspace bulk), skills (`skill` + `skill_version` + `skill_file`,
      per-item `CopySkill` + bulk), connections + credentials (+ bindings; the
      credential cipher uses an instance-wide master key, so ciphertext copies
      unchanged), roles/groups/permissions (`cerebro_role` +
      `cerebro_role_assignment` (member subjects) + `cerebro_group` +
      members (by user)/capabilities; agent/project-scoped access — group→agent,
      agent role assignments, project→group — healed post-agent/
      project by `RelinkGroupAccess`), workspace settings
      (`cerebro_workspace_settings` + auth settings).
- [x] Documents/folders/notes: `CopyWorkspaceArtifacts` (earlier increment).

Wiring:
- [x] Handler entity_types: `foundation`, `skill`, `group_access` added; conflict
      policy plumbed (`handler.go`). Route unchanged (`/cerebro/copy`).
- [x] Frontend `packages/cerebro-workspace-copy/`: fixed-order console (foundation
      "do this first" step → agents → projects → issues → chats → autopilots),
      conflict-policy selector, issue status filter, select-all + copy-all,
      squad-assigned autopilots labelled, foundation + documents bulk buttons,
      relink + group-access heal. Feature flag `cerebro_workspace_copy`.
- [ ] Guide doc: re-pair a runtime + reconnect GitHub in the target workspace.

## Excluded (deliberate scope decisions)

- **Done + cancelled issues** stay in the source archive.
- **Squads** are set up manually; a squad-assigned autopilot is shown but its
  squad assignee is not carried.
- **Tasks / runs** (agent execution history) are NOT copied. They are runtime
  bookkeeping (a `task` is a unit of dispatched work for a specific runtime;
  `agent_run` rows are its execution log) tied to runtimes that don't exist in
  the target until re-paired. Carrying them would point at dead runtimes and
  re-trigger or mis-attribute work. Chat messages keep their text (`task_id`
  cleared); the conversation survives, the execution log does not. Re-running
  work in the target produces fresh tasks/runs.
- **GitHub installations** cannot be duplicated: `installation_id` is globally
  unique (one GitHub App install ↔ one row), so an installation still held by
  the source is skipped and must be reconnected in the target after the merge
  (the runtime re-pair pattern). Connected when free.
- **Group → runtime access**: copied agents are parked (no runtime); runtime
  grants are configured in the target when the owner re-pairs a runtime.
