# Qoder Cloud Agents integration

Multica's `qodercloud` runtime uses Qoder's hosted Session API. Qoder built-in
tools run inside the selected Qoder Environment. Multica business tools are
client-side custom tools: Qoder requests them, the Multica daemon executes an
exact allowlisted operation, and the daemon sends the result back to the same
Qoder Session.

## Agent tool configuration

Configure the complete `tools` array below on the Qoder Agent selected by
`MULTICA_QODERCLOUD_AGENT_ID`. The update API replaces the entire tools array,
so do not send only the entries you are adding.

The custom-tool schemas intentionally use only the minimal JSON Schema subset
that works reliably with Qoder Ultimate. Richer constraints may be accepted and
returned by the Agent API but can still be rejected by the upstream model. The
Multica daemon's validation is authoritative for UUIDs, enum values, numeric
limits, string lengths, required update fields, and task scope.

```json
{
  "tools": [
    {
      "type": "agent_toolset_20260401",
      "enabled_tools": [
        "Bash",
        "Read",
        "Write",
        "Edit",
        "Glob",
        "Grep",
        "WebFetch",
        "WebSearch"
      ]
    },
    {
      "type": "custom",
      "name": "multica_list_issues",
      "description": "List issues. During a Chat run, lists the current Multica workspace; during an Issue or comment run, returns only the assigned issue. Returns issue UUIDs for subsequent calls. If status is provided, it must be one of backlog, todo, in_progress, in_review, done, blocked, or cancelled. If limit is provided, it must be an integer from 1 through 50.",
      "input_schema": {
        "type": "object",
        "properties": {
          "status": { "type": "string" },
          "limit": { "type": "integer" }
        },
        "additionalProperties": false
      }
    },
    {
      "type": "custom",
      "name": "multica_get_issue",
      "description": "Get one issue in the current Multica workspace. issue_id must be a UUID.",
      "input_schema": {
        "type": "object",
        "properties": {
          "issue_id": { "type": "string" }
        },
        "required": ["issue_id"],
        "additionalProperties": false
      }
    },
    {
      "type": "custom",
      "name": "multica_list_issue_comments",
      "description": "List recent comments for an issue in the current Multica workspace. issue_id must be a UUID. If limit is provided, it must be an integer from 1 through 50.",
      "input_schema": {
        "type": "object",
        "properties": {
          "issue_id": { "type": "string" },
          "limit": { "type": "integer" }
        },
        "required": ["issue_id"],
        "additionalProperties": false
      }
    },
    {
      "type": "custom",
      "name": "multica_create_issue",
      "description": "Create an unassigned issue in the current Multica workspace. title must contain 1 through 500 characters. If description is provided, it must contain at most 20000 characters. If status is provided, it must be one of backlog, todo, in_progress, in_review, done, blocked, or cancelled. If priority is provided, it must be one of urgent, high, medium, low, or none.",
      "input_schema": {
        "type": "object",
        "properties": {
          "title": { "type": "string" },
          "description": { "type": "string" },
          "status": { "type": "string" },
          "priority": { "type": "string" }
        },
        "required": ["title"],
        "additionalProperties": false
      }
    },
    {
      "type": "custom",
      "name": "multica_update_issue",
      "description": "Update allowed fields on an issue. issue_id must be a UUID, and during an Issue run it must be the assigned issue UUID. Provide at least one of title, description, status, or priority. If provided, title must contain 1 through 500 characters and description at most 20000 characters. status must be one of backlog, todo, in_progress, in_review, done, blocked, or cancelled. priority must be one of urgent, high, medium, low, or none.",
      "input_schema": {
        "type": "object",
        "properties": {
          "issue_id": { "type": "string" },
          "title": { "type": "string" },
          "description": { "type": "string" },
          "status": { "type": "string" },
          "priority": { "type": "string" }
        },
        "required": ["issue_id"],
        "additionalProperties": false
      }
    },
    {
      "type": "custom",
      "name": "multica_add_issue_comment",
      "description": "Add a comment to an issue. issue_id must be a UUID. content must contain 1 through 20000 characters. If parent_id is provided, it must be a UUID. A comment-triggered Issue run must use its assigned trigger comment UUID as parent_id.",
      "input_schema": {
        "type": "object",
        "properties": {
          "issue_id": { "type": "string" },
          "content": { "type": "string" },
          "parent_id": { "type": "string" }
        },
        "required": ["issue_id", "content"],
        "additionalProperties": false
      }
    }
  ]
}
```

