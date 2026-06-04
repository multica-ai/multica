export type MobileIssueLinkTarget = {
  commentId?: string;
  issueId: string;
  workspaceSlug: string;
};

export function buildMobileIssueWebHref({
  baseUrl,
  commentId,
  issueId,
  workspaceSlug,
}: {
  baseUrl: string;
  commentId?: string;
  issueId: string;
  workspaceSlug: string;
}): string {
  const base = baseUrl.replace(/\/$/, "");
  const path = `/${encodeURIComponent(workspaceSlug)}/issues/${encodeURIComponent(issueId)}`;
  if (!commentId) return `${base}${path}`;
  return `${base}${path}?comment=${encodeURIComponent(commentId)}`;
}

export function parseMobileIssueLink(
  href: string,
  allowedBaseUrls: readonly (string | undefined)[],
): MobileIssueLinkTarget | null {
  let parsed: URL;
  try {
    parsed = new URL(href);
  } catch {
    return null;
  }

  if (parsed.protocol === "wujieai-multicam:") {
    const parts = [
      parsed.hostname,
      ...parsed.pathname.split("/").filter(Boolean),
    ].filter(Boolean);
    return parseWorkspaceIssuePath(parts, parsed.searchParams.get("comment"));
  }

  if (!/^https?:$/i.test(parsed.protocol)) return null;

  const allowedOrigins = new Set<string>();
  for (const baseUrl of allowedBaseUrls) {
    if (!baseUrl) continue;
    try {
      allowedOrigins.add(new URL(baseUrl).origin);
    } catch {
      // Ignore invalid runtime config values.
    }
  }
  if (!allowedOrigins.has(parsed.origin)) return null;

  return parseWorkspaceIssuePath(
    parsed.pathname.split("/").filter(Boolean),
    parsed.searchParams.get("comment"),
  );
}

function parseWorkspaceIssuePath(
  parts: string[],
  rawCommentId: string | null,
): MobileIssueLinkTarget | null {
  if (parts.length !== 3 || parts[1] !== "issues") return null;

  const workspaceSlug = safeDecodeURIComponent(parts[0] ?? "");
  const issueId = safeDecodeURIComponent(parts[2] ?? "");
  if (!workspaceSlug || !issueId) return null;

  const commentId = rawCommentId?.trim() || undefined;
  return { workspaceSlug, issueId, commentId };
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
