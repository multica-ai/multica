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

## Post-deploy live Runtime tool gate

Run the live gate after every staging and production deployment, after the
services report healthy and before the deployment is accepted:

```bash
MULTICA_POST_DEPLOY_CLI=/path/to/deployed/multica \
MULTICA_POST_DEPLOY_PROFILE=<live-profile> \
scripts/cerebro/check-live-runtime-tools-post-deploy.sh
```

The profile must authenticate to the deployed live workspace. The check reads
every online Runtime through one `multica runtime list` snapshot and fails
closed when no online Runtime can be verified or any online Runtime has an
empty `capabilities` report. This is the measurable zero-state that previously
left an agent with none of its callable surface; an intentionally empty
provider-native `tools` array is not equivalent when the Runtime still reports
its protocol and discovery capabilities.

`.github/workflows/cerebro-post-deploy-live-runtime-tools.yml` runs this gate on
every push to the Sliplane deploy branches, `main` and `production`. Configure
the `staging` and `production` GitHub environments with `MULTICA_TOKEN`,
`MULTICA_SERVER_URL`, `MULTICA_WORKSPACE_ID`, and `MULTICA_APP_URL`; add
`CEREBRO_CF_ACCESS_CLIENT_ID` and `CEREBRO_CF_ACCESS_CLIENT_SECRET` when the API
is behind Cloudflare Access. The workflow waits until `MULTICA_APP_URL/version`
reports the pushed commit before reading the workspace, so an old healthy
deployment cannot satisfy the new deployment's gate. Both Cloudflare Access
headers are applied to `/version` when configured, and a half-configured pair
fails closed. After the web commit appears, the workflow holds a two-minute
settle window for the independently deployed backend and Runtime reports before
reading them. `.deploy/deploy.sh`
invokes the same commit-bound check on the separate canonical local path. A
failed check is a failed deployment acceptance; it does not justify enabling
`cerebro_task_mandate_enforcement`.

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

## `legacy` retirement gate

Do not delete the parallel `legacy` read path merely because enforcement has
been enabled. Retirement is allowed only when one release record proves every
criterion below for the same approved cohort:

1. The release owner approves and records the cohort before observation starts:
   workspace IDs, Runtime IDs, agent IDs, release owner, rollback owner, start
   timestamp, and the exact deployed commit. Any cohort or permission-surface
   change starts a new observation window.
2. The complete cohort runs with `cerebro_task_mandate_enforcement` enabled for
   seven consecutive 24-hour periods. A flag-off interval, unavailable
   diagnostic source, provider/discovery version mismatch, or cohort change
   resets the seven-day clock.
3. The cohort records zero parity denials. A parity denial is any production
   call with a Task Mandate denial `reason_code` where the same identity and
   callable resolves to Allow or Ask through live Settings → Permissions.
   Deliberate negative canaries are excluded only when their task IDs and
   expected `reason_code` were recorded before the window began. Record the
   ledger query interval, total calls, total mandate denials, and parity-denial
   count; the accepted parity-denial count is exactly `0`.
4. After the seven-day evidence is accepted, keep the `legacy` path available
   and keep the rollback owner on call for a further 72-hour rollback window.
   Continue the same ledger and diagnostic checks throughout that window. Any
   parity denial, missing source, version mismatch, or rollback resets both the
   acceptance and the 72-hour clock.
5. Only after the 72-hour rollback window closes with the count still at zero
   may a separate reviewed change remove the `legacy` path. That change must
   link the cohort record and preserve the feature-flag rollback until its own
   deployment is verified.

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
