import type { AgentVisibility } from "../types";

/**
 * Display labels for agent visibility. The DB stores `private` as the value
 * but the UI surface name is "Personal" — better matches what the field
 * actually means now that workspace admins can also assign private agents.
 */
export const VISIBILITY_LABEL: Record<AgentVisibility, string> = {
  workspace: "Workspace",
  private: "Personal",
};

/**
 * Honest descriptions for assignability. The previous "Only you can assign"
 * text was a lie — workspace owners and admins can assign private agents too
 * (server `issue.go:1471-1490`).
 */
export const VISIBILITY_DESCRIPTION: Record<AgentVisibility, string> = {
  workspace: "All members can assign",
  private: "Only you and workspace admins can assign",
};

/** Tooltip suitable for read-only badges on hover/list rows. */
export const VISIBILITY_TOOLTIP: Record<AgentVisibility, string> = {
  workspace: "Workspace — all members can assign",
  private: "Personal — only you and workspace admins can assign",
};

// CEREBRO-PATCH(group-access-locked-tooltip): JEH-1066 — tooltip + toast copy
// shown when an agent is visible but the cerebro group-permission gate denies
// trigger. The same string is shown on the picker lock icon and as the
// trigger-failure toast so users get a consistent answer to "why is this
// agent disabled?" and "why did my trigger fail?".
export const GROUP_ACCESS_LOCKED_TOOLTIP =
  "You don't have group access to this agent — ask a workspace admin to add you to a group that grants access.";

export function visibilityLabel(v: AgentVisibility): string {
  return VISIBILITY_LABEL[v];
}
