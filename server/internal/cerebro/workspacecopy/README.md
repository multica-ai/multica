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
- [x] Channel/DM: copied via `CopyIssue` (kind passthrough).
- [ ] Agent extras: skills bindings (after bulk skill sync), wakeups, tasks/runs.
- [ ] Chat (+ messages, attachments); Channel/DM (issue kind).
- [ ] Autopilot (+ triggers, runs).
- [ ] Conflict handling on name clashes (3 agents, 19 skills): keep-Web / rename / overwrite.

One-time bulk (Settings extension):
- [ ] Labels, roles/permissions, connections, credentials, GitHub install,
      skills, documents/folders/notes, workspace settings.

Wiring:
- [ ] sqlc/raw-SQL store methods per entity; handler package; routes in
      `server/cmd/server/router.go` (admin/owner gated); update
      `docs/agents/permission-system.md` (CI guard).
- [ ] Frontend `packages/cerebro-workspace-copy/`: Settings tab (bulk) + per-item
      "Copy to workspace" action; feature flag `cerebro_workspace_copy`.
- [ ] Guide doc: re-pair a runtime in the target workspace.

## Excluded

- Done + cancelled issues (stay in source archive).
- Squads (set up manually).
