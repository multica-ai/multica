// Bearer-token bridge for embedded hosts (currently the Feishu Project plugin
// iframe at projectplg.feishupkg.com). Safari drops third-party cookies inside
// the iframe, so the outer page hands ship a JWT via `#shipToken=…` and we
// authenticate via Authorization: Bearer instead.
//
// This must run synchronously during the first render — before children mount
// and React Query fires its initial wave of fetches — otherwise those fetches
// race the token install and come back 401, which TanStack Query caches and
// does not auto-retry.

export const SESSION_TOKEN_KEY = "shipToken";

// pickSessionToken returns the token to use this session, in priority order:
//   1. URL hash `#shipToken=…` (consumed and stripped from the URL)
//   2. sessionStorage[SESSION_TOKEN_KEY] (set by a prior call this tab)
// Returns null when neither is present, when called during SSR, or when
// sessionStorage is unavailable.
export function pickSessionToken(): string | null {
  if (typeof window === "undefined") return null;

  const hash = window.location.hash.startsWith("#")
    ? window.location.hash.slice(1)
    : "";
  if (hash) {
    const params = new URLSearchParams(hash);
    const fromHash = params.get(SESSION_TOKEN_KEY);
    if (fromHash) {
      try {
        window.sessionStorage.setItem(SESSION_TOKEN_KEY, fromHash);
      } catch {
        /* sessionStorage disabled — value still returned and used in-memory */
      }
      params.delete(SESSION_TOKEN_KEY);
      const remainingHash = params.toString();
      const cleanUrl =
        window.location.pathname +
        window.location.search +
        (remainingHash ? `#${remainingHash}` : "");
      window.history.replaceState(null, "", cleanUrl);
      return fromHash;
    }
  }

  try {
    return window.sessionStorage.getItem(SESSION_TOKEN_KEY);
  } catch {
    return null;
  }
}

export function clearSessionToken(): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.removeItem(SESSION_TOKEN_KEY);
  } catch {
    /* nothing to do */
  }
}
