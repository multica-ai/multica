export type MobileRouteKey =
  | "kanban"
  | "issues"
  | "projects"
  | "inbox"
  | "runtime"
  | "chat"
  | "settings";

export interface MobileNavItem {
  label: string;
  routeKey: MobileRouteKey;
}

export function mobileRoutes(workspaceSlug: string): Record<MobileRouteKey | "root", string> {
  const base = `/${encodeURIComponent(workspaceSlug)}/m`;
  return {
    root: `${base}/issues`,
    kanban: `${base}/kanban`,
    issues: `${base}/issues`,
    projects: `${base}/projects`,
    inbox: `${base}/inbox`,
    runtime: `${base}/runtime`,
    chat: `${base}/chat`,
    settings: `${base}/settings`,
  };
}

export const MOBILE_BOTTOM_NAV_ITEMS = [
  { label: "Kanban", routeKey: "kanban" },
  { label: "Issues", routeKey: "issues" },
  { label: "Projects", routeKey: "projects" },
  { label: "Inbox", routeKey: "inbox" },
  { label: "Settings", routeKey: "settings" },
] as const satisfies readonly MobileNavItem[];

export const MOBILE_DRAWER_NAV_ITEMS = [
  { label: "Runtime", routeKey: "runtime" },
  { label: "Chat", routeKey: "chat" },
] as const satisfies readonly MobileNavItem[];
