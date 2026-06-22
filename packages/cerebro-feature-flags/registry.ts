/**
 * Single source of truth for the cerebro fork's feature flags.
 *
 * Defaults are held in TypeScript (no migration when a new flag ships).
 * Server-side persistence stores only overrides — flags toggled away from
 * default for a given (workspace, user) pair.
 */

export type CerebroFlagKey =
  | "cerebro_access_control"
  | "cerebro_members_admin"
  | "cerebro_sandbox_ui"
  | "cerebro_mcp_guide"
  | "cerebro_channels"
  | "cerebro_chat_message_cost"
  // FIR-39: per-comment cost badge on issues + channels (mirror of the chat
  // per-reply badge from FIR-31). One run can post multiple comments; cost is
  // pinned to the run's last comment so it sums to the issue total chip
  // already shown in the sidebar without double-counting.
  | "cerebro_comment_cost"
  | "cerebro_web_push"
  | "cerebro_browser_push_prompt"
  | "cerebro_dashboard"
  | "cerebro_inbox_row_actions"
  | "cerebro_channel_row_actions"
  // FIR-407: in-conversation message search for channels + DMs.
  | "cerebro_channel_message_search"
  // FIR-407: channel/DM messages surfaced in the global Cmd+K search.
  | "cerebro_global_message_search"
  | "cerebro_typing_indicators"
  | "cerebro_chat_row_actions"
  | "cerebro_inbox_action_grouping"
  | "cerebro_inbox_parent_grouping"
  | "cerebro_inbox_wakeup_running"
  | "cerebro_inbox_pinned_filter"
  // TECH-3535: after archiving the open message, return to the inbox list
  // instead of auto-advancing to the next message. Per-user preference.
  | "cerebro_inbox_archive_to_list"
  | "cerebro_inbox_dynamic"
  // FIR-1854: split a channel/DM thread with unread replies into its own
  // inbox row so replies buried in a thread are not missed.
  | "cerebro_inbox_thread_split"
  | "cerebro_notes"
  // FIR-1590: per-folder access control — "Only you / Selected colleagues /
  // Whole team" on note + document folders. Gates the folder and its contents.
  | "cerebro_folder_access"
  // FIR-1590 (follow-up): folder ACTION permissions. When a folder owner has
  // this on, only they + workspace admins may delete/rename/move folders they
  // own. Resolved per folder OWNER so it can roll out to one user first.
  | "cerebro_folder_action_guard"
  // TECH-3556 (Wave 3): the heavy Google-Docs note features, each independently
  // flagged so they can ship + be QA'd one at a time on staging.
  //   - comments + suggestions on a span of a note (margin comments, replies,
  //     accept/reject a proposed edit),
  | "cerebro_note_comments"
  //   - version history (see who changed what, restore an earlier version),
  | "cerebro_note_versions"
  //   - interim single-writer edit lock (stops two people overwriting each
  //     other until full live co-editing lands).
  | "cerebro_note_lock"
  // TECH-3690 (Jesper): clicking a note in the inbox Notes box opens it in the
  // same detail pane messages use, with an "Åbn fuldt" button to the full Notes
  // surface — instead of navigating straight away. Default on; off reverts to
  // the deep-link-to-full-page behavior.
  | "cerebro_note_inbox_pane"
  // FIR-1621 (Jesper): notes/documents as an agent collaboration surface.
  // Couple a note/document to an issue or chat (via references), accumulate
  // comments as local drafts, and explicitly send all-or-selected to the agent
  // (no auto-fire on @-mention). Gates the coupling UI, the "unsent comments"
  // notice, and the send controls. Default off until QA'd on staging.
  | "cerebro_note_agent_collab"
  // FIR-1800: reference an artifact (document/note) inside a comment / chat /
  // DM / channel body via a `mention://artifact/<id>` token, rendered as a
  // compact white card that opens the full-page note editor.
  | "cerebro_artifact_references"
  // TECH-3422: Slack-block in the dynamic inbox — a people/DM/channels block
  // with live presence dots and a typing indicator. Default off; the block is
  // only offered in the dynamic inbox's "Add section" menu when this is on.
  | "cerebro_inbox_slack_block"
  // TECH-3557: Secretary block in the dynamic inbox. Default off until QA has
  // verified desktop/mobile flows against the approved mockup.
  | "cerebro_inbox_secretary"
  // TECH-3579: Favorite conversations in the dynamic inbox — star a row to
  // float it to the top of the "All messages" box, plus a standalone Favorites
  // block. The star toggle replaces the row avatar on hover.
  | "cerebro_inbox_favorites"
  | "cerebro_voice_dictation_enabled"
  | "cerebro_voice_output_enabled"
  | "cerebro_voice_summary_enabled"
  | "cerebro_autopilot_scopes"
  | "cerebro_groups_enabled"
  | "cerebro_runtime_pause"
  | "cerebro_runtime_accounts"
  | "cerebro_tasks"
  | "cerebro_pin_input"
  | "cerebro_workflows"
  | "cerebro_persona_permissions"
  | "cerebro_skill_mention"
  | "cerebro_grants"
  | "cerebro_tool_policy"
  // FIR-1496: surface the group/person → tool grant editor alongside the unified
  // tool-policy table on the runtime page (the grant layer that silently denied
  // Jakob had no UI once cerebro_tool_policy hid the legacy grants card).
  | "cerebro_runtime_tool_grant_ui"
  | "cerebro_simple_tool_policy"
  // FIR-1771: the "Test as user" profile-menu entry that resolves another
  // user+agent's effective tool verdict. The real gate is the
  // tools:test-as-user permission (default Allow for the owner only); this flag
  // is a workspace-wide on/off for the whole feature.
  | "cerebro_test_as_user"
  // FIR-1479: credentials as a first-class permission type — assign a vault
  // box (one credential = one Agent Vault box) to an actor (agent / person /
  // group) per layer, exactly like a tool grant. A check = access to that one
  // box; least-privilege, opens nothing on its own. OFF by default — the
  // credentials column only appears in the permissions interface once an admin
  // turns it on, and access is deny-by-default until a box is granted.
  | "cerebro_credentials_per_actor"
  | "cerebro_web_fetch_policy"
  | "cerebro_platform_capabilities"
  | "cerebro_approvals"
  | "cerebro_move_comment_to_subissue"
  | "cerebro_move_comment_to_thread"
  | "cerebro_agent_passes"
  // JEH-216: skill ownership, approvers, version history, change requests, forks.
  | "cerebro_skill_ownership"
  | "cerebro_references"
  // CEREBRO-PATCH(agent-avatar-generate): JEH-1563
  | "cerebro_agent_avatar"
  // FIR-2385: private agents visible-but-locked + tag → run-request to owner.
  | "cerebro_private_agent_requests"
  // TECH-3670: per-surface agent discovery visibility ("Skjult" + advanced).
  | "cerebro_agent_surface_visibility"
  // FIR-2412: notify the assignee when an issue's start/due date arrives.
  | "cerebro_date_reminders"
  // FIR-2490: Firtal-branded welcome page for new members (replaces upstream onboarding).
  | "cerebro_firtal_welcome"
  // FIR-252: turn off upstream onboarding (questionnaire + create-workspace) on the
  // fork — members with a workspace go straight in; 0-workspace users land on
  // NewWorkspacePage, which always exposes a Log out / switch-account escape.
  | "cerebro_disable_onboarding"
  // FIR-2504: show similar open issues + LLM verdict when creating an issue.
  | "cerebro_duplicate_check_on_create"
  // FIR-2523: Auth & Permissions settings tab + Google Workspace auto-membership hook.
  | "cerebro_google_identity"
  // FIR-2580: per-workspace logo (upload + sidebar/breadcrumbs + web favicon + desktop dock icon).
  | "cerebro_workspace_logo"
  // FIR-2666: project sprint feature (sprint settings, auto-create next sprint, recurring tasks).
  | "cerebro_sprints"
  // TECH-3064: recurring issues — mark an issue recurring; on close, spawn the next occurrence.
  | "cerebro_recurring_issues"
  // FIR-1704: comment sessions UI. OFF — phase 1 model was unsound (comments never linked to a session); rebuilding as threads=sessions (FIR-1741). Internal key kept as `cerebro_comment_chapters` so existing per-user overrides keep working; everything user-facing now says "session".
  | "cerebro_comment_chapters"
  // FIR-1769 (P2): fold a session's activity events into one collapsed "Activity" section so comments stay prominent. Requires cerebro_comment_chapters.
  | "cerebro_session_activity_fold"
  // FIR-1769 (P3): whisper-subtle context-window hairline on the active session + an almost-full nudge to Start fresh. Requires cerebro_comment_chapters.
  | "cerebro_session_context_hairline"
  // FIR-1839: the "CLI runs" tab (the local Claude Code / CLI work-sessions list, formerly the "Sessions" tab). OFF by default — hidden unless a workspace opts in.
  | "cerebro_cli_runs_tab"
  // FIR-1597: optional time-of-day on an issue's start/due date; start time auto-starts an agent, due time times the reminder.
  | "cerebro_issue_date_times"
  // FIR-1659: personal saved filters — name a filter on the issue list and recall it with one click. Sharing + permissions land in later phases.
  | "cerebro_saved_filters"
  // FIR-1812: reference-aligned My Issues date filter — Field/Operator/Value rows with the full searchable value list (calendar week/month/quarter/year, dynamic Last/Next N, on/before/after/range, Is set/Is not set).
  | "cerebro_date_filter_v2"
  // TECH-3738 Bid C: capability drift watcher — periodically alert owners when an agent uses a tool its policy denies.
  | "cerebro_capability_drift_watcher"
  // TECH-3511: note types — reusable note templates with recurrence (business reviews).
  | "cerebro_note_types"
  // FIR-2661: render uploaded PDFs inline (native browser PDF view) instead of dumping extracted text.
  | "cerebro_pdf_inline_render"
  // FIR-1673: recover a stalled/half-loaded image on the document page — loading
  // state, automatic retry, and a manual "Reload image" button.
  | "cerebro_image_reload"
  // FIR-2641: "Remind me" on a specific comment — reuses the personal reminder engine.
  | "cerebro_comment_reminders"
  // FIR-394: the unified reminder system — reminders as their own entity plus the
  // per-surface "remind me" entries (chat message, etc.). The backend entity +
  // sweeper are always live; this gates the in-app remind-me actions. The
  // standalone reminder page has its own toggle (cerebro_reminders_page).
  | "cerebro_reminders"
  // FIR-394 (Jesper): show or hide the standalone reminder page (the "Reminders"
  // sidebar entry + the /reminders overview). Separate from the reminder system
  // itself so the page can be hidden without losing the "remind me" actions.
  | "cerebro_reminders_page"
  // FIR-394 (Jesper): hide reminder rows from the inbox "All messages" box so a
  // fired reminder only lives in its own Reminders box, not in two places.
  // Opt-in per user; OFF by default (reminders show in All messages).
  | "cerebro_inbox_hide_reminders"
  // FIR-2674: reject agent comments that mention no target (person, agent, or issue).
  | "cerebro_comment_target_guard"
  // TECH-3761: sub-toggle of cerebro_comment_target_guard. Exempt an agent from
  // the recipient requirement when it already has an active wakeup on the issue.
  | "cerebro_comment_target_guard_wakeup_exempt"
  // FIR-2409: friendly "Agent-start" permission tab — who may trigger an agent they don't own.
  | "cerebro_agent_trigger_permissions"
  // TECH-2880: collapsible Projects entry in the sidebar (with nested project tree).
  | "cerebro_projects"
  // FIR-2660: restrict channel creation to workspace owners/admins. Default off
  // preserves today's behavior (any member can create); turn on to require
  // owner/admin role for POST /api/channels (kind='channel'; DMs always open).
  | "cerebro_channel_create_restricted"
  // TECH-2925: admin UI for firtal_registry data source allowlist on agents.
  | "cerebro_firtal_registry_allowlist_ui"
  // FIR-33 (sub of FIR-18): auto-switch the "Create with agent" modal to manual
  // create when the picked agent's daemon CLI is below the quick-create gate,
  // instead of leaving the user on a warning banner with Create disabled.
  | "cerebro_quick_create_version_autoswitch"
  // FIR-40: per-workspace display currency (USD/DKK/EUR) for cost — settings
  // tab + display-time conversion. Off => cost shows in raw USD as before.
  | "cerebro_display_currency"
  // TECH-2947: personal focus list pinned to the top of the inbox.
  | "cerebro_focus_list"
  // TECH-3006: controls whether parent is notified when a sub-issue reaches done.
  | "cerebro_child_done_notify_parent"
  // TECH-3006: controls whether parent is notified when a sub-issue reaches in_review.
  | "cerebro_child_in_review_notify_parent"
  // TECH-3006: controls whether parent is notified when a sub-issue reaches blocked.
  | "cerebro_child_blocked_notify_parent"
  // Interactive terminal (cerebro-terminal): per-runtime presentation mode + xterm.js panel.
  | "cerebro_interactive_terminal"
  // TECH-3108: workspace connection registry (API + MCP connections managed from settings).
  | "cerebro_connections"
  // TECH-3077: skill metadata — category/domain/tag filtering, data-domain links, impact analysis.
  | "cerebro_skill_metadata"
  // TECH-3077: skill self-learning — observation recording, pattern extraction, auto change-requests.
  | "cerebro_skill_learning"
  // TECH-3099: sub-issue comment guard — three checks extending cerebro_comment_target_guard.
  // All default OFF; each is independently toggled so workspaces can adopt them one by one.
  | "cerebro_sub_issue_no_owner_mention"
  | "cerebro_sub_issue_require_agent_tag"
  | "cerebro_sub_issue_no_split_session"
  // FIR-2563: per-workspace toggle for the approval enforcement gate. When off,
  // the server-side gate lets every tool call through for this workspace without
  // an inbox ask or a deny — even when CEREBRO_APPROVAL_GATE_ENABLED is true on
  // the server. Defaults ON so existing workspaces keep their current behaviour.
  | "cerebro_approval_gate"
  // TECH-3176: per-type on/off for agent wakeup scheduling. Each trigger type
  // is independently gated at create-time and at fire/dispatch-time so an admin
  // can disable a wakeup kind without touching the others.
  | "cerebro_wakeup_time"
  | "cerebro_wakeup_issue_status"
  | "cerebro_wakeup_github_ci"
  // FIR-1521: prominent orange "scheduled wakeup" banner at the top of an issue,
  // mirroring the running banner (live countdown / waited-for status / CI).
  | "cerebro_wakeup_bar"
  // FIR-1521 (part 2): stack a small orange scheduled-wakeup clock pip next to the
  // running-agent indicator on issue lists + board cards; click expands the list.
  | "cerebro_activity_wakeup_dot"
  // TECH-3173: staged rollout of per-tool enforcement on LOCAL CLI runtimes
  // (Claude/Codex/Cursor/Gemini). Master on/off; when on the daemon resolves each
  // tool call through the same tool-policy chain + approval inbox as the gateway.
  // Default OFF (no behaviour change until an admin opts in from Settings).
  | "cerebro_local_tool_policy"
  // TECH-3173: when cerebro_local_tool_policy is on, flips the stage from observe
  // (resolve + log what WOULD block, allow everything) to enforce (Allow proceeds,
  // Block stops, Ask → inbox + wait). Default OFF = observe-only dry run.
  | "cerebro_local_tool_policy_enforce"
  // FIR-1609: enables the CEL expression escape hatch on tool-policy Conditions
  // (the WHEN layer of a rule). While OFF, only structured conditions (host-
  // allowlist, action-list) bite; a rule carrying an `expr` is undecidable and
  // fails closed by effect. Turning it ON wires the cel-go evaluator into both
  // gates so genuine dynamics ("only in business hours") can be expressed.
  // Default OFF — structured terms cover the common cases.
  | "cerebro_policy_cel"
  // FIR-1609 Phase 7 keystone: lets an EXPLICIT tool-policy Allow row grant
  // credential access, making the unified chain an Allow-source for secrets so the
  // legacy grant table can eventually be retired. While OFF the chain stays a pure
  // tighten-only cap and credential governance is byte-for-byte the prior behaviour;
  // a no-row default NEVER grants, so this can never open a default-allow hole on
  // reveal. Default OFF — flip on only after the grant→tool-policy migration is
  // verified 1:1.
  | "cerebro_credential_chain_grant"
  // TECH-3196: Agent Vault — per-agent secret brokering via the internal path.
  | "cerebro_agent_vault"
  // TECH-3491: per-device draft persistence for the comment / channel / DM
  // composers — a half-written message survives navigating away or a reload.
  | "cerebro_comment_drafts"
  // TECH-3698: per-channel permission settings (who may rename / add-remove
  // participants / leave) surfaced in the channel settings sheet and the
  // create-channel dialog. Gates only the configuration UI.
  | "cerebro_channel_permissions"
  // TECH-3582: Workspace copy console — a Settings tab to copy individual
  // entities (issues, channels, projects, agents, chats, autopilots) into
  // another workspace when merging two workspaces. Non-destructive.
  | "cerebro_workspace_copy"
  // FIR-recent-chats: show the 3 most recent chat sessions at the top of the
  // inbox new-chat screen, with a "see earlier" expander to scroll & resume
  // older ones in place.
  | "cerebro_chat_recent_list"
  // FIR-1412: folders (with sub-folders) for the Skills and Autopilots lists.
  | "cerebro_skill_folders"
  | "cerebro_autopilot_folders";

