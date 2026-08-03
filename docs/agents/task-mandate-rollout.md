# Task Mandate rollout and rollback

`cerebro_task_mandate_enforcement` and `cerebro_access_diagnostics` default to
off. Enable `cerebro_access_diagnostics` only for the named observation cohort;
this reveals the read-only evidence without enabling call-time denial. While
`cerebro_task_mandate_enforcement` is off, Multica
still freezes a Task Mandate for every claimed run and records diagnostic
observations, but that snapshot cannot reject a call. Settings → Permissions
remains the live authoring surface; a task transcript is a historical ceiling,
not a place to edit access.

## Release notes

- Runtime capability discovery now separates provider probing from MCP
  `tools/list` and exposes the same content-versioned report in the Runtime UI,
  REST, `multica runtime diagnostics`, and
  `get_runtime_access_diagnostics`.
- Connection Test & discover now explains MCP/OpenAPI success, empty,
  unavailable, and error outcomes with the affected capability, source policy,
  and recovery action.
- Settings → Permissions now explains that a later Deny or safety ceiling can
  tighten an active run, while a later Allow cannot widen its frozen Task
  Mandate. The task transcript shows that ceiling and observed exact-call
  denials for the current or historical run.
- `cerebro_task_mandate_enforcement` remains off for observation. The release
  owner enables `cerebro_access_diagnostics` for the observation cohort and
  controls enforcement cohort enablement after the gates below are clean.

## Operator evidence

Enable `cerebro_access_diagnostics` for the observation cohort and use all three
views before changing `cerebro_task_mandate_enforcement`:

1. Runtime capability discovery: open the Runtime capability card or run
   `multica runtime diagnostics <runtime-id> --output json` or
   `get_runtime_access_diagnostics`. Provider probing and MCP `tools/list` must
   each be current, and each content version must match the inventory being
   evaluated.
2. Connection discovery: use Connection Test & discover. MCP Connections must
   show a successful or intentionally empty `tools/list` result; API
   Connections must show a successful or intentionally empty OpenAPI result.
   Resolve unavailable or error recovery actions before the cohort advances.
3. Task access: inspect the task transcript and
   `multica permissions task <task-id>`. Confirm the Task Mandate is finalized,
   the offered and authorized counts are understood, and every observed denial
   names the affected callable, source policy, and recovery action.

## Release gates

Keep enforcement off until the observation cohort proves all of these paths:

- ordinary issue read/write, comment, artifact, wakeup, and handoff calls;
- a task with partial MCP discovery, including exact tools and any authorized
  `mcp__<server>__*` scope;
- an API Connection endpoint with its exact Connection policy identity;
- `open_loop_step` and its existing task/resource scope;
- expired, reclaimed, empty, denied, unavailable, stale, partial, and error
  diagnostics without an unexplained difference between listing and call time.

The release owner records the observation window, affected Runtime and agent
cohort, provider/MCP content versions, unexpected-denial count, and recovery
outcome. Advance only when there are no unexplained call-time denials, no
callable capability missing from the finalized Task Mandate, and no stale or
unavailable discovery result treated as current.

## Controlled rollout

1. Deploy the diagnostic surfaces with enforcement off, enable
   `cerebro_access_diagnostics` for the named observation cohort, and complete
   the observation gates above.
2. Enable `cerebro_task_mandate_enforcement` for one explicitly named workspace
   cohort. Do not combine the first enforcement change with Runtime, provider,
   Connection, Role, or Settings → Permissions changes.
3. Repeat the evidence matrix and inspect every new denial in the task
   transcript and `cerebro_access_decision_ledger`.
4. Expand the cohort only after the release owner records a clean window and
   confirms the rollback owner is available.

## Rollback

Set `cerebro_task_mandate_enforcement` off for the affected workspace cohort at
the first unexplained denial, missing diagnostic source, or provider/discovery
version mismatch. This immediately returns Task Mandate to observation-only;
Settings → Permissions and the independent credential, sandbox, repository,
approval, and resource-scope ceilings continue to enforce normally.

The diagnostic REST, CLI, MCP, Runtime, Connection, and task transcript
surfaces are read-only. `cerebro_access_diagnostics` may remain on during
rollback while `cerebro_task_mandate_enforcement` is turned off. Preserve the
affected task IDs, exact `verdict.code`, content versions, ledger rows, and
recovery actions for investigation. Do not widen Settings → Permissions to
work around a frozen task; fix the source and start a new task.
