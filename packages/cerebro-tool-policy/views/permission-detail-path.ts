// FIR-3199: one route contract for web and desktop permission-detail links.
export function permissionDetailPath(
  workspaceSlug: string,
  toolKey: string,
): string {
  return `/${encodeURIComponent(workspaceSlug)}/cerebro/permissions/${encodeURIComponent(toolKey)}`;
}
