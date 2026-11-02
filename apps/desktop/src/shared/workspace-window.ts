export const WORKSPACE_WINDOW_ARGUMENT_PREFIX = "--multica-workspace-window=";

/** Renderer → main: the workspace this main window is showing (or null). */
export const WINDOW_WORKSPACE_REPORT_CHANNEL = "window:report-workspace";
/** Renderer → main (sync): workspaces shown by every other main window. */
export const OTHER_WINDOW_WORKSPACES_CHANNEL = "window:other-workspaces";
/** Renderer → main (sync): focus the window showing a workspace, if any. */
export const FOCUS_WORKSPACE_WINDOW_CHANNEL = "window:focus-workspace";
/** Renderer → main: open a workspace in its own main window. */
export const OPEN_WORKSPACE_WINDOW_CHANNEL = "window:open-workspace";

const WORKSPACE_SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;
const MAX_WORKSPACE_PATH_LENGTH = 2_048;

export function parseWorkspaceSlug(value: unknown): string | null {
  return typeof value === "string" && WORKSPACE_SLUG_PATTERN.test(value)
    ? value
    : null;
}

/**
 * Accept a renderer-supplied path only when it stays inside `slug`'s routes,
 * so focusing another window can never hand it a foreign or absolute URL.
 */
export function parseWorkspaceWindowPath(
  slug: string,
  value: unknown,
): string | null {
  if (typeof value !== "string") return null;
  if (value.length === 0 || value.length > MAX_WORKSPACE_PATH_LENGTH) {
    return null;
  }
  const prefix = `/${slug}`;
  if (!value.startsWith(prefix)) return null;
  const rest = value.slice(prefix.length);
  return rest === "" || /^[/?#]/.test(rest) ? value : null;
}

export function encodeWorkspaceWindowArgument(slug: string): string {
  const valid = parseWorkspaceSlug(slug);
  if (!valid) throw new Error("Invalid workspace window slug");
  return `${WORKSPACE_WINDOW_ARGUMENT_PREFIX}${valid}`;
}

/** The workspace a main window was launched for, or null when unspecified. */
export function readWorkspaceWindowSlug(argv: readonly string[]): string | null {
  const argument = argv.find((item) =>
    item.startsWith(WORKSPACE_WINDOW_ARGUMENT_PREFIX),
  );
  if (!argument) return null;
  return parseWorkspaceSlug(
    argument.slice(WORKSPACE_WINDOW_ARGUMENT_PREFIX.length),
  );
}