/**
 * Default value for each flag. Applied at read time when no override exists.
 *
 * Most cerebro features default to enabled — opt-out, not opt-in. The voice
 * toggles are the exception: they default to OFF until the cerebro-inference
 * container is reachable and the user opts in. The user-facing settings UI
 * groups the three voice flags under a single "Voice" section.
 */
export const CEREBRO_FLAG_DEFAULTS: Record<CerebroFlagKey, boolean> = {
  cerebro_access_control: true,
  cerebro_members_admin: true,
  cerebro_sandbox_ui: true,
  cerebro_mcp_guide: true,
  cerebro_channels: true,
  cerebro_channel_permissions: true, // TECH-3698
  cerebro_chat_message_cost: true,
  cerebro_comment_cost: true,
  cerebro_web_push: true,
  cerebro_browser_push_prompt: true,
  cerebro_dashboard: true,
  cerebro_inbox_row_actions: true,
  cerebro_channel_row_actions: true,
  cerebro_channel_message_search: true,
  cerebro_global_message_search: true,
  cerebro_typing_indicators: true,
  cerebro_chat_row_actions: true,
  cerebro_inbox_action_grouping: true,
  cerebro_inbox_parent_grouping: true,
  cerebro_inbox_wakeup_running: true,
  cerebro_inbox_pinned_filter: true,
  // OFF: preserve today's auto-advance-to-next behavior unless the user opts in.
  cerebro_inbox_archive_to_list: false,
  cerebro_inbox_dynamic: false,
  // FIR-1854 (Jesper): ON — a thread reply that would otherwise hide inside
  // the channel row gets its own inbox row so it is not missed.
  cerebro_inbox_thread_split: true,
  // TECH-3421: OFF by default until the Notes UI ships + is QA'd on staging.
  // Gates the Notes feature (private-by-default notes built on artifacts):
  // the Notes nav entry, quick-capture, the notes list/editor surface, and the
  // Notes box in the dynamic inbox.
  cerebro_notes: false,
  // FIR-1590: OFF until the folder access picker is QA'd on staging. Server
  // enforcement is always correct (folders default to "Whole team"); this flag
  // only gates the UI that lets a user restrict a folder.
  cerebro_folder_access: false,
  // FIR-1590 (follow-up): OFF by default. When a folder owner turns this on,
  // only they + workspace owners/admins may delete/rename/move folders they
  // own; everyone else's folders are unaffected. Server enforces by resolving
  // the flag for the folder's OWNER, so it can roll out to one user at a time.
  cerebro_folder_action_guard: false,
  // TECH-3556 (Wave 3): comments + suggestions on notes/documents. ON (FIR-1647):
  // shared surface + the comment composer can @-tag people/agents/issues.
  cerebro_note_comments: true,
  cerebro_note_versions: false,
  cerebro_note_lock: false,
  // TECH-3690: on by default — only takes effect where cerebro_notes is on.
  cerebro_note_inbox_pane: true,
  // FIR-1621: ON (FIR-1647) — coupling + send-to-agent flow shipped.
  cerebro_note_agent_collab: true,
  // FIR-1800: ON — render artifact references in comments/chat/DM/channels.
  cerebro_artifact_references: true,
  cerebro_inbox_slack_block: false,
  cerebro_inbox_secretary: false,
  cerebro_inbox_favorites: true,
  cerebro_voice_dictation_enabled: false,
  cerebro_voice_output_enabled: false,
  cerebro_voice_summary_enabled: false,
  cerebro_autopilot_scopes: true,
  cerebro_groups_enabled: true,
  cerebro_runtime_pause: true,
  cerebro_runtime_accounts: true,
  cerebro_tasks: false,
  cerebro_pin_input: true,
  cerebro_workflows: false,
  cerebro_persona_permissions: true,
  cerebro_skill_mention: true,
  cerebro_grants: false,
  cerebro_tool_policy: true,
  // FIR-1496: grant editor (group/person → tool) on the runtime page. Default OFF
  // — nothing changes until an admin turns it on.
  cerebro_runtime_tool_grant_ui: false,
  cerebro_simple_tool_policy: true,
  // FIR-1771: Test as user. Default ON — the feature is still locked down by the
  // tools:test-as-user permission (default Allow for the workspace owner only),
  // so turning the flag on exposes nothing to members who lack the permission.
  cerebro_test_as_user: true,
  // FIR-1479: credentials-per-actor grant column. Default OFF — deny-by-default
  // until an admin turns it on and grants a box to an actor.
  cerebro_credentials_per_actor: false,
  cerebro_web_fetch_policy: true,
  // FIR-2594: surface the Multica platform actions (create issue, add comment,
  // trigger autopilot, manage agents/runtimes/grants) in the tool-policy table
  // so they are settable Allow/Ask/Deny on every layer. Default OFF — nothing
  // new appears until an admin turns it on.
  cerebro_platform_capabilities: false,
  // Deliberately ON (FIR-2230 phase 5): the legacy duplicate "Pending" tab on
  // the Access page was removed, so the approvals inbox is now the ONLY surface
  // for needs_approval asks. Leaving this off would leave prod with no approvals
  // surface at all — the change is intentional and coupled to that removal, not
  // an accidental prod-behaviour flip.
  cerebro_approvals: true,
  cerebro_move_comment_to_subissue: true,
  cerebro_move_comment_to_thread: true,
  cerebro_agent_passes: true,
  // JEH-216: ON by default. Surfaces ownership/approvers, version history,
  // change-request review, and forking on the skill detail page. Off restores
  // the plain upstream skill editor with no governance UI.
  cerebro_skill_ownership: true,
  cerebro_references: true,
  // CEREBRO-PATCH(agent-avatar-generate): JEH-1563
  cerebro_agent_avatar: true,
  // FIR-2385: private agents are visible-but-locked and a non-owner tag turns
  // into a run-request in the owner's inbox. Off restores the old behavior
  // (private agents hidden, tags silently dropped).
  cerebro_private_agent_requests: true,
  // TECH-3670: on by default. Lets an agent owner hide a personal agent from
  // non-owner members on specific discovery surfaces (lists, @-mention, chat,
  // channels) — or all of them ("Skjult"). Off makes every agent visible
  // everywhere again (the legacy behavior); owner + admins always see it.
  cerebro_agent_surface_visibility: true,
  // FIR-2412: on by default — the assignee gets an inbox + push reminder when
  // a start/due date arrives. Off hides the settings rows and the UI control.
  cerebro_date_reminders: true,
  // FIR-2490: ON by default for the cerebro fork. New members are routed to
  // the Firtal-branded welcome page (desktop install guide with hard gate, PWA
  // install guide, members docs, bug-melding link to the Multica support
  // workspace) instead of upstream `/onboarding`. Per-user override still
  // lets anyone opt out from the cerebro settings panel.
  cerebro_firtal_welcome: true,
  // FIR-252: ON by default for the fork. Upstream onboarding (questionnaire →
  // "create workspace") never applies to invited Firtal members and traps anyone
  // who lands on the wrong/empty Google account on desktop (no URL bar, no
  // logout). When on, users with a workspace go straight in and users without
  // one land on NewWorkspacePage, which always exposes Log out / switch account.
  // Off restores the upstream onboarding flow.
  cerebro_disable_onboarding: true,
  // FIR-2504: surface similar open issues + Haiku verdict in the create-issue
  // modal so users can open an existing sag or attach as a sub-issue instead
  // of duplicating. Defaults ON so the feature lands behind the standard
  // workspace/user override (Off restores upstream create flow).
  cerebro_duplicate_check_on_create: true,
  // FIR-2523: Auth & Permissions tab + Google Workspace auto-membership.
  // Default ON for the cerebro fork (Jesper, 2026-05-30): firtal.com auto-
  // signup is the launch feature, and the table starts empty so a fresh
  // workspace with no configured domains is still a no-op.
  cerebro_google_identity: true,
  // FIR-2580: ships default OFF — opt-in per workspace before the logo
  // surfaces (upload UI, favicon swap, desktop dock icon).
  cerebro_workspace_logo: false,
  // FIR-2666: project sprint feature. Defaults OFF — turn on per workspace
  // when ready. Hides the Sprints tab on the project page, the sprint picker
  // in the issue sidebar, and skips the sprint sweeper.
  cerebro_sprints: false,
  // TECH-3064: OFF by default. Hides the recurring panel on issues and skips
  // the recurring-issue sweeper.
  cerebro_recurring_issues: false,
  // FIR-1704: OFF. Phase-1 sessions model was unsound (comments never linked to
  // a session -> empty sessions, dead buttons). Hidden while rebuilt as
  // threads=sessions (FIR-1741). Off renders the flat comment timeline.
  cerebro_comment_chapters: false,
  // FIR-1769 (P2/P3): OFF by default — ship dark behind the sessions rebuild,
  // turn on per-workspace once QA'd. Both no-op unless cerebro_comment_chapters
  // is also on.
  cerebro_session_activity_fold: false,
  cerebro_session_context_hairline: false,
  // FIR-1839: OFF by default — the "CLI runs" tab (local CLI work-sessions) is
  // hidden unless a workspace opts in.
  cerebro_cli_runs_tab: false,
  // FIR-1597: OFF by default. Hides the time-of-day control next to the
  // start/due date pickers. The sweeper still treats a no-time date as before.
  cerebro_issue_date_times: false,
  // FIR-1659: OFF by default while the feature is built across phases. Hides the
  // "Saved filters" section + "Save current filter" action in the issue Filter menu.
  cerebro_saved_filters: false,
  cerebro_date_filter_v2: false,
  // TECH-3738 Bid C: OFF by default. The capability drift watcher does nothing
  // until an admin turns it on; then it periodically alerts owners/admins when
  // an agent uses a tool its declared policy denies.
  cerebro_capability_drift_watcher: false,
  // TECH-3511: OFF by default. Hides the Note types admin in Documents and
  // skips the note-types sweeper until a workspace opts in.
  cerebro_note_types: false,
  // FIR-2661: ON by default. Uploaded PDFs in Documents and the attachment
  // viewer render in the browser's native PDF view (scroll, zoom, search).
  // Off restores the prior behaviour (extracted-text dump in the attachment
  // viewer; direct file iframe in the document viewer).
  cerebro_pdf_inline_render: true,
  // FIR-1673: ON by default. Image documents recover from a stalled/half-load:
  // a loading indicator, up to two automatic retries, and a manual "Reload
  // image" button. Off restores the plain image with no retry affordance.
  cerebro_image_reload: true,
  // FIR-2641: ON by default. Adds "Remind me" to the comment menu — a personal
  // reminder that points at one comment and, when it fires, deep-links the
  // inbox back to that comment. Reuses the existing reminder engine (inbox-only
  // by default; per-channel push stays opt-in). Off hides the menu action and
  // the server rejects comment-referencing reminders.
  cerebro_comment_reminders: true,
  // FIR-394: ON by default — the unified reminder is the product's reminder
  // system. Gates the per-surface "remind me" entries; the backend entity +
  // sweeper are always live. The standalone page is gated separately by
  // cerebro_reminders_page.
  cerebro_reminders: true,
  // FIR-394 (Jesper): OFF by default — hide the standalone reminder page. The
  // "remind me" actions keep working; only the dedicated "Reminders" sidebar
  // entry + /reminders overview are hidden. Turn on to show the page again.
  cerebro_reminders_page: false,
  // FIR-394 (Jesper): OFF by default — reminders show in "All messages". Turn on
  // to hide reminder rows from "All messages" so they only appear in the
  // standalone Reminders box (no duplicate across two boxes).
  cerebro_inbox_hide_reminders: false,
  // FIR-2674: OFF by default. When on, an agent-authored comment that mentions
  // no target at all (no person, agent, or issue) is rejected by the server
  // with a 422 telling the agent to add one. Members are never affected. Off
  // restores the prior behaviour (comments with no target allowed).
  cerebro_comment_target_guard: false,
  // TECH-3761: OFF by default. When on (and the base guard is on), an agent
  // that already has an active wakeup scheduled on the issue is exempt from the
  // recipient requirement — the wakeup is the follow-up action, so the comment
  // need not also tag a human. Off keeps the base guard's behaviour unchanged.
  cerebro_comment_target_guard_wakeup_exempt: false,
  // FIR-2409: opt-in until the Agent-start tab + per-agent rows are reviewed.
  cerebro_agent_trigger_permissions: false,
  // TECH-2880: OFF by default — workspace opts in to surface the Projects
  // collapsible (header + sub-items) in the sidebar.
  cerebro_projects: false,
  // FIR-2660: OFF by default. When on, POST /api/channels with kind='channel'
  // is restricted to workspace owners/admins (members get 403). DMs are never
  // gated. Off restores today's behaviour (any member or agent may create).
  cerebro_channel_create_restricted: false,
  // TECH-2925: OFF by default until QA on staging; hides Konfigurer on firtal_registry.
  cerebro_firtal_registry_allowlist_ui: false,
  // FIR-33 (sub of FIR-18): ON by default. When the agent picked in the Quick
  // Create modal runs a daemon CLI below the quick-create gate, the modal flips
  // to manual create (carrying the typed prompt/project/parent) instead of
  // stranding the user on a warning banner. Off restores the warning-only
  // behaviour.
  cerebro_quick_create_version_autoswitch: true,
  cerebro_display_currency: true,
  // TECH-2947: ON by default. Personal focus list at the top of the inbox —
  // a lightweight to-do surface for ADHD-friendly task tracking. Off hides
  // the panel and the backend endpoints reject requests.
  cerebro_focus_list: true,
  // TECH-3006: ON by default — preserves current behavior. Post a system
  // comment on the parent and wake its assignee when a sub-issue reaches done.
  // Off silences the notification for the effective flag scope.
  cerebro_child_done_notify_parent: true,
  // TECH-3006: ON by default — preserves current behavior. Post a system
  // comment on the parent and wake its assignee when a sub-issue reaches
  // in_review. Off silences the notification for the effective flag scope.
  cerebro_child_in_review_notify_parent: true,
  // TECH-3006: ON by default — preserves current behavior. Post a system
  // comment on the parent and wake its assignee when a sub-issue reaches
  // blocked. Off silences the notification for the effective flag scope.
  cerebro_child_blocked_notify_parent: true,
  cerebro_interactive_terminal: false,
  // TECH-3108: ON by default — feature shipped and QA done. TECH-3209: switching
  // to true-default so admins don't lose access when personal/workspace override is cleared.
  cerebro_connections: true,
  // TECH-3077: ON — skill metadata schema (category, domain, tags, data-domain links).
  cerebro_skill_metadata: true,
  // TECH-3077: OFF — skill self-learning is a later phase; enable when observation
  // infrastructure is in place.
  cerebro_skill_learning: false,
  // TECH-3099: sub-issue comment guard checks — all OFF by default.
  cerebro_sub_issue_no_owner_mention: false,
  cerebro_sub_issue_require_agent_tag: false,
  cerebro_sub_issue_no_split_session: false,
  // FIR-2563: ON by default. When the server gate is active
  // (CEREBRO_APPROVAL_GATE_ENABLED=true), this per-workspace flag lets an admin
  // disable enforcement for their workspace without a server restart. Off = all
  // tool calls are allowed through for this workspace regardless of policy rows.
  cerebro_approval_gate: true,
  // TECH-3176: agent wakeup trigger types. All default ON — wakeup scheduling
  // ships enabled; an admin turns a type off to stop new creates and any
  // pending fires of that type for the workspace.
  cerebro_wakeup_time: true,
  cerebro_wakeup_issue_status: true,
  cerebro_wakeup_github_ci: true,
  // FIR-1521: ON by default — additive top-of-issue banner. Off restores the
  // sidebar-only wakeup list with no banner.
  cerebro_wakeup_bar: true,
  // FIR-1521 (part 2): ON by default — additive clock pip on list/board rows. Off
  // restores the rows to running-only indicators with no scheduled-wakeup dot.
  cerebro_activity_wakeup_dot: true,
  // TECH-3173: OFF by default — local-runtime per-tool enforcement stays dormant
  // until an admin opts in from Settings, so a deploy never changes behaviour.
  cerebro_local_tool_policy: false,
  // TECH-3173: OFF by default — when the master is on, observe-only (dry run)
  // until an admin explicitly flips to enforce. Staged rollout, fail-safe.
  cerebro_local_tool_policy_enforce: false,
  // FIR-1609: OFF by default — the CEL expression escape hatch on tool-policy
  // Conditions stays dormant; structured host/action terms bite, an `expr` rule
  // fails closed until an admin opts in. No deploy-time behaviour change.
  cerebro_policy_cel: false,
  // FIR-1609 Phase 7 keystone: OFF by default — the unified chain stays a pure
  // tighten-only cap for credentials; an explicit Allow row only grants once an
  // admin flips this on, after the grant→tool-policy migration is verified 1:1.
  // No deploy-time behaviour change; can never open a default-allow hole on reveal.
  cerebro_credential_chain_grant: false,
  // TECH-3196: OFF by default — Agent Vault per-agent secret brokering ships
  // dormant until an admin opts in and the access table is configured.
  cerebro_agent_vault: false,
  // TECH-3491: ON by default — saving an unsent comment/channel/DM message as a
  // per-device draft is the whole point of the feature; a deploy turning it on
  // is the intended behaviour change. Off restores the old "lose it on navigate"
  // behaviour and hides the "Kladde gemt" hint.
  cerebro_comment_drafts: true,
  // TECH-3582: OFF by default. Ships dormant — the Workspace copy console only
  // appears in Settings once an admin opts in to run a one-time workspace merge.
  cerebro_workspace_copy: false,
  cerebro_chat_recent_list: true,
  // FIR-1412: default ON — folders are additive and harmless when unused.
  cerebro_skill_folders: true,
  cerebro_autopilot_folders: true,
};

