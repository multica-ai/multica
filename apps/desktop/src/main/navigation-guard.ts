/**
 * Guard document-level renderer navigation to trusted origins or the packaged
 * entry file. Same-window foreign documents must not inherit renderer privileges.
 * Client-side route changes do not fire will-navigate and remain the router's
 * responsibility. New windows and external links use openExternalSafely.
 */

/** The subset of BrowserWindow this module needs — keeps it unit-testable. */
export type NavigationGuardWindow = {
  webContents: {
    on(
      event: "will-navigate",
      listener: (event: { preventDefault(): void }, url: string) => void,
    ): unknown;
  };
};

/**
 * Report whether `url` is the trusted renderer document itself.
 *
 * `trustedURL` is whatever the caller is about to load: the dev server origin in
 * development, the packaged renderer's file URL in production. Deriving trust
 * from the same value we load keeps this from drifting out of sync.
 */
export function isTrustedRendererURL(url: string, trustedURL: string): boolean {
  let target: URL;
  let trusted: URL;
  try {
    target = new URL(url);
    trusted = new URL(trustedURL);
  } catch {
    return false;
  }

  if (trusted.protocol === "file:") {
    // A file:// document has an opaque ("null") origin, so origin equality is
    // useless here — compare the document path instead. Only the exact renderer
    // entry point is trusted: a sibling file in the same directory is not the
    // renderer, and neither is any other path on disk.
    return target.protocol === "file:" && target.pathname === trusted.pathname;
  }
  return target.protocol !== "file:" && target.origin === trusted.origin;
}

/**
 * Describe a blocked navigation for the log.
 *
 * Origin only, never the path: a blocked URL is attacker- or agent-controlled
 * and its path may spell out a local filesystem layout. Scheme and host are
 * enough to diagnose what was blocked.
 */
export function describeBlockedNavigation(url: string): string {
  try {
    const { protocol, host } = new URL(url);
    return host ? `${protocol}//${host}` : protocol;
  } catch {
    return "invalid URL";
  }
}

/** Install the guard. Call before loading the window so the first load is covered. */
export function installNavigationGuard(
  window: NavigationGuardWindow,
  trustedURL: string,
): void {
  window.webContents.on("will-navigate", (event, url) => {
    if (isTrustedRendererURL(url, trustedURL)) return;
    event.preventDefault();
    console.warn(
      `[security] blocked will-navigate to a non-renderer origin: ${describeBlockedNavigation(url)}`,
    );
  });
}
