import { isReservedSlug, parseTabSubject, type TabSubject } from "@multica/core/paths";

export type DeepLink =
  | { kind: "open"; workspaceSlug: string; subject: TabSubject }
  /** A link into a different Multica server than the one this build talks to. */
  | { kind: "other-instance" };

/**
 * Accepts the web app's "open in app" link (`<scheme>://open/issue?url=<web
 * URL>`, the same shape Desktop handles) and plain web URLs, and resolves them
 * against this build's own server only, so an identifier is never looked up
 * in a workspace it does not belong to.
 */
export function parseDeepLink(value: string, webUrl: string, scheme: string): DeepLink | null {
  let url = parseUrl(value);
  if (!url) return null;
  if (url.protocol === `${scheme}:`) {
    if (url.host !== "open" || url.pathname !== "/issue") return null;
    url = parseUrl(url.searchParams.get("url") ?? "");
    if (!url) return null;
  }
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) return null;

  const app = new URL(webUrl);
  const basePath = app.pathname.replace(/\/+$/, "");
  if (url.origin !== app.origin || !url.pathname.startsWith(`${basePath}/`)) {
    return { kind: "other-instance" };
  }
  const path = url.pathname.slice(basePath.length);
  const workspaceSlug = path.split("/").find(Boolean);
  if (!workspaceSlug || isReservedSlug(workspaceSlug)) return null;
  const subject = parseTabSubject(path + url.search);
  if (subject.kind === "unknown") return null;
  return { kind: "open", workspaceSlug, subject };
}

function parseUrl(value: string): URL | null {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}
