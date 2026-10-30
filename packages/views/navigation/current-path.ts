import type { NavigationAdapter } from "./types";

/**
 * Rebuild the adapter's location as a single in-app path —
 * `/pathname?search#fragment` — the form `getShareableUrl()` expects.
 *
 * Every caller that turns "where the user is" into a link must go through
 * here. Composing only `pathname` + `searchParams` silently drops the
 * fragment, which downgrades a `#comment-…` deep link to the whole issue.
 */
export function currentPath(
  navigation: Pick<NavigationAdapter, "pathname" | "searchParams" | "hash">,
): string {
  const search = navigation.searchParams.toString();
  return `${navigation.pathname}${search ? `?${search}` : ""}${navigation.hash}`;
}
