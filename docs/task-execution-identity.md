# Current task execution identity

`GET /api/tasks/current/identity` requires a live `mat_` bearer token. It
accepts no identity selectors. Auth middleware supplies the bound task,
agent and workspace; caller-supplied actor headers and workspace selectors
cannot change them.

Successful response:

```json
{"task_id":"UUID","agent_id":"UUID","workspace_id":"UUID","kind":"human","user_id":"UUID"}
```

`kind` is `human` or `automation`. Automation returns an empty `user_id` even
when its trigger has a human owner. Consumers must allowlist their workspace
and reject unknown kinds, malformed IDs and unsuccessful responses. Responses
are `Cache-Control: no-store`; verify the token again for each external action.
This endpoint supplies provenance, not GitHub credentials or repository rights.

The handler requires a running/dispatched task, matches its agent to the token,
and checks current workspace membership for the requesting human. Resolution
follows retry parents and delegation/comment-source parents within the same
workspace, bounded to 32 tasks with cycle and originator-consistency checks.
`direct_human` roots identify the requester; `trigger_owner` and `rule_owner`
identify automation. Manual reruns use their new human root, rather than the
historical `rerun_of_task_id`. Missing or degraded provenance returns 403.
Expired/invalid task tokens return 401 through normal authentication.

Implementation: `server/internal/handler/task_identity.go` and
`server/internal/attribution/execution_identity.go`. The handler tests exercise
the auth and workspace middleware with forged headers and selectors; pure
attribution tests cover human/automation lineage without a database.
