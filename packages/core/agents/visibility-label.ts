import type { AgentVisibility } from "../types";

/**
 * Display labels for agent visibility. The DB stores `private` as the value
 * but the UI surface name is "Personal" — it reads better next to "Workspace"
 * and matches the wording used in the access picker.
 */
export const VISIBILITY_LABEL: Record<AgentVisibility, string> = {
  workspace: "Workspace",
  private: "Personal",
};

/**
 * Descriptions for the visibility CHOICE (create dialog / picker), where
 * "Personal" is submitted as `permission_mode: "private"` — owner-only, with
 * no workspace-admin bypass since MUL-3963 (`canInvokeAgent` in
 * `server/internal/handler/agent_access.go`). The older
 * "…and workspace admins…" copy predates that gate and is no longer true.
 */
export const VISIBILITY_DESCRIPTION: Record<AgentVisibility, string> = {
  workspace: "All members can assign",
  private: "Only you can assign",
};

/**
 * Tooltip suitable for read-only badges on hover / list rows. Worded for an
 * EXISTING agent, where `visibility` is the lossy two-state projection of the
 * permission model: `private` covers both a truly owner-only agent and one
 * shared with specific people, so the copy names the owner's grants rather
 * than promising either extreme. It must not claim workspace admins can
 * assign — admins keep management + view access, not invocation.
 */
export const VISIBILITY_TOOLTIP: Record<AgentVisibility, string> = {
  workspace: "Workspace — all members can assign",
  private: "Personal — only the owner and people they allow can assign",
};

export function visibilityLabel(v: AgentVisibility): string {
  return VISIBILITY_LABEL[v];
}
