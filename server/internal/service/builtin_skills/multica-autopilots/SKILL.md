---
name: multica-autopilots
description: "Use when creating, updating, inspecting, triggering, or debugging a Multica autopilot (scheduled, webhook, or manual)."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Autopilots

An autopilot is not an agent: a rule dispatching work to an agent. Chain: trigger (`schedule`|`webhook`|`manual`) → `autopilot_run` →
`execution_mode` → assignee → task.

Read first:

```bash
multica autopilot list --output json
multica autopilot get <autopilot-id> --output json
multica autopilot runs <autopilot-id> --output json
```

Do not run `trigger`, `delete`, `trigger-delete`, or `trigger-rotate-url` to
test — real side effects; `trigger` only on explicit request; rotate only
for URL rotation (old URL dies).

```bash
multica autopilot create --title "<title>" --description "<task prompt>" --agent <agent-name-or-id> --mode create_issue|run_only --output json
multica autopilot update <autopilot-id> --status active|paused --output json
multica autopilot trigger-add <autopilot-id> --kind schedule --cron "0 9 * * *" --timezone Asia/Shanghai --output json
multica autopilot trigger-add <autopilot-id> --kind webhook --label "ci" --output json
multica autopilot trigger <autopilot-id> --output json
multica autopilot trigger-rotate-url <autopilot-id> <trigger-id> --yes --output json
```

`create_issue` = run visible as issue state; `run_only` = task only. `issue-title-template` supports only `{{date}}` — never invent
`{{trigger_id}}`/`{{branch}}`. Webhooks: ingress queues `webhook_delivery`, idempotent run, `200`
`status=accepted|skipped` + `run_id`; same delivery key reuses it. Redact
webhook tokens everywhere.

Squad-assigned autopilots resolve to the squad's leader agent. Debug: get
→ runs → squad get → agent get / runtime list. Webhook: `queued` = dispatch unfinished; `failed` = worker error. Side effects: `create`, `update`, `delete`, trigger*/rotate, webhook
calls.

Details: `references/autopilots-source-map.md`.
