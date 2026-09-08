const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export interface IssueDeepLink {
  issueId: string;
  workspaceId: string;
  commentId?: string;
}

/**
 * Parse the only resource route accepted from the multica: protocol.
 * Keeping this boundary narrow prevents an external URL from injecting an
 * arbitrary renderer path while still allowing stable UUIDs to be resolved
 * to the workspace's current slug after authentication.
 */
export function parseIssueDeepLink(value: string): IssueDeepLink | null {
  try {
    const url = new URL(value);
    if (
      url.protocol !== "multica:" ||
      url.hostname !== "issue" ||
      url.username !== "" ||
      url.password !== "" ||
      url.port !== "" ||
      url.hash !== ""
    ) {
      return null;
    }

    const match = /^\/([^/]+)$/.exec(url.pathname);
    if (!match) return null;

    const issueId = decodeURIComponent(match[1]);
    const workspaceIds = url.searchParams.getAll("workspace");
    const commentIds = url.searchParams.getAll("comment");
    const allowedParams = new Set(["workspace", "comment"]);
    for (const key of url.searchParams.keys()) {
      if (!allowedParams.has(key)) return null;
    }
    if (workspaceIds.length !== 1 || commentIds.length > 1) return null;

    const workspaceId = workspaceIds[0];
    const commentId = commentIds[0];
    if (!UUID_PATTERN.test(issueId) || !UUID_PATTERN.test(workspaceId)) {
      return null;
    }
    if (commentId !== undefined && !UUID_PATTERN.test(commentId)) return null;

    return {
      issueId: issueId.toLowerCase(),
      workspaceId: workspaceId.toLowerCase(),
      ...(commentId ? { commentId: commentId.toLowerCase() } : {}),
    };
  } catch {
    return null;
  }
}
