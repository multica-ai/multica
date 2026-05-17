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
  | "cerebro_web_push"
  | "cerebro_dashboard"
  | "cerebro_inbox_row_actions"
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
  | "cerebro_move_comment_to_subissue";

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
  cerebro_web_push: true,
  cerebro_dashboard: true,
  cerebro_inbox_row_actions: true,
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
  cerebro_move_comment_to_subissue: true,
};

export interface CerebroFlagDefinition {
  key: CerebroFlagKey;
  label: string;
  description: string;
}

/**
 * Display metadata for the settings UI. Order here is the order shown to
 * the user.
 */
export const CEREBRO_FLAGS: CerebroFlagDefinition[] = [
  {
    key: "cerebro_access_control",
    label: "Cerebro access control",
    description:
      "Enable the cerebro-fork access-control flow (combined restrict + pick) for issues and projects.",
  },
  {
    key: "cerebro_members_admin",
    label: "Cerebro members admin",
    description:
      "Use the cerebro-fork members admin tab with bulk actions and richer filters.",
  },
  {
    key: "cerebro_sandbox_ui",
    label: "Cerebro sandbox UI",
    description:
      "Show the cerebro-fork sandbox panels and developer affordances inside agent runs.",
  },
  {
    key: "cerebro_mcp_guide",
    label: "Cerebro MCP guide",
    description:
      "Show the cerebro-fork MCP setup guide in the runtimes and docs panels.",
  },
  {
    key: "cerebro_channels",
    label: "Channels",
    description:
      "Enable channel-style conversations (kind=channel issues, /channels/{id} route, channel list in inbox).",
  },
  {
    key: "cerebro_web_push",
    label: "Web Push notifications",
    description:
      "Enable browser/PWA push notifications for new inbox items, comments, and mentions.",
  },
  {
    key: "cerebro_dashboard",
    label: "Cerebro dashboard",
    description:
      "Enable the cerebro workspace operations dashboard at /:workspace/dashboard (agent strip, KPI cards, recent tasks).",
  },
  {
    key: "cerebro_inbox_row_actions",
    label: "Inbox row actions",
    description:
      "Show the cerebro inbox row-actions surface: mute, mark-unread, hover menu, mobile swipe gestures, long-press menu, and the `e` keyboard shortcut.",
  },
  {
    key: "cerebro_voice_dictation_enabled",
    label: "Dictation",
    description:
      "Push-to-talk Whisper dictation (hviske-v3) in chat input and other text fields. Requires the cerebro-inference container.",
  },
  {
    key: "cerebro_voice_output_enabled",
    label: "Voice output",
    description:
      "Read assistant replies aloud in Danish via plapre-nano. Per-message read button + global voice mode.",
  },
  {
    key: "cerebro_voice_summary_enabled",
    label: "Voice summary",
    description:
      "When voice mode is on, summarise long replies into spoken-style Danish before reading them aloud. Reduces TTS latency on long answers and keeps the conversation natural in hands-free use.",
  },
  {
    key: "cerebro_autopilot_scopes",
    label: "Autopilot scopes",
    description:
      "Enable scoped autopilots (workspace, personal, group) — gated visibility on top of the workspace-wide default.",
  },
  {
    key: "cerebro_groups_enabled",
    label: "Groups",
    description:
      "Enable workspace groups: named collections of members used by Cerebro features such as scoped resources.",
  },
  {
    key: "cerebro_runtime_pause",
    label: "Runtime pause / resume",
    description:
      "Pause and resume agent runtimes manually or automatically. When a provider returns 429, the runtime auto-pauses until the rate-limit window resets, then resumes interrupted work on its own.",
  },
  {
    key: "cerebro_runtime_accounts",
    label: "Runtime account availability",
    description:
      "Show availability status per account on the runtime detail card: how many runtimes are free, throttled, or paused. Coordinator agents use this to pick the right runtime.",
  },
  {
    key: "cerebro_tasks",
    label: "Tasks page",
    description:
      "Enable the cross-agent tasks page at /:workspace/tasks (full task list with filters and pagination).",
  },
  {
    key: "cerebro_pin_input",
    label: "Pin issue input",
    description:
      "Enable a pin toggle on issue comment and reply inputs that keeps the active input stuck to the bottom of the viewport while scrolling. Issue pages only — channels and DMs are unaffected.",
  },
  {
    key: "cerebro_workflows",
    label: "Workflow engine",
    description:
      "Enable the cerebro workflow engine and the /:workspace/workflows page (data-driven status/trigger rules, builder UI, run log). Server-side execution is additionally gated by the CEREBRO_WORKFLOWS_ENABLED env var.",
  },
  {
    key: "cerebro_persona_permissions",
    label: "Persona permissions",
    description:
      "Enable the workspace permissions admin page at /:workspace/permissions — list, create, edit, and audit Persona grants (subject × resource × capability).",
  },
  {
    key: "cerebro_skill_mention",
    label: "Skill mentions",
    description:
      "Enable the /skill trigger in editor inputs. Selecting a skill from the popover inserts a reference link to the skill detail page — no side effect, no skill execution.",
  },
  {
    key: "cerebro_grants",
    label: "Grant control plane",
    description:
      "Enable the Persona grant control plane API and CLI (POST/PATCH/DELETE /api/workspaces/{id}/grants and `multica grant` commands).",
  },
  {
    key: "cerebro_move_comment_to_subissue",
    label: "Move comment thread to sub-issue",
    description:
      "Show a 'Move to sub-issue' action on root comments. Lifts the thread (root + replies) into a new sub-issue and leaves a 'Moved to MUL-NN' breadcrumb on the original comment. JEH-1309.",
  },
];
