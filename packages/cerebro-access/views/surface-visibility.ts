import type { Agent } from "@multica/core/types";
import type { PermissionContext } from "@multica/core/permissions";

/**
 * TECH-3670 — per-surface agent discovery visibility.
 *
 * A NEW axis, orthogonal to `agent.visibility` (workspace|private, which gates
 * who may ASSIGN/TRIGGER). `agent.surface_visibility` gates where a non-owner
 * MEMBER can DISCOVER the agent: the agent directory + assignee picker
 * ("lists"), @-mention autocomplete ("mention"), the chat/DM picker ("chat"),
 * and channel member pickers ("channels").
 *
 * Owner and workspace owner/admin always see the agent regardless of this
 * field — mirrors the Go gate in agent_surface_visibility_cerebro.go. A
 * missing key defaults to visible; null/undefined = visible everywhere.
 */
export const AGENT_SURFACES = [
  "lists",
  "mention",
  "chat",
  "channels",
] as const;

export type AgentSurface = (typeof AGENT_SURFACES)[number];

/** Human-readable labels for the advanced per-surface toggles. */
export const SURFACE_LABEL: Record<AgentSurface, string> = {
  lists: "Agent lists & assignee picker",
  mention: "@-mention autocomplete",
  chat: "Chat / direct message picker",
  channels: "Channel member pickers",
};

const isAdminLike = (role: PermissionContext["role"]): boolean =>
  role === "owner" || role === "admin";

/** Owner or workspace owner/admin — always sees the agent on every surface. */
function alwaysSees(agent: Agent, ctx: PermissionContext): boolean {
  if (ctx.userId === null) return false;
  if (isAdminLike(ctx.role)) return true;
  return agent.owner_id !== null && agent.owner_id === ctx.userId;
}

/**
 * Whether the current user can see `agent` on `surface`. Returns true (visible)
 * for owner/admin, for a missing/true surface key, and when the field is unset.
 * Defaults to VISIBLE so a missing field or unknown surface never hides an
 * agent that the server still returned (enum/contract drift safety).
 */
export function isAgentVisibleOnSurface(
  agent: Agent,
  surface: AgentSurface,
  ctx: PermissionContext,
): boolean {
  if (alwaysSees(agent, ctx)) return true;
  const map = agent.surface_visibility;
  if (map === null || map === undefined) return true;
  return map[surface] !== false;
}

/** Inverse of {@link isAgentVisibleOnSurface} — convenience for `.filter`. */
export function isAgentHiddenOnSurface(
  agent: Agent,
  surface: AgentSurface,
  ctx: PermissionContext,
): boolean {
  return !isAgentVisibleOnSurface(agent, surface, ctx);
}

/** True when every known surface is explicitly hidden — the "Skjult" preset. */
export function isFullySurfaceHidden(
  map: Record<string, boolean> | null | undefined,
): boolean {
  if (map === null || map === undefined) return false;
  return AGENT_SURFACES.every((s) => map[s] === false);
}

/**
 * The three presets the simple UI offers. `custom` is derived, not stored —
 * it's whatever doesn't match the other two.
 */
export type SurfaceVisibilityPreset = "visible" | "hidden" | "custom";

/** Classify a stored surface_visibility map into one of the presets. */
export function surfaceVisibilityPreset(
  map: Record<string, boolean> | null | undefined,
): SurfaceVisibilityPreset {
  if (map === null || map === undefined) return "visible";
  if (AGENT_SURFACES.every((s) => map[s] !== false)) return "visible";
  if (isFullySurfaceHidden(map)) return "hidden";
  return "custom";
}

/** Build the map for a preset. `visible` → {} (visible everywhere). */
export function presetToSurfaceMap(
  preset: Exclude<SurfaceVisibilityPreset, "custom">,
): Record<AgentSurface, boolean> | Record<string, never> {
  if (preset === "visible") return {};
  return { lists: false, mention: false, chat: false, channels: false };
}
