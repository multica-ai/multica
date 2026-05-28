---
name: multica-issue-sync
description: Bind the current Codex App work to a Multica issue and sync Codex App conversation context, attachments, and token usage through the local Multica helper.
---

# Multica Issue Sync

Use this skill when the user wants Codex App work recorded in a Multica issue.

## Rules

- Treat Codex App as the runtime owner. Do not ask non-technical users to run
  `multica run <issue> -- codex`.
- Use the local Multica helper MCP tools for issue lookup, binding, event
  sync, conversation-thread comments, direct comments, and usage. The plugin starts them with
  `multica codex-plugin mcp`.
- After `session_bind`, plugin-bundled Codex lifecycle hooks are responsible
  for automatic per-turn conversation sync. `UserPromptSubmit` records the
  prompt, and `Stop` writes the matching assistant reply to the bound localrun
  comment thread.
- Reuse the user's local Multica login state. Never request, store, or print a
  Multica token in the plugin.
- Prefer issue search and selection. Treat pasted issue identifiers, UUIDs, or
  Multica issue URLs as fallback input.
- Include `source: "codex_app_plugin"` and a stable `source_key` on
  `session_bind` and `runtime_event_append` writes.
- `session_bind` creates a localrun issue comment thread with
  `comments_mode: "thread"`. Use that thread as the visible home for Codex App
  context.
- For normal progress noise, use `visibility: "timeline_only"`. For user
  prompts, proposed plans, and final results that should remain visible to
  issue readers, write them into the localrun thread.
- If the user asks why a later message did not sync, explain that normal chat
  only syncs automatically when Codex lifecycle hooks are enabled and trusted.
  Without trusted hooks, only explicit tool calls such as `conversation_sync`
  can write to the issue.
- The final visible conversation turn should normally be split into two
  localrun thread replies:

  ```text
  用户：<user request>

  bot：<assistant result>
  ```

  Use the user's actual language for the labels when appropriate.
- Do not fabricate token usage. If usage is unavailable, say that usage is
  unavailable or partial.
- Report usage only as cumulative totals. The first helper implementation
  reuses Multica localrun usage snapshots and does not accept delta usage.

## Workflow

1. Search or resolve the target issue with `issue_search` or `issue_get`.
2. Bind the current Codex context with `session_bind`. This stores local
   binding state for later hook-based turn sync.
3. Optionally sync key process events with `runtime_event_append` and
   `visibility: "timeline_only"`.
4. Use `usage_update` only when cumulative token usage is available.
5. Use direct Multica attachment flows for relevant files or artifacts until
   `attachment_upload` is added to the MCP server.
6. Prefer `conversation_sync` for the visible final result. Pass the latest user
   request as `user_message` and the assistant result as `bot_message`; the
   helper writes a `user_input` reply for the user text and a separate `final`
   reply for the bot text in the localrun issue thread.
7. If `conversation_sync` is unavailable, write separate visible localrun
   messages when possible: `user_input` for the user request and `final` for the
   assistant result. If that path is not available, fall back to
   `runtime_event_append` using `event_type: "final"` and
   `visibility: "issue_comment"`.
8. When extra Codex App context must be visible but is not a localrun final
   message, use `comment_add` with `parent_id` set to the binding run's
   `top_comment_id` when available.

## Event Guidance

- Use `plan` for proposed implementation plans.
- Use `progress` sparingly for meaningful milestones.
- Use `tool_summary` for compact command or tool outcomes.
- Use `approval_waiting` when user action is required.
- Use `error` for failures that affect task completion.
- Use `user_input` for the visible user prompt and `final` for the visible bot
  result that should appear as separate replies in the issue comment thread.
