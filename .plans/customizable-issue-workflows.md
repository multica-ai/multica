# Plan: Customizable Issue Workflows
Date: 2026-05-05
Status: IN_PROGRESS

## Problem
Issue status is currently a hard-coded text lifecycle (`backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`). That blocks richer workflows: status descriptions, valid next actions, transition graphs, graph display, agent guidance, and future per-project/multi-workflow selection.

## Approach
Ship the smallest server-first v1 substrate:

- Keep `issue.status` as the public stable string key for compatibility.
- Add project/workspace-scoped workflow tables behind it.
- Seed the existing lifecycle as the default workflow.
- Resolve existing API/CLI status strings against the default workflow.
- Add available-transitions read surface for UI/agents.
- Avoid full graph editor and multi-workflow selector in this pass.

Important constraints from current code:

- `issue.project_id` is nullable, so workflow resolution must support workspace-level orphan/default workflows or keep issue workflow fields nullable during v1.
- The current `issue.status` CHECK must be dropped safely by migration.
- CLI hard-coded status validation must not block custom keys.
- Handler tests silently skip when Postgres is unavailable; verify with DB when possible and record if unavailable.

## Steps
- [x] Create follow-up issue for tool-exhaustion/no-final-comment failure.
- [x] Read DRV-68 and DRV-70 context.
- [ ] Add RED server tests for workflow seeding / custom status acceptance / available transitions.
- [ ] Add schema migration for workflows/status definitions/transitions.
- [ ] Add sqlc queries and generated code for workflow lookups.
- [ ] Update issue create/update/status path to resolve status key to workflow/status definition.
- [ ] Add available transitions handler + route + CLI command.
- [ ] Remove CLI closed-set rejection for issue status.
- [ ] Run focused Go tests and full check if feasible.
- [ ] Post concise Multica status with exact landed scope and blockers.

## Open Questions
- V1 handling for projectless issues: workspace-level orphan workflow vs nullable `workflow_id`/`status_id`. Prefer workspace-level orphan workflow to preserve future invariant.
- Whether transition enforcement should be enabled immediately or initially read-only. Prefer read-only available transitions first if compatibility risk is high.
- Whether to include UI read-only graph viewer in this change or split into a frontend follow-up.
