import { parseIssueWindowPath } from "./issue-window";

export type IssueDeepLink =
  | { kind: "issue"; workspaceSlug: string; path: string }
  | { kind: "other-instance" };

/** Never interpret an issue identifier against a different server. */
export function parseIssueDeepLink(
  value: string,
  appUrl: string,
): IssueDeepLink | null {
  try {
    const link = new URL(value);
    if (
      link.protocol !== "multica:" ||
      link.host !== "open" ||
      link.pathname !== "/issue"
    ) return null;
    const url = new URL(link.searchParams.get("url") ?? "");
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password
    ) return null;
    const app = new URL(appUrl);
    const basePath = app.pathname.replace(/\/+$/, "");
    if (url.origin !== app.origin || !url.pathname.startsWith(`${basePath}/`)) {
      return { kind: "other-instance" };
    }
    const target = parseIssueWindowPath(
      url.pathname.slice(basePath.length) + url.search + url.hash,
    );
    if (!target) return null;
    return {
      kind: "issue",
      workspaceSlug: target.workspaceSlug,
      path: target.path,
    };
  } catch {
    return null;
  }
}
