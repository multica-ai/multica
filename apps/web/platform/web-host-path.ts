const TAG_CREATE_WORKSPACE_PATH = "/tag/workspaces/new";

type WebRouter = {
  push(path: string): void;
  replace(path: string): void;
  prefetch(path: string): void;
};

/** Injectable boundary: tests assert document handoffs without faking Next. */
export const documentNavigation = {
  assign(path: string) {
    window.location.assign(path);
  },
  replace(path: string) {
    window.location.replace(path);
  },
};

/** Mount global shared destinations in the browser host that owns them. */
export function toWebHostPath(path: string): string {
  if (
    path === "/workspaces/new" ||
    path.startsWith("/workspaces/new?") ||
    path.startsWith("/workspaces/new#")
  ) {
    return `${TAG_CREATE_WORKSPACE_PATH}${path.slice("/workspaces/new".length)}`;
  }

  // This adapter only receives app-navigation destinations, never API paths.
  // A Workspace-scoped Issues path belongs to the mounted Tag host.
  const workspaceIssues = path.match(/^\/([^/?#]+)\/issues(?:[/?#]|$)/u);
  if (workspaceIssues?.[1] && workspaceIssues[1] !== "tag") {
    return `/tag${path}`;
  }
  return path;
}

/** Resolve the ordinary post-auth landing surface owned by the Tag host. */
export function toWebPostAuthPath(path: string): string {
  const workspaceIssues = path.match(
    /^\/([^/?#]+)\/issues(?<suffix>[?#].*)?$/u,
  );
  if (workspaceIssues?.[1] && workspaceIssues[1] !== "tag") {
    return `/tag/${workspaceIssues[1]}/chat${workspaceIssues.groups?.suffix ?? ""}`;
  }
  return toWebHostPath(path);
}

export function isDocumentHandoffPath(path: string): boolean {
  return path === "/tag" || path.startsWith("/tag/");
}

export function pushWebHostPath(router: WebRouter, path: string): void {
  const destination = toWebHostPath(path);
  if (isDocumentHandoffPath(destination)) {
    documentNavigation.assign(destination);
    return;
  }
  router.push(destination);
}

export function replaceWebHostPath(router: WebRouter, path: string): void {
  const destination = toWebHostPath(path);
  if (isDocumentHandoffPath(destination)) {
    documentNavigation.replace(destination);
    return;
  }
  router.replace(destination);
}

export function prefetchWebHostPath(router: WebRouter, path: string): void {
  const destination = toWebHostPath(path);
  if (!isDocumentHandoffPath(destination)) router.prefetch(destination);
}
