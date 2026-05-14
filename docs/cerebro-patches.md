# CEREBRO Patch Registry

Permanent inline modifications and fork-additions in upstream-zone files. Each entry
documents one named patch + its rationale + the file location(s).

**Marker format:** `// CEREBRO-PATCH(<name>): <description>` (or language-appropriate
comment for SQL/CSS/SBPL/JSON-with-_comment-field).

## Summary

- **Unique patch names:** 294 (273 baseline + 15 PR #118 markers + 6 JEH-725 markers)
- **Files marked (including chunks 4-8 pre-existing markers):** 302
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
| `agent-runs-history-expanded-default` | packages/views/issues/components/agent-live-card.tsx | 1 | Initialize `TaskRunHistory` open state to `true` so the "Execution history" section is expanded by default when the Agent Runs tab is opened (JEH-1247). |
| `agent-tools-api` | packages/core/api/client.ts | 10 | JEH-1290 W8 — `getAgentTools` / `updateAgentTool` wrappers for the W3 registry endpoints (`GET/PUT /api/agents/:id/tools/:name`). Enables the tools tab + toggle. |
| `agent-tools-tab` | packages/views/agents/components/agent-overview-pane.tsx<br>packages/views/agents/components/tabs/cerebro-tools-tab.tsx (new) | 8 inline + ~220 new | JEH-1290 W8 — "Tools" tab on the agent editor. Fetches tool grants via W3 API, renders name/description/enabled toggle, tool-specific config fields (BQ row-limit, Sheets spreadsheet-id, web-fetch URL-allowlist). Gracefully empty before W3 is deployed. |
| `workspace-tool-credentials` | packages/views/settings/components/workspace-tab.tsx | 50 | JEH-1290 W8 — "Tool Credentials" section in workspace settings. Google Service Account JSON (Sheets) stored in `workspace.settings.tool_credentials`. FDR Gateway Key (BQ) references the existing gateway config with a status indicator. Redacted-on-read pattern matches gateway section. |
| `fix-paste-image-selection` | packages/views/editor/extensions/file-upload.ts | 1 | Call event.preventDefault() before handleFiles so pasted images are never inserted inline into the editor. Without this the browser inserts the image as an inline node and then selects it, forcing users to click before typing (JEH-1194). |
| `runtime-pause-cerebro` | server/internal/handler/runtime_pause_cerebro.go | 158 | Net-new fork file: HTTP handlers for cerebro pause/unpause. Delegates to cerebroruntime.Service via the RuntimePauseInvoker seam (JEH-848). |
| `runtime-detail-pause` | packages/views/runtimes/components/runtime-detail.tsx | 4 | Mount PauseRuntimeButton in topbar and PauseBanner above HeroCard from cerebro-runtime. |
| `api-client-runtime-pause` | packages/core/api/client.ts | 8 | pauseRuntime / unpauseRuntime methods so cerebro-runtime mutations can call them through the central client. |
| `runtime-pause-response` | server/internal/handler/runtime.go | 6 | Surface paused_at / unpause_at / pause_reason on AgentRuntimeResponse so the UI can render pause state. |
| `handler-runtime-pause` | server/internal/handler/handler.go | 2 | RuntimePause field on Handler; assigned by router so runtime_pause_cerebro.go can delegate. |
| `handler-runtime-pause-iface` | server/internal/handler/handler.go | 22 | RuntimePauseInvoker / RuntimePauseOptions / RuntimePauseState seam — lets the upstream Handler call into the cerebro pause service without an import cycle. |
| `router-runtime-pause` | server/cmd/server/router.go | 6 | Mount cerebroruntime.New on h.RuntimePause and wire POST /pause and POST /unpause. |
| `main-runtime-pause-sweeper` | server/cmd/server/main.go | 2 | Goroutine for the auto-unpause sweeper (30s tick, runs alongside the upstream runtime sweeper). |
| `main-workflows-engine` | server/cmd/server/main.go | 5 | JEH-1047 — import + bootstrap for the cerebro workflow engine: builds `cerebroworkflows.Service`, attaches its listener to the event bus, and runs the retry sweeper alongside the existing cerebro sweepers. Gated at the service layer by `CEREBRO_WORKFLOWS_ENABLED` so the wiring is always present but silent until the env var is on. |
| `cerebro-workflows-cron-sweeper` | server/cmd/server/main.go | 2 | JEH-1108 PR 2 — goroutine for the cron sweeper (1-minute tick); runs alongside retry sweeper. Gated per-tick on `CEREBRO_WORKFLOWS_ENABLED`. |
| `cerebro-credentials-routes` | server/cmd/server/router.go | 4 | JEH-1196/1197 — import + handler instance + a single `cerebroCredentialsHandler.Mount(r)` call under the workspace-scoped group. JEH-1197 extends the handler line with `.WithPolicy(newCredentialsPolicy(queries))`; the new factory lives in the cerebro zone (`server/cmd/server/cerebro_credentials_policy.go`). New routes are added inside the cerebro `credentials` package's `Mount` method, not on router.go. |
| `cerebro-credentials-policy` | server/cmd/server/cerebro_credentials_policy.go | 130 | JEH-1197 — net-new cerebro-only file: builds the production `PolicyChecker` for the credential registry (Owner + Persona layers, deny-by-default). Lives under `server/cmd/server/` alongside `cerebro_persona_mask.go` so the router's `cerebro-credentials-routes` patch stays a single line. |
| `cerebro-workflows-routes` | server/cmd/server/router.go | 14 | JEH-1047 — import + handler instance + 7 REST endpoints under `/api/cerebro/workflows` (list / get / create / update / delete / toggle / runs). JEH-1108 PR 3 also injects the engine Service into the handler so the test-only cron-sweep endpoint can fire the sweeper synchronously. Same pattern as `cerebro-tasks-route`. |
| `cerebro-workflows-regenerate-token` | server/cmd/server/router.go | 4 | JEH-1108 PR 2 — three regenerate endpoints under `/api/cerebro/workflows/{id}/regenerate-*` (inbound token, inbound signing secret, outbound secret). JEH-1108 PR 3 adds the env-gated `_test/cron-sweep` debug endpoint. Each rotate rotates one column and returns the value exactly once; subsequent GETs mask. |
| `cerebro-workflows-webhook-ingress` | server/cmd/server/router.go | 5 | JEH-1108 PR 2 — public POST `/api/cerebro/workflows/webhook/{token}` mounted outside auth scope (token + HMAC are the auth surface). Wired only when `opts.WorkflowService != nil` so tests that omit the engine don't break. |
| `cerebro-workflows-client` | packages/core/api/client.ts | 60 | JEH-1047 — typed wrappers for the `/api/cerebro/workflows` endpoints, consumed by the cerebro-workflows package's TanStack queries. Same pattern as `cerebro-tasks-client`. JEH-1108 PR 3 extends the wrapper with the three phase-3 regenerate endpoints (`regenerate-token`, `regenerate-signing-secret`, `regenerate-outbound-secret`). |
| `daemon-pause-claim-gate` | server/internal/handler/daemon.go | 9 | Paused runtimes return no claimable task without touching Postgres. Pure orchestration gate — daemon stays unchanged. |
| `auto-pause-invoker` | server/internal/service/task.go | 8 | JEH-1035 — `AutoPauseInvoker` seam + `AutoPause` field on `TaskService`. The concrete invoker lives in `server/internal/cerebro/runtime/auto_pause.go`; the interface keeps the upstream package import-cycle-free. |
| `auto-pause-on-failure` | server/internal/service/task.go | 5 | JEH-1035 — `FailTask` calls `AutoPause.MaybeAutoPauseOnFailure` after the auto-retry decision so a runtime-side rate-limit / monthly-cap / expired-token signal pauses the runtime instead of burning through max_attempts. |
| `router-auto-pause-on-failure` | server/cmd/server/router.go | 3 | JEH-1035 — wires the cerebro pause Service into `h.TaskService.AutoPause` immediately after `h.RuntimePause` is constructed. |
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
| `firtal-gateway-model-discovery` | server/internal/handler/runtime_models.go<br>server/pkg/agent/firtal_gateway.go<br>server/internal/cerebro/firtalgateway/model_discovery.go (new)<br>server/internal/cerebro/firtalgateway/model_discovery_test.go (new) | 14 inline + 171 new | JEH-1356 — small handler hook routes server-owned Firtal Gateway cloud runtimes to Cerebro-zone model discovery, using workspace gateway settings instead of daemon heartbeat pickup. |
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
| `autopilot-recovery-preflight` | server/internal/service/autopilot.go<br>server/internal/cerebro/autopilotutil/template.go<br>server/internal/cerebro/issue_recovery/preflight.go | 9 (hook) + 364 (cerebro zone) | JEH-822 — generate deterministic issue-recovery worklists from platform data before agent runs and support Firtal autopilot title tokens (`{{.Date}}`, `{{.ISOWeek}}`) |
| `autopilot-scope-cli` | server/cmd/multica/cerebro_autopilot_scope.go | ~330 | CLI flags, validation, and owner/group resolution for autopilot scopes — JEH-725 |
| `autopilot-scope-cli-create-body` | server/cmd/multica/cmd_autopilot.go | 2 | Add scope/owner_user_id/group_id to autopilot create request bodies — JEH-725 |
| `autopilot-scope-cli-list-query` | server/cmd/multica/cmd_autopilot.go | 2 | Add optional `scope` query parameter for autopilot list — JEH-725 |
| `autopilot-scope-cli-test` | server/cmd/multica/cerebro_autopilot_scope_test.go | ~270 | Unit coverage for autopilot scope CLI helper behavior — JEH-725 |
| `autopilot-scope-cli-test-snapshot` | server/cmd/multica/cmd_autopilot_test.go | 2 | Assert create/list/update expose the new scope flags — JEH-725 |
| `autopilot-scope-cli-update-body` | server/cmd/multica/cmd_autopilot.go | 2 | Add scope/owner_user_id/group_id to autopilot update request bodies — JEH-725 |
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
| `autopilot-detail-header-shrink` | packages/views/autopilots/components/autopilot-detail-page.tsx | 4 | JEH-1144 — flex-1 + min-w-0 on the breadcrumb wrapper and shrink-0 on the status toggle + action buttons so they keep natural width when the header has to scroll horizontally on mobile |
| `batch-action-toolbar-cerebro` | packages/views/issues/components/batch-action-toolbar.tsx | 1 | Issues board/list view cerebro additions |
| `board-column-cerebro` | packages/views/issues/components/board-column.tsx | 4 | Issues board/list view cerebro additions |
| `board-view-cerebro` | packages/views/issues/components/board-view.tsx | 4 | Issues board/list view cerebro additions |
| `budget-handler` | server/internal/handler/budget_test.go<br>server/internal/handler/budget_preclaim_test.go | 371 | Budget/spending caps (cerebro feature) |
| `cerebro-inbox-*` | server/internal/cerebro/notifications/handler.go | 0 | Cerebro modification (see file for details) |
| `cerebro-inbox-actions` | packages/core/api/client.ts | 16 | API-client wrappers for the cerebro mute / unmute / mark-unread inbox endpoints (JEH-663) |
| `cerebro-inbox-fields` | server/internal/handler/inbox.go<br>server/internal/handler/notifications.go | 0 | Cerebro modification (see file for details) — adds Route, ProjectID, MutedUntil response fields |
| `cerebro-inbox-folders` | server/internal/handler/inbox.go | 0 | Cerebro modification (see file for details) |
| `cerebro-inbox-routes` | server/cmd/server/router.go | 0 | Mounts cerebro inbox routes (active-issue-tasks; mute / unmute / mark-unread) |
| `cerebro-inbox-unarchive` | server/cmd/server/router.go<br>packages/core/api/client.ts | 2 + 4 | JEH-1166 — unarchive action surface: POST /api/inbox/{id}/unarchive route + `api.unarchiveInbox` client method. Mirrors archive, lives behind a cerebro handler so upstream merges stay clean. |
| `inbox-unarchive-mount` | packages/views/inbox/components/inbox-list-item.tsx<br>packages/views/inbox/components/inbox-page.tsx | 4 + 5 | JEH-1166 — wires the unarchive action through the upstream inbox list. Adds `onUnarchive` prop to InboxListItemShell / InboxListItem / ChannelListItem; archived view passes a real callback that the cerebro row-actions surface uses to swap the archive icon + swipe-right gesture for an unarchive variant. |
| `cerebro-listeners` | server/cmd/server/notification_listeners.go<br>server/cmd/server/notification_routing.go | 0 | Cerebro modification (see file for details) |
| `channel-listen-mode` | server/internal/handler/comment.go | 4 | JEH-699 — invoke cerebro channel listen-mode service so non-mentioned, non-assignee agents subscribed to a channel are triggered when their listen_mode is 'always' |
| `channel-listen-routes` | server/cmd/server/router.go | 3 | JEH-699 — channel listen-mode list/upsert HTTP routes |
| `channel-archive-routes` | server/cmd/server/router.go | 4 | JEH-855/912 — per-(channel, user) archive HTTP routes (POST/DELETE /api/channels/{id}/archive) |
| `router-channel-listen` | server/cmd/server/router.go | 5 | JEH-699 — wire cerebro channel-listen service into the upstream Handler so the comment trigger path can dispatch always-listening agents |
| `handler-channel-listen` | server/internal/handler/handler.go | 1 | JEH-699 — ChannelListen field on Handler struct |
| `handler-channel-listen-iface` | server/internal/handler/handler.go | 4 | JEH-699 — ChannelListenInvoker interface seam (avoids handler→cerebro import cycle) |
| `channel-listen-client` | packages/core/api/client.ts | 2 | JEH-699 — listChannelAgentSettings + setChannelAgentListenMode methods |
| `core-channels-index` | packages/core/channels/index.ts | 5 | JEH-699 — re-export listen-mode queries/mutations |
| `core-channels-listen-mut` | packages/core/channels/mutations.ts | 36 | JEH-699 — useSetChannelAgentListenMode optimistic mutation |
| `core-channels-listen-q` | packages/core/channels/queries.ts | 11 | JEH-699 — channelAgentSettingsOptions query factory |
| `core-types-channel-listen` | packages/core/types/channel.ts | 13 | JEH-699 — ChannelAgentListenMode + response/setting shapes |
| `core-types-index-channel-listen` | packages/core/types/index.ts | 3 | JEH-699 — re-export listen-mode types |
| `core-permissions-current-member` | packages/core/permissions/index.ts | 2 | JEH-1199/JEH-1217 — re-export `useCurrentMember` so cerebro admin surfaces (cerebro-credentials, cerebro-permissions) can read the viewer's workspace role without duplicating the lookup. |
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
| `chat-input-expand` | packages/views/chat/components/chat-input.tsx<br>packages/views/locales/en/chat.json<br>packages/views/locales/zh-Hans/chat.json | ~30 | JEH-887 — expand toggle on the chat input (Maximize2/Minimize2), mirroring the issue comment-input button. Lets the user grow a small chat compose field to 70vh, especially valuable on mobile where the keyboard halves the screen. |
| `chat-message-list-cerebro` | packages/views/chat/components/chat-message-list.tsx | 41 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-running-task-preserve` | packages/views/chat/components/chat-window.tsx | 7 | JEH-654 — preserve a still-running pending task instead of clobbering it with the queued successor's optimistic id; the successor surfaces via WS chat:done → invalidate → refetch once the running task finishes. Required after upstream presence-v4 (#1856) added an unconditional optimistic seed on top of the JEH-654 conditional seed |
| `chat-session-scoped-agent` | packages/views/chat/components/chat-window.tsx<br>packages/views/inbox/components/inbox-chat-panel.tsx | 4 | JEH-806 — existing chat sessions derive the displayed/active agent from the session's `agent_id` instead of the workspace-global selected agent, preventing agent selector leakage between conversations |
| `chat-session-scoped-draft` | packages/views/chat/components/chat-input.tsx | 3 | JEH-806 — ChatInput accepts an explicit draft session id so embedded chat panels do not read or write drafts against the floating chat's global active session |
| `chat-status-line` | packages/views/chat/components/chat-status-line.tsx | 93 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-tool-summary` | packages/views/chat/components/tool-summary.ts | 35 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-window-cerebro` | packages/views/chat/components/chat-window.tsx | 7 | Chat view cerebro additions (MCP onboarding, status, archive) |
| `chat-actions-cerebro` | server/internal/handler/chat_actions_cerebro.go | ~210 | JEH-799 — net-new fork file: handlers for chat-session header actions (PATCH /api/chat/sessions/{id} for rename/archive, POST /convert-to-issue for issue conversion) |
| `chat-session-actions-routes` | server/cmd/server/router.go | 2 | JEH-799 — register PATCH and convert-to-issue routes for the chat-session header |
| `chat-search-cerebro` | server/internal/handler/chat_search_cerebro.go | ~240 | JEH-901 — net-new fork file: SearchChatSessions endpoint backing Cmd+K, mirrors SearchIssues / SearchProjects (LIKE on LOWER(content) + pg_bigm trigram) |
| `chat-search-route` | server/cmd/server/router.go | 5 | JEH-901 — register GET /api/chat/sessions/search before the {sessionId} subtree (chi greedy-routing trap) |
| `chat-session-updated-event` | server/pkg/protocol/events.go<br>server/pkg/protocol/messages.go<br>packages/core/types/events.ts | 1 + 7 + 2 | JEH-799 — new `chat:session_updated` WS event so other tabs sync title/status changes from the session header |
| `api-chat-session-actions` | packages/core/api/client.ts | 18 | JEH-799 — chat-session API methods: `updateChatSession`, `convertChatSessionToIssue` |
| `chat-session-header` | packages/views/chat/components/chat-window.tsx | 2 | JEH-799 — mount `<ChatSessionHeader />` (from `@multica/cerebro-chat`) above the message list |
| `channel-favorites-storage` | packages/core/platform/storage-cleanup.ts | 1 | JEH-718 — registers `multica_channel_favorites` as a workspace-scoped storage key so it is cleared on logout/workspace deletion |
| `channels-components-participants-panel` | packages/views/channels/components/index.ts | 1 | JEH-700 — re-exports new ParticipantsPanel side-panel component |
| `channels-index-rename-participants` | packages/core/channels/index.ts | 3 | JEH-700 — re-exports useUpdateChannel + useToggleChannelParticipant |
| `channels-mutations` | packages/core/channels/mutations.ts | 130 | JEH-700 — useUpdateChannel (rename) + useToggleChannelParticipant (subscribe/unsubscribe) with optimistic cache updates for channel detail/list |
| `channels-participants-panel` | packages/views/channels/components/participants-panel.tsx | 280 | JEH-700 — Sheet-based side panel listing channel participants with remove (confirm dialog) + add picker; reuses canAssignAgent + ActorAvatar |
| `channels-rename-participants` | packages/views/channels/components/channel-detail.tsx | 110 | JEH-700 — inline-editable channel title (kind='channel' only) + clickable participant stack opening the ParticipantsPanel sheet |
| `channels-favorites-cycle-break` | packages/views/channels/components/channel-detail.tsx | 1 | JEH-718 — inject ActorAvatar into ChannelAgentInlineRow so cerebro-channels does not need an @multica/views dependency (would close a turbo task cycle once views imports favorites-store from cerebro-channels) |
| `channels-slack-message-view` | packages/views/channels/components/channel-detail.tsx<br>packages/views/channels/components/slack-message-view.tsx<br>packages/views/channels/components/thread-side-panel.tsx | ~700 | JEH-1017 — Slack-style message stream for channels and DMs: compact rows with avatar/name only on the first message of a same-author group, indented continuation rows, sticky date separators ("Today" / "Yesterday" / weekday), inline reactions, and a "💬 N replies · last HH:mm" pill under any message that has descendants. Clicking the pill (or "Reply in thread" in the hover toolbar) opens a right-docked `ThreadSidePanel` showing the parent + flattened replies + a reply input. The panel is a layout-flex sibling of the message column, so it docks against the messages instead of overlaying them; closing it collapses to zero width and the message column reclaims the space. CommentCard rendering for channels/DMs is replaced by SlackMessageView; issues continue to use CommentCard. |
| `channels-thread-fullscreen-mobile` | packages/views/channels/components/channel-detail.tsx<br>packages/views/channels/components/thread-side-panel.tsx | ~30 | JEH-1045 — responsive override on top of `channels-slack-message-view`. On narrow viewports (`useIsMobile()`, <768px), opening a thread CSS-hides the channel header + message column (display:none, NOT unmount — so `CommentInput`'s Tiptap draft and upload map survive the thread round-trip). The `ThreadSidePanel` renders full-width (no max-w/border-l). The panel header swaps to a Slack-style "← #channel" / "← peer-name" back button that calls `onClose` to reopen the channel. Desktop behaviour is unchanged (420px right dock + "Thread" header with X close). |
| `channels-create-cache-write` | packages/core/channels/mutations.ts | 7 | JEH-1017 — write the new channel into the workspace channel list cache on `useCreateChannel.onSuccess` so the inbox redirect-fallback effect sees it via `channelMap.has(selectedKey)` on the very next render (instead of after the invalidate → refetch round-trip). Without this, creating a channel/DM from the NewMessageModal races the redirect into `/issues/<channel-id>`, tearing the inbox split-view out. Parallel to the JEH-850 fix for clicks. |
| `channel-create-dm-unarchive` | server/internal/handler/channel.go | 4 | JEH-1046 — when `CreateChannel` hits its idempotent DM path (peer already has an open DM), call `ChannelListen.MaybeUnarchiveForUser` so the caller's per-user archive row is cleared and `cerebro_channel_unarchived` is broadcast. Without this, re-DM'ing a previously archived peer returned the existing channel ID but the list-filter still hid it on the next refetch → `channelMap.has(selectedKey)` flipped false → the inbox redirect-fallback routed to `/issues/<channel-id>` → blank screen. The MaybeUnarchiveForUser method is idempotent (no-op when not archived), so the un-archived path pays no extra cost. |
| `claude-account-alias` | server/internal/daemon/daemon.go | 4 | Alias `CLAUDE_ACCOUNT=<email>` → `CLAUDE_CONFIG_DIR=$HOME/.claude-accounts/<email>` so per-agent CustomEnv reads as an email instead of a raw path (multi-Claude-OAuth-account-per-Mac flow) |
| `claude-oauth-strips-api-key` | server/pkg/agent/claude.go | ~12 | Cerebro Claude-agenter kører altid OAuth via `CLAUDE_CONFIG_DIR` (`~/.claude-accounts/<email>/`). `mergeEnv` stripper `ANTHROPIC_API_KEY` + `ANTHROPIC_AUTH_TOKEN` både fra host `os.Environ()` og fra CustomEnv — så en evt. global API-key i shellen eller en manuelt sat key i agent-UI'et kan ikke shadow OAuth-credentials. Ingen escape hatch: hvis du vil have API-key tilbage, reverse'r du denne patch. |
| `claude-stderr-tail-tests` | server/pkg/agent/claude_test.go | 0 | Claude stderr-tail diagnostic tests |
| `cli-attachments` | server/internal/cli/client.go | 0 | Cerebro modification (see file for details) |
| `comment-card-cerebro` | packages/views/issues/components/comment-card.tsx | 42 | Comment cerebro additions (replies, attachments, reactions) |
| `comment-handler` | server/internal/handler/comment.go | 7 | Comment cerebro additions (replies, attachments, reactions) |
| `comment-content-nullable` | server/internal/handler/comment.go<br>server/cmd/server/notification_listeners.go | ~10 | JEH-1215 — `CommentResponse.Content` is `*string` so a persona-redacted row serialises as `"content": null` rather than `""`. The `string` overload couldn't distinguish a redacted comment from a legitimately empty body and leaked the fact that a real value once existed. Notification listener derefs the new pointer. |
| `persona-mask-timeline` | server/internal/handler/activity.go | 3 | JEH-1216 — pass the resolved issue into `mergeTimeline` and run embedded comment entries through `maskCommentEntriesForCaller`. Deny drops the comment entry from the timeline (same leak reasoning as `ListComments`); mask zeros `content` to `nil`. Other activity types (status changes, assignee changes) are unaffected. |
| `comment-input-cerebro` | packages/views/issues/components/comment-input.tsx | 3 | Comment cerebro additions (replies, attachments, reactions) |
| `core-chat-queries` | packages/core/chat/queries.ts | 19 | Cerebro chat-archive/coalesce additions |
| `dashboard-nav` | packages/views/layout/app-sidebar.tsx | 4 | JEH-684 — sidebar entry for the cerebro dashboard at /:workspace/dashboard. Imports `<DashboardNavItem />` from `@multica/cerebro-dashboard` and renders it as the first item in the Workspace nav group. Gated on `cerebro_dashboard` feature flag. |
| `cerebro-dashboard-route` | server/cmd/server/router.go | 4 | JEH-684 — mounts `/api/cerebro/dashboard` overview endpoint. Two patch lines: one import (`cerebrodashboard`) and one handler instantiation; one route registration. Handler lives in `server/internal/cerebro/dashboard/`. |
| `cerebro-grants-routes` | server/cmd/server/router.go | 5 | JEH-1179 — import + handler instantiation + GET/POST/PATCH/DELETE routes under `/api/workspaces/{id}/grants`. Handler/service live in `server/internal/cerebro/grants/`. |
| `cerebro-grants-cli` | server/cmd/multica/main.go | 2 | JEH-1179 — registers `grantCmd` in the core command group and adds it to rootCmd. |
| `cerebro-groups-routes` | server/cmd/server/router.go | 4 | JEH-721 — mounts Cerebro workspace group CRUD and membership endpoints. Handler/service live in `server/internal/cerebro/groups/`. |
| `cerebro-groups-router-tests` | server/cmd/server/integration_test.go | 137 | JEH-721 — integration coverage for Cerebro group CRUD, member management, validation, and cross-workspace visibility. |
| `cerebro-groups-client` | packages/core/api/client.ts | ~40 | JEH-1006 — typed wrappers around `/api/workspaces/{id}/groups` and `/api/groups/{id}` for the Cerebro groups settings UI. The `fetch<T>` helper is private so the cerebro-only client lives here. |
| `cerebro-groups-cli` | server/cmd/multica/cerebro_group.go<br>server/cmd/multica/main.go | ~720 | JEH-1172 — `multica group` CLI mirrors the cerebro groups + grouppermissions APIs (create/list/get/update/delete, member add/remove/list, capability/runtime/agent set/list/remove, project group-access list/add/remove). Permissions are enforced server-side via the same admin gates as the REST surface. |
| `cerebro-groups-cli-test` | server/cmd/multica/cerebro_group_test.go | ~370 | JEH-1172 — unit coverage for `multica group` CLI: resolver behaviour, name → UUID lookup for users/groups, required-flag validation, and request shape for create/capability/runtime/project paths. |
| `cerebro-groups-mcp` | server/cmd/multica/cerebro_mcp_tools_group.go<br>server/cmd/multica/cmd_mcp_tools.go | ~420 | JEH-1172 — MCP tools (`list_groups`, `create_group`, `add_group_member`, `set_group_capability`, `list_group_runtimes`, `add_project_group`, etc.) so agents can administer workspace groups over the MCP transport. One registration line in `cmd_mcp_tools.go` calls into the cerebro-only registration file. |
| `cerebro-group-events` | packages/core/types/events.ts | 6 | JEH-1006 — `group:created`/`group:updated`/`group:deleted`/`group:member_added`/`group:member_removed` WS event types for the Cerebro groups settings UI. Payloads are server-defined in `server/internal/cerebro/groups/service.go`. |
| `settings-page-groups-tab` | packages/views/settings/components/settings-page.tsx | ~25 | JEH-1006 — adds a Cerebro "Groups" tab to the Workspace settings section. Gated on the `cerebro_groups_enabled` feature flag; trigger + content + mobile-nav entry + tab-value whitelist are all flag-aware so the tab can be flipped off without redeploying. Tab content lives in `@multica/cerebro-groups/views`. |
| `handler-group-permissions` | server/internal/handler/handler.go | 2 | JEH-1009 — `GroupPermissions GroupPermissionsInvoker` field on the upstream Handler so capability gates can call into the cerebro group-permission service without an import cycle. |
| `handler-group-permissions-iface` | server/internal/handler/handler.go | (same file) | JEH-1009 — interface seam + `GroupPermissionsViewer` shape at the handler-package boundary; the concrete adapter lives in `server/internal/cerebro/grouppermissions/handler_seam.go`. |
| `group-permissions-cerebro` | server/internal/handler/group_permissions_cerebro.go | ~140 | JEH-1009 — `cerebroGroupViewer` + `cerebroRequireCapability` helpers that gate write endpoints on `create_runtime` / `create_agent` capabilities. Admin override + nil-invoker fail-open for upstream-only test fixtures. |
| `group-permissions-cerebro-test` | server/internal/handler/group_permissions_cerebro_test.go | ~80 | JEH-1009 — pins the gate decision tree (nil-invoker pass, unknown capability fail-closed) against a stub invoker. |
| `create-agent-capability-gate` | server/internal/handler/agent.go | 4 | JEH-1009 — `CreateAgent` rejects callers without the `create_agent` capability via the cerebro gate. Admins bypass. |
| `create-agent-runtime-allowlist` | server/internal/handler/agent.go | 4 | JEH-1009 — `CreateAgent` also gates the chosen `runtime_id` against the user's runtime allowlist. Otherwise a member with `create_agent` but a restricted runtime allowlist could point a new agent at a runtime their groups forbid. |
| `create-runtime-capability-gate` | server/internal/handler/runtime_setup.go | 4 | JEH-1009 — `CreateRuntimeSetupToken` rejects callers without the `create_runtime` capability. The setup-token mint is the user-facing entry point for adding a runtime; the daemon side (`UpsertAgentRuntime`) is unchanged. |
| `router-group-permissions-seam` | server/cmd/server/router.go | 2 | JEH-1009 — wires the concrete `grouppermissions.HandlerSeam` into `Handler.GroupPermissions` so the upstream-handler capability gate can call the cerebro service. |
| `cerebro-group-permissions-client` | packages/core/api/client.ts | ~90 | JEH-1009 — typed wrappers for `/api/groups/{id}/capabilities`, `/runtimes`, `/agents` and `/api/projects/{id}/group-access`. The `fetch<T>` helper is private so the cerebro-only client lives in upstream-zone client.ts. |
| `cerebro-group-permission-events` | packages/core/types/events.ts | 4 | JEH-1009 — `group:capability_changed` / `group:runtime_changed` / `group:agent_changed` / `project:group_access_changed` WS event types. Payloads carry `group_id` (or `project_id`) so the realtime invalidator can target the correct query key. |
| `list-agents-visibility-split` | server/internal/handler/agent.go | ~12 | JEH-1066 — `ListAgents` returns every workspace agent regardless of group allowlist; trigger eligibility is surfaced via the new `can_trigger` field. Replaces the older `list-agents-group-filter` / `list-agents-owner-exempt` skip logic — the trigger gate (`cerebroRequireAgentAccess`) is the sole authority for enforcement. |
| `agent-can-trigger` | server/internal/handler/agent.go<br>server/internal/handler/group_permissions_cerebro.go<br>packages/core/types/agent.ts | ~35 | JEH-1066 — adds `can_trigger` to `AgentResponse` and the `cerebroCanTrigger` helper that mirrors `cerebroCanUseAgent` (group allowlist + owner-exemption + admin bypass) without writing to the response. Populated by ListAgents/GetAgent/CreateAgent/UpdateAgent/ArchiveAgent/RestoreAgent so the UI can render a lock state without a second permission round-trip. Frontend mirrors the field as `Agent.can_trigger?: boolean` (optional — older servers omit it). |
| `agents-page-group-lock` | packages/views/agents/components/agent-columns.tsx | 4 | JEH-1066 — surfaces an amber lock icon + tooltip on agent rows where `can_trigger === false`, so workspace members can see *why* a visible agent isn't usable. Suppressed on private/archived rows where another signal already explains the lock state. |
| `assignee-picker-group-lock` | packages/views/issues/components/pickers/assignee-picker.tsx | 6 | JEH-1066 — layers the `can_trigger` gate on top of the upstream visibility/role check inside the issue assignee picker so members can see (but not pick) group-locked agents. |
| `autopilot-agent-picker-group-lock` | packages/views/autopilots/components/pickers/agent-picker.tsx | 12 | JEH-1066 — disables + lock-icons rows whose `can_trigger === false`, replacing the previous "agent silently disappears from picker" behaviour for non-admin members. |
| `chat-picker-group-lock` | packages/views/chat/components/chat-window.tsx | ~18 | JEH-1320 — extends the JEH-1066 visibility/trigger split to the chat-window agent picker (the original PR missed it). Group-locked agents render with a lock icon + disabled `DropdownMenuItem` + tooltip; the dropdown's `handleSelectAgent` keyboard path defends against accidental selection; `triggerableAgents` is used for the default-active fallback so opening the chat never lands on an agent the user is blocked from sending to. |
| `chat-trigger-denied-toast` | packages/views/chat/components/chat-window.tsx | ~12 | JEH-1320 — catches the `cerebroRequireAgentAccess` 403 on `POST /api/chat/sessions` inside `handleSend` and surfaces the server's friendly message as a `sonner` toast. Previously the request failed silently in the UI; the toast text matches the picker lock tooltip (`GROUP_ACCESS_LOCKED_TOOLTIP` / `agentTriggerDeniedMessage`). |
| `group-access-locked-tooltip` | packages/core/agents/visibility-label.ts | 4 | JEH-1066 — shared frontend copy for the group-access lock tooltip and the friendly trigger-denied toast, kept in sync with the server-side `agentTriggerDeniedMessage` constant. |
| `agent-trigger-denied-copy` | server/internal/handler/group_permissions_cerebro.go | 3 | JEH-1066 — friendly 403 message for trigger-denied responses (`cerebroRequireAgentAccess`, `cerebroAgentAccessAsValidatorError`) so the toast text matches the picker lock tooltip. |
| `agent-access-owner-exemption` | server/internal/handler/group_permissions_cerebro.go | ~50 | JEH-1057 — `cerebroCanUseAgent` / `cerebroRequireAgentAccess` / `cerebroAgentAccessAsValidatorError` accept an `ownerID` so owners with `create_agent` capability pass the trigger gate even when the agent is outside their group allowlist. Mirrors the list-handler rule so anything visible in the list is also assignable / chattable. |
| `list-runtimes-group-filter` | server/internal/handler/runtime.go | 5 | JEH-1009 PR 4 — `ListAgentRuntimes` narrows non-admin viewers to the runtime IDs granted via their groups. |
| `list-runtimes-owner-exempt` | server/internal/handler/runtime.go | 4 | JEH-1056 — owners always see their own runtimes, gated on `create_runtime` capability (mirrors `list-agents-owner-exempt`). |
| `can-assign-agent-group` | server/internal/handler/issue.go | 4 | JEH-1009 PR 4 — `canAssignAgent` (issue assignment + channel creation) layers the cerebro agent allowlist gate on top of the existing private-agent check. |
| `validate-assignee-agent-group` | server/internal/handler/issue.go | 4 | JEH-1009 PR 4 — `validateAssigneePair` enforces the agent allowlist when the issue's assignee becomes an agent. |
| `create-chat-session-agent-allowlist` | server/internal/handler/chat.go | 4 | JEH-1009 PR 4 — `CreateChatSession` rejects members without group access to the agent. Mirrors `canAssignAgent` for the chat trigger surface. |
| `list-projects-group-access` | server/pkg/db/queries/project.sql | 3 | JEH-1009 PR 4 — `ListProjectsAccessibleToUser` ORs in cerebro_project_group_member so non-admin members see projects granted via any of their groups. |
| `list-issues-group-access` | server/pkg/db/queries/issue.sql | 2 | JEH-1009 PR 4 — `ListIssues` cascades project group access into issue visibility. CountIssues is intentionally untouched (no access predicate pre-existing). |
| `can-access-project-group` | server/internal/handler/access.go | 3 | JEH-1009 PR 4 — `canAccessProject` ORs in `cerebro_group_member`-derived access. Single-project paths (`GetProject`, `loadIssueForUser`) honour group access without the list-SQL detour. |
| `audience-restricted-group` | server/internal/handler/access.go | 9 | JEH-1009 PR 4 — restricted-project WS audience also includes group members so realtime events reach group-only viewers (otherwise they'd only see updates on a manual refresh). |
| `project-detail-group-access` | packages/views/projects/components/project-detail.tsx | 4 | JEH-1009 PR 4 — `ProjectGroupAccessSection` rendered inside the Access tab. Admins get the add/remove controls; members see the granted groups read-only. |
| `runtime-approval-issue` | server/internal/handler/runtime_approval_issue_cerebro.go<br>server/pkg/db/queries/issue_runtime_approval_cerebro.sql<br>server/migrations/9023_cerebro_runtime_approval_origin.up.sql | net-new | JEH-1152 — when `cerebroRequireRuntimeAccess` denies a CreateAgent call, the handler best-effort creates (or reuses an existing open) "Godkend runtime: …" issue stamped with `origin_type='runtime_approval'` + `origin_id=runtime_id` and returns a structured 403 (`code: runtime_not_approved_by_group`, `approval_issue: {id, identifier, title}`) so the UI can render a toast with a link. Admins are assigned + subscribed; the requester is also subscribed. |
| `runtime-approval-toast` | packages/views/agents/components/create-agent-dialog.tsx<br>packages/cerebro-groups/views/runtime-approval-toast.tsx | 6 (dialog) + net-new (helper) | JEH-1152 — dialog catch-block calls `showRuntimeApprovalToastIfApplicable` to render a rich sonner toast with an action that navigates to the auto-created approval issue. Falls back to the generic toast when the error isn't a runtime-approval denial. Helper lives in `cerebro-groups/views` to keep group-permission framing close together. |
| `cerebro-dashboard-client` | packages/core/api/client.ts | 4 | JEH-684 — `getCerebroDashboardOverview` typed wrapper around `/api/cerebro/dashboard`. The `fetch<T>` helper is private so cerebro-only callers go through this method instead of duplicating the auth/credentials handling. |
| `cerebro-tasks-route` | server/cmd/server/router.go | 5 | JEH-900 — mounts `/api/cerebro/tasks` cross-agent task list endpoint. Three patch lines: one import (`cerebrotasks`), one handler instantiation, and one route registration. Handler lives in `server/internal/cerebro/tasks/`. |
| `cerebro-tasks-client` | packages/core/api/client.ts | ~20 | JEH-900 — `getCerebroTasks` typed wrapper around `/api/cerebro/tasks`. Builds the query string from filter object (agent_id, status, type, since, limit, offset). |
| `cerebro-tasks-search` | packages/core/api/client.ts | 2 | JEH-1237 — adds `q?: string | null` param to `getCerebroTasks` filter type and serialises it as `?q=` in the query string. Enables server-side full-text search across agent name, task title, and issue title (ILIKE). |
| `cerebro-paths-permissions` | packages/core/paths/paths.ts | 2 | JEH-1180 — `paths.workspace(slug).permissions()` for the Persona grants admin page at `/:workspace/permissions`. |
| `cerebro-persona-grants-client` | packages/core/api/client.ts | ~95 | JEH-1180 — typed wrappers for the Persona grant control plane (`list/get/create/update/delete` grants + `audit`) under `/api/workspaces/{id}/grants`. Request/response bodies are `unknown` so the cerebro-permissions package owns the schema and parseWithFallback handles drift against Fætta's API (JEH-1179) once landed. |
| `cerebro-permissions-sidebar` | packages/views/layout/app-sidebar.tsx | 3 | JEH-1180 — sidebar entry for the cerebro permissions page (`PermissionsNavItem` import + workspace-group render). Same pattern as `cerebro-tasks-sidebar` / `cerebro-workflows-sidebar`. |
| `cerebro-tasks-sidebar` | packages/views/layout/app-sidebar.tsx | 4 | JEH-900 — sidebar entry for the cerebro tasks page at /:workspace/tasks. Imports `<TasksNavItem />` from `@multica/cerebro-tasks` and renders it next to the dashboard nav item in the Workspace group. Gated on `cerebro_tasks` feature flag (default off in Phase 1). |
| `cerebro-reserved-slugs` | server/internal/handler/reserved_slugs.json<br>packages/core/paths/reserved-slugs.ts | 1 (data) + 1 (generated) | JEH-900 — adds the `tasks` slug to the reserved list so a workspace can't shadow `/:workspace/tasks`. Lives in its own "Cerebro routes" group so upstream sync diffs stay clean; the TS file is regenerated from the JSON by `pnpm generate:reserved-slugs`. |
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
| `daemon-config-firtal-gateway-strict-bool` | server/internal/daemon/config.go | 1 | Refuse to silently disable the gateway on unrecognized `MULTICA_FIRTAL_GATEWAY_ENABLED` values |
| `daemon-config-test-firtal-gateway` | server/internal/daemon/config_test.go | 41 | Tests for explicit / inferred / strict-bool managed gateway runtime registration |
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
| `inbox-chat-row-swipe` | packages/views/inbox/components/inbox-page.tsx | 4 | Mount cerebro swipe-archive on chat session rows so they match issue/channel swipe behavior (JEH-663) |
| `inbox-folder-handler` | server/internal/handler/inbox_folder.go | 413 | Inbox-folder server handler (cerebro-only feature) |
| `inbox-keyboard-shortcuts` | packages/views/inbox/components/inbox-page.tsx | 4 | Mounts cerebro `e` = archive shortcut for the inbox page (JEH-663) |
| `inbox-list-item-cerebro` | packages/views/inbox/components/inbox-list-item.tsx | 17 | Inbox view additions |
| `inbox-row-actions-mount` | packages/views/inbox/components/inbox-list-item.tsx | 8 | Mounts the cerebro inbox row-actions surface (mute, mark-unread, hover menu, mobile swipe + long-press) on issue rows (JEH-663) |
| `inbox-muted-filter` | packages/core/inbox/view-store.ts<br>packages/views/inbox/components/inbox-page.tsx | 8 | JEH-663 — adds a "Muted" entry to the inbox filter dropdown and the `InboxView` type union, hides muted notifs from all non-muted views (auto-resurface when `muted_until` passes), and matches notifs whose `muted_until` is still in the future when the user picks the Muted filter. Uses client-side `isMuted` from `@multica/cerebro-inbox`. |
| `inbox-muted-timestamp` | packages/views/inbox/components/inbox-list-item.tsx | 3 | JEH-663 — swaps the row's relative timestamp for `Muted til HH:MM` when the row is currently muted (only visible because the user picked the Muted filter). Renders via `<CerebroInboxTimestamp>` in `@multica/cerebro-inbox`. |
| `inbox-mobile-detail-flex-height` | packages/views/inbox/components/inbox-page.tsx | 1 | Mobile inbox detail wrapper must be a flex column so embedded IssueDetail/ChannelDetail/InboxChatPanel get a defined height — overflow-y-auto block parent collapsed the body to zero height (JEH-697) |
| `inbox-desktop-agent-chat-start` | packages/views/inbox/components/inbox-page.tsx | 5 | Desktop NewMessageModal mount must pass `onAgentChatStarted` like the mobile mount; without it, tapping an agent fell through to a one-agent DM which the server rejects (JEH-846) |
| `inbox-page-channel-fallback` | packages/views/inbox/components/inbox-page.tsx | 4 | Skip the deep-link `/issues/<id>` redirect when the selected key matches a cerebro channel/DM — cerebro-only because channels are gated behind `cerebro_channels`. Without the guard, clicking a channel/DM row navigated away from /inbox (JEH-850). Parallels `inbox-page-chat-fallback`. |
| `inbox-page-stub` | packages/views/inbox/components/inbox-page.tsx | 0 | Routes to cerebro inbox-page when feature flag enabled |
| `input-autofocus` | packages/views/editor/content-editor.tsx<br>packages/views/chat/components/chat-input.tsx<br>packages/views/issues/components/comment-input.tsx<br>packages/views/inbox/components/inbox-chat-panel.tsx<br>packages/views/channels/components/channel-detail.tsx | ~40 | Autofocus the message/chat compose input when entering a chat or selecting an agent so the user can start typing without an extra click (JEH-756). Routed through Tiptap's `autofocus` option (not a post-mount RAF) because `immediatelyRender: false` defers editor creation past any effect that calls `editor.commands.focus()` — the call no-ops because `editor` is `null`. ContentEditor checks `document.activeElement` once at mount and yields silently if a `[role="dialog"]` already owns focus. Parents (`ChatInput`, `CommentInput`) gate the prop on `disabled` / `noAgent` and re-key the editor on session/agent/channel switch so Tiptap re-applies autofocus for the new context. |
| `install-runtime-script` | server/internal/handler/install_runtime.sh<br>server/internal/handler/install_runtime_embed.go | 236 | Runtime-setup token + install script (cerebro feature) |
| `invite-page-cerebro` | packages/views/invite/invite-page.tsx | 3 | Invite-page additions |
| `issue-chip-readable` | packages/views/issues/components/issue-chip.tsx | 3 | JEH-1113 — issue mention chips truncated the title with `…` on a single line, making the chip unreadable without clicking. Switching the title span from `truncate` to `break-words` allows long titles to wrap inside the existing `max-w-72` cap (so narrow-screen layout is unchanged), and adding `title={issue.title}` on the outer span gives a native hover tooltip with the full title as a backup for titles long enough to still wrap. |
| `issue-chip-multilne` | packages/views/issues/components/issue-chip.tsx | 12 | JEH-1253 — badge UX improvements: (1) removed `max-w-72` cap and `flex-wrap` so the full title is always visible on a single line and the badge as a whole wraps inline (no internal line-breaking); (2) `whitespace-nowrap` on the title span; (3) identifier styled as secondary (`text-[10px] text-muted-foreground`) so the title is the visual focus; (4) active-run indicator: a small pulsing dot (`.animate-pulse bg-primary`) appears next to the status icon when any task for the issue is `queued`, `dispatched`, or `running`. |
| `issue-chip-run-indicator` | packages/views/issues/components/issue-chip.tsx | 8 | JEH-1253 — import `issueKeys` and `api` to support the active-run query added by `issue-chip-multilne`. Query is enabled only when the issue is resolved and uses staleTime 30s to piggyback on the same cache entry maintained by ExecutionLogSection. |
| `issue-detail-cerebro-extras` | packages/views/issues/components/issue-detail.tsx | 129 | Issue-view cerebro additions |
| `issue-detail-mobile-overflow-x` | packages/views/issues/components/issue-detail.tsx | 1 | JEH-707 — the issue-body scroll container is `absolute inset-0 overflow-y-auto`; CSS spec promotes overflow-x:visible to auto when overflow-y is non-visible, so wide markdown content (e.g. nowrap tables) made the description scroll horizontally on mobile instead of staying inside the table wrapper. Pinning `overflow-x: hidden` lets the inner `.tableWrapper { overflow-x: auto }` provide the only horizontal scroll, matching desktop behaviour. |
| `issue-detail-highlight-scroll-hook` | packages/views/issues/components/issue-detail.tsx<br>packages/cerebro-ui/hooks/use-highlight-comment-scroll.ts<br>packages/cerebro-ui/index.ts | 4 (upstream) + 73 (cerebro zone) | JEH-1002 — inbox → issue auto-scroll. Replaces the original single-shot effect (which used `block: "center"` and was keyed on `timeline.length`) with a cerebro-zone hook that (1) uses `block: "start"` so the comment anchors to viewport top, and (2) retries on rAF until the `#comment-<id>` node is in the DOM — fixes the cached-remount path where re-tapping the same inbox item left the page at the top of the issue because `timeline.length` never transitioned 0→N. Upstream change is two marked import lines + one marked hook-call line; the bulk of the logic lives in `packages/cerebro-ui`. |
| `issue-handler` | server/internal/handler/issue.go | 51 | Server handler additions |
| `issue-mention-new-tab` | packages/views/editor/readonly-content.tsx | 28 | JEH-874 + JEH-1048 + JEH-1112 — clicking an issue mention chip in a comment or description opens it in a new tab on desktop/wide-web via `openInNewTab(path)` / `window.open(path)`, with `href={path}` (relative). Earlier revisions used `getShareableUrl(path)` so right-click → Copy Link returned a full https:// URL; on cerebro deployments the desktop `appUrl` default (`https://multica.ai` in `apps/desktop/src/shared/runtime-config.ts`) made every chip visibly point at multica.ai instead of the host the app is actually served from, so the relative path is preferable. Copy-Link now returns a relative path. On mobile (`useIsMobile()` true) the chip falls back to SPA `push(path)` because the PWA renders in `display: standalone` (no tab UI) and `target="_blank"` either navigates the current window (iOS) or breaks out into the system browser (Android), both of which lose the thread context. Browser-back restores scroll + tiptap draft via the Next.js router cache. |
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
| `mcp-cli-cmd-mcp-tools-grants` | server/cmd/multica/cmd_mcp_tools_grants.go | 320 | Persona grant control plane MCP tools (JEH-1181); in-memory mock backend until JEH-1179 lands the HTTP API |
| `mcp-cli-cmd-mcp-tools-grants-wire` | server/cmd/multica/cmd_mcp_tools.go | 3 | JEH-1181 — registerTools() call site that wires the Persona grant tools into the upstream MCP tools registration |
| `mcp-cli-cmd-mcp-tools-grants-test` | server/cmd/multica/cmd_mcp_tools_grants_test.go | 195 | JEH-1181 — Go test file for the Persona grant MCP tools; header marker because `*.test.*` exclusion in cerebro-zones.txt is for JS/TS, not Go's `_test.go` |
| `mcp-repo-config` | server/internal/mcp/repo_config.go | 132 | MCP server additions |
| `mcp-server` | server/internal/mcp/server.go | 212 | MCP server additions |
| `mcp-tools-inventory` | server/internal/mcp/server.go | 9 | JEH-1171 — expose registered tools via `Server.Tools()` so the permguard regression test can enumerate them without speaking JSON-RPC |
| `permguard-http-test` | server/cmd/server/permission_guard_cerebro_test.go | 50 | JEH-1171 — HTTP route inventory regression test. Walks the live chi router and diffs against `server/internal/cerebro/permguard/inventory.json` |
| `permguard-mcp-cli-test` | server/cmd/multica/permission_guard_cerebro_test.go | 100 | JEH-1171 — MCP tool + CLI command inventory regression tests. Enumerate via `Server.Tools()` and cobra's command tree |
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
| `new-message-modal-redesign` | packages/views/channels/components/new-message-modal.tsx | (full rewrite) | JEH-718 — replaces the DM/Channel-tabbed picker with a single actor list (members + agents) supporting multiselect, live search, comma-separated default channel name, and per-workspace pinned favorites (favorites store lives in `@multica/cerebro-channels`). Subsumes the prior `channels-dm-gate-hint` patch (DM tab removed). JEH-846 — toggle button labels its destination state ("Switch to channel" / "Switch to message") with matching icon. |
| `new-workspace-page-cerebro` | packages/views/workspace/new-workspace-page.tsx | 3 | New-workspace page cerebro additions |
| `notification-routing-test-cerebro` | server/cmd/server/notification_routing_test.go | 520 | Server cmd additions |
| `notify-all-mobile-inbox` | server/cmd/server/notification_routing.go<br>server/cmd/server/notification_routing_test.go | 8 | JEH-737 master toggle: when `preferences.notifications.notify_all_mobile_inbox` is true, mobile channel resolution mirrors the inbox channel for the same key so every inbox item also fires a Web Push without curating the per-key matrix. |
| `push-deep-link` | server/cmd/server/notification_listeners.go<br>server/cmd/server/notification_routing_test.go | 30 | Web Push payload `URL` now includes the workspace slug (`/<slug>/inbox?issue=<id>`) so tapping the notification deep-links to the inbox row that triggered it; old `/?issue=<id>` lost the query through the landing-page redirect. JEH-737. |
| `notifications-handler` | server/internal/handler/notifications.go | 134 | Notification handler additions |
| `orphan-task-test` | server/internal/handler/daemon_test.go | 0 | Cerebro orphan-task test additions |
| `page-header-cerebro` | packages/views/layout/page-header.tsx | 1 | Layout cerebro additions |
| `page-header-sticky` | packages/views/layout/page-header.tsx | 2 | JEH-821 — keep dashboard headers visible while mobile keyboard focus changes the visual viewport |
| `page-header-overflow-scroll` | packages/views/layout/page-header.tsx | 2 | JEH-1144 — overflow-x-auto + hidden scrollbar so page headers scroll horizontally when title + actions exceed the viewport on mobile |
| `pricing-pricing` | server/pkg/pricing/pricing.go | 108 | Pricing additions (token cost calc) |
| `pricing-pricing-test` | server/pkg/pricing/pricing_test.go | 90 | Pricing additions (token cost calc) |
| `pricing-anthropic-rates-2026-05` | server/pkg/pricing/pricing.go | 1 | Correct Opus 4.5+ rates to actual Anthropic pricing (was 3× over-charging, JEH-996); add Opus 4 / 4.1 / Sonnet 4 / Haiku 3.5 rows + date-suffix stripping in lookup |
| `pricing-test-anthropic-rates-2026-05` | server/pkg/pricing/pricing_test.go | 3 | Expectations updated for corrected Opus 4.5+ rates and the new date-suffix stripping in lookup() (JEH-996) |
| `privacy-toggle` | packages/views/issues/components/privacy-toggle.tsx | 59 | Privacy/restricted-access UI primitives |
| `profile-compile` | server/internal/profile/compile.go<br>server/internal/profile/compile_test.go | 372 | Profile-compile server logic |
| `project-chip-readable` | packages/views/projects/components/project-chip.tsx | 3 | JEH-1113 — mirror of `issue-chip-readable` so project chips also wrap long titles inside the existing `max-w-72` cap and expose the full title via a native `title=` hover tooltip. |
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
| `references-routes` | server/cmd/server/router.go | 4 | JEH-837 — mounts `/api/issues/{id}/references` (list/create) and `/api/cerebro/references` (reverse-lookup, patch, delete). Two patch lines: one import (`cerebroreferences`) and one handler instantiation; two route blocks. Handler lives in `server/internal/cerebro/references/`. |
| `comments-move-to-subissue` | server/cmd/server/router.go | 3 | JEH-1309 — adds `POST /api/comments/{commentId}/move-to-subissue`. Three patch lines: one import (`cerebrocomments`), one handler instantiation, and one route registration inside the existing `/api/comments/{commentId}` block. Handler lives in `server/internal/cerebro/comments/` and runs the lift in a single transaction (create sub-issue, copy replies, delete originals, rewrite root comment to a breadcrumb). |
| `sharetoken-routes` | server/cmd/server/router.go | 4 | JEH-1076 — public-link share-token mint/revoke + unauth GET. One import line, one handler-init line, one POST + one DELETE inside the auth tree. The public GET sits with the other unauth `sharetoken-public-route` marker. Handler lives in `server/internal/cerebro/sharetoken/`. |
| `sharetoken-public-route` | server/cmd/server/router.go | 1 | JEH-1076 — anonymous visitor route `GET /api/cerebro/public/share/{token}`. Mounted in the unauth tree because persona's anonymous-tolerant `/v1/check` is the only authority. |
| `reply-input-cerebro` | packages/views/issues/components/reply-input.tsx | 7 | Reply-input cerebro additions |
| `restricted-lock` | packages/views/common/restricted-lock.tsx | 37 | Privacy/restricted-access UI primitives |
| `restricted-ref` | packages/views/common/restricted-ref.tsx | 56 | Privacy/restricted-access UI primitives |
| `runtime-detail` | packages/views/runtimes/components/runtime-detail.tsx | 0 | Cerebro additions to runtime-detail |
| `runtime-detail-mobile-nav` | packages/views/runtimes/components/runtime-detail.tsx | 2 | JEH-821 — expose the global sidebar trigger on runtime detail pages at mobile widths |
| `runtime-handler-runtime` | server/internal/handler/runtime.go | 114 | Runtime handler additions |
| `runtime-handler-runtime-test` | server/internal/handler/runtime_test.go | 211 | Runtime handler additions |
| `runtime-list-cerebro` | packages/views/runtimes/components/runtime-list.tsx | 8 | Runtime view cerebro additions |
| `runtime-setup-handler` | server/internal/handler/runtime_setup.go | 224 | Runtime-setup token + install script (cerebro feature) |
| `runtime-setup-page` | packages/views/docs/runtime-setup-page.tsx | 182 | Runtime-setup token + install script (cerebro feature) |
| `runtimes-utils-cerebro` | packages/views/runtimes/utils.ts | 4 | Runtime view cerebro additions |
| `mobile-sidebar-burger-icon` | packages/ui/components/ui/sidebar.tsx | 3 | JEH-821 — show the standard hamburger menu icon for the mobile global navigation trigger while keeping the desktop panel icon |
| `server-integration-test-cerebro` | server/cmd/server/integration_test.go | 157 | Server bootstrapping additions |
| `server-listeners-cerebro` | server/cmd/server/listeners.go | 9 | Server bootstrapping additions |
| `server-firtal-gateway-runtime` | server/cmd/server/main.go | 12 | Start the Cerebro server-side Firtal Gateway HTTPS runtime worker for daemonless chat tasks |
| `server-main-cerebro` | server/cmd/server/main.go | 6 | Server bootstrapping additions |
| `server-setup-jwt-test` | server/cmd/server/setup_jwt_test.go | 9 | Server bootstrapping additions |
| `service-budget` | server/internal/service/budget.go | 125 | Service-layer additions (budget/task/workspace-pause) |
| `service-task-cerebro` | server/internal/service/task.go | 182 | Service-layer additions (budget/task/workspace-pause) |
| `service-workspace-pause` | server/internal/service/workspace_pause.go | 53 | Service-layer additions (budget/task/workspace-pause) |
| `settings-mobile-nav` | packages/views/settings/components/settings-page.tsx<br>packages/views/settings/components/cerebro-mobile-tab-nav.tsx | 62 | JEH-821 — replace Settings' stacked mobile tab sidebar with a sticky Select and global nav trigger |
| `settings-page-cerebro` | packages/views/settings/components/settings-page.tsx | 119 | Settings page cerebro additions |
| `skill-detail-mobile-nav` | packages/views/skills/components/skill-detail-page.tsx | 4 | JEH-821 — expose the global sidebar trigger on skill detail pages at mobile widths |
| `skills-mobile-toolbar` | packages/views/skills/components/skills-page.tsx | ~10 | JEH-885 — stack the search + scope filters and tighten the body padding so the Skills list is usable at 375px |
| `skills-mobile-detail-layout` | packages/views/skills/components/skill-detail-page.tsx<br>packages/views/locales/en/skills.json<br>packages/views/locales/zh-Hans/skills.json | ~60 | JEH-885 — collapse the file tree pane and metadata sidebar into Sheet drawers under md so the editor takes full width on phones; topbar exposes Files/Info triggers (new translation keys for the sheet triggers) |
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
| `sqlc-chat-update-status` | server/pkg/db/queries/chat.sql | 1 | JEH-799 — UpdateChatSessionStatus query for the chat-session header archive/restore action |
| `sqlc-chat-list-recent` | server/pkg/db/queries/chat.sql | 6 | JEH-757 — ListRecentChatMessages caps claim-path chat history at the SQL layer for long-lived sessions |
| `daemon-handler-chat-history-cap` | server/internal/handler/daemon.go | 7 | JEH-757 — claim path uses ListRecentChatMessages(limit=30) and reverses to chronological order |
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
| `submit-shortcut-chat-mode-newline` | packages/views/editor/extensions/submit-shortcut.ts | 4 | JEH-1025 — in chat-style mode (`submitOnEnter=true`), drop the `Mod-Enter` submit binding so Cmd/Ctrl+Enter falls through to Tiptap's hardBreak for a newline. Matches the Composer preference copy ("Enter sends. Use Shift+Enter or ⌘+Enter for a new line"). Shift+Enter already routed to hardBreak by default. |
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
| `reply-input-pin` | packages/views/issues/components/reply-input.tsx | ~60 | JEH-1065 — opt-in pin toggle that floats the reply input to the bottom of the viewport while pinned. Auto-unpins on submit. |
| `comment-input-pin` | packages/views/issues/components/comment-input.tsx | ~50 | JEH-1065 — `pinnable` prop on CommentInput plus pin button + portal float. Issue pages opt in; channels/DMs leave it off so chat surfaces stay unchanged. |
| `issue-detail-pin-comment-input` | packages/views/issues/components/issue-detail.tsx | 5 | JEH-1065 — passes `pinnable` to the bottom CommentInput on issue pages. |
| `i18n-comment-pin` | packages/views/locales/en/issues.json<br>packages/views/locales/zh-Hans/issues.json | 4 | JEH-1065 — pin/unpin tooltip + pinned-placeholder copy for the issue comment input. |
| `i18n-reply-pin` | packages/views/locales/en/issues.json<br>packages/views/locales/zh-Hans/issues.json | 4 | JEH-1065 — pin/unpin tooltip + pinned-placeholder copy for the issue reply input. |
| `reply-input-pin-floating-box` | packages/views/issues/components/reply-input.tsx | 5 | JEH-1065 — opaque box (`bg-background` + border + shadow) on the wrapper while the pinned reply floats over arbitrary thread content. |
| `reply-input-mobile-paperclip` | packages/views/issues/components/reply-input.tsx | ~10 | JEH-1065 — on mobile, move paperclip to the left to match CommentInput so pin/expand/send aren't cramped on the right. |
| `reply-input-min-height` | packages/views/issues/components/reply-input.tsx | 3 | JEH-1065 — minimum 2 lines + always-reserved bottom padding so the placeholder + icon row don't share one cramped line. |
| `comment-input-min-height` | packages/views/issues/components/comment-input.tsx | 1 | JEH-1065 — minimum 2 lines on the bottom comment input for the same reason. |

| `cerebro-account-availability` | packages/core/api/client.ts | 4 | JEH-881 — adds `runtime_count`, `available_runtime_count`, `nearest_unpause_at`, `status` fields to `CerebroAccount` interface. Backend derives values from a LEFT JOIN between `cerebro_account` and `agent_runtime`. |
| `heartbeat-account-id-ack` | server/pkg/protocol/messages.go, server/internal/handler/daemon.go (×2), server/internal/daemon/client.go, server/internal/daemon/daemon.go, server/internal/daemon/wakeup.go | 12 | JEH-881 — server returns the registered `cerebro_account_id` in the heartbeat ack (HTTP + WS). Daemon caches it per runtime. After each task run `maybeReportAccountUsage` parses the adapter output for rate-limit/usage-percent signals and POSTs them to `/api/daemon/accounts/{id}/usage` so `usage_window_pct` / `throttled_until` stay fresh. |
| `skill-mention-prefix` | packages/views/editor/extensions/mention-extension.ts | 5 | JEH-1094 — `skill` mention type renders bare (no `@` prefix), matching the existing `issue` carve-out, so a skill chip shows the skill name not `@SkillName`. |
| `skill-mention-view` | packages/views/editor/extensions/mention-view.tsx | 8 | JEH-1094 — adds a `type === "skill"` branch that renders `SkillMentionChip` from `@multica/cerebro-skill-mention`. |
| `skill-mention-readonly` | packages/views/editor/readonly-content.tsx | 2 | JEH-1094 — imports `SkillMentionChip` for readonly rendering of skill mentions. |
| `skill-mention-readonly-route` | packages/views/editor/readonly-content.tsx | 11 | JEH-1094 — extends the mention href regex to accept `skill`, and routes `mention://skill/<id>` to a `SkillMentionChip` instead of a plain mention span. |
| `skill-mention-register` | packages/views/editor/extensions/index.ts | 7 | JEH-1094 — imports and registers `createSkillMentionExtension` next to `BaseMentionExtension` so the `/skill` trigger fires in every editor that already supports `@`. Feature-flagged inside the extension. |
| `chat-message-id-claim` | server/internal/handler/agent.go<br>server/internal/handler/daemon.go<br>server/internal/handler/chat_message_attachment_cerebro.go<br>server/internal/service/task.go<br>server/internal/daemon/types.go<br>server/internal/daemon/daemon.go<br>server/internal/daemon/prompt.go<br>packages/views/chat/components/chat-message-list.tsx | ~80 | JEH-1083 — pre-create the assistant chat_message at chat-task claim time and expose its UUID to the agent as MULTICA_CHAT_MESSAGE_ID. CompleteTask / CancelTask / FailTask now upsert that row by task_id instead of inserting a duplicate. The chat UI gates "pending already persisted" on elapsed_ms / failure_reason so the StatusPill stays visible until the run actually finishes. |
| `sqlc-chat-assistant-by-task` | server/pkg/db/queries/chat.sql | 16 | JEH-1083 — GetAssistantChatMessageByTaskID + UpdateAssistantChatMessageContent backing the pre-created assistant chat_message lifecycle. |
| `mcp-add-attachment-chat` | server/cmd/multica/cmd_mcp_tools.go | ~30 | JEH-1083 — `add_attachment` MCP tool accepts `chat_message_id` and uses `UploadAttachmentTo` so chat-task agents can attach files to their reply (then reference them inline with `![filename](url)` markdown). |
| `reply-input-click-to-focus` | packages/views/issues/components/reply-input.tsx | 8 | JEH-1200 — `onMouseDown` on the card focuses the editor when the click lands on padding or empty flex space instead of the placeholder text. Skips interactive descendants so buttons/links keep working. |
| `comment-input-click-to-focus` | packages/views/issues/components/comment-input.tsx | 8 | JEH-1200 — same click-to-focus fix on the bottom-of-issue comment composer. |
| `sidebar-new-message-modal` | packages/core/modals/store.ts<br>packages/views/modals/registry.tsx | 4 | JEH-1296 — adds `"new-message"` to `ModalType` and wires `CerebroNewMessageModal` (new file in cerebro zone) into the global modal registry so the New Message button in the sidebar can open it from anywhere. |
| `sidebar-new-message-header` | packages/views/layout/app-sidebar.tsx | 8 | JEH-1296 — New Message button (feature-flagged behind `cerebro_channels`) added to SidebarHeader. Opens the global new-message modal. Imports `MessageSquarePlus` and `Bell` icons. |
| `sidebar-inbox-header` | packages/views/layout/app-sidebar.tsx | 8 | JEH-1296 — Inbox nav item moved from personal nav group to SidebarHeader (fixed top section), with unread count badge. |
| `sidebar-workspace-reorder` | packages/views/layout/app-sidebar.tsx | ~30 | JEH-1296 — removes `personalNav` array; workspace group reordered to: My Issues, Dashboard, Issues, Tasks, Documents, Permissions, Projects. Agents/Autopilot/Workflows moved to configure. `workspaceNav` and `configureNav` arrays updated. |
| `sidebar-configure-reorder` | packages/views/layout/app-sidebar.tsx | ~15 | JEH-1296 — configure group reordered: Agents, Runtimes, Autopilot, then WorkflowsNavItem (feature-flagged, inline after autopilots), Skills, Settings. |
| `sidebar-notification-badge` | packages/views/layout/app-sidebar.tsx | ~30 | JEH-1296 — removes standalone Notifications footer link; moves Notifications into user-avatar Popover (with Bell icon + count + Clear); adds grey count badge overlaid on avatar in top-right corner. |
| `i18n-sidebar-1296` | packages/views/locales/en/layout.json<br>packages/views/locales/zh-Hans/layout.json | 6 | JEH-1296 — adds `new_message`, `notifications`, `notifications_clear` keys to sidebar locale section. |
