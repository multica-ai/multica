// Nav items reference WorkspacePaths method names so they can be resolved
// against the current workspace slug at render time (see AppSidebar body).
// Only parameterless paths are valid nav destinations.
export type SidebarNavKey =
  | "inbox"
  | "chat"
  | "myIssues"
  | "issues"
  | "projects"
  | "autopilots"
  | "agents"
  | "squads"
  | "usage"
  | "runtimes"
  | "skills"
  | "settings";

// Static schema (key only) — labels resolved at render via useT("layout"),
// icons derived from the destination path via routeIconForPath.
export type SidebarNavLabelKey =
  | "inbox"
  | "chat"
  | "my_issues"
  | "issues"
  | "projects"
  | "autopilots"
  | "agents"
  | "squads"
  | "usage"
  | "runtimes"
  | "skills"
  | "settings";

export type SidebarNavItem = {
  key: SidebarNavKey;
  labelKey: SidebarNavLabelKey;
};

// Nav icons are NOT declared here: they are derived from each item's
// destination path at render time, so the sidebar and the desktop tab bar
// always agree. See route-icon-components.tsx.
export const personalNav: SidebarNavItem[] = [
  { key: "inbox", labelKey: "inbox" },
  { key: "chat", labelKey: "chat" },
  { key: "myIssues", labelKey: "my_issues" },
];

export const workspaceNav: SidebarNavItem[] = [
  { key: "issues", labelKey: "issues" },
  { key: "projects", labelKey: "projects" },
];

// Agents live under the Runtimes item (see AgentsRuntimesPage). Autopilot,
// Squads, and Analytics moved here from the Workspace group.
export const configureNav: SidebarNavItem[] = [
  { key: "runtimes", labelKey: "runtimes" },
  { key: "autopilots", labelKey: "autopilots" },
  { key: "squads", labelKey: "squads" },
  { key: "usage", labelKey: "usage" },
  { key: "skills", labelKey: "skills" },
  { key: "settings", labelKey: "settings" },
];
