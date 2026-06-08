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
  | "cerebro_dashboard"
  | "cerebro_inbox_row_actions"
  | "cerebro_inbox_action_grouping"
  | "cerebro_inbox_pinned_filter"
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
  | "cerebro_simple_tool_policy"
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
  // FIR-2412: notify the assignee when an issue's start/due date arrives.
  | "cerebro_date_reminders"
  // FIR-2490: Firtal-branded welcome page for new members (replaces upstream onboarding).
  | "cerebro_firtal_welcome"
  // FIR-2504: show similar open issues + LLM verdict when creating an issue.
  | "cerebro_duplicate_check_on_create"
  // FIR-2523: Auth & Permissions settings tab + Google Workspace auto-membership hook.
  | "cerebro_google_identity"
  // FIR-2580: per-workspace logo (upload + sidebar/breadcrumbs + web favicon + desktop dock icon).
  | "cerebro_workspace_logo"
  // FIR-2666: project sprint feature (sprint settings, auto-create next sprint, recurring tasks).
  | "cerebro_sprints"
  // FIR-2661: render uploaded PDFs inline (native browser PDF view) instead of dumping extracted text.
  | "cerebro_pdf_inline_render"
  // FIR-2641: "Remind me" on a specific comment — reuses the personal reminder engine.
  | "cerebro_comment_reminders"
  // FIR-2674: reject agent comments that mention no target (person, agent, or issue).
  | "cerebro_comment_target_guard"
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
  // Interactive terminal (cerebro-terminal): per-runtime presentation mode + xterm.js panel.
  | "cerebro_interactive_terminal";

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
  cerebro_chat_message_cost: true,
  cerebro_comment_cost: true,
  cerebro_web_push: true,
  cerebro_dashboard: true,
  cerebro_inbox_row_actions: true,
  cerebro_inbox_action_grouping: true,
  cerebro_inbox_pinned_filter: true,
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
  cerebro_simple_tool_policy: true,
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
  // FIR-2412: on by default — the assignee gets an inbox + push reminder when
  // a start/due date arrives. Off hides the settings rows and the UI control.
  cerebro_date_reminders: true,
  // FIR-2490: ON by default for the cerebro fork. New members are routed to
  // the Firtal-branded welcome page (desktop install guide with hard gate, PWA
  // install guide, members docs, bug-melding link to the Multica support
  // workspace) instead of upstream `/onboarding`. Per-user override still
  // lets anyone opt out from the cerebro settings panel.
  cerebro_firtal_welcome: true,
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
  // FIR-2661: ON by default. Uploaded PDFs in Documents and the attachment
  // viewer render in the browser's native PDF view (scroll, zoom, search).
  // Off restores the prior behaviour (extracted-text dump in the attachment
  // viewer; direct file iframe in the document viewer).
  cerebro_pdf_inline_render: true,
  // FIR-2641: ON by default. Adds "Remind me" to the comment menu — a personal
  // reminder that points at one comment and, when it fires, deep-links the
  // inbox back to that comment. Reuses the existing reminder engine (inbox-only
  // by default; per-channel push stays opt-in). Off hides the menu action and
  // the server rejects comment-referencing reminders.
  cerebro_comment_reminders: true,
  // FIR-2674: OFF by default. When on, an agent-authored comment that mentions
  // no target at all (no person, agent, or issue) is rejected by the server
  // with a 422 telling the agent to add one. Members are never affected. Off
  // restores the prior behaviour (comments with no target allowed).
  cerebro_comment_target_guard: false,
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
  cerebro_interactive_terminal: false,
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
    key: "cerebro_web_push",
    label: "Web Push notifications",
    group: "inbox",
    description:
      "Enable browser/PWA push notifications for new inbox items, comments, and mentions.",
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
    key: "cerebro_inbox_action_grouping",
    label: "Inbox group by action",
    group: "inbox",
    description:
      "Add a \"Group by → Action\" option to the inbox that buckets items by what to do next (Act now / Watching / Waiting / Calm) instead of by status. Default grouping for new users; switch it off or pick another grouping from the inbox's Group by menu.",
  },
  {
    key: "cerebro_inbox_pinned_filter",
    label: "Inbox pinned filter",
    group: "inbox",
    description:
      "Add a \"Pinned\" option to the inbox view dropdown that shows only items tied to something you pinned — the item's own issue, that issue's parent, or its project, plus pinned channels and DMs. Off removes the option from the dropdown.",
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
    key: "cerebro_simple_tool_policy",
    label: "Simple tool permissions",
    group: "permissions",
    description:
      "Show the simplified, user-facing tool permission table on the agent Tools tab: one Allow/Ask/Block toggle per tool, grouped into Read · Execute · Fetch · Destructive. Reuses the cerebro_tool_policy data layer — writes the agent layer only. The rich Effective-chain table stays behind cerebro_tool_policy as a power-view. FIR-2358.",
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
    key: "cerebro_comment_reminders",
    label: "Comment reminders",
    group: "issues",
    description:
      "Add a 'Remind me' action to the comment menu. Sets a personal reminder that points at that specific comment; when it fires, the inbox opens the issue and scrolls straight to the comment. The reminder text is suggested from the comment and editable before saving. Inbox-only by default (per-channel push stays opt-in). Off hides the action and the server rejects comment-referencing reminders. FIR-2641.",
  },
  {
    key: "cerebro_comment_target_guard",
    label: "Require a target on agent comments",
    group: "issues",
    description:
      "Reject an agent-authored comment that mentions no target at all — it must point at a person, an agent, or an issue (e.g. MUL-123). An issue link counts and has no side effect, so an agent can always satisfy the rule without waking another agent; member comments are never affected. Off restores the prior behaviour (agent comments with no target allowed). FIR-2674.",
  },
  {
    key: "cerebro_firtal_welcome",
    label: "Firtal-branded welcome page",
    group: "onboarding",
    description:
      "Replace the upstream onboarding flow with a Firtal-branded welcome page for new members: desktop install guide (with hard-gate modal), PWA install guide (iOS Safari + Android Chrome), members documentation link, and a bug-melding button that opens the Multica support workspace at multica.firtal.com. Each member is shown the page once — completion is tracked client-side. FIR-2490.",
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
    key: "cerebro_display_currency",
    label: "Display currency",
    group: "workspace",
    description:
      "Let a workspace show cost in a chosen display currency (USD, DKK, EUR). Cost is always stored in USD and converted at display time with a cached daily rate; a Currency settings tab picks the workspace currency. Off shows raw USD everywhere as before. FIR-40.",
  },
];

/** Flags belonging to a group, in their CEREBRO_FLAGS display order. */
export function flagsForGroup(group: CerebroFlagGroupKey): CerebroFlagDefinition[] {
  return CEREBRO_FLAGS.filter((flag) => flag.group === group);
}
