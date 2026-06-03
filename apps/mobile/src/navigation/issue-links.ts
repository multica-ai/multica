export type MobileIssueLinkTarget = {
  commentId?: string;
  issueId: string;
  workspaceSlug: string;
};

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

  const parts = parsed.pathname.split("/").filter(Boolean);
  if (parts.length !== 3 || parts[1] !== "issues") return null;

  const workspaceSlug = safeDecodeURIComponent(parts[0] ?? "");
  const issueId = safeDecodeURIComponent(parts[2] ?? "");
  if (!workspaceSlug || !issueId) return null;

  const commentId = parsed.searchParams.get("comment")?.trim() || undefined;
  return { workspaceSlug, issueId, commentId };
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
