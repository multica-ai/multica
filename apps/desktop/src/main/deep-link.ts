/**
 * Pure parser for the `multica://` URL scheme.
 *
 * The main process registers this scheme with the OS, so any surface that can
 * render a link — a browser, a terminal, Slack, an agent comment — can hand a
 * destination to the installed desktop app. Keeping the parsing here (instead
 * of inline in `index.ts`) makes every accepted shape testable without booting
 * Electron.
 *
 * Namespace rule: the URL *host* is either a reserved global namespace
 * (`auth`, `invite`) or a workspace slug. Both `auth` and `invite` are reserved
 * slugs on the backend (`server/internal/handler/reserved_slugs.json`), so no
 * real workspace can ever collide with them — matching the global namespaces
 * first is unambiguous, not a heuristic.
 *
 * This module must stay dependency-free: `electron.vite.config.ts` externalizes
 * main-process dependencies, so main/preload code cannot import the raw
 * TypeScript shipped by `@multica/core`. Path *building* therefore happens in
 * the renderer, which owns `paths.workspace(slug).issueDetail(id)`; main only
 * emits the identifiers it parsed.
 */

/** OS-registered URL scheme. Mirrored in `electron-builder.yml`. */
export const PROTOCOL = "multica";

export type DeepLink =
  /** `multica://auth/callback?token=<jwt>` — web login handing a session back. */
  | { kind: "auth-token"; token: string }
  /** `multica://invite/<invitationId>` — "Open in desktop app" on the web invite page. */
  | { kind: "invite"; invitationId: string }
  /** `multica://<slug>/issues/<id>` — a single issue in a specific workspace. */
  | { kind: "issue"; slug: string; issueId: string };

/**
 * Workspace slug shape, mirroring `workspaceSlugPattern` in
 * `server/internal/handler/workspace.go`. Slugs are always lowercase, so a host
 * that cannot match this pattern cannot name a workspace and is rejected rather
 * than forwarded to the renderer as a route.
 */
const WORKSPACE_SLUG_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

function decodeSegment(segment: string): string | null {
  try {
    return decodeURIComponent(segment);
  } catch {
    // Malformed percent-encoding — treat the whole link as unparseable.
    return null;
  }
}

/**
 * Resolve a `multica://` URL into the destination it names, or null when the
 * URL is malformed, uses another scheme, or names a shape we do not handle.
 *
 * Returning null (rather than a partial match) is deliberate: an unrecognized
 * deep link must be a no-op, never a navigation to an approximate destination.
 */
export function parseDeepLink(url: string): DeepLink | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }

  if (parsed.protocol !== `${PROTOCOL}:`) return null;

  // `new URL` leaves the host of a non-special scheme in its original case.
  // Hosts are conventionally case-insensitive and slugs are always lowercase,
  // so fold the case here — otherwise `multica://Acme/issues/1` would be a
  // dead link for a workspace that does exist.
  const host = parsed.hostname.toLowerCase();
  // `new URL` already resolved any `..` segments, so a traversal attempt like
  // `multica://acme/issues/../../x` arrives as `/x` and simply fails to match.
  const segments = parsed.pathname.split("/").filter(Boolean);

  if (host === "auth" && segments.length === 1 && segments[0] === "callback") {
    const token = parsed.searchParams.get("token");
    return token ? { kind: "auth-token", token } : null;
  }

  if (host === "invite") {
    if (segments.length !== 1) return null;
    const invitationId = decodeSegment(segments[0]);
    return invitationId ? { kind: "invite", invitationId } : null;
  }

  // Workspace-scoped destinations. Only issue detail is supported today; the
  // host/segment split is what a second route would extend.
  if (
    WORKSPACE_SLUG_PATTERN.test(host) &&
    segments.length === 2 &&
    segments[0] === "issues"
  ) {
    const issueId = decodeSegment(segments[1]);
    if (!issueId) return null;
    return { kind: "issue", slug: host, issueId };
  }

  return null;
}