/**
 * Group a flag belongs to in the settings UI. Grouping is presentation-only —
 * it changes how the flags are laid out, never what a flag does. FIR-2284 Bite 2.
 */
export type CerebroFlagGroupKey =
  | "permissions"
  | "agents"
  | "workspace"
  | "issues"
  | "inbox"
  | "voice"
  | "onboarding";

export interface CerebroFlagGroup {
  key: CerebroFlagGroupKey;
  label: string;
  description: string;
}

/**
 * Group metadata for the settings UI. Order here is the order the groups are
 * shown to the user; flags within a group keep their order in CEREBRO_FLAGS.
 */
export const CEREBRO_FLAG_GROUPS: CerebroFlagGroup[] = [
  {
    key: "permissions",
    label: "Permissions & approvals",
    description:
      "What agents are allowed to do, who has to sign off, and how access is granted.",
  },
  {
    key: "agents",
    label: "Agents & runtimes",
    description: "How agents run on machines — sandbox, pausing, accounts, and setup.",
  },
  {
    key: "workspace",
    label: "Workspace & pages",
    description: "Top-level surfaces and navigation across the workspace.",
  },
  {
    key: "issues",
    label: "Issues & comments",
    description: "Affordances when working inside an issue or composing a new one.",
  },
  {
    key: "inbox",
    label: "Inbox & notifications",
    description: "How the inbox is organised and how you get notified.",
  },
  {
    key: "voice",
    label: "Voice",
    description:
      "Speech features (dictation, read-aloud). Require the cerebro-inference container.",
  },
  {
    key: "onboarding",
    label: "Onboarding",
    description: "What new members see the first time they join the workspace.",
  },
];

