/**
 * Single source of truth for the cerebro fork's feature flags.
 *
 * Defaults are held in TypeScript (no migration when a new flag ships).
 * Server-side persistence stores only overrides — flags toggled away from
 * default for a given (workspace, user) pair.
 */

export type CerebroFlagKey =
  | "cerebro_inbox"
  | "cerebro_access_control"
  | "cerebro_members_admin"
  | "cerebro_sandbox_ui"
  | "cerebro_mcp_guide";

/**
 * Default value for each flag. Applied at read time when no override exists.
 * All cerebro features default to enabled — opt-out, not opt-in.
 */
export const CEREBRO_FLAG_DEFAULTS: Record<CerebroFlagKey, boolean> = {
  cerebro_inbox: true,
  cerebro_access_control: true,
  cerebro_members_admin: true,
  cerebro_sandbox_ui: true,
  cerebro_mcp_guide: true,
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
    key: "cerebro_inbox",
    label: "Cerebro inbox",
    description:
      "Use the cerebro-fork inbox UI in place of upstream multica's inbox.",
  },
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
];
