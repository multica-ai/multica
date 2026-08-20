/**
 * Open a URL in the browser. This defaults to `window.open` with the standard `noopener`+
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
  // Async-created Stripe URLs are commonly returned after the original click
  // task has finished, so opening a new tab can be blocked as a popup. The
  // same-tab option keeps Checkout/Portal reliable.
  if (options?.webTarget === "same-tab") {
    window.location.assign(url);
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}
