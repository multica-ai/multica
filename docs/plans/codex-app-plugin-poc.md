# Codex App Plugin POC

Issue: OPE-1493

This plan is the implementation baseline for the Multica Codex App plugin work.
All code for the related child issues should stay on one branch so the final PR
can be reviewed as a single product slice.

## Product Boundary

The Codex App is the user-facing entry point. Multica should not ask
non-technical users to start Codex with `multica run <issue> -- codex`.

Responsibilities:

- Codex App manages threads, local project folders, workdirs, worktrees,
  terminal execution, review UI, and Git interactions.
- The Multica Codex App plugin helps the user search, select, and bind a
  Multica issue to the current Codex work.
- The local Multica helper reuses the existing `multica` login state and
  exposes issue sync tools to Codex.
- The Multica server reuses the existing localrun storage for issue bindings,
  runtime messages, final conversation comments, usage records, and
  idempotency keys.

## Current Capability Assumptions

These assumptions intentionally keep the POC conservative:

- OpenAI's Codex plugin packaging docs define `.codex-plugin/plugin.json` as
  the required manifest, with optional `skills/`, `.mcp.json`, `.app.json`,
  `hooks/`, and `assets/` entries kept at the plugin root.
- Manifest component paths should stay relative to the plugin root and start
  with `./`; the POC plugin follows this for `skills`, `mcpServers`, and
  bundled hooks.
- OpenAI's install guidance treats bundled skills as immediately available
  after plugin install, while MCP servers may require extra setup or
  authentication. This matches the Multica helper approach: install the plugin,
  then rely on the user's existing local `multica` login.
- A Codex plugin can package skills and MCP server configuration.
- A plugin should call Multica through a local helper/MCP surface rather than
  storing Multica tokens in plugin files.
- Codex plugin-bundled hooks are available through `hooks/hooks.json`, but
  Codex requires the user to review and trust hook definitions before they run.
- If Codex cannot expose full token usage to the plugin, usage must be reported
  as partial or unavailable. The plugin must not invent token counts.

Reference docs:

- <https://developers.openai.com/codex/plugins/build#plugin-structure>
- <https://developers.openai.com/codex/plugins/build#path-rules>
- <https://developers.openai.com/codex/plugins#how-permissions-and-data-sharing-work>

## First POC Flow

1. User installs the Multica Codex App plugin.
2. User asks Codex to bind work to a Multica issue.
3. Plugin/helper searches issues by title, identifier, UUID, URL, assignee,
   project, and status.
4. User selects an issue or pastes a fallback reference.
5. Plugin/helper creates or reuses a binding for the current Codex context.
6. The helper stores local binding state so lifecycle hooks can find the active
   issue in follow-up turns.
7. Codex calls helper tools at meaningful points:
   - plan
   - progress
   - tool summary
   - approval waiting
   - error
   - final answer
   - usage update
   - attachment upload
8. The helper writes process events as localrun timeline messages by default.
9. Plugin hooks automatically mirror each trusted follow-up turn after
   binding:
   - `UserPromptSubmit` stores the user prompt.
   - `Stop` reads `last_assistant_message` and writes separate user/bot replies
     to the bound localrun thread.
10. The helper mirrors the user/bot conversation result as two issue thread
   comments, for example:

   ```text
   用户：hello

   bot：hello
   ```

## Helper Tool Contract

The initial machine-readable contract is exposed by:

```bash
multica codex-plugin schema --output json
```

This contract is the source of truth for the first MCP/helper implementation.
It covers:

- `issue_search`
- `issue_get`
- `session_bind`
- `runtime_event_append`
- `conversation_sync`
- `comment_add`
- `usage_update`
- `attachment_upload`

The first stdio MCP implementation is exposed by:

```bash
multica codex-plugin mcp
```

It currently implements:

- `issue_search`
- `issue_get`
- `session_bind`
- `runtime_event_append`
- `conversation_sync`
- `comment_add`
- `usage_update`

`attachment_upload` remains in the schema contract but is not exposed through
the first MCP server slice yet. Keep attachment uploads on the existing
Multica attachment/comment flow until the helper adds idempotent attachment
source keys.

Every tool returns this envelope:

```json
{
  "ok": true,
  "data": {},
  "error": null,
  "request_id": "..."
}
```

Failed calls use:

```json
{
  "ok": false,
  "data": null,
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Multica login is required",
    "retryable": false
  },
  "request_id": "..."
}
```

## Local POC Usage

The repo now contains the plugin package at:

```text
plugins/multica-codex-app
```

The first POC uses:

- `plugins/multica-codex-app/.codex-plugin/plugin.json` for plugin metadata.
- `plugins/multica-codex-app/.mcp.json` to launch the local helper with
  `multica codex-plugin mcp`.
- `plugins/multica-codex-app/hooks/hooks.json` to call
  `multica codex-plugin hook` on `UserPromptSubmit` and `Stop`.
- `plugins/multica-codex-app/skills/multica-issue-sync/SKILL.md` to instruct
  Codex when to search, bind, sync events, post final results, and report usage.

Before using the plugin, install or build a `multica` binary that includes this
branch and make sure it is on `PATH`. The helper intentionally reuses the
existing CLI login state, so run the normal Multica setup/login flow first.

For development, the helper schema can be inspected with:

```bash
cd server
go run ./cmd/multica codex-plugin schema --output json
```

The MCP server can be smoke-tested without Codex by piping JSON-RPC lines to
the command:

