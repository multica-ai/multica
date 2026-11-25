// Package-time settings for self-hosted builds. electron.vite.config.ts bakes
// MULTICA_DESKTOP_SERVER_URL and MULTICA_DESKTOP_UPDATE_URL into the main
// bundle with `define`; both are empty in `pnpm dev`, in tests, and in
// official builds, which keeps the Multica Cloud defaults and GitHub updates.

declare const __MULTICA_DESKTOP_SERVER_URL__: string | undefined;
declare const __MULTICA_DESKTOP_UPDATE_URL__: string | undefined;

/** Server a packaged build connects to when ~/.multica/desktop.json is absent. */
export const BUILD_SERVER_URL: string =
  typeof __MULTICA_DESKTOP_SERVER_URL__ === "string"
    ? __MULTICA_DESKTOP_SERVER_URL__
    : "";

/** electron-updater "generic" feed: latest-*.yml next to the installers. */
export const BUILD_UPDATE_URL: string =
  typeof __MULTICA_DESKTOP_UPDATE_URL__ === "string"
    ? __MULTICA_DESKTOP_UPDATE_URL__
    : "";

export type UpdateFeed =
  /** Keep the feed in app-update.yml (official GitHub releases). */
  | { kind: "default" }
  | { kind: "generic"; url: string }
  /** No feed for this build: never check, never download. */
  | { kind: "disabled" };

/**
 * A build that connects to a self-hosted server must not update itself to an
 * official release, which would connect to Multica Cloud instead. Such builds
 * update only from their own feed, or not at all.
 */
export function resolveUpdateFeed(
  build: { serverUrl: string; updateUrl: string } = {
    serverUrl: BUILD_SERVER_URL,
    updateUrl: BUILD_UPDATE_URL,
  },
): UpdateFeed {
  if (build.updateUrl) return { kind: "generic", url: build.updateUrl };
  if (build.serverUrl) return { kind: "disabled" };
  return { kind: "default" };
}