Update an existing Agent with its current optimistic-lock version:

```bash
jq --argjson version "${QODER_AGENT_VERSION}" '. + {version: $version}' agent-tools.json | \
curl --request POST \
  --url "https://api.qoder.com/api/v1/cloud/agents/${QODER_AGENT_ID}" \
  --header "Authorization: Bearer ${QODER_PAT}" \
  --header "Content-Type: application/json" \
  --data-binary @-
```

`QODER_AGENT_VERSION` must be the version returned by the preceding Agent read.
Read the Agent back after updating and record its new version in
`MULTICA_QODERCLOUD_AGENT_VERSION`, or leave that daemon setting unset so
Multica resolves the latest version before creating a Session.

Tool changes apply to new Sessions only. An already-created Multica Chat or
Issue Session remains pinned to the Agent version it was created with. Start a
new conversation or clear the prior session pointer before verification.

## Custom-tool protocol

When Qoder emits `agent.custom_tool_use`, Multica:

1. uses the incoming event ID as `custom_tool_use_id`;
2. buffers the request and emits a local tool-use message without executing it;
3. waits for `session.status_idle` with
   `stop_reason.type=requires_action` to declare the pending `event_ids`;
4. validates the whole declared batch before any tool runs, then executes each
   daemon allowlist call once in `event_ids` order;
5. posts each `user.custom_tool_result` to
   `/api/v1/cloud/sessions/{session_id}/events`; and
6. continues the SSE stream until the turn becomes idle.

The daemon caches each result for the lifetime of the execution and advances
the stream cursor only after the entire declared batch succeeds. A replay after
an SSE reconnect or an ambiguous result POST can resend an unsent cached result,
but cannot execute a mutating tool twice. Unknown action IDs, an empty action
batch, or a terminal idle event with unresolved custom tools fail closed.
Built-in or MCP tools that request an interactive permission decision also fail
closed.

## Security boundary

- The Qoder PAT exists only in the daemon's Qoder API client.
- The `mat_` task token exists only in a separate daemon-side Multica API
  client. It is never serialized into a Qoder request, prompt, tool input, tool
  result, or log.
- The server binds that token to the current user, agent, task, and workspace.
- The dispatcher accepts only the six names above, rejects unknown JSON keys,
  validates values and lengths, and uses fixed REST paths.
- An Issue task can read or mutate only its assigned issue. A comment-triggered
  task can reply only under its assigned trigger or coalesced comments.
- Server responses are checked against the task workspace, compacted, redacted,
  and size-limited before being returned to Qoder.
- `multica_update_issue` sets `suppress_run=true` so a cloud-side field update
  cannot unexpectedly enqueue another agent run.
- Bash and file tools execute in Qoder's hosted container. They do not grant
  access to the daemon host, its checkout, or its filesystem.

## Browser E2E verification (2026-08-10)

The live Agent v8 configuration was verified through the Multica web UI with
all six custom tools available.

- A single turn ran `multica_get_issue`, `multica_list_issue_comments`, and
  `multica_list_issues` in parallel. All three tool-use/result pairs completed.
- A second requirement was created as MUL-4, updated to `in_progress` and
  `high`, commented on, and then read back with two parallel tools in the same
  turn. All four tool-use/result pairs completed, and the Issue page confirmed
  that the writes persisted.
- The Qoder Cloud focused and race-enabled Go test suites passed, including a
  simulated partial batch failure where the first result succeeds and the
  second returns HTTP 503 before reconnecting.

The bridge guarantees handler exactly-once only for the lifetime of one daemon
process. Persisted call-ID idempotency across a daemon crash, and reconciliation
after an ambiguous result POST that Qoder accepted upstream, remain hardening
work rather than blockers for the verified Chat and Issue flows.

## Supported task surfaces

The bridge supports Chat, ordinary Issue assignment, and Issue comment-reply
tasks. Autopilot, quick-create, quick-actions, and squad execution remain
fail-closed until they receive purpose-built schemas and lifecycle handling.