```bash
cd server
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | go run ./cmd/multica codex-plugin mcp
```

Against a running Multica backend and logged-in CLI, the intended tool sequence
is:

```text
issue_search
session_bind
runtime_event_append
usage_update
conversation_sync
```

The final answer path should prefer `conversation_sync`, which receives
`user_message` and `bot_message`, then stores them as separate localrun
messages: `user_input` for the user comment and `final` for the assistant
reply. If that helper is unavailable, fall back to separate localrun messages
when possible, or `runtime_event_append` with `event_type=final` and
`visibility=issue_comment` as a compatibility fallback. The binding uses
localrun `thread` mode, so visible conversation context is preserved as replies
under the issue comment thread. Noisy process events can keep
`visibility=timeline_only`.

After `session_bind`, normal follow-up chat is handled by hooks instead of a
model-initiated MCP call. If hooks are not trusted, a follow-up like `HI` will
stay only in Codex App. Once hooks are trusted, `UserPromptSubmit` and `Stop`
sync the turn to the same issue comment thread.

## Idempotency

Binding and runtime event writes accept `source` and `source_key`.

Rules:

- `source` for this plugin is `codex_app_plugin`.
- `source_key` should include the Codex thread/session and a stable event id.
- `session_bind` stores the binding as a `local_cli_run` with
  `cli_name=codex_app` and `comments_mode=thread`.
- `session_bind` also stores local hook state under the Multica CLI profile so
  later `Stop` hooks can locate the binding.
- `runtime_event_append` stores events as `local_cli_message` rows.
- Final answers should use `conversation_sync`, which writes separate
  `user_input` and `final` messages so existing localrun thread-reply mirroring
  and message idempotency are reused. If unavailable, use
  `runtime_event_append(event_type=final, visibility=issue_comment)` as a
  compatibility fallback.
- `usage_update` reuses `local_cli_usage`, which stores cumulative snapshots by
  run/provider/model. Callers must send cumulative totals, not deltas.
- Direct `comment_add` reuses the existing issue comment API and is not
  idempotent in the first reuse-first slice.

## Data Model Direction

The first implementation intentionally avoids a separate Codex backend model.
It reuses existing localrun tables and adds only minimal binding metadata:

- `local_cli_run.source`
- `local_cli_run.source_key`

The plugin-facing schema still uses neutral names like `session_bind` and
`runtime_event_append`, but the current helper maps them onto localrun rows:

- `session_bind` -> `POST /api/issues/{id}/local-runs`
- `runtime_event_append` -> `POST /api/local-runs/{runId}/messages`
- `usage_update` -> `PUT /api/local-runs/{runId}/usage`
- `comment_add` -> `POST /api/issues/{id}/comments`

This keeps the user-facing plugin design from drifting while minimizing new
backend surface area. A more neutral session/event/usage model can still be
introduced later if localrun semantics become a product limitation.

## POC Acceptance

The first working POC should demonstrate:

- Issue search and fallback paste by identifier, UUID, or Multica issue URL.
- Binding creation or idempotent binding reuse.
- At least one plan/progress event written to Multica.
- Final answer mirrored to an issue comment.
- Follow-up user/bot turns mirrored automatically after hook trust.
- Token usage record written when usage is available.
- Idempotency for repeated binding and event submissions.
- Cumulative usage updates through the existing localrun usage API.
- Clear unauthorized, forbidden, and issue-not-found errors.
- No requirement for `multica run <issue> -- codex`.

## Current Branch Acceptance Snapshot

The unified branch `feat/OPE-1493-codex-plugin` currently covers:

- Plugin package scaffold with manifest, skill, and MCP server config.
- Plugin-bundled lifecycle hooks for `UserPromptSubmit` and `Stop`.
- CLI schema output through `multica codex-plugin schema --output json`.
- Reuse of existing localrun APIs for binding, runtime events, final comments,
  and usage.
- Stdio MCP server through `multica codex-plugin mcp`.
- MCP tools for `issue_search`, `issue_get`, `session_bind`,
  `runtime_event_append`, `conversation_sync`, `comment_add`, and
  `usage_update`.
- Idempotent binding/event writes using `source/source_key`.
- `comments_mode=thread` for Codex App plugin bindings, so Codex App context is
  preserved under the issue comment thread.
- Tests that simulate the MCP sequence and localrun source-key reuse.
- Tests that simulate hook-based prompt capture and stop-time conversation
  sync.

Known POC gaps intentionally left out of this slice:

- `attachment_upload` is still part of the schema contract but is not exposed
  by the first MCP server implementation.
- Automatic turn sync depends on Codex lifecycle hooks being enabled and
  trusted. Official Codex hooks expose `UserPromptSubmit.prompt`,
  `Stop.last_assistant_message`, and `transcript_path`, but transcript format
  is documented as not stable for hooks.
- Complete token usage capture remains capability-dependent.
- A polished Codex App UI issue selector is not implemented in this repo slice;
  the first integration path is skill-guided MCP tool use.

## Implementation Order

1. Ship plugin skeleton and schema contract.
2. Add server data model and API for binding, events, and usage.
3. Implement local helper/MCP server using the schema contract.
4. Wire the plugin to the local helper.
5. Add end-to-end tests for binding, event writes, final comment, usage, and
   duplicate source keys.
6. Package and validate lifecycle hooks; require users to trust them in Codex
   before expecting automatic follow-up turn sync.
