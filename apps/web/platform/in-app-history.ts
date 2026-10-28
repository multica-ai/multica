/**
 * Determine whether web navigation has a safe in-app back destination.
 * Use Navigation API entries for committed same-origin history. history.length
 * includes external entries, and counting router.push requests includes navigations
 * that may be replaced or abandoned. Return false when the browser cannot prove
 * a destination so callers use their fallback instead of leaving the app.
 */

/** Minimal shape of the Navigation API — not in TypeScript's DOM lib yet. */
type NavigationApi = { canGoBack: boolean };

/**
 * Whether a step back stays inside the app. `false` whenever the browser has
 * no Navigation API: the caller then navigates to its fallback, which is the
 * behaviour those browsers had before any of this existed.
 */
export function canGoBackInApp(): boolean {
  if (typeof window === "undefined") return false;
  const navigation = (window as { navigation?: unknown }).navigation as
    | NavigationApi
    | undefined;
  return navigation?.canGoBack === true;
}
