/**
 * Shared link handling utilities for the editor system.
 *
 * Used by content-editor (ProseMirror click handler), readonly-content
 * (react-markdown link component), and link-hover-card (Open button).
 */

import { isGlobalPath } from "@multica/core/paths";

/**
 * Top-level workspace-scoped routes. Used to detect "/{route}/..." paths that
 * were authored without a workspace slug — we prepend the current slug so they
 * resolve correctly under the new /{slug}/{route}/... URL shape.
 *
 * Why a hardcoded allowlist: the heuristic must be conservative. A path like
 * "/acme/issues/abc" already has a slug (first segment "acme" isn't a known
 * route), so leaving it alone is correct. A path like "/foo/bar" where "foo"
 * isn't a known route is ambiguous — we don't rewrite it, treating the author
 * as intentional. Only "/issues/..." style paths get auto-prefixed.
 */
const WORKSPACE_ROUTE_SEGMENTS = new Set([
  "usage",
  "issues",
  "projects",
  "autopilots",
  "agents",
  "chat",
  "inbox",
  "my-issues",
  "runtimes",
  "skills",
  "settings",
]);

/**
 * Path prefixes served by the backend rather than the app router. A link to one
 * of these is a download / raw resource even though it sits on the app origin,
 * so it must stay an external open instead of becoming an in-app route
 * (`/api/attachments/<id>/download` is the common one in agent-written content).
 */
const NON_ROUTE_PREFIXES = ["/api/", "/_next/"];

/**
 * Convert an absolute URL that points at this deployment's own app into the
 * in-app path it addresses; `null` for anything else.
 *
 * An agent or a user pasting `https://<app-host>/acme/issues/123` means the same
 * destination as `/acme/issues/123`. Without this, the URL reads as external and
 * the desktop app hands it to the system browser instead of opening a tab
 * (MUL-5208).
 *
 * `appOrigin` is the deployment's public app URL, which only the platform layer
 * knows (web: the current origin; desktop: the connected environment's app URL).
 * See `useAppOrigin()`.
 */
export function toInternalAppPath(
  href: string,
  appOrigin?: string | null,
): string | null {
  if (!appOrigin) return null;
  let target: URL;
  let app: URL;
  try {
    target = new URL(href);
    app = new URL(appOrigin);
  } catch {
    return null;
  }
  if (target.origin !== app.origin) return null;
  // Opaque origins (file:, data:) compare equal to each other; only real web
  // origins identify the app.
  if (target.protocol !== "http:" && target.protocol !== "https:") return null;
  if (NON_ROUTE_PREFIXES.some((prefix) => target.pathname.startsWith(prefix))) {
    return null;
  }
  return `${target.pathname}${target.search}${target.hash}`;
}

/**
 * Open a link — internal paths dispatch multica:navigate, external open new tab.
 *
 * If `currentSlug` is provided and `href` is a workspace-scoped path lacking a
 * slug (e.g. "/issues/abc" instead of "/{slug}/issues/abc"), the slug is
 * prepended. This is for legacy markdown content authored before the URL
 * refactor, or future content where users forget the slug when pasting.
 *
 * `appOrigin` lets absolute URLs pointing back at this deployment take the same
 * internal route as a relative path.
 */
export function openLink(
  href: string,
  currentSlug?: string | null,
  appOrigin?: string | null,
): void {
  const internalPath = href.startsWith("/")
    ? href
    : toInternalAppPath(href, appOrigin);
  if (internalPath) {
    let path = internalPath;
    if (currentSlug && !isGlobalPath(path)) {
      const firstSegment = path.split("/")[1];
      if (firstSegment && WORKSPACE_ROUTE_SEGMENTS.has(firstSegment)) {
        // Path looks like /issues/abc (no slug) — prepend current slug.
        path = `/${currentSlug}${path}`;
      }
      // Otherwise the first segment is either already a slug (e.g. "acme" in
      // "/acme/issues") or something unknown (e.g. "/foo"). Leave it alone —
      // the user wrote what they meant.
    }
    window.dispatchEvent(
      new CustomEvent("multica:navigate", { detail: { path } }),
    );
  } else {
    window.open(href, "_blank", "noopener,noreferrer");
  }
}

/** Check if a href is a mention protocol link (should not be opened as a regular link). */
export function isMentionHref(href: string | null | undefined): href is string {
  return !!href && href.startsWith("mention://");
}