export interface CerebroFlagDefinition {
  key: CerebroFlagKey;
  label: string;
  description: string;
  group: CerebroFlagGroupKey;
}

/**
 * Display metadata for the settings UI. Order here is the order shown to
 * the user within each group (see CEREBRO_FLAG_GROUPS for group order).
 */
export const CEREBRO_FLAGS: CerebroFlagDefinition[] = [
  {
    key: "cerebro_access_control",
    label: "Cerebro access control",
    group: "permissions",
    description:
      "Enable the cerebro-fork access-control flow (combined restrict + pick) for issues and projects.",
  },
  {
    key: "cerebro_credentials_per_actor",
    label: "Credentials per actor",
    group: "permissions",
    description:
      "Add credentials as a permission type in the permissions interface: tick a vault box (one credential = one Agent Vault box) for an agent, person, or group to grant access to exactly that box — like a tool grant. Least-privilege; access is denied until a box is ticked.",
  },
  {
    key: "cerebro_folder_access",
    label: "Folder access control",
    group: "permissions",
    description:
      "Let folder owners set who can see a note or document folder — Only you, Selected colleagues, or Whole team. The choice locks the whole folder and its contents.",
  },
  {
    key: "cerebro_folder_action_guard",
    label: "Protect my folders",
    group: "permissions",
    description:
      "When on, only you and workspace admins can delete, rename, or move folders you own. Other people's folders are unaffected until they turn this on too.",
  },
  {
    key: "cerebro_members_admin",
    label: "Cerebro members admin",
    group: "workspace",
    description:
      "Use the cerebro-fork members admin tab with bulk actions and richer filters.",
  },
  {
    key: "cerebro_sandbox_ui",
    label: "Cerebro sandbox UI",
    group: "agents",
    description:
      "Show the cerebro-fork sandbox panels and developer affordances inside agent runs.",
  },
  {
    key: "cerebro_mcp_guide",
    label: "Cerebro MCP guide",
    group: "agents",
    description:
      "Show the cerebro-fork MCP setup guide in the runtimes and docs panels.",
  },
  {
    key: "cerebro_channels",
    label: "Channels",
    group: "workspace",
    description:
      "Enable channel-style conversations (kind=channel issues, /channels/{id} route, channel list in inbox).",
  },
  {
    key: "cerebro_chat_message_cost",
    label: "Per-reply chat cost",
    group: "workspace",
    description:
      "Show the spend ($) of each assistant reply in the chat footer, next to \"Replied in …\". Hover for the token/model breakdown. Off hides the per-reply badge (the session-total chip in the header stays).",
  },
  {
    key: "cerebro_comment_cost",
    label: "Per-comment issue & channel cost",
    group: "issues",
    description:
      "Show the spend ($) of each agent comment on issues and channels, with a token/model breakdown on hover. A run that posts progress + a result places the badge on the run's last comment, so per-comment numbers sum to the issue total already shown in the sidebar (JEH-736) without double-counting. Off hides the per-comment badge.",
  },
  {
    key: "cerebro_artifact_references",
    label: "Artifact references in comments",
    group: "issues",
    description:
      "Let a comment, chat message, DM, or channel message reference an artifact (document/note) via a `mention://artifact/<id>` token, rendered as a compact white card (white = real document; grey = uploaded file). Clicking the card opens the full-page note editor (same rule as document cards, FIR-1782). Posted from the CLI with `multica issue comment add --artifact <id>`. Off renders the reference as plain link text.",
  },
  {
    key: "cerebro_web_push",
    label: "Web Push notifications",
    group: "inbox",
    description:
      "Enable browser/PWA push notifications for new inbox items, comments, and mentions.",
  },
  {
    key: "cerebro_browser_push_prompt",
    label: "Browser push prompt",
    group: "inbox",
    description:
      "Show a dismissible banner in the browser inviting the user to turn on push notifications (asks for permission, links to notification settings). Only appears in a browser, never in the desktop app.",
  },
  {
    key: "cerebro_dashboard",
    label: "Cerebro dashboard",
    group: "workspace",
    description:
      "Enable the cerebro workspace operations dashboard at /:workspace/dashboard (agent strip, KPI cards, recent tasks).",
  },
  {
    key: "cerebro_inbox_row_actions",
    label: "Inbox row actions",
    group: "inbox",
    description:
      "Show the cerebro inbox row-actions surface: mute, mark-unread, hover menu, mobile swipe gestures, long-press menu, and the `e` keyboard shortcut.",
  },
  {
    key: "cerebro_channel_row_actions",
    label: "Channel row actions",
    group: "inbox",
    description:
      "Give channels and DMs the same inbox row controls as notifications: \"remind me\" (snooze) and \"mark as unread\", via the hover menu, mobile swipe gestures, and long-press menu.",
  },
  {
    key: "cerebro_channel_message_search",
    label: "Search in conversation",
    group: "inbox",
    description:
      "Add a search icon to every channel and DM header. Opens a search bar that highlights matching messages, dims the rest, and lets you step between results with the up/down arrows or Enter/Shift+Enter — Esc closes it. Searches the currently loaded message history of the open conversation.",
  },
  {
    key: "cerebro_global_message_search",
    label: "Messages in global search",
    group: "inbox",
    description:
      "Include channel and DM messages in the global Cmd+K search. Matches from any conversation you take part in show up in a \"Messages\" group with a snippet, the conversation name, and the sender — so you can find an old link or note without opening each conversation. Access-scoped: you only see messages from your own conversations.",
  },
  {
    key: "cerebro_typing_indicators",
    label: "Typing indicators",
    group: "inbox",
    description:
      "Show a Slack-style \"is typing…\" line inside a channel or direct-message conversation when another person is composing a reply, and surface when an agent is generating its response. The other person's composer sends a lightweight typing ping (throttled); the indicator clears after a few seconds of silence.",
  },
  {
    key: "cerebro_chat_row_actions",
    label: "Chat row actions",
    group: "inbox",
    description:
      "Give agent chat sessions the same inbox row menu as notifications and channels: a 3-dot menu (and mobile swipe) to mark read, rename, convert to issue, archive, unarchive, and delete a chat — so an archived chat can be reopened and continued.",
  },
  {
    key: "cerebro_inbox_action_grouping",
    label: "Inbox group by action",
    group: "inbox",
    description:
      "Add a \"Group by → Action\" option to the inbox that buckets items by what to do next (Act now / Watching / Waiting / Calm) instead of by status. Default grouping for new users; switch it off or pick another grouping from the inbox's Group by menu.",
  },
  {
    key: "cerebro_inbox_parent_grouping",
    label: "Inbox group by parent issue",
    group: "inbox",
    description:
      "Add a \"Group by → Parent issue\" option to the inbox that clusters every message about sub-issues of the same parent into one group, so related items are seen together. Items with no parent land in a \"No parent issue\" group. Works as a primary or secondary grouping from the inbox's Group by menu.",
  },
  {
    key: "cerebro_inbox_dynamic",
    label: "Dynamic inbox",
    group: "inbox",
    description:
      "Let each user build their own inbox out of stackable sections (Unread / Running / Pinned / Project / Assigned …) inside one box, with tabs at the top and per-section filter, grouping and sort. Users switch between the Classic and Dynamic inbox from the inbox's ⋯ menu; the layout is saved per user and follows them across devices, with an optional separate layout for mobile/PWA.",
  },
  {
    key: "cerebro_note_inbox_pane",
    label: "Open notes in inbox pane",
    group: "inbox",
    description:
      "Clicking a note in the inbox Notes box opens it in the same detail pane that messages use — read and edit it without leaving the inbox — with an \"Åbn fuldt\" button to jump to the full Notes surface. Off reverts to opening notes on the full Notes page. Requires Notes.",
  },
  {
    key: "cerebro_inbox_slack_block",
    label: "Inbox Slack-block",
    group: "inbox",
    description:
      "Add a Slack-style block to the dynamic inbox: a list of people with live online dots, your direct messages and channels, and a \"is typing…\" indicator — open a conversation right inside the inbox. Offered in the dynamic inbox's \"Add section\" menu when on. Requires the Dynamic inbox.",
  },
  {
    key: "cerebro_inbox_secretary",
    label: "Inbox Secretary",
    group: "inbox",
    description:
      "Add a focused Secretary block to the dynamic inbox. Users pick a batch size, let the app choose unread/oldest messages or select rows manually, then work through that small list in the existing message detail view. Default off until QA signs off.",
  },
  {
    key: "cerebro_inbox_favorites",
    label: "Inbox favorites",
    group: "inbox",
    description:
      "Star a conversation in the dynamic inbox to make it a favorite and float it to the top of the \"All messages\" box, in its own Favorites section. Each row's avatar turns into a star on hover so you can toggle it in place. The top Favorites section can be switched off per box (favorites then stay in their normal position but can still be starred), and a standalone Favorites block can be added from the \"Add section\" menu. Favorites are saved per user and follow you across devices. Requires the Dynamic inbox.",
  },
  {
    // FIR-394 (Jesper): the unified Reminders feature is one flag, with its two
    // display options (cerebro_reminders_page, cerebro_inbox_hide_reminders) as
    // settings inside this box — see RemindersSettings (FLAG_SETTINGS). Off turns
    // reminders off entirely.
    key: "cerebro_reminders",
    label: "Reminders",
    group: "inbox",
    description:
      "The reminder system — a \"Remind me\" action on messages, comments and issues that lands in your inbox (and re-opens the source) when it fires. Off turns reminders off entirely. While on, use the settings below to show the standalone Reminders page and to keep reminders out of \"All messages\".",
  },
  {
    key: "cerebro_inbox_wakeup_running",
    label: "Inbox wakeup → Running",
    group: "inbox",
    description:
      "When an issue has a pending agent wakeup, show it in the inbox's \"Running\" action group and mark its row with a clock (the approximate next-run time) instead of the live agent pip. Off hides the clock and keeps wakeup-only issues out of Running.",
  },
  {
    key: "cerebro_inbox_pinned_filter",
    label: "Inbox pinned filter",
    group: "inbox",
    description:
      "Add a \"Pinned\" option to the inbox view dropdown that shows only items tied to something you pinned — the item's own issue, that issue's parent, or its project, plus pinned channels and DMs. Off removes the option from the dropdown.",
  },
  {
    key: "cerebro_inbox_archive_to_list",
    label: "Archive returns to the inbox",
    group: "inbox",
    description:
      "When you archive the message you're reading, go back to the inbox list instead of automatically opening the next message. Off (default) keeps auto-advancing to the next message. Applies to archiving from the row menu, swipe, the `e` shortcut, and the open message's toolbar.",
  },
  {
    key: "cerebro_voice_dictation_enabled",
    label: "Dictation",
    group: "voice",
    description:
      "Push-to-talk Whisper dictation (hviske-v3) in chat input and other text fields. Requires the cerebro-inference container.",
  },
  {
    key: "cerebro_voice_output_enabled",
    label: "Voice output",
    group: "voice",
    description:
      "Read assistant replies aloud in Danish via plapre-nano. Per-message read button + global voice mode.",
  },
  {
    key: "cerebro_voice_summary_enabled",
    label: "Voice summary",
    group: "voice",
    description:
      "When voice mode is on, summarise long replies into spoken-style Danish before reading them aloud. Reduces TTS latency on long answers and keeps the conversation natural in hands-free use.",
  },
  {
    key: "cerebro_autopilot_scopes",
    label: "Autopilot scopes",
    group: "agents",
    description:
      "Enable scoped autopilots (workspace, personal, group) — gated visibility on top of the workspace-wide default.",
  },
  {
    key: "cerebro_groups_enabled",
    label: "Groups",
    group: "workspace",
    description:
      "Enable workspace groups: named collections of members used by Cerebro features such as scoped resources.",
  },
  {
    key: "cerebro_runtime_pause",
    label: "Runtime pause / resume",
    group: "agents",
    description:
      "Pause and resume agent runtimes manually or automatically. When a provider returns 429, the runtime auto-pauses until the rate-limit window resets, then resumes interrupted work on its own.",
  },
  {
    key: "cerebro_runtime_accounts",
    label: "Runtime account availability",
    group: "agents",
    description:
      "Show availability status per account on the runtime detail card: how many runtimes are free, throttled, or paused. Coordinator agents use this to pick the right runtime.",
  },
  {
    key: "cerebro_tasks",
    label: "Tasks page",
    group: "workspace",
    description:
      "Enable the cross-agent tasks page at /:workspace/tasks (full task list with filters and pagination).",
  },
  {
    key: "cerebro_pin_input",
    label: "Pin issue input",
    group: "issues",
    description:
      "Enable a pin toggle on issue comment and reply inputs that keeps the active input stuck to the bottom of the viewport while scrolling. Issue pages only — channels and DMs are unaffected.",
  },
  {
    key: "cerebro_workflows",
    label: "Workflow engine",
    group: "workspace",
    description:
      "Enable the cerebro workflow engine and the /:workspace/workflows page (data-driven status/trigger rules, builder UI, run log). Server-side execution is additionally gated by the CEREBRO_WORKFLOWS_ENABLED env var.",
  },
  {
    key: "cerebro_persona_permissions",
    label: "Persona permissions",
    group: "permissions",
    description:
      "Enable the workspace permissions admin page at /:workspace/permissions — list, create, edit, and audit Persona grants (subject × resource × capability).",
  },
  {
    key: "cerebro_skill_mention",
    label: "Skill mentions",
    group: "issues",
    description:
      "Enable the /skill trigger in editor inputs. Selecting a skill from the popover inserts a reference link to the skill detail page — no side effect, no skill execution.",
  },
  {
    key: "cerebro_grants",
    label: "Grant control plane",
    group: "permissions",
    description:
      "Enable the Persona grant control plane API and CLI (POST/PATCH/DELETE /api/workspaces/{id}/grants and `multica grant` commands).",
  },
  {
    key: "cerebro_tool_policy",
    label: "Unified tool permissions",
    group: "permissions",
    description:
      "Enable the capability catalog on agent and runtime pages: one flat, filterable list of every tool (Tool · Class · Side effect · Decision · Resolved by), narrowed by combinable class / side-effect / decision filters + search, with one editable decision pill per row and a mobile card layout. Backed by the four-layer Runtime › Agent › Group › User chain. GET /api/workspaces/{id}/tool-policy (member) + PUT/DELETE (admin/owner). FIR-2284 (redesign of FIR-2230).",
  },
  {
    key: "cerebro_runtime_tool_grant_ui",
    label: "Tool access grants (groups & people)",
    group: "permissions",
    description:
      "Show the grant editor on the runtime page even when the unified tool-policy table is on: attach a group or a person to a specific tool so that runtime exposes it to them. This is the grant layer (an allow-list of who a tool is opened for) — the layer that silently gave a user zero tools when no UI could set it (FIR-426 / Saga). Reads GET and writes POST/DELETE /api/runtimes/{id}/tools/{tool}/groups|users. Default OFF.",
  },
  {
    key: "cerebro_simple_tool_policy",
    label: "Simple tool permissions",
    group: "permissions",
    description:
      "Show the simplified, user-facing tool permission table on the agent Tools tab: one Allow/Ask/Block toggle per tool, grouped into Read · Execute · Fetch · Destructive. Reuses the cerebro_tool_policy data layer — writes the agent layer only. The rich Effective-chain table stays behind cerebro_tool_policy as a power-view. FIR-2358.",
  },
  {
    key: "cerebro_test_as_user",
    label: "Test as user",
    group: "permissions",
    description:
      "Show the 'Test as user' entry in the profile menu: pick a user + an agent and see how every tool resolves for that combination — the same answer as the tool-policy explain CLI, with the user's real groups resolved automatically. The feature is locked behind the tools:test-as-user permission (default Allow for the workspace owner only), so turning this flag on exposes nothing to members who lack the permission. FIR-1771.",
  },
  {
    key: "cerebro_web_fetch_policy",
    label: "Web fetch URL policy",
    group: "permissions",
    description:
      "Let workspace admins control which URLs agents may fetch with the web_fetch tool. Choose a mode — allow-list (only listed hosts) or disallow-list (everything except listed hosts) — and manage the host rules (github.com, *.github.com). The active list is shown to agents so they can explain to the user when a host is blocked. When off, the legacy hardcoded allow-list applies. Default ON. TECH-3522.",
  },
  {
    key: "cerebro_platform_capabilities",
    label: "Platform actions in permissions",
    group: "permissions",
    description:
      "Add the Multica platform actions to the tool-policy table alongside reported runtime tools: create/edit/delete issues, comments, sub-issues, autopilots, artifacts, and the management of agents, runtimes, groups, grants, and projects. Each becomes a settable Allow/Ask/Deny row on every layer (workspace › runtime › agent › group › user). Actions governed elsewhere (membership ACL, daemon token, webhook secret) are listed but marked as managed externally. Catalog is code-owned (server platformcatalog package, traceable to permguard/inventory.json). FIR-2594 phase 1.",
  },
  {
    key: "cerebro_approvals",
    label: "Approval inbox",
    group: "permissions",
    description:
      "Enable the approval inbox at /:workspace/approvals — when the permission engine returns needs_approval, the ask lands here for a human to approve, reject, or delegate, with an audit trail per decision. Owner/admin only. FIR-2131.",
  },
  {
    key: "cerebro_move_comment_to_subissue",
    label: "Move comment thread to sub-issue",
    group: "issues",
    description:
      "Show a 'Move to sub-issue' action on root comments. Lifts the thread (root + replies) into a new sub-issue and leaves a 'Moved to MUL-NN' breadcrumb on the original comment. JEH-1309.",
  },
  {
    key: "cerebro_move_comment_to_thread",
    label: "Move comments to a new thread",
    group: "issues",
    description:
      "Add a 'Reply in new thread' action on comments. Enters a select mode where you pick comments in the thread and lift them into a new thread on the same issue; each moved comment is left as a breadcrumb linking to the new thread. JEH-2488.",
  },
  {
    key: "cerebro_agent_passes",
    label: "Agent passes admin",
    group: "permissions",
    description:
      "Enable the workspace agent-pass admin page at /:workspace/agent-passes — issue, list, and revoke agent passes (machine-readable mandates that scope what an agent may do on an issue). Owner/admin only. JEH-1731.",
  },
  {
    key: "cerebro_skill_ownership",
    label: "Skill ownership & change requests",
    group: "permissions",
    description:
      "Surface ownership, approvers, version history, change-request review (approve/reject with diff), and forking on the skill detail page. Owner / approvers / workspace admins can manage; everyone else can open a change request. Off hides the governance panel and the fork action. JEH-216.",
  },
  {
    key: "cerebro_skill_learning",
    label: "Skill self-learning",
    group: "permissions",
    description:
      "Let agent runs record observations and have a background sweeper (every 6h) turn repeated patterns (>=10 runs, >=70% consistency) into additive skill change requests automatically. Proposals only add suggestions — never rewrite — and wait as pending change requests for the skill owner to approve or reject via the normal governance flow. Off keeps observation recording and the proposal engine inert for this workspace. TECH-3077.",
  },
  {
    key: "cerebro_references",
    label: "Issue references",
    group: "issues",
    description:
      "Show the references section on issue detail — link GitHub PRs and other external objects to an issue, with live updates and a registry-driven 'add reference' dialog. JEH-838b.",
  },
  // CEREBRO-PATCH(agent-avatar-generate): JEH-1563 AI avatar generation feature flag.
  {
    key: "cerebro_agent_avatar",
    label: "AI agent avatar generation",
    group: "agents",
    description:
      "Show a 'Generate AI avatar' button in the agent creation dialog. Uses the Firtal Data Registry AI Gateway with a Scandinavian-appearance prompt. Requires FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL and FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY.",
  },
  {
    key: "cerebro_private_agent_requests",
    label: "Private agent run requests",
    group: "permissions",
    description:
      "Show private agents you don't own as visible-but-locked in the agent list and @-picker (name + description only). Tagging one no longer silently drops — it sends a run-request to the agent's owner, who can run it from their inbox. The owner stays in control; server-side foreign-trigger blocking is unchanged. FIR-2385.",
  },
  {
    key: "cerebro_agent_surface_visibility",
    label: "Agent surface visibility",
    group: "permissions",
    description:
      "Let an agent's owner hide a personal agent from non-owner members on specific discovery surfaces — agent lists & assignee picker, @-mention autocomplete, chat picker, channel pickers — or all of them at once ('Skjult'). The agent still appears on public issues it already participates in. Owner + workspace admins always see it. Off makes every agent visible everywhere (legacy). TECH-3670.",
  },
  {
    key: "cerebro_date_reminders",
    label: "Start / due date reminders",
    group: "issues",
    description:
      "Notify the assignee when an issue's start or due date arrives (the day-of, in their timezone). Delivery follows each user's per-channel notification preferences (inbox / mobile push / desktop). Off hides the settings rows. FIR-2412.",
  },
  {
    key: "cerebro_agent_trigger_permissions",
    label: "Agent-start permissions",
    group: "permissions",
    description:
      "Add a friendly 'Agent-start' tab under Settings → Permissions to control who may start (trigger) an agent they don't own — a workspace-wide default plus a per-agent override (Allow / run-request / owner-only). Reuses the tool-policy engine; server enforcement is unchanged when off. FIR-2409.",
  },
  {
    key: "cerebro_image_reload",
    label: "Reload stalled images",
    group: "workspace",
    description:
      "On a document page showing an image, recover when the image stops mid-load: a loading indicator while it fetches, up to two automatic retries on failure, and a manual \"Reload image\" button so you can try again without reloading the whole page. Off restores the plain image with no retry. FIR-1673.",
  },
  {
    key: "cerebro_comment_reminders",
    label: "Comment reminders",
    group: "issues",
    description:
      "Add a 'Remind me' action to the comment menu. Sets a personal reminder that points at that specific comment; when it fires, the inbox opens the issue and scrolls straight to the comment. The reminder text is suggested from the comment and editable before saving. Inbox-only by default (per-channel push stays opt-in). Off hides the action and the server rejects comment-referencing reminders. FIR-2641.",
  },
  {
    key: "cerebro_comment_target_guard",
    label: "Require a recipient on agent comments",
    group: "issues",
    description:
      "Reject an agent-authored comment that addresses no recipient — it must point at a person, an agent, or a squad. A bare issue link (e.g. MUL-123) no longer counts: it points at a case, not a person, so it never satisfies the rule. Member comments are never affected. Off restores the prior behaviour (agent comments with no recipient allowed). FIR-2674.",
  },
  {
    key: "cerebro_comment_target_guard_wakeup_exempt",
    label: "Exempt agents with a scheduled wakeup",
    group: "issues",
    description:
      "Sub-setting of 'Require a recipient on agent comments'. When on, an agent that has already scheduled an active wakeup on this issue may post without naming a recipient — the wakeup is the follow-up action, so a human tag is not also required. Only the recipient requirement is waived; the sub-issue checks still apply. Requires 'Require a recipient on agent comments' to be on. Off keeps every agent comment subject to the recipient rule. TECH-3761.",
  },
  {
    key: "cerebro_firtal_welcome",
    label: "Firtal-branded welcome page",
    group: "onboarding",
    description:
      "Replace the upstream onboarding flow with a Firtal-branded welcome page for new members: desktop install guide (with hard-gate modal), PWA install guide (iOS Safari + Android Chrome), members documentation link, and a bug-melding button that opens the Multica support workspace at multica.firtal.com. Each member is shown the page once — completion is tracked client-side. FIR-2490.",
  },
  {
    key: "cerebro_disable_onboarding",
    label: "Disable upstream onboarding",
    group: "onboarding",
    description:
      "Turn off Multica's generic onboarding (questionnaire + 'create workspace') on the fork. Invited members who already have a workspace go straight in; anyone without a workspace lands on the create-workspace page, which always shows a Log out / switch-account button (and a 'no workspace' notice when workspace creation is disabled). This closes the desktop trap where a user signed into the wrong Google account had only 'Create workspace' and no way out. Off restores the upstream onboarding flow. FIR-252.",
  },
  {
    key: "cerebro_duplicate_check_on_create",
    label: "Find similar at create",
    group: "issues",
    description:
      "When composing a new issue, show up to 3 similar open issues with an LLM-judged verdict (duplicate / related) so the user can open the existing sag or create the new one as a sub-issue. Off restores the upstream create flow. Requires the Firtal AI Gateway credentials. FIR-2504.",
  },
  {
    key: "cerebro_google_identity",
    label: "Google Workspace identity",
    group: "permissions",
    description:
      "Adds an Auth & Permissions tab to workspace settings: owner/admin can list email domains that auto-provision into this workspace on first Google login, and pick the default role new members get. Off hides the tab and disables the auto-membership hook. FIR-2523.",
  },
  {
    key: "cerebro_workspace_logo",
    label: "Workspace logo",
    group: "workspace",
    description:
      "Let owners/admins upload a workspace logo in Settings → General. The logo replaces the letter tile in the sidebar, switcher and breadcrumbs, the browser-tab favicon on web, and the dock/window icon in the desktop app. Off hides the uploader and keeps the default icons. FIR-2580.",
  },
  {
    key: "cerebro_sprints",
    label: "Project sprints",
    group: "workspace",
    description:
      "Turn a project into a sprint container: per-project sprint settings (duration, start-day, lead days for auto-create, move-incomplete), a Sprints tab on the project page, a sprint picker in the issue sidebar, and a daily sweeper that creates the next sprint, moves incomplete issues, and clones recurring tasks. Every period (lead days, duration) is a setting — no hardcoded values. FIR-2666.",
  },
  {
    key: "cerebro_comment_chapters",
    label: "Comment sessions",
    group: "workspace",
    description:
      "Wrap an issue's comment timeline in named sessions with a status chip, a Start fresh action, and a Handoff brief. OFF — the phase-1 model was unsound: comments were never linked to a session, so sessions rendered empty and the actions did nothing. Being rebuilt around threads=sessions (FIR-1741). Off renders the plain flat comment timeline. FIR-1704. (Internal key kept as cerebro_comment_chapters so existing overrides keep working.)",
  },
  {
    key: "cerebro_session_activity_fold",
    label: "Session activity fold",
    group: "workspace",
    description:
      "Within each session, collapse all activity events (status changes, task started/failed/completed, linked PRs, attachments) into a single foldable \"Activity\" section so comments stay prominent. No effect unless Comment sessions is on. FIR-1769 (P2).",
  },
  {
    key: "cerebro_session_context_hairline",
    label: "Session context hairline",
    group: "workspace",
    description:
      "A whisper-subtle context-window hairline on the active session that warms as the session grows, plus a single \"almost full → Start fresh\" nudge. The fill is an approximate estimate of the session's content size against a 200k window — not exact per-run usage. No effect unless Comment sessions is on. FIR-1769 (P3).",
  },
  {
    key: "cerebro_cli_runs_tab",
    label: "CLI runs tab",
    group: "workspace",
    description:
      "Show the \"CLI runs\" tab on an issue — the list of local Claude Code / CLI work-sessions started from a terminal (start, stop, resume, fork, transcript). This was previously the \"Sessions\" tab; renamed to avoid colliding with comment sessions. OFF by default — hidden unless a workspace opts in. FIR-1839.",
  },
  {
    key: "cerebro_recurring_issues",
    label: "Recurring issues",
    group: "workspace",
    description:
      "Let users mark an individual issue as recurring (frequency, on-status-change trigger, create-new-task, recur-forever, update-status-to, sync-to-due-date). When the issue reaches its trigger status, a sweeper spawns the next occurrence with a fresh due date and the same data (assignee, text, labels, attachments) and chains the rule onto the new issue. TECH-3064.",
  },
  {
    key: "cerebro_issue_date_times",
    label: "Start/due time of day",
    group: "workspace",
    description:
      "Add an optional time-of-day next to an issue's Start date and Due date, plus an opt-in switch to auto-start the assigned agent on that date+time. Auto-start is off by default and requires a time chosen in the same control; a workspace default (below) sets the starting point for new issues. A due time with auto-start off still fires the due reminder at that moment. Leaving a time empty keeps the date behaving exactly as before. Off hides the controls. FIR-1597.",
  },
  {
    key: "cerebro_saved_filters",
    label: "Saved filters",
    group: "workspace",
    description:
      "Let users save the current issue filter as a named, personal filter and recall it with one click from the Filter menu — across the Issues list, My Issues, project view and member/agent panels. Personal filters only for now; sharing with colleagues/groups/the whole team and the group permission for who may create shared filters land in later phases. Off hides the Saved filters section and the Save action. FIR-1659.",
  },
  {
    key: "cerebro_date_filter_v2",
    label: "Date filter v2 (reference-aligned)",
    group: "workspace",
    description:
      "Replace the My Issues date filter with the reference-aligned builder: each condition is a Field / Operator / Value row with a searchable value list — Today/Yesterday/Tomorrow, calendar This/Last/Next week, month, quarter and year, Overdue, Today & earlier, Later than today, dynamic Last/Next N days/weeks/months/years, exact On / Before / After / Range, and Is set / Is not set. Off keeps the previous stacked date submenu. FIR-1812.",
  },
  {
    key: "cerebro_capability_drift_watcher",
    label: "Capability drift watcher",
    group: "agents",
    description:
      "Periodically scan each agent for capability drift — a tool it actually used (observed access) that its declared policy does not allow (blocked or unmapped). When drift is found, alert the workspace owners/admins in their inbox with the agent and the offending tools. Read-only and off by default; turn it on to get proactive alerts instead of only seeing drift on the Capabilities tab. TECH-3738 Bid C.",
  },
  {
    key: "cerebro_note_types",
    label: "Note types",
    group: "workspace",
    description:
      "Reusable note templates with recurrence, for business reviews. Create a note type with a fixed template plus a behaviour: one rolling document with the newest section prepended each period, or a fresh note per period in a folder. A daily sweeper materialises scheduled types (weekly/monthly/quarterly) and a 'run now' action covers off-cycle reviews. Off hides the Note types admin in Documents and skips the sweeper. TECH-3511.",
  },
  {
    key: "cerebro_skill_folders",
    label: "Skill folders",
    group: "workspace",
    description:
      "Organise skills into folders (with sub-folders) on the Skills page. A folder sidebar lets you create, rename and delete folders and file skills into them; off hides the sidebar and shows the flat list. FIR-1412.",
  },
  {
    key: "cerebro_autopilot_folders",
    label: "Autopilot folders",
    group: "workspace",
    description:
      "Organise autopilots into folders (with sub-folders) on the Autopilots page. A folder sidebar lets you create, rename and delete folders and file autopilots into them; off hides the sidebar and shows the flat list. FIR-1412.",
  },
  {
    key: "cerebro_pdf_inline_render",
    label: "Inline PDF rendering",
    group: "workspace",
    description:
      "Render uploaded PDFs in Documents and the attachment viewer using the browser's native PDF view (scroll, zoom, search) instead of dumping the extracted text. Off restores the extracted-text view. FIR-2661.",
  },
  {
    key: "cerebro_projects",
    label: "Projects sidebar entry",
    group: "workspace",
    description:
      "Show the collapsible Projects entry in the workspace sidebar (project list, nested tree, and the 'New Project' shortcut). Off hides the entry entirely — projects can still be reached via direct URL and the Projects page. TECH-2880.",
  },
  {
    key: "cerebro_channel_create_restricted",
    label: "Restrict who can create channels",
    group: "permissions",
    description:
      "When on, creating a named channel (POST /api/channels with kind='channel') requires workspace owner or admin role; members get 403. DMs are never gated. Off restores today's behaviour where any workspace member or agent can create a channel. FIR-2660.",
  },
  {
    key: "cerebro_firtal_registry_allowlist_ui",
    label: "firtal_registry allowlist UI",
    group: "agents",
    description:
      "Show Konfigurer on the agent Tools tab for firtal_registry so admins can pick which data sources the agent may access. TECH-2925.",
  },
  {
    key: "cerebro_quick_create_version_autoswitch",
    label: "Auto-switch to manual on stale agent CLI",
    group: "issues",
    description:
      "In the \"Create with agent\" modal, when the picked agent's daemon runs a multica CLI below the quick-create minimum, automatically switch to manual create (carrying the typed prompt, project, and parent over) instead of leaving the user on a warning banner with Create disabled. Off restores the warning-only behaviour. FIR-33.",
  },
  {
    key: "cerebro_interactive_terminal",
    label: "Interactive terminal",
    group: "agents",
    description:
      "Enable the per-runtime presentation_mode toggle and the in-app xterm.js terminal panel. Runtimes flipped to 'interactive' stream a live shell to the Multica UI so the user can watch and take over an agent session.",
  },
  {
    key: "cerebro_inbox_thread_split",
    label: "Split channel threads into their own inbox rows",
    group: "inbox",
    description:
      "On a channel or DM, when someone replies inside a thread, surface that thread as its own inbox row (deep-linking into the thread) instead of folding the reply into the single channel row where it is easy to miss. Only threads with unread replies get a row. FIR-1854.",
  },
  {
    key: "cerebro_child_done_notify_parent",
    label: "Notify parent when sub-issue is done",
    group: "inbox",
    description:
      "Post a system comment on the parent issue and wake its assignee when a sub-issue transitions to done. Off silences the notification for the effective flag scope. TECH-3006.",
  },
  {
    key: "cerebro_child_in_review_notify_parent",
    label: "Notify parent when sub-issue is in review",
    group: "inbox",
    description:
      "Post a system comment on the parent issue and wake its assignee when a sub-issue transitions to in_review. Off silences the notification for the effective flag scope. TECH-3006.",
  },
  {
    key: "cerebro_child_blocked_notify_parent",
    label: "Notify parent when sub-issue is blocked",
    group: "inbox",
    description:
      "Post a system comment on the parent issue and wake its assignee when a sub-issue transitions to blocked. Off silences the notification for the effective flag scope. TECH-3006.",
  },
  {
    key: "cerebro_sub_issue_no_owner_mention",
    label: "Block on-behalf-of user @mention on sub-issues",
    group: "issues",
    description:
      "Reject an agent comment on a sub-issue that @mentions the user the task was started for (on-behalf-of user) directly. Agents must post status on the parent issue instead of mentioning that user from a sub-issue. Requires cerebro_comment_target_guard to be on. TECH-3099.",
  },
  {
    key: "cerebro_sub_issue_require_agent_tag",
    label: "Require parent-agent tag on sub-issues",
    group: "issues",
    description:
      "Reject an agent comment on a sub-issue that mentions no agent at all. Forces the agent to tag the parent agent (mention://agent/…) so it stays in the loop. Requires cerebro_comment_target_guard to be on. TECH-3099.",
  },
  {
    key: "cerebro_sub_issue_no_split_session",
    label: "Block split-session across parent and sub-issue",
    group: "issues",
    description:
      "Reject an agent comment on a sub-issue when the same task session (X-Task-ID) has already posted on the parent issue, preventing a single conversation from being split across both. Requires cerebro_comment_target_guard to be on. TECH-3099.",
  },
  {
    key: "cerebro_display_currency",
    label: "Display currency",
    group: "workspace",
    description:
      "Let a workspace show cost in a chosen display currency (USD, DKK, EUR). Cost is always stored in USD and converted at display time with a cached daily rate; a Currency settings tab picks the workspace currency. Off shows raw USD everywhere as before. FIR-40.",
  },
  // TECH-3108: workspace connection registry feature flag.
  {
    key: "cerebro_connections",
    label: "Workspace connections",
    group: "agents",
    description:
      "Enable the Connections settings tab where admins can register API and MCP endpoints (external or internal Sliplane paths) available to all runtimes, with per-layer tool-policy permissions. TECH-3108.",
  },
  // TECH-3196: Agent Vault per-agent secret brokering.
  {
    key: "cerebro_agent_vault",
    label: "Agent Vault secret brokering",
    group: "permissions",
    description:
      "Broker per-agent access to secrets via Infisical Agent Vault over the internal path: the backend swaps a placeholder for the real credential on the way out, so an agent uses a secret without ever holding it. An admin-controlled table sets which key-boxes each agent may reach. TECH-3196.",
  },
  // TECH-3491: per-device drafts for the comment / channel / DM composers.
  {
    key: "cerebro_comment_drafts",
    label: "Save unsent messages as drafts",
    group: "issues",
    description:
      "Keep what you have typed in a comment, channel message, DM, or thread reply if you navigate away, reload, or the editor scrolls out of view — it reappears when you come back. Saved on this device only (not synced across devices). A small \"Kladde gemt\" hint shows when a draft is stored. Off restores the old behaviour where an unsent message is lost on navigate. TECH-3491.",
  },
  // TECH-3582: Workspace copy console — one-time workspace merge tool.
  {
    key: "cerebro_workspace_copy",
    label: "Workspace copy console",
    group: "workspace",
    description:
      "Add a \"Workspace copy\" tab to Settings (owners/admins only) for merging one workspace into another: pick a target workspace, then copy individual issues, channels, projects, agents, chats, or autopilots into it. Copies are non-destructive — the source workspace is never changed. Off hides the tab. TECH-3582.",
  },
  // FIR-recent-chats: recent chat list on the inbox new-chat screen.
  {
    key: "cerebro_chat_recent_list",
    label: "Recent chats on new-chat screen",
    group: "inbox",
    description:
      "Show the 3 most recent chat sessions at the top of the inbox new-chat screen, with a \"See earlier\" expander to scroll through and resume older conversations in place. Off keeps the plain empty state.",
  },
  // FIR-2563: per-workspace approval gate toggle.
  {
    key: "cerebro_approval_gate",
    label: "Tool approval enforcement",
    group: "permissions",
    description:
      "When on, the server enforces the per-tool Allow / Ask / Block policy for every agent tool call — tools marked Ask route to the approval inbox and block until a human approves or rejects. Turning this off lets all tool calls through for this workspace without an inbox ask, even when the server gate is active. Requires the server's CEREBRO_APPROVAL_GATE_ENABLED flag to have any effect. FIR-2563.",
  },
  // TECH-3176: agent wakeup scheduling — one toggle per trigger type.
  {
    key: "cerebro_wakeup_time",
    label: "Wakeup: time-based",
    group: "agents",
    description:
      "Let agents schedule a time-based wakeup (fire at a specific moment) so they re-enter an issue later. Off blocks new time wakeups and stops any pending ones from firing for this workspace; turning it back on lets pending ones resume. TECH-3176.",
  },
  {
    key: "cerebro_wakeup_issue_status",
    label: "Wakeup: on issue status",
    group: "agents",
    description:
      "Let agents schedule a wakeup that fires when a watched issue reaches a chosen status. Off blocks new status wakeups and stops any pending ones from firing for this workspace; turning it back on lets pending ones resume. TECH-3176.",
  },
  {
    key: "cerebro_wakeup_github_ci",
    label: "Wakeup: on GitHub CI",
    group: "agents",
    description:
      "Let agents schedule a wakeup that fires on a GitHub pull-request / CI update for a watched issue. Off blocks new CI wakeups and stops any pending ones from firing for this workspace; turning it back on lets pending ones resume. TECH-3176.",
  },
  {
    key: "cerebro_wakeup_bar",
    label: "Scheduled wakeup banner",
    group: "issues",
    description:
      "Show a prominent orange banner at the top of an issue when it has a pending agent wakeup, mirroring the running-agent banner: a live countdown for time-based wakeups, the watched status for status wakeups, and a CI label for GitHub-CI wakeups, each with a cancel button. Multiple pending wakeups fold into one expandable banner. Off keeps the wakeups visible only in the sidebar list. FIR-1521.",
  },
  {
    key: "cerebro_activity_wakeup_dot",
    label: "Scheduled wakeup dot on lists & boards",
    group: "issues",
    description:
      "Show a small orange clock dot on issue list rows and board cards when the issue has a pending agent wakeup, stacked next to the running-agent indicator so a row can show both at once. Click the dot to expand a list of every scheduled run with a live countdown and a cancel button. Off keeps lists and boards showing only the running-agent indicator. FIR-1521.",
  },
  // TECH-3173: local-runtime per-tool enforcement, staged from settings.
  {
    key: "cerebro_local_tool_policy",
    label: "Tool enforcement on local runtimes",
    group: "permissions",
    description:
      "Extend the per-tool Allow / Ask / Block policy to agents running on LOCAL CLI runtimes (Claude, Codex, Cursor, Gemini), which otherwise bypass the gateway gate entirely. When on, the daemon resolves each tool call through the SAME tool-policy chain and approval inbox as the gateway. Off by default — turning it on starts in observe-only mode (see 'Enforce on local runtimes'). No restart needed. TECH-3173.",
  },
  {
    key: "cerebro_local_tool_policy_enforce",
    label: "Enforce on local runtimes (else observe-only)",
    group: "permissions",
    description:
      "Only matters when 'Tool enforcement on local runtimes' is on. Off = observe-only dry run: the daemon resolves every tool call and logs what an enforce WOULD block, but allows everything through (safe to watch before committing). On = enforce: Allow proceeds, Block stops, Ask routes to the approval inbox and blocks until a human decides. Default off so you can watch the would-block stream first. TECH-3173.",
  },
  {
    key: "cerebro_policy_cel",
    label: "Expression conditions on rules (CEL)",
    group: "permissions",
    description:
      "Let a per-tool rule carry a CEL expression as its WHEN condition, for genuine dynamics that the structured terms (host allowlist, action list) can't express — for example 'only during business hours'. Off by default: only the structured conditions apply, and a rule that carries an expression stays inert (it fails closed, never silently allows). On wires the expression evaluator into both the gateway and local-runtime gates. No restart needed. FIR-1609.",
  },
  {
    key: "cerebro_credential_chain_grant",
    label: "Grant credentials from the tool-policy chain",
    group: "permissions",
    description:
      "Let an explicit Allow rule in the unified per-tool policy GRANT access to a credential, instead of only ever tightening it. This makes the one policy chain the place to grant secret access, so the old separate grant store can eventually be retired. Off by default and fail-safe: with no explicit rule a credential stays deny-by-default exactly as today, so turning it off can never widen who can reveal a secret. Only flip on once existing grants have been migrated onto the chain 1:1. FIR-1609 Phase 7.",
  },
];

/** Flags belonging to a group, in their CEREBRO_FLAGS display order. */
export function flagsForGroup(group: CerebroFlagGroupKey): CerebroFlagDefinition[] {
  return CEREBRO_FLAGS.filter((flag) => flag.group === group);
}
