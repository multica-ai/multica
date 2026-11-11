# Raw REST API (CLI gaps)

The `multica` CLI covers the common surface (issues CRUD, comments, steering,
runs, run-messages, wakeups, agents, squads, members, labels, projects,
workflows, properties). Use raw HTTP only for:

- inbox endpoints (see inbox-wakeups.md)
- comment trigger preview and `suppress_agent_ids`
- task supplements (note on one exact run)
- PR link/unlink/auto-complete
- batch operations (`POST /api/issues/batch-update`, `/query`)

## Transport

```bash
curl -s "$MULTICA_API_URL/api/..." \
  -H "Authorization: Bearer $MULTICA_TOKEN" \   # a user PAT (mul_...)
  -H "X-Workspace-ID: <workspace-uuid>" \
  -H "Content-Type: application/json"
```

Point `$MULTICA_API_URL` at the instance's API origin (the same URL the CLI
was logged in against; `MULTICA_SERVER_URL` in the CLI config). Get the
workspace uuid via `multica workspace list --output json`. Use the same PAT
the CLI was logged in with.

## Comment create (full payload)

`POST /api/issues/{id}/comments`:

```json
{
  "content": "markdown with [@Name](mention://agent/<uuid>) links",
  "type": "comment",
  "parent_id": "<comment-uuid>",
  "suppress_agent_ids": ["<uuid>"],
  "steer_task_ids": ["<run-uuid>"],
  "steer": false
}
```

- `parent_id`: reply threading. A comment-triggered agent run must reply under
  its trigger comment.
- `suppress_agent_ids`: render an @agent mention without enqueueing it.
- `steer` / `steer_task_ids`: same semantics as the CLI's `--steer-active` /
  `--steer` — prefer the CLI for steering.
- `type` is `"comment"` for normal comments; do not invent other values.

Trigger preview: `POST /api/issues/{id}/comments/trigger-preview` body
`{"content": "..."}` (optionally `"editing_comment_id"` when previewing an
edit). Lists agents that will run and mentions that are blocked.

Update/delete a comment: `PUT/DELETE /api/comments/{commentId}`.

## Task supplement

`POST /api/issues/{id}/tasks/{taskId}/supplements`:

```json
{ "content": "note text", "client_request_id": "<fresh random uuid>" }
```

Records one historical comment bound to that exact run — annotate or correct a
run without triggering a new one. Human-actor accounts only (the assistant
account qualifies; platform agent actors are denied). If the task completes
first, no supplement is created.

## Issue create/update (reference payload)

The CLI (`multica issue create/update`) is the primary path; raw shapes when
needed:

- `POST /api/issues`: `title` (required), `description`, `status`,
  `priority`, `assignee_type` (`member|agent`), `assignee_id`,
  `parent_issue_id`, `project_id`, `stage`, `start_date`, `due_date`,
  `label_ids`, `properties`.
- `PUT /api/issues/{id}`: same fields, all optional (patch semantics). Notable:
  `suppress_run: true` applies an assignee/status change without starting the
  run it would trigger; `expected_revision` for optimistic concurrency;
  `stop_previous_assignee_runs: true` cancels the previous assignee's active
  runs on handoff.

## Listing filters

`GET /api/issues?status=a,b&priority=high&project_id=<uuid>&label_ids=<uuid>[,<uuid>]&q=<text>&limit=&offset=`;
relevance-ranked search: `GET /api/issues/search?q=...`. Reads under an issue:
`/comments`, `/timeline`, `/children`, `/attachments`, `/labels`,
`child-progress?parent_ids=...`.
