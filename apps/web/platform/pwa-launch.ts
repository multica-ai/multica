import { isReservedSlug, pageForSegment } from "@multica/core/paths";
import { PWA_SHORTCUT_PARAM, PWA_START_URL } from "@/app/manifest";

/**
 * Launches of the installed app (launcher icon, manifest shortcut) reuse the
 * existing window: the manifest's `launch_handler` is `focus-existing`, so
 * Chromium only focuses that window and hands the launch URL to
 * `window.launchQueue`. Navigating there is up to us, and doing it with the
 * client router keeps the page mounted, so in-flight uploads and open UI
 * survive. Browsers without the Launch Handler API keep their old behaviour.
 *
 * Kept free of React so the manifest route, a server module, never pulls in
 * client code; the launch queue consumer lives in platform/navigation.tsx.
 */

/**
 * Where an existing window should go for a launch, or null to stay put.
 *
 * - A plain launch (the start URL) only brings the window forward, the way the
 *   Electron app's second launch does.
 * - A workspace page addressed without a slug (`/my-issues`) opens in the
 *   window's current workspace. Elsewhere it is left to the proxy, which
 *   resolves it to the last workspace.
 * - Query and hash are part of the destination: a filtered view or a
 *   `#comment-…` link must land on exactly that.
 * - A window already at the target stays. Chromium also delivers the launch
 *   that opened the window, and pushing it again would add a duplicate
 *   history entry.
 */
export function resolveLaunchPath(
  targetURL: string,
  current: URL,
): string | null {
  const target = new URL(targetURL, current);
  if (target.origin !== current.origin) return null;

  const isShortcut = target.searchParams.has(PWA_SHORTCUT_PARAM);
  const targetPath = withoutShortcutFlag(target);
  if (!isShortcut && targetPath === PWA_START_URL) return null;

  const currentSlug = workspaceSlugOf(current.pathname);
  const isLegacyWorkspacePage =
    pageForSegment(firstSegment(target.pathname)) !== null;
  const prefix = currentSlug && isLegacyWorkspacePage ? `/${currentSlug}` : "";
  const path = prefix + targetPath;
  return path === withoutShortcutFlag(current) ? null : path;
}

/** Path, query and hash, without the shortcut flag. */
function withoutShortcutFlag(url: URL): string {
  const copy = new URL(url);
  copy.searchParams.delete(PWA_SHORTCUT_PARAM);
  return copy.pathname + copy.search + copy.hash;
}

function firstSegment(pathname: string): string {
  return pathname.split("/")[1] ?? "";
}

function workspaceSlugOf(pathname: string): string | null {
  const segment = firstSegment(pathname);
  return segment && !isReservedSlug(segment) ? segment : null;
}
