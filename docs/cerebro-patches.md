# CEREBRO Patch Registry

Permanent inline modifications and fork-additions in upstream-zone files. Each entry
documents one named patch + its rationale + the file location(s).

**Marker format:** `// CEREBRO-PATCH(<name>): <description>` (or language-appropriate
comment for SQL/CSS/SBPL/JSON-with-_comment-field).

## Summary

- **Unique patch names:** 288 (273 baseline + 15 PR #118 markers)
- **Files marked (including chunks 4-8 pre-existing markers):** 298
- **Files newly marked in chunk 11:** 280 (chunk-11 scope)
- **Total marker lines added in chunk 11:** ~280 (one comment line per file)
- **Marker lines added in PR #118:** 16 (managed Firtal Gateway runtime scope)
- **Total fork-vs-upstream `+` lines (chunk-11 scope, across 280 files):** 22,170
- **Phase 4 target for cerebro-mod lines:** ≤200 (escalation threshold: >300)

> **Status: ESCALATED.** The 22,170 `+`-line count is far above the original Phase 4 target of ≤200.
> The original audit estimated 42 L3-marked files; reality includes ~280 upstream-zone files,
> the majority of which are net-new fork features (artifacts, profile, MCP, inbox-folders,
> work-sessions, project-access, budgets) sitting in upstream paths rather than `cerebro-*` packages.
> True L3 inline-modifications (where cerebro logic is woven into upstream files) are a small
> subset; the rest should ideally be RELOCATED to `cerebro-*` packages in a follow-up phase.
> See `Categories` section below.

## Categories

| Category | File count | Lines (`+`) | Treatment |
|---|---|---|---|
| Net-new fork files in upstream paths | 126 | 17917 | Marker on file header; candidate for relocation to `cerebro-*` |
| True L3 inline modifications | 154 | 4253 | Marker on file header (or inline at modification site for chunks 4-8) |

## Patch index

| Patch name | File(s) | `+` lines | Rationale |
|---|---|---|---|
| `api-client-401-warn` | packages/core/api/client.ts | 1 | Log 401 as warn (like 404) — expected pre-login state, keeps Next.js dev overlay quiet |
| `auth-init-401-info` | packages/core/platform/auth-initializer.tsx | 5 | No-active-session (401) is expected pre-login state — log as info, not error |
| `docs-panel-mdx-shims` | apps/web/app/[workspaceSlug]/(dashboard)/settings/docs-panel.tsx<br>apps/web/app/[workspaceSlug]/(dashboard)/settings/cerebro-mdx-shims.tsx | 2 (panel) + 79 (shim file) | MDX components (`NumberedCard`, `Step`, ...) used in cerebro docs MDX but missing after upstream's docs rewrite (commit 8c2e0841) |
| `SA3-project-picker` | packages/views/projects/components/project-picker.tsx | 29 | Project-picker — RestrictedLock + custom icon rendering (no clean wrap-point) |
| `access-handler` | server/internal/handler/access_test.go<br>server/internal/handler/access_ws_test.go<br>server/internal/handler/access.go<br>...+2 more | 873 | Project-access + privacy enforcement (cerebro feature) |
| `account-tab-cerebro` | packages/views/settings/components/account-tab.tsx | 3 | Settings page cerebro additions |
| `add-runtime-dialog` | packages/views/runtimes/components/add-runtime-dialog.tsx | 217 | Runtime view cerebro additions |
| `agent-cerebro` | server/pkg/agent/agent.go | 4 | Cerebro additions to agent runtime |
| `agent-firtal-gateway-runtime` | server/pkg/agent/firtal_gateway.go<br>server/pkg/agent/agent.go | 407 | Managed Firtal Data Registry AI Gateway HTTP backend and provider registration (PR #118) |
| `agent-firtal-gateway-tests` | server/pkg/agent/firtal_gateway_test.go<br>server/pkg/agent/agent_test.go | 165 | Request/response, usage/cost, model discovery, and backend factory coverage for the managed gateway runtime |
| `agent-firtal-gateway-usage-cost` | server/pkg/agent/agent.go | 1 | Preserve gateway-reported `cost_cents` alongside token usage so downstream budget rollups can use the exact managed-runtime spend |
| `agent-models-firtal-gateway` | server/pkg/agent/models.go | 5 | Include managed gateway model discovery in the runtime model-list API |
| `agent-codex-semantic-inactivity-test` | server/pkg/agent/codex_test.go | 3 | Make semantic-inactivity progress tests scheduler-tolerant while still verifying real inactivity timeouts |
| `agent-claude-cerebro` | server/pkg/agent/claude.go | 123 | Cerebro additions to agent runtime |
| `agent-copilot-cerebro` | server/pkg/agent/copilot.go | 13 | Cerebro additions to agent runtime |
| `agent-cursor-cerebro` | server/pkg/agent/cursor.go | 13 | Cerebro additions to agent runtime |
| `agent-gemini-cerebro` | server/pkg/agent/gemini.go | 13 | Cerebro additions to agent runtime |
| `agent-handler` | server/internal/handler/agent.go | 16 | Cerebro additions to agent runtime |
| `agent-live-card-cerebro` | packages/views/issues/components/agent-live-card.tsx | 224 | Cerebro additions to agent runtime |
| `agent-profile-tab` | packages/views/settings/components/agent-profile-tab.tsx | 439 | Cerebro additions to agent runtime |
| `agent-sandbox-cerebro` | server/pkg/agent/sandbox.go | 113 | Cerebro sandbox/seatbelt additions for macOS agent runtime |
| `agent-sandbox-default-golden` | server/pkg/agent/sandbox/testdata/default.golden.sb | 74 | Cerebro sandbox/seatbelt additions for macOS agent runtime |
| `agent-sandbox-macos` | server/pkg/agent/sandbox/macos.go | 453 | Cerebro sandbox/seatbelt additions for macOS agent runtime |
| `agent-sandbox-macos-seatbelt-test` | server/pkg/agent/sandbox/macos_seatbelt_test.go | 397 | Cerebro sandbox/seatbelt additions for macOS agent runtime |
| `agent-sandbox-macos-test` | server/pkg/agent/sandbox/macos_test.go | 417 | Cerebro sandbox/seatbelt additions for macOS agent runtime |
| `agent-transcript-dialog` | packages/views/issues/components/agent-transcript-dialog.tsx | 1 | Cerebro additions to agent runtime |
| `handler-agent-firtal-gateway-chat-history` | server/internal/handler/agent.go | 49 | Add bounded chat transcript to daemon claim responses for stateless managed HTTP runtimes |
| `handler-chat-coalesce-firtal-gateway` | server/internal/handler/chat_coalesce_test.go | 11 | Regression coverage for the chat transcript payload used by the managed gateway runtime |
| `handler-daemon-firtal-gateway-usage-cost` | server/internal/handler/daemon.go | 28 | Accept exact gateway `cost_cents` in task usage reports and use it for live budget rollups |
| `runtime-provider-logo-firtal-gateway` | packages/views/runtimes/components/provider-logo.tsx | 4 | Show managed gateway runtimes with the cloud provider icon |
| `agents-page-cerebro-extras` | packages/views/agents/components/agents-page.tsx | 131 | Agents page/tabs cerebro extras |
| `agents-tasks-tab` | packages/views/agents/components/tabs/tasks-tab.tsx | 35 | Agents page/tabs cerebro extras |
| `app-sidebar-cerebro` | packages/views/layout/app-sidebar.tsx | 142 | Layout cerebro additions |
| `artifact-cli-cmd-artifact` | server/cmd/multica/cmd_artifact.go | 445 | Artifact (documents/files) system |
| `artifact-handler` | server/internal/handler/artifact_upload.go<br>server/internal/handler/artifact.go<br>server/internal/handler/artifact_test.go<br>...+1 more | 1572 | Artifact (documents/files) system |
| `attachment-cli` | server/cmd/multica/cmd_attachment.go | 89 | Attachment CLI subcommand |
| `attachment-list-cerebro` | packages/views/issues/components/attachment-list.tsx | 119 | Attachment list view |
| `auth-handler-auth` | server/internal/handler/auth.go | 19 | Auth additions (master-code, JWT) |
| `auth-jwt` | server/internal/auth/jwt.go | 45 | Auth additions (master-code, JWT) |
| `auth-jwt-test` | server/internal/auth/jwt_test.go | 65 | Auth additions (master-code, JWT) |
| `auth-master-code-handler` | server/internal/handler/auth_master_code_test.go | 69 | Auth additions (master-code, JWT) |
| `autopilot-cerebro` | server/internal/handler/autopilot_cerebro.go | 130 | Scope visibility helpers (workspace/personal/group) for the upstream autopilot handler — JEH-724 |
| `autopilot-scope-create` | server/internal/handler/autopilot.go | 6 | Apply scope/owner_user_id/group_id after CreateAutopilot — JEH-724 |
| `autopilot-scope-create-req` | server/internal/handler/autopilot.go | 4 | Scope fields on CreateAutopilotRequest — JEH-724 |
| `autopilot-scope-delete` | server/internal/handler/autopilot.go | 4 | Edit-permission check on Delete — JEH-724 |
| `autopilot-scope-get` | server/internal/handler/autopilot.go | 4 | Visibility check on Get — JEH-724 |
| `autopilot-scope-import` | server/internal/handler/autopilot.go | 2 | Import access helper for scope checks — JEH-724 |
| `autopilot-scope-list` | server/internal/handler/autopilot.go | 5 | Scope-aware list filters by visibility — JEH-724 |
| `autopilot-scope-response` | server/internal/handler/autopilot.go | 5 | scope/owner_user_id/group_id on AutopilotResponse — JEH-724 |
| `autopilot-scope-response-fields` | server/internal/handler/autopilot.go | 4 | scope columns wired into autopilotToResponse — JEH-724 |
| `autopilot-scope-trigger` | server/internal/handler/autopilot.go | 4 | Trigger-permission check on TriggerAutopilot — JEH-724 |
| `autopilot-scope-update` | server/internal/handler/autopilot.go | 4 | Edit-permission check on Update — JEH-724 |
| `autopilots-autopilot-detail-page` | packages/views/autopilots/components/autopilot-detail-page.tsx | 1 | Autopilots cerebro additions |
| `batch-action-toolbar-cerebro` | packages/views/issues/components/batch-action-toolbar.tsx | 1 | Issues board/list view cerebro additions |
| `board-column-cerebro` | packages/views/issues/components/board-column.tsx | 4 | Issues board/list view cerebro additions |
| `board-view-cerebro` | packages/views/issues/components/board-view.tsx | 4 | Issues board/list view cerebro additions |
| `budget-handler` | server/internal/handler/budget_test.go<br>server/internal/handler/budget_preclaim_test.go | 371 | Budget/spending caps (cerebro feature) |
| `cerebro-inbox-*` | server/internal/cerebro/notifications/handler.go | 0 | Cerebro modification (see file for details) |
| `cerebro-inbox-actions` | packages/core/api/client.ts | 16 | API-client wrappers for the cerebro mute / unmute / mark-unread inbox endpoints (JEH-663) |
| `cerebro-inbox-fields` | server/internal/handler/inbox.go<br>server/internal/handler/notifications.go | 0 | Cerebro modification (see file for details) — adds Route, ProjectID, MutedUntil response fields |
| `cerebro-inbox-folders` | server/internal/handler/inbox.go | 0 | Cerebro modification (see file for details) |
| `cerebro-inbox-routes` | server/cmd/server/router.go | 0 | Mounts cerebro inbox routes (active-issue-tasks; mute / unmute / mark-unread) |
| `cerebro-listeners` | server/cmd/server/notification_listeners.go<br>server/cmd/server/notification_routing.go | 0 | Cerebro modification (see file for details) |
| `channel-listen-mode` | server/internal/handler/comment.go | 4 | JEH-699 — invoke cerebro channel listen-mode service so non-mentioned, non-assignee agents subscribed to a channel are triggered when their listen_mode is 'always' |
| `channel-listen-routes` | server/cmd/server/router.go | 3 | JEH-699 — channel listen-mode list/upsert HTTP routes |
| `router-channel-listen` | server/cmd/server/router.go | 5 | JEH-699 — wire cerebro channel-listen service into the upstream Handler so the comment trigger path can dispatch always-listening agents |
| `handler-channel-listen` | server/internal/handler/handler.go | 1 | JEH-699 — ChannelListen field on Handler struct |
| `handler-channel-listen-iface` | server/internal/handler/handler.go | 4 | JEH-699 — ChannelListenInvoker interface seam (avoids handler→cerebro import cycle) |
| `channel-listen-client` | packages/core/api/client.ts | 2 | JEH-699 — listChannelAgentSettings + setChannelAgentListenMode methods |
| `core-channels-index` | packages/core/channels/index.ts | 5 | JEH-699 — re-export listen-mode queries/mutations |
| `core-channels-listen-mut` | packages/core/channels/mutations.ts | 36 | JEH-699 — useSetChannelAgentListenMode optimistic mutation |
| `core-channels-listen-q` | packages/core/channels/queries.ts | 11 | JEH-699 — channelAgentSettingsOptions query factory |
| `core-types-channel-listen` | packages/core/types/channel.ts | 13 | JEH-699 — ChannelAgentListenMode + response/setting shapes |
| `core-types-index-channel-listen` | packages/core/types/index.ts | 3 | JEH-699 — re-export listen-mode types |
| `channel-agent-inline-row` | packages/views/channels/components/channel-detail.tsx | 2 | JEH-698 — mount `<ChannelAgentInlineRow />` (from `@multica/cerebro-channels`) between the comment stream and `CommentInput`. Component itself lives in cerebro-zone; this marker is for the import + JSX in upstream-zone `channel-detail.tsx` |
| `channel-detail-listeners` | packages/views/channels/components/channel-detail.tsx | 2 | JEH-699 — render Listeners popover in channel header |
| `channel-listeners-panel` | packages/views/channels/components/channel-listeners-panel.tsx | 3 | JEH-699 — net-new Listeners popover (Switch per agent) |
| `chat-handler-chat` | server/internal/handler/chat.go | 87 | Chat handler additions (cancel/coalesce/attachment) |
| `chat-handler-chat-attachment-test` | server/internal/handler/chat_attachment_test.go | 263 | Chat handler additions (cancel/coalesce/attachment) |
| `chat-handler-chat-cancel-test` | server/internal/handler/chat_cancel_test.go | 154 | Chat handler additions (cancel/coalesce/attachment) |
| `chat-handler-chat-coalesce-test` | server/internal/handler/chat_coalesce_test.go | 335 | Chat handler additions (cancel/coalesce/attachment) |
| `chat-handler-chat-complete-test` | server/internal/handler/chat_complete_test.go | 116 | JEH-720 — regression guard for the duplicate assistant chat_message bug in CompleteTask |
| `chat-collapse-dup-assistant` | server/internal/service/task.go | 6 | JEH-720 — collapse the post-tx assistant chat_message insert into the in-tx one so completing chat tasks no longer races with the WS broadcast |
| `chat-handler-chat-test` | server/internal/handler/chat_test.go | 247 | Chat handler additions (cancel/coalesce/attachment) |
| `chat-index-cerebro` | packages/views/chat/index.ts | 0 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-input-mcp-onboarding` | packages/views/chat/components/chat-input.tsx | 53 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-message-list-cerebro` | packages/views/chat/components/chat-message-list.tsx | 41 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-running-task-preserve` | packages/views/chat/components/chat-window.tsx | 7 | JEH-654 — preserve a still-running pending task instead of clobbering it with the queued successor's optimistic id; the successor surfaces via WS chat:done → invalidate → refetch once the running task finishes. Required after upstream presence-v4 (#1856) added an unconditional optimistic seed on top of the JEH-654 conditional seed |
| `chat-status-line` | packages/views/chat/components/chat-status-line.tsx | 93 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-tool-summary` | packages/views/chat/components/tool-summary.ts | 35 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-window-cerebro` | packages/views/chat/components/chat-window.tsx | 7 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `channels-components-participants-panel` | packages/views/channels/components/index.ts | 1 | JEH-700 — re-exports new ParticipantsPanel side-panel component |
| `channels-dm-gate-hint` | packages/views/channels/components/new-message-modal.tsx | 4 | JEH-700 — DM tab hint "DMs are 1-on-1 — create a channel for groups" so users see the constraint before submitting |
| `channels-index-rename-participants` | packages/core/channels/index.ts | 3 | JEH-700 — re-exports useUpdateChannel + useToggleChannelParticipant |
| `channels-mutations` | packages/core/channels/mutations.ts | 130 | JEH-700 — useUpdateChannel (rename) + useToggleChannelParticipant (subscribe/unsubscribe) with optimistic cache updates for channel detail/list |
| `channels-participants-panel` | packages/views/channels/components/participants-panel.tsx | 280 | JEH-700 — Sheet-based side panel listing channel participants with remove (confirm dialog) + add picker; reuses canAssignAgent + ActorAvatar |
| `channels-rename-participants` | packages/views/channels/components/channel-detail.tsx | 110 | JEH-700 — inline-editable channel title (kind='channel' only) + clickable participant stack opening the ParticipantsPanel sheet |
| `claude-stderr-tail-tests` | server/pkg/agent/claude_test.go | 0 | Claude stderr-tail diagnostic tests |
| `cli-attachments` | server/internal/cli/client.go | 0 | Cerebro modification (see file for details) |
| `comment-card-cerebro` | packages/views/issues/components/comment-card.tsx | 42 | Comment cerebro additions (replies, attachments, reactions) |
| `comment-handler` | server/internal/handler/comment.go | 7 | Comment cerebro additions (replies, attachments, reactions) |
| `comment-input-cerebro` | packages/views/issues/components/comment-input.tsx | 3 | Comment cerebro additions (replies, attachments, reactions) |
| `core-chat-queries` | packages/core/chat/queries.ts | 19 | Cerebro chat-archive/coalesce additions |
| `dashboard-nav` | packages/views/layout/app-sidebar.tsx | 4 | JEH-684 — sidebar entry for the cerebro dashboard at /:workspace/dashboard. Imports `<DashboardNavItem />` from `@multica/cerebro-dashboard` and renders it as the first item in the Workspace nav group. Gated on `cerebro_dashboard` feature flag. |
| `core-chat-store` | packages/core/chat/store.ts | 5 | Cerebro chat-archive/coalesce additions |
| `core-inbox-folders` | packages/core/inbox/folders.ts | 194 | Cerebro inbox-folder/archive additions |
| `core-inbox-index` | packages/core/inbox/index.ts | 1 | Cerebro inbox-folder/archive additions |
| `core-inbox-queries` | packages/core/inbox/queries.ts | 32 | Cerebro inbox-folder/archive additions |
| `core-inbox-ws-updaters` | packages/core/inbox/ws-updaters.ts | 17 | Cerebro inbox-folder/archive additions |
| `core-issues-view-store` | packages/core/issues/stores/view-store.ts | 6 | Cerebro modification (see file for details) |
| `core-package-cerebro-deps` | packages/core/package.json | 8 | Package.json cerebro export/dependency additions |
| `core-paths-cerebro` | packages/core/paths/paths.ts | 10 | Reserved-slug additions for cerebro routes |
| `core-profile-compile` | packages/core/profile/compile.ts | 185 | Cerebro profile system (settings persistence) |
| `core-profile-index` | packages/core/profile/index.ts | 6 | Cerebro profile system (settings persistence) |
| `core-profile-mutations` | packages/core/profile/mutations.ts | 22 | Cerebro profile system (settings persistence) |
| `core-profile-presets` | packages/core/profile/presets.ts | 118 | Cerebro profile system (settings persistence) |
| `core-profile-queries` | packages/core/profile/queries.ts | 16 | Cerebro profile system (settings persistence) |
| `core-profile-schema` | packages/core/profile/schema.ts | 94 | Cerebro profile system (settings persistence) |
| `core-profile-tokens` | packages/core/profile/tokens.ts | 33 | Cerebro profile system (settings persistence) |
| `core-projects-config` | packages/core/projects/config.ts | 17 | Cerebro project-config additions |
| `core-projects-index` | packages/core/projects/index.ts | 1 | Cerebro project-config additions |
| `core-reserved-slugs-cerebro` | packages/core/paths/reserved-slugs.ts | 1 | Reserved-slug additions for cerebro routes |
| `core-runtimes-mutations` | packages/core/runtimes/mutations.ts | 46 | Cerebro runtime mutations |
| `core-types-agent` | packages/core/types/agent.ts | 22 | Cerebro type augmentations |
| `core-types-api` | packages/core/types/api.ts | 35 | Cerebro type augmentations |
| `core-types-artifact` | packages/core/types/artifact.ts | 97 | Cerebro type augmentations |
| `core-types-events` | packages/core/types/events.ts | 4 | Cerebro type augmentations |
| `core-types-inbox` | packages/core/types/inbox.ts | 27 | Cerebro type augmentations |
| `core-types-index` | packages/core/types/index.ts | 27 | Cerebro type augmentations |
| `core-types-issue` | packages/core/types/issue.ts | 4 | Cerebro type augmentations |
| `core-types-pin` | packages/core/types/pin.ts | 1 | Cerebro type augmentations |
| `core-types-project` | packages/core/types/project.ts | 17 | Cerebro type augmentations |
| `core-types-workspace` | packages/core/types/workspace.ts | 10 | Cerebro type augmentations |
| `core-workspace-queries` | packages/core/workspace/queries.ts | 10 | Cerebro workspace queries |
| `create-project-modal-cerebro` | packages/views/modals/create-project.tsx | 22 | Create-project modal additions |
| `daemon-client` | server/internal/daemon/client.go | 6 | Daemon additions (sandbox/prompt/types) |
| `daemon-config` | server/internal/cerebro/runtime/config.go<br>server/internal/daemon/config.go | 0 | Daemon additions (sandbox/prompt/types) |
| `daemon-config-firtal-gateway` | server/internal/daemon/config.go | 66 | Register the managed gateway runtime from central URL/key/model environment variables |
| `daemon-config-test-firtal-gateway` | server/internal/daemon/config_test.go | 31 | Tests for explicit and inferred managed gateway runtime registration |
| `daemon-daemon` | server/internal/daemon/daemon.go | 31 | Daemon additions (sandbox/prompt/types) |
| `daemon-daemon-firtal-gateway-usage-cost` | server/internal/daemon/daemon.go | 11 | Include exact gateway spend when converting backend usage into task usage reports |
| `daemon-daemon-test-timing` | server/internal/daemon/daemon_test.go | 39 | Scheduler-tolerant polling assertion for cancellation watcher tests |
| `daemon-execenv` | server/internal/daemon/execenv/execenv.go | 1 | Daemon additions (sandbox/prompt/types) |
| `daemon-handler` | server/internal/handler/daemon.go | 270 | Daemon additions (sandbox/prompt/types) |
| `daemon-prompt` | server/internal/daemon/prompt.go | 17 | Daemon additions (sandbox/prompt/types) |
| `daemon-prompt-firtal-gateway-chat` | server/internal/daemon/prompt.go | 28 | Build explicit transcript prompts for stateless managed HTTP chat tasks |
| `daemon-prompt-test` | server/internal/daemon/prompt_test.go | 66 | Daemon additions (sandbox/prompt/types) |
| `daemon-runtime-config` | server/internal/daemon/execenv/runtime_config.go | 20 | Daemon additions (sandbox/prompt/types) |
| `daemon-sandbox` | server/internal/daemon/sandbox.go | 147 | Daemon additions (sandbox/prompt/types) |
| `daemon-sandbox-test` | server/internal/daemon/sandbox_test.go | 183 | Daemon additions (sandbox/prompt/types) |
| `daemon-types` | server/internal/daemon/types.go | 23 | Daemon additions (sandbox/prompt/types) |
| `daemon-types-firtal-gateway-usage-cost` | server/internal/daemon/types.go | 35 | Forward exact managed gateway spend in daemon task usage payloads |
| `dashboard-layout-cerebro` | packages/views/layout/dashboard-layout.tsx | 1 | Layout cerebro additions |
| `docs-index-cerebro` | packages/views/docs/index.ts | 1 | Docs page additions |
| `editor-css-cerebro` | packages/views/editor/content-editor.css | 12 | Editor additions |
| `editor-mobile-table-overflow` | packages/views/editor/content-editor.css | 3 | JEH-707 — wide markdown tables on mobile pushed the page past the viewport because the `.tableWrapper`'s `overflow-x: auto` only clips when the wrapper has a constrained width; `min-width: 0`/`max-width: 100%` on `.rich-text-editor` plus `max-width: 100%` on `.tableWrapper` keep the table scrolling inside its wrapper instead of widening every ancestor. |
| `editor-link-handler` | packages/views/editor/utils/link-handler.ts | 3 | Editor additions |
| `editor-preprocess` | packages/views/editor/utils/preprocess.ts | 8 | Editor additions |
| `enter-preference-section` | packages/views/settings/components/enter-preference-section.tsx | 73 | Settings page cerebro additions |
| `events-bus-cerebro` | server/internal/events/bus.go | 5 | Events-bus additions |
| `feature-flags-client` | packages/core/api/client.ts | 0 | Adds listFeatureFlags + setFeatureFlag to API client |
| `feature-flags-routes` | server/cmd/server/router.go | 0 | Mounts feature-flags routes |
| `feature-flags-storage` | packages/core/platform/storage-cleanup.ts | 0 | Cleans cerebro feature-flag storage on logout |
| `file-handler-file` | server/internal/handler/file.go | 117 | File handler additions |
| `handler-cerebro-routes` | server/internal/handler/handler.go | 37 | Server handler additions |
| `inbox-chat-panel` | packages/views/inbox/components/inbox-chat-panel.tsx | 298 | Inbox view additions |
| `inbox-folder-handler` | server/internal/handler/inbox_folder.go | 413 | Inbox-folder server handler (cerebro-only feature) |
| `inbox-keyboard-shortcuts` | packages/views/inbox/components/inbox-page.tsx | 4 | Mounts cerebro `e` = archive shortcut for the inbox page (JEH-663) |
| `inbox-list-item-cerebro` | packages/views/inbox/components/inbox-list-item.tsx | 17 | Inbox view additions |
| `inbox-row-actions-mount` | packages/views/inbox/components/inbox-list-item.tsx | 8 | Mounts the cerebro inbox row-actions surface (mute, mark-unread, hover menu, mobile swipe + long-press) on issue rows (JEH-663) |
| `inbox-mobile-detail-flex-height` | packages/views/inbox/components/inbox-page.tsx | 1 | Mobile inbox detail wrapper must be a flex column so embedded IssueDetail/ChannelDetail/InboxChatPanel get a defined height — overflow-y-auto block parent collapsed the body to zero height (JEH-697) |
| `inbox-page-stub` | packages/views/inbox/components/inbox-page.tsx | 0 | Routes to cerebro inbox-page when feature flag enabled |
| `input-autofocus` | packages/views/chat/components/chat-input.tsx<br>packages/views/issues/components/comment-input.tsx<br>packages/views/inbox/components/inbox-chat-panel.tsx<br>packages/views/channels/components/channel-detail.tsx | ~30 | Autofocus the message/chat compose input when entering a chat or selecting an agent so the user can start typing without an extra click (JEH-756). RAF defers past the agent-picker dropdown's close + focus restoration; an open `[role="dialog"]` keeps its focus (we don't yank). |
| `install-runtime-script` | server/internal/handler/install_runtime.sh<br>server/internal/handler/install_runtime_embed.go | 236 | Runtime-setup token + install script (cerebro feature) |
| `invite-page-cerebro` | packages/views/invite/invite-page.tsx | 3 | Invite-page additions |
| `issue-detail-cerebro-extras` | packages/views/issues/components/issue-detail.tsx | 129 | Issue-view cerebro additions |
| `issue-handler` | server/internal/handler/issue.go | 51 | Server handler additions |
| `issues-header-cerebro` | packages/views/issues/components/issues-header.tsx | 29 | Issue-view cerebro additions |
| `issues-page-cerebro` | packages/views/issues/components/issues-page.tsx | 29 | Issue-view cerebro additions |
| `kill-switch-section` | packages/views/settings/components/kill-switch-section.tsx | 139 | Settings page cerebro additions |
| `list-row-cerebro` | packages/views/issues/components/list-row.tsx | 87 | Issues board/list view cerebro additions |
| `list-view-cerebro` | packages/views/issues/components/list-view.tsx | 14 | Issues board/list view cerebro additions |
| `mcp-cli-cmd-mcp` | server/cmd/multica/cmd_mcp.go | 301 | Cerebro MCP CLI subcommand |
| `mcp-cli-cmd-mcp-install` | server/cmd/multica/cmd_mcp_install.go | 92 | Cerebro MCP CLI subcommand |
| `mcp-cli-cmd-mcp-install-test` | server/cmd/multica/cmd_mcp_install_test.go | 72 | Cerebro MCP CLI subcommand |
| `mcp-cli-cmd-mcp-integration-test` | server/cmd/multica/cmd_mcp_integration_test.go | 283 | Cerebro MCP CLI subcommand |
| `mcp-cli-cmd-mcp-test` | server/cmd/multica/cmd_mcp_test.go | 126 | Cerebro MCP CLI subcommand |
| `mcp-cli-cmd-mcp-tools` | server/cmd/multica/cmd_mcp_tools.go | 959 | Cerebro MCP CLI subcommand |
| `mcp-cli-cmd-mcp-tools-artifact` | server/cmd/multica/cmd_mcp_tools_artifact.go | 526 | Cerebro MCP CLI subcommand |
| `mcp-repo-config` | server/internal/mcp/repo_config.go | 132 | MCP server additions |
| `mcp-server` | server/internal/mcp/server.go | 212 | MCP server additions |
| `mcp-types` | server/internal/mcp/types.go | 93 | MCP server additions |
| `member-detail-handler` | server/internal/handler/member_detail.go | 156 | Member-detail handler (cerebro feature) |
| `members-tab-cerebro` | packages/views/settings/components/members-tab.tsx | 10 | Settings page cerebro additions |
| `middleware-auth` | server/internal/middleware/auth.go | 43 | Middleware augmentation |
| `middleware-scope` | server/internal/middleware/scope.go | 155 | Middleware augmentation |
| `middleware-scope-test` | server/internal/middleware/scope_test.go | 92 | Middleware augmentation |
| `middleware-setup-jwt-test` | server/internal/middleware/setup_jwt_test.go | 9 | Middleware augmentation |
| `migrate-idempotent-test` | server/cmd/migrate/idempotent_test.go | 152 | Migration idempotency test |
| `migration-idempotent-001-init` | server/migrations/001_init.up.sql | 23 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-002-agent-config` | server/migrations/002_agent_config.up.sql | 4 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-003-task-context` | server/migrations/003_task_context.up.sql | 8 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-004-agent-runtime-loop` | server/migrations/004_agent_runtime_loop.up.sql | 33 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-005-daemon-pairing` | server/migrations/005_daemon_pairing.up.sql | 3 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-006-workspace-context` | server/migrations/006_workspace_context.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-008-structured-skills` | server/migrations/008_structured_skills.up.sql | 7 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-009-verification-code` | server/migrations/009_verification_code.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-010-verification-code-attempts` | server/migrations/010_verification_code_attempts.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-011-personal-access-tokens` | server/migrations/011_personal_access_tokens.up.sql | 3 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-013-runtime-usage` | server/migrations/013_runtime_usage.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-014-workspace-repos` | server/migrations/014_workspace_repos.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-015-issue-subscriber` | server/migrations/015_issue_subscriber.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-017-comment-parent-id` | server/migrations/017_comment_parent_id.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-020-issue-number` | server/migrations/020_issue_number.up.sql | 17 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-020-task-session` | server/migrations/020_task_session.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-021-agent-instructions` | server/migrations/021_agent_instructions.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-022-task-lifecycle-guards` | server/migrations/022_task_lifecycle_guards.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-025-comment-workspace-id` | server/migrations/025_comment_workspace_id.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-026-comment-reactions` | server/migrations/026_comment_reactions.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-026-task-messages` | server/migrations/026_task_messages.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-027-issue-reactions` | server/migrations/027_issue_reactions.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-028-task-trigger-comment` | server/migrations/028_task_trigger_comment.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-029-attachment` | server/migrations/029_attachment.up.sql | 4 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-029-daemon-token` | server/migrations/029_daemon_token.up.sql | 3 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-031-agent-archive` | server/migrations/031_agent_archive.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-032-issue-search-index` | server/migrations/032_issue_search_index.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-032-runtime-owner` | server/migrations/032_runtime_owner.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-032-task-usage` | server/migrations/032_task_usage.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-033-chat` | server/migrations/033_chat.up.sql | 6 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-033-comment-search-index` | server/migrations/033_comment_search_index.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-034-projects` | server/migrations/034_projects.up.sql | 4 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-035-project-priority` | server/migrations/035_project_priority.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-036-search-index-lower` | server/migrations/036_search_index_lower.up.sql | 3 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-037-fix-pending-task-unique-index` | server/migrations/037_fix_pending_task_unique_index.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-038-pinned-items` | server/migrations/038_pinned_items.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-039-project-search-index` | server/migrations/039_project_search_index.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-040-agent-custom-env` | server/migrations/040_agent_custom_env.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-040-chat-unread-since` | server/migrations/040_chat_unread_since.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-041-agent-custom-args` | server/migrations/041_agent_custom_args.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-041-workspace-invitation` | server/migrations/041_workspace_invitation.up.sql | 4 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-046-agent-mcp-config` | server/migrations/046_agent_mcp_config.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-046-agent-unique-name` | server/migrations/046_agent_unique_name.up.sql | 6 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-048-runtime-daemon-uuid` | server/migrations/048_runtime_daemon_uuid.up.sql | 1 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-049-work-session` | server/migrations/049_work_session.up.sql<br>server/migrations/049_work_session.down.sql | 33 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-050-project-color` | server/migrations/050_project_color.down.sql<br>server/migrations/050_project_color.up.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-051-work-session-name` | server/migrations/051_work_session_name.down.sql<br>server/migrations/051_work_session_name.up.sql | 4 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-052-project-repo` | server/migrations/052_project_repo.up.sql<br>server/migrations/052_project_repo.down.sql | 2 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-053-inbox-folders` | server/migrations/053_inbox_folders.up.sql<br>server/migrations/053_inbox_folders.down.sql | 31 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-054-inbox-folder-parent` | server/migrations/054_inbox_folder_parent.down.sql<br>server/migrations/054_inbox_folder_parent.up.sql | 10 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-055-user-profile` | server/migrations/055_user_profile.down.sql<br>server/migrations/055_user_profile.up.sql | 26 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-056-attachment-chat-message` | server/migrations/056_attachment_chat_message.down.sql<br>server/migrations/056_attachment_chat_message.up.sql | 9 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-057-chat-task-coalesce` | server/migrations/057_chat_task_coalesce.down.sql<br>server/migrations/057_chat_task_coalesce.up.sql | 11 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-058-budget-caps` | server/migrations/058_budget_caps.up.sql<br>server/migrations/058_budget_caps.down.sql | 99 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-059-task-tokens` | server/migrations/059_task_tokens.down.sql<br>server/migrations/059_task_tokens.up.sql | 20 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `migration-idempotent-060-runtime-sandbox-enabled` | server/migrations/060_runtime_sandbox_enabled.down.sql<br>server/migrations/060_runtime_sandbox_enabled.up.sql | 8 | Adds `IF NOT EXISTS` / `IF EXISTS` clauses for idempotent migration replay |
| `multica-cli-cerebro-cmds` | server/cmd/multica/main.go | 3 | Cerebro CLI command wiring |
| `new-workspace-page-cerebro` | packages/views/workspace/new-workspace-page.tsx | 3 | New-workspace page cerebro additions |
| `notification-routing-test-cerebro` | server/cmd/server/notification_routing_test.go | 520 | Server cmd additions |
| `notify-all-mobile-inbox` | server/cmd/server/notification_routing.go<br>server/cmd/server/notification_routing_test.go | 8 | JEH-737 master toggle: when `preferences.notifications.notify_all_mobile_inbox` is true, mobile channel resolution mirrors the inbox channel for the same key so every inbox item also fires a Web Push without curating the per-key matrix. |
| `push-deep-link` | server/cmd/server/notification_listeners.go<br>server/cmd/server/notification_routing_test.go | 30 | Web Push payload `URL` now includes the workspace slug (`/<slug>/inbox?issue=<id>`) so tapping the notification deep-links to the inbox row that triggered it; old `/?issue=<id>` lost the query through the landing-page redirect. JEH-737. |
| `notifications-handler` | server/internal/handler/notifications.go | 134 | Notification handler additions |
| `orphan-task-test` | server/internal/handler/daemon_test.go | 0 | Cerebro orphan-task test additions |
| `page-header-cerebro` | packages/views/layout/page-header.tsx | 1 | Layout cerebro additions |
| `pricing-pricing` | server/pkg/pricing/pricing.go | 108 | Pricing additions (token cost calc) |
| `pricing-pricing-test` | server/pkg/pricing/pricing_test.go | 90 | Pricing additions (token cost calc) |
| `privacy-toggle` | packages/views/issues/components/privacy-toggle.tsx | 59 | Privacy/restricted-access UI primitives |
| `profile-compile` | server/internal/profile/compile.go<br>server/internal/profile/compile_test.go | 372 | Profile-compile server logic |
| `project-access-handler` | server/internal/handler/project_access_test.go<br>server/internal/handler/project_access.go | 562 | Project-access + privacy enforcement (cerebro feature) |
| `project-access-tab` | packages/views/projects/components/project-access-tab.tsx | 552 | Project-access + privacy enforcement (cerebro feature) |
| `project-detail-access` | packages/views/projects/components/project-detail.tsx | 0 | Access-tab integration in project-detail |
| `project-detail-tabs` | packages/views/projects/components/project-detail.tsx | 0 | Project-detail tab/access additions |
| `project-handler` | server/internal/handler/project.go | 83 | Server handler additions |
| `projects-page-cerebro` | packages/views/projects/components/projects-page.tsx | 19 | Projects page/access-tab cerebro additions |
| `protocol-events` | server/pkg/protocol/events.go | 7 | Protocol event additions |
| `readonly-content-cerebro` | packages/views/editor/readonly-content.tsx | 93 | Readonly-content editor additions |
| `realtime-events` | server/internal/realtime/hub.go | 0 | Realtime additions |
| `realtime-handlers` | packages/core/realtime/use-realtime-sync.ts | 0 | Wires cerebro realtime handlers |
| `realtime-setup-jwt-test` | server/internal/realtime/setup_jwt_test.go | 9 | Realtime additions |
| `reply-input-cerebro` | packages/views/issues/components/reply-input.tsx | 7 | Reply-input cerebro additions |
| `restricted-lock` | packages/views/common/restricted-lock.tsx | 37 | Privacy/restricted-access UI primitives |
| `restricted-ref` | packages/views/common/restricted-ref.tsx | 56 | Privacy/restricted-access UI primitives |
| `runtime-detail` | packages/views/runtimes/components/runtime-detail.tsx | 0 | Cerebro additions to runtime-detail |
| `runtime-handler-runtime` | server/internal/handler/runtime.go | 114 | Runtime handler additions |
| `runtime-handler-runtime-test` | server/internal/handler/runtime_test.go | 211 | Runtime handler additions |
| `runtime-list-cerebro` | packages/views/runtimes/components/runtime-list.tsx | 8 | Runtime view cerebro additions |
| `runtime-setup-handler` | server/internal/handler/runtime_setup.go | 224 | Runtime-setup token + install script (cerebro feature) |
| `runtime-setup-page` | packages/views/docs/runtime-setup-page.tsx | 182 | Runtime-setup token + install script (cerebro feature) |
| `runtimes-utils-cerebro` | packages/views/runtimes/utils.ts | 4 | Runtime view cerebro additions |
| `server-integration-test-cerebro` | server/cmd/server/integration_test.go | 157 | Server bootstrapping additions |
| `server-listeners-cerebro` | server/cmd/server/listeners.go | 9 | Server bootstrapping additions |
| `server-main-cerebro` | server/cmd/server/main.go | 6 | Server bootstrapping additions |
| `server-setup-jwt-test` | server/cmd/server/setup_jwt_test.go | 9 | Server bootstrapping additions |
| `service-budget` | server/internal/service/budget.go | 125 | Service-layer additions (budget/task/workspace-pause) |
| `service-task-cerebro` | server/internal/service/task.go | 182 | Service-layer additions (budget/task/workspace-pause) |
| `service-workspace-pause` | server/internal/service/workspace_pause.go | 53 | Service-layer additions (budget/task/workspace-pause) |
| `settings-page-cerebro` | packages/views/settings/components/settings-page.tsx | 119 | Settings page cerebro additions |
| `setup-jwt-handler` | server/internal/handler/setup_jwt_test.go | 12 | JWT setup helper additions |
| `sqlc-agent` | server/pkg/db/queries/agent.sql | 19 | Cerebro sqlc query additions |
| `sqlc-agent-task-title` | server/pkg/db/queries/agent.sql | 3 | JEH-698 — `title` column on agent_task_queue + INSERT param. Curated short display label generated at enqueue time, distinct from `trigger_summary` (verbatim provenance) |
| `sqlc-artifact` | server/pkg/db/queries/artifact.sql | 114 | Cerebro sqlc query additions |
| `sqlc-artifact-folder` | server/pkg/db/queries/artifact_folder.sql | 36 | Cerebro sqlc query additions |
| `sqlc-attachment` | server/pkg/db/queries/attachment.sql | 12 | Cerebro sqlc query additions |
| `sqlc-autopilot-scope` | server/pkg/db/queries/autopilot.sql | 30 | Scope-aware ListAutopilotsForUser + SetAutopilotScope (JEH-724) |
| `sqlc-budget` | server/pkg/db/queries/budget.sql | 42 | Cerebro sqlc query additions |
| `sqlc-cerebro-package` | server/sqlc.yaml | 0 | Adds cerebro sqlc package config |
| `sqlc-chat` | server/pkg/db/queries/chat.sql | 41 | Cerebro sqlc query additions |
| `sqlc-inbox` | server/pkg/db/queries/inbox.sql | 30 | Cerebro sqlc query additions |
| `sqlc-inbox-folder` | server/pkg/db/queries/inbox_folder.sql | 175 | Cerebro sqlc query additions |
| `sqlc-issue` | server/pkg/db/queries/issue.sql | 40 | Cerebro sqlc query additions |
| `sqlc-member` | server/pkg/db/queries/member.sql | 21 | Cerebro sqlc query additions |
| `sqlc-project` | server/pkg/db/queries/project.sql | 32 | Cerebro sqlc query additions |
| `sqlc-project-member` | server/pkg/db/queries/project_member.sql | 36 | Cerebro sqlc query additions |
| `sqlc-runtime` | server/pkg/db/queries/runtime.sql | 8 | Cerebro sqlc query additions |
| `sqlc-runtime-setup-token` | server/pkg/db/queries/runtime_setup_token.sql | 26 | Cerebro sqlc query additions |
| `sqlc-task-token` | server/pkg/db/queries/task_token.sql | 18 | Cerebro sqlc query additions |
| `sqlc-user-preferences` | server/pkg/db/queries/user_preferences.sql | 14 | Cerebro sqlc query additions |
| `sqlc-user-profile` | server/pkg/db/queries/user_profile.sql | 20 | Cerebro sqlc query additions |
| `sqlc-work-session` | server/pkg/db/queries/work_session.sql | 60 | Cerebro sqlc query additions |
| `sqlc-workspace-pause` | server/pkg/db/queries/workspace_pause.sql | 38 | Cerebro sqlc query additions |
| `subscriber-listeners` | server/cmd/server/subscriber_listeners_test.go<br>server/cmd/server/subscriber_listeners.go | 315 | Subscriber-listener additions |
| `task-title-builder` | server/internal/service/task.go<br>server/internal/handler/agent.go<br>packages/core/types/agent.ts<br>packages/views/issues/components/agent-live-card.tsx<br>packages/views/issues/components/execution-log-section.tsx<br>packages/views/agents/components/tabs/activity-tab.tsx | 22 | JEH-698 — wires the cerebro `agent_title` package into the upstream task-enqueue path and surfaces the generated title in AgentLiveCard, the Tasks list, and the execution log. Title-builder lives in `server/internal/cerebro/agent_title/`; these markers cover the upstream-zone integration sites |
| `task-token-handler` | server/internal/handler/task_token_test.go | 164 | Task-token API |
| `ui-button-cerebro` | packages/ui/components/ui/button.tsx | 5 | Cerebro UI primitive additions |
| `ui-hook-use-sticky-bottom` | packages/ui/hooks/use-sticky-bottom.ts | 133 | Cerebro UI primitive additions |
| `ui-jump-to-latest-button` | packages/ui/components/common/jump-to-latest-button.tsx | 44 | Cerebro UI primitive additions |
| `ui-markdown-file-cards` | packages/ui/markdown/file-cards.ts | 20 | Cerebro UI primitive additions |
| `ui-submit-button` | packages/ui/components/common/submit-button.tsx | 3 | Cerebro UI primitive additions |
| `use-submit-on-enter` | packages/views/preferences/use-submit-on-enter.ts | 18 | Submit-on-enter preference hook |
| `user-preferences-handler` | server/internal/handler/user_preferences.go<br>server/internal/handler/user_preferences_test.go | 252 | User preferences API |
| `user-profile-handler` | server/internal/handler/user_profile.go | 193 | User profile API (cerebro feature) |
| `util-pgx-cerebro` | server/internal/util/pgx.go | 14 | PGX util additions |
| `vapid-mailto-fix` | server/internal/service/push.go<br>server/internal/service/push_test.go | 4 | Strip `mailto:` prefix from `VAPID_SUBJECT` so webpush-go's auto-prepend doesn't yield `mailto:mailto:...` (Apple 403 BadJwtToken). JEH-563. Should be upstreamed. |
| `views-package-cerebro-deps` | packages/views/package.json | 7 | Package.json cerebro export/dependency additions |
| `work-session-handler` | server/internal/handler/work_session.go | 364 | Work-session API (cerebro feature) |
| `workspace-handler-cerebro` | server/internal/handler/workspace.go | 27 | Server handler additions |
| `workspace-pause-handler` | server/internal/handler/workspace_pause.go<br>server/internal/handler/workspace_pause_test.go | 379 | Workspace-pause (cerebro feature) |
| `workspace-reserved-slugs-cerebro` | server/internal/handler/workspace_reserved_slugs.go | 1 | Server handler additions |
| `workspace-tab-cerebro` | packages/views/settings/components/workspace-tab.tsx | 8 | Settings page cerebro additions |
