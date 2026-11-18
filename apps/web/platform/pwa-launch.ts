import { useEffect } from "react";
import { isReservedSlug, pageForSegment } from "@multica/core/paths";

/**
 * Launches of the installed app (launcher icon, manifest shortcut) reuse the
 * existing window: the manifest's `launch_handler` is `focus-existing`, so
 * Chromium only focuses that window and hands the launch URL to
 * `window.launchQueue`. Navigating there is up to us, and doing it with the
 * client router keeps the page mounted, so in-flight uploads and open UI
 * survive. Browsers without the Launch Handler API keep their old behaviour.
 */

/** Launch path. Shortcuts must not reuse it; see resolveLaunchPath. */
export const PWA_START_URL = "/inbox";

/**
 * Query flag that sets a shortcut apart from a plain launch when both point at
 * the same page. The launch URL is the only signal the launch queue delivers.
 */
export const PWA_SHORTCUT_PARAM = "launch";

/**
 * Where an existing window should go for a launch, or null to stay put.
 *
 * - A plain launch (the start URL) only brings the window forward, the way the
 *   Electron app's second launch does.
 * - A workspace page addressed without a slug (`/my-issues`) opens in the
 *   window's current workspace. Elsewhere it is left to the proxy, which
 *   resolves it to the last workspace.
 * - A window already on the target page stays. Chromium also delivers the
 *   launch that opened the window, and pushing it again would add a duplicate
 *   history entry.
 */
export function resolveLaunchPath(
  targetURL: string,
  current: URL,
): string | null {
  const target = new URL(targetURL, current);
  if (target.origin !== current.origin) return null;

  const isShortcut = target.searchParams.has(PWA_SHORTCUT_PARAM);
  target.searchParams.delete(PWA_SHORTCUT_PARAM);
  const targetPath = target.pathname + target.search;
  if (!isShortcut && targetPath === PWA_START_URL) return null;

  const currentSlug = workspaceSlugOf(current.pathname);
  const isLegacyWorkspacePage =
    pageForSegment(firstSegment(target.pathname)) !== null;
  const prefix = currentSlug && isLegacyWorkspacePage ? `/${currentSlug}` : "";
  if (prefix + target.pathname === current.pathname) return null;

  return prefix + targetPath;
}

function firstSegment(pathname: string): string {
  return pathname.split("/")[1] ?? "";
}

function workspaceSlugOf(pathname: string): string | null {
  const segment = firstSegment(pathname);
  return segment && !isReservedSlug(segment) ? segment : null;
}

/** Minimal shape of the Launch Handler API — not in TypeScript's DOM lib yet. */
type LaunchQueue = {
  setConsumer(consumer: (params: { targetURL?: string }) => void): void;
};

export function useLaunchQueueNavigation(push: (path: string) => void) {
  useEffect(() => {
    const launchQueue = (window as { launchQueue?: LaunchQueue }).launchQueue;
    if (!launchQueue) return;
    launchQueue.setConsumer(({ targetURL }) => {
      if (!targetURL) return;
      const path = resolveLaunchPath(
        targetURL,
        new URL(window.location.href),
      );
      if (path) push(path);
    });
    return () => launchQueue.setConsumer(() => {});
  }, [push]);
}
