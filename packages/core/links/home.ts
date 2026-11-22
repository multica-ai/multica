import type { WorkspaceLink } from "../types/workspace-link";
import { paths } from "../paths/paths";
import { validateLinkUrl } from "./url";

export function findHomeLink(links: WorkspaceLink[]): WorkspaceLink | undefined {
  return links.find((link) =>
    link.isHome === true &&
    link.openMode === "embed" &&
    validateLinkUrl(link.urlTemplate, !!link.projectId) === null,
  );
}

export function workspaceLandingPath(slug: string, links: WorkspaceLink[]): string {
  const home = findHomeLink(links.filter((link) => !link.projectId));
  const workspace = paths.workspace(slug);
  return home ? workspace.link(home.id) : workspace.issues();
}
