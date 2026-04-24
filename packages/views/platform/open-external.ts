/**
 * Open a URL in the user's default browser, regardless of platform.
 *
 * On Electron (desktop) this routes through `window.desktopAPI.openExternal`,
 * which in turn calls the IPC-gated `shell.openExternal` in the main process —
 * that's the only channel with the `http/https`-only guard. Direct
 * `window.open(url, "_blank")` inside Electron would create a new renderer
 * window instead of handing the URL to the OS shell.
 *
 * On web this falls back to `window.open` with the standard `noopener`+
 * `noreferrer` flags, which is the same thing an `<a target="_blank">` would
 * do but without requiring markup.
 *
 * SSR-safe: no-op if `window` is not defined.
 */
export function openExternal(url: string): void {
  if (typeof window === "undefined") return;
  const desktopAPI = (
    window as unknown as {
      desktopAPI?: { openExternal?: (u: string) => Promise<void> | void };
    }
  ).desktopAPI;
  if (desktopAPI?.openExternal) {
    void desktopAPI.openExternal(url);
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}
