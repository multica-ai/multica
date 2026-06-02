# Multica Codex App Plugin

This plugin binds Codex App work to Multica issues and syncs Codex App
conversation context back to Multica through the local `multica` CLI helper.

## Contents

- `.codex-plugin/plugin.json` - Codex plugin manifest.
- `.mcp.json` - MCP server config that starts `multica codex-plugin mcp`.
- `hooks/hooks.json` - Codex lifecycle hooks that sync each user prompt when
  it is submitted and sync the matching assistant reply after each turn stops.
- `skills/multica-issue-sync/SKILL.md` - workflow rules for issue search,
  binding, localrun thread sync, conversation comments, and usage
  reporting.

The structure follows the Codex plugin package shape documented by OpenAI:
manifest under `.codex-plugin/`, with `skills/` and `.mcp.json` at the plugin
root.

## Local Requirements

- A `multica` binary built from this branch and available on `PATH`.
- A logged-in Multica CLI profile. The plugin does not store Multica tokens.
- Access to the target workspace and issue.

## Install Script

The repository includes a reusable installer at:

```bash
scripts/install_multica_codex_app.sh
```

From a development checkout, build the CLI first and then install the Codex App
plugin:

```bash
make build
scripts/install_multica_codex_app.sh
```

For a dry run:

```bash
scripts/install_multica_codex_app.sh --dry-run
```

If the user's Multica CLI is already installed and should not be replaced:

```bash
scripts/install_multica_codex_app.sh --skip-cli-install
```

The script expects this repository or distribution-package layout:

```text
.agents/plugins/marketplace.json
cli/multica                         # optional with --skip-cli-install
plugins/multica-codex-app/
scripts/install_multica_codex_app.sh
```

It removes the previous Codex plugin install/cache, optionally backs up and
replaces the local `multica` CLI, restarts `multica daemon`, rewrites the MCP
and hook command paths to the installed CLI, then installs:

```text
multica-codex-app@multica-local
```

The installer supports both Codex CLI variants that expose plugin install as
`codex plugin add` or `codex plugin a`.

## Packaging

Build a user-distributable package with the same layout expected by the
installer:

```bash
make build
scripts/package_multica_codex_app.sh ~/Downloads/multica --force
```

The output directory contains the directly runnable install layout and also
versioned `.tar.gz` and `.zip` archives:

```text
~/Downloads/multica/
~/Downloads/multica/multica-codex-app-<version>.tar.gz
~/Downloads/multica/multica-codex-app-<version>.zip
```

Set `MULTICA_SOURCE_BIN=/path/to/multica` to package a specific CLI binary.

## Helper Commands

Inspect the helper contract:

```bash
multica codex-plugin schema --output json
```

Start the stdio MCP server:

```bash
multica codex-plugin mcp
```

Smoke-test MCP initialization and tool discovery:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | multica codex-plugin mcp
```

## First POC Workflow

1. Use `issue_search` or `issue_get` to find the Multica issue.
2. Use `session_bind` with `source: "codex_app_plugin"` and a stable
   `source_key`. This creates a localrun issue comment thread with
   `comments_mode=thread`.
   The helper also stores the binding in local CLI state with the current
   Codex session id so plugin hooks can find the active issue in later turns.
3. Use `runtime_event_append` for meaningful plan, progress, tool summary,
   approval, and error events. Use `visibility: "timeline_only"` for noisy
   process details and `visibility: "issue_comment"` when the context should
   remain visible in the issue thread.
4. Use `usage_update` only when cumulative token usage is actually available.
5. Prefer `conversation_sync` for the visible result. It accepts
   `user_message` and `bot_message`, then writes them as two issue thread
   replies. Comment roles are carried by the localrun message type, so the
   visible comment body is the original text without a `user` / `bot` prefix:

   ```text
   hello

   hello
   ```

6. If `conversation_sync` is unavailable, write separate `user_input` and
   `final` localrun messages when possible. As a fallback, use
   `runtime_event_append` with
   `event_type: "final"` and `visibility: "issue_comment"` and pass the same
   conversation-style content manually.
7. Use `comment_add` with the binding run's `top_comment_id` as `parent_id`
   when ordinary direct comment behavior is needed inside the same issue
   thread.

## Automatic Turn Sync

The plugin now bundles Codex lifecycle hooks:

- `UserPromptSubmit` writes the submitted user prompt to the bound issue
  thread immediately, without waiting for the agent turn to finish.
- `Stop` reads `last_assistant_message` and writes the matching assistant reply
  to the same issue thread. The visible comments contain only the original
  prompt and reply text:

  ```text
  <user prompt>

  <assistant reply>
  ```

The two replies are idempotent and tracked separately, so a retried
`UserPromptSubmit` hook does not duplicate the user prompt, and a later `Stop`
hook only fills in the assistant reply. Codex requires plugin-bundled hooks to
be reviewed and trusted before they run; until the user trusts the hooks, only
explicit MCP tool calls from the skill workflow will sync.

Automatic hook sync is isolated by Codex chat session. A hook only writes to a
binding whose stored `codex_session_id` matches the hook input `session_id`; it
does not fall back to matching by project folder or by the only local binding.
If no stable session id is available, automatic hook sync is skipped to avoid
mixing unrelated chat sessions into the bound issue.

## Idempotency

Binding and runtime-event writes should include a stable `source_key` derived
from the Codex thread/session plus a stable event id. Repeating the same
binding or runtime-event write reuses the existing localrun row/message.
For `session_bind`, the helper normalizes binding idempotency to the current
Codex `session_id` when it is available. This avoids reusing older thread-level
bindings that may not have a visible issue comment thread.

The plugin binds localrun with `comments_mode=thread`, so the binding creates a
top-level issue comment and visible Codex App context is preserved as replies in
that thread. User prompts are mirrored as `user_input` replies, while assistant
results are mirrored as `final` replies. If the server returns a binding without
`top_comment_id`, the helper rejects it instead of storing a binding that hooks
cannot visibly sync to.

Usage reuses the existing localrun cumulative snapshot endpoint, so callers
must send cumulative totals by binding/provider/model. Direct `comment_add`
uses the existing issue comment API and is not idempotent in this first slice.

## Current Limits

- `attachment_upload` is defined in the schema contract but is not exposed by
  the first MCP server implementation.
- Automatic per-turn sync depends on Codex lifecycle hooks being enabled and
  trusted. If hooks are disabled or not trusted, Codex will not call the hook
  command and normal follow-up chat will not be mirrored automatically.
- Token usage must be omitted or marked partial when the Codex host does not
  expose exact usage fields.
