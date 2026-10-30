/**
 * Open a URL in the user's default browser, regardless of platform.
 *
 * On Electron (desktop) this routes through `window.desktopAPI.openExternal`,
 * which in turn calls the IPC-gated `shell.openExternal` in the main process —
 * that's the only channel with the `http/https`-only guard. Direct
 * `window.open(url, "_blank")` inside Electron would create a new renderer
 * window instead of handing the URL to the OS shell.
 *
 * On web this defaults to `window.open` with the standard `noopener`+
 * `noreferrer` flags. Callers that receive a URL after an async request can
 * choose `webTarget: "same-tab"` to avoid popup blocking.
 *
 * SSR-safe: no-op if `window` is not defined.
 */
export function openExternal(
  url: string,
  options?: { webTarget?: "new-tab" | "same-tab" },
): void {
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
  // Async-created Stripe URLs are commonly returned after the original click
  // task has finished, so opening a new tab can be blocked as a popup. The
  // same-tab option keeps Checkout/Portal reliable on web while Electron still
  // hands the URL to the system browser above.
  if (options?.webTarget === "same-tab") {
    window.location.assign(url);
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}
