import { parseIssueWindowPath } from "./issue-window";
import { parseWorkspaceSlug } from "./workspace-window";

/**
 * `multica://open/issue?url=<issue page URL>` and
 * `multica://open/chat?url=<chat page URL>`, the links another app uses to
 * open an issue or a chat session in the desktop app.
 */
export type IssueDeepLink =
  | { kind: "issue" | "chat"; workspaceSlug: string; path: string }
  | { kind: "other-instance" };

const CHAT_SESSION_PATTERN = /^[0-9A-Za-z][0-9A-Za-z-]{0,63}$/;

/** Never interpret an issue identifier or a chat session against a different server. */
export function parseIssueDeepLink(
  value: string,
  appUrl: string,
): IssueDeepLink | null {
  try {
    const link = new URL(value);
    if (link.protocol !== "multica:" || link.host !== "open") return null;
    const kind = link.pathname === "/issue" ? "issue" : link.pathname === "/chat" ? "chat" : null;
    if (!kind) return null;
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
    const appPath = url.pathname.slice(basePath.length) + url.search + url.hash;
    const target = kind === "issue" ? parseIssueWindowPath(appPath) : parseChatPath(appPath);
    if (!target) return null;
    return { kind, workspaceSlug: target.workspaceSlug, path: target.path };
  } catch {
    return null;
  }
}

/**
 * `/<workspace>/chat`, optionally with `?session=<id>`. Only the session is
 * carried over: the chat page reads nothing else from its address.
 */
function parseChatPath(value: string): { workspaceSlug: string; path: string } | null {
  const url = new URL(value, "https://desktop.multica.invalid");
  if (url.origin !== "https://desktop.multica.invalid") return null;
  const segments = url.pathname.split("/");
  if (segments.length !== 3 || segments[0] !== "" || segments[2] !== "chat") return null;
  let workspaceSlug: string;
  try {
    workspaceSlug = decodeURIComponent(segments[1] ?? "");
  } catch {
    return null;
  }
  if (!parseWorkspaceSlug(workspaceSlug)) return null;
  const session = url.searchParams.get("session");
  if (session === null) return { workspaceSlug, path: `/${workspaceSlug}/chat` };
  if (!CHAT_SESSION_PATTERN.test(session)) return null;
  return { workspaceSlug, path: `/${workspaceSlug}/chat?session=${session}` };
}
