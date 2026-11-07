const LEGACY_ROUTE_SEGMENTS = new Set([
  "issues",
  "projects",
  "agents",
  "squads",
  "inbox",
  "my-issues",
  "autopilots",
  "runtimes",
  "skills",
  "settings",
  "usage",
]);

export function isLegacyWorkspacePath(path: string): boolean {
  return LEGACY_ROUTE_SEGMENTS.has(path.split(/[/?#]/, 2)[1] ?? "");
}
