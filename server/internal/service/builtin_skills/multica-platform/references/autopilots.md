# Autopilots

An autopilot is not an agent. It is a rule that dispatches work to an agent, or
to a squad's leader agent.

## Core model

The chain is: trigger fires (`schedule`, `webhook`, or `manual`) → an autopilot
run is recorded → `execution_mode` decides the output → assignee readiness check
→ issue/task execution → run status sync.

Webhooks have a durable admission step in front: HTTP ingress stores a queued
webhook delivery, synchronously creates or reuses its idempotent run, and
returns `200` with `status=accepted|skipped` plus `run_id`; a leased worker then
resumes accepted runs and owns recoverable issue/task dispatch.

Execution modes:

- `create_issue` creates a Multica issue, making the run visible as issue state.
- `run_only` creates an agent task directly. No issue is created; any durable
  report location has to come from other task context or instructions.

`issue-title-template` only supports `{{date}}`. Do not invent `{{trigger_id}}`,
`{{branch}}`, or other variables.

## CLI

```bash
multica autopilot list --output json
multica autopilot get <autopilot-id> --output json
multica autopilot create --title "<title>" --description "<task prompt>" --agent <agent-name-or-id> --mode create_issue|run_only --output json
multica autopilot update <autopilot-id> --status active|paused --output json
multica autopilot runs <autopilot-id> --output json
multica autopilot trigger-add <autopilot-id> --kind schedule --cron "0 9 * * *" --timezone Asia/Shanghai --output json
multica autopilot trigger-add <autopilot-id> --kind webhook --label "ci" --output json
multica autopilot trigger <autopilot-id> --output json
multica autopilot trigger-rotate-url <autopilot-id> <trigger-id> --yes --output json
```

Do not run `trigger`, `delete`, `trigger-delete`, or `trigger-rotate-url` to
test — those are real side effects. Use `trigger` only when the user explicitly
asks for a manual run, and `trigger-rotate-url` only when rotating a webhook
URL; the old URL stops being valid immediately.

A schedule trigger without `--timezone` runs in **UTC**. Name the zone whenever
a human confirmed a wall-clock time, or they will confirm a morning job and
receive an afternoon one.

`autopilot get` redacts `webhook_token`, `webhook_path`, and `webhook_url` by
default while reporting whether a token exists and its non-sensitive hint. Only
add `--show-secrets` when the user explicitly asks to retrieve the live webhook
credential; the command warns on stderr. Do not paste webhook tokens or signing
material into comments, logs, docs, or PRs.

## Debugging "why didn't it run"

1. `multica autopilot get <id> --output json` — status, mode, assignee, triggers.
2. `multica autopilot runs <id> --output json` — run status and failure reason.
3. If assigned to a squad, inspect the squad: `multica squad get <squad-id> --output json`; execution goes to the leader.
4. Inspect the target agent/runtime: `multica agent get <agent-id> --output json` and `multica runtime list --output json`.
5. For webhooks, inspect delivery status: `queued` means the worker has not completed dispatch; `failed` carries the worker error. A provider retry with the same `X-GitHub-Delivery` / `Idempotency-Key` reuses the original delivery.
6. For `create_issue`, inspect the created issue if the run records one.

## Access

Reads (list / get / runs / deliveries) are open to any workspace member, but
`get` redacts the webhook token for callers without write access — the token
alone can trigger the autopilot.

Editing, deleting, triggering, replaying deliveries, and managing
triggers/webhook secrets require the autopilot's creator, a workspace
owner/admin, or an explicit collaborator. Granting or revoking a collaborator is
narrower still: creator or workspace owner/admin only, so a granted collaborator
keeps write/execute but cannot re-grant or revoke peers. `get` stamps two
per-caller booleans — `can_write` and the narrower `can_manage_access` — read
those rather than inferring from role.

## Side effects

These mutate durable state or start work: `create`, `update`, `delete`, trigger
add/update/delete/rotate, `trigger`, and webhook calls to
`/api/webhooks/autopilots/{token}`.
