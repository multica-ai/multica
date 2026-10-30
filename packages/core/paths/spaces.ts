/**
 * Workspace "spaces": the top-level groups the app rail switches between.
 *
 * Every workspace page belongs to at most one space, keyed by its route
 * segment (`/{slug}/{segment}/...`). The rail highlights the space of the
 * current route, and the shell decides from the space which panel to show.
 */

export type AppSpaceId =
  | "inbox"
  | "work"
  | "team"
  | "chat"
  | "autopilots"
  | "settings";

/** Settings lives in the rail footer rather than the space list. */
export type RailSpaceId = Exclude<AppSpaceId, "settings">;

/** Spaces in rail order. */
export const RAIL_SPACES: readonly RailSpaceId[] = [
  "inbox",
  "work",
  "team",
  "chat",
  "autopilots",
];

const SPACE_BY_SEGMENT: Record<string, AppSpaceId> = {
  inbox: "inbox",
  "my-issues": "work",
  issues: "work",
  projects: "work",
  views: "work",
  agents: "team",
  squads: "team",
  members: "team",
  skills: "team",
  runtimes: "team",
  usage: "team",
  team: "team",
  chat: "chat",
  autopilots: "autopilots",
  settings: "settings",
};

/**
 * The space a workspace-scoped path or full URL belongs to, or null for
 * routes outside every space (attachment previews, unknown segments).
 */
/**
 * Pages inside a panel space that render their own list column instead of the
 * space panel: the Team page lists agents, channels, squads and members itself.
 */
const OWN_LIST_SEGMENTS: ReadonlySet<string> = new Set(["team"]);

export function pathHasOwnList(path: string): boolean {
  const pathname = path.split(/[?#]/)[0] ?? "";
  return OWN_LIST_SEGMENTS.has(pathname.split("/").filter(Boolean)[1] ?? "");
}

export function spaceForPath(path: string): AppSpaceId | null {
  const pathname = path.split(/[?#]/)[0] ?? "";
  const segment = pathname.split("/").filter(Boolean)[1] ?? "";
  return SPACE_BY_SEGMENT[segment] ?? null;
}
