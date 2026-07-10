/**
 * Single source of truth for route → icon mapping.
 *
 * Keyed by the URL route segment — the segment at index 1 of a
 * workspace-scoped path `/{slug}/{segment}/...`. Both the sidebar nav
 * (packages/views/layout/app-sidebar.tsx) and the desktop tab bar
 * (apps/desktop/.../components/tab-bar.tsx) resolve their icons from this map,
 * so a route's icon is guaranteed to be identical in both places.
 *
 * Values are icon *names* (not React components) so this module stays
 * React-free and safe to import inside `@multica/core`. The name → component
 * registry lives in `packages/views/layout/route-icon-components.tsx`
 * (`ROUTE_ICON_COMPONENTS`) and MUST cover every name declared here. The
 * `route-icons.consistency` test asserts that invariant.
 */
export type RouteIconName =
  | "Inbox"
  | "MessageSquare"
  | "CircleUser"
  | "ListTodo"
  | "FolderKanban"
  | "Zap"
  | "Bot"
  | "Users"
  | "BarChart3"
  | "Monitor"
  | "BookOpenText"
  | "Settings";

/** Route segment → icon name. Keep aligned with the nav destinations in paths.ts. */
export const ROUTE_ICON_NAMES: Record<string, RouteIconName> = {
  inbox: "Inbox",
  chat: "MessageSquare",
  "my-issues": "CircleUser",
  issues: "ListTodo",
  projects: "FolderKanban",
  autopilots: "Zap",
  agents: "Bot",
  squads: "Users",
  usage: "BarChart3",
  runtimes: "Monitor",
  skills: "BookOpenText",
  settings: "Settings",
};

/** Fallback icon name used when a path's route segment has no explicit entry. */
export const DEFAULT_ROUTE_ICON_NAME: RouteIconName = "ListTodo";

/**
 * Resolve the icon name for a workspace-scoped path.
 *
 * Tab / nav paths are always `/{slug}/{segment}/...`, so the route segment
 * lives at index 1. Returns {@link DEFAULT_ROUTE_ICON_NAME} for unknown or
 * too-short paths.
 */
export function resolveRouteIconName(pathname: string): RouteIconName {
  const segments = pathname.split("/").filter(Boolean);
  return ROUTE_ICON_NAMES[segments[1] ?? ""] ?? DEFAULT_ROUTE_ICON_NAME;
}
