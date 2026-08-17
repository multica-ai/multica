export const ISSUE_WINDOW_ARGUMENT_PREFIX = "--multica-issue-window=";

const MAX_ISSUE_WINDOW_PATH_LENGTH = 2_048;
const MAX_ISSUE_WINDOW_TITLE_LENGTH = 256;
const WORKSPACE_SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;
const ISSUE_REF_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;

export type IssueWindowRequest = {
  path: string;
  title: string;
};

export type IssueWindowContext = IssueWindowRequest & {
  kind: "issue";
  workspaceSlug: string;
  issueId: string;
};

export type DesktopWindowContext =
  | { kind: "main" }
  | IssueWindowContext;

/**
 * Validate renderer-controlled input before it can become a BrowserWindow
 * launch argument. Dedicated windows are intentionally limited to one
 * workspace-scoped issue-detail route; list pages and every other app route
 * stay in the main tabbed window.
 */
export function parseIssueWindowRequest(value: unknown): IssueWindowContext | null {
  if (!value || typeof value !== "object") return null;

  const input = value as Record<string, unknown>;
  if (typeof input.path !== "string") return null;
  if (input.path.length === 0 || input.path.length > MAX_ISSUE_WINDOW_PATH_LENGTH) {
    return null;
  }

  const parsedPath = parseIssueWindowPath(input.path);
  if (!parsedPath) return null;

  const rawTitle = typeof input.title === "string" ? input.title.trim() : "";
  const title = rawTitle.slice(0, MAX_ISSUE_WINDOW_TITLE_LENGTH) || "Issue";

  return {
    kind: "issue",
    path: parsedPath.path,
    title,
    workspaceSlug: parsedPath.workspaceSlug,
    issueId: parsedPath.issueId,
  };
}

/** Return a canonical issue-detail path, or null for every other URL shape. */
export function parseIssueWindowPath(
  value: unknown,
): Pick<IssueWindowContext, "path" | "workspaceSlug" | "issueId"> | null {
  if (typeof value !== "string") return null;
  if (!value.startsWith("/") || value.startsWith("//")) return null;

  let url: URL;
  try {
    url = new URL(value, "https://desktop.multica.invalid");
  } catch {
    return null;
  }
  if (url.origin !== "https://desktop.multica.invalid") return null;

  const segments = url.pathname.split("/");
  if (segments.length !== 4 || segments[0] !== "" || segments[2] !== "issues") {
    return null;
  }

  let workspaceSlug: string;
  let issueId: string;
  try {
    workspaceSlug = decodeURIComponent(segments[1] ?? "");
    issueId = decodeURIComponent(segments[3] ?? "");
  } catch {
    return null;
  }

  if (!WORKSPACE_SLUG_PATTERN.test(workspaceSlug)) return null;
  if (!ISSUE_REF_PATTERN.test(issueId)) return null;

  const path = `/${workspaceSlug}/issues/${encodeURIComponent(issueId)}${url.search}${url.hash}`;
  if (path.length > MAX_ISSUE_WINDOW_PATH_LENGTH) return null;

  return { path, workspaceSlug, issueId };
}

export function encodeIssueWindowArgument(request: IssueWindowRequest): string {
  const context = parseIssueWindowRequest(request);
  if (!context) {
    throw new Error("Invalid issue window request");
  }
  return `${ISSUE_WINDOW_ARGUMENT_PREFIX}${encodeURIComponent(
    JSON.stringify({ path: context.path, title: context.title }),
  )}`;
}

export function readDesktopWindowContext(
  argv: readonly string[],
): DesktopWindowContext {
  const argument = argv.find((item) =>
    item.startsWith(ISSUE_WINDOW_ARGUMENT_PREFIX),
  );
  if (!argument) return { kind: "main" };

  try {
    const encoded = argument.slice(ISSUE_WINDOW_ARGUMENT_PREFIX.length);
    const decoded = JSON.parse(decodeURIComponent(encoded)) as unknown;
    return parseIssueWindowRequest(decoded) ?? { kind: "main" };
  } catch {
    return { kind: "main" };
  }
}
