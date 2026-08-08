/** Default settings surface when `?tab=` is missing or unknown. */
export const DEFAULT_SETTINGS_TAB = "profile";

/**
 * Legacy `?tab=…` values that have been collapsed into another tab. Old
 * bookmarks still land on the correct surface without us preserving a dead
 * TabsContent entry. Lark used to be its own top-level workspace tab; it now
 * lives inside Integrations.
 */
export const LEGACY_SETTINGS_TAB_REDIRECTS: Readonly<Record<string, string>> = {
  lark: "integrations",
};

/**
 * Map a raw `?tab=` query value to the canonical tab id.
 * Applies legacy redirects first, then falls back to the default when the
 * candidate is missing from the allowed set (or when no value is provided).
 */
export function resolveSettingsTab(
  tabFromUrl: string | null | undefined,
  validTabs: ReadonlySet<string> | ReadonlyArray<string>,
): string {
  const allowed = validTabs instanceof Set ? validTabs : new Set(validTabs);
  if (!tabFromUrl) return DEFAULT_SETTINGS_TAB;
  const candidate = LEGACY_SETTINGS_TAB_REDIRECTS[tabFromUrl] ?? tabFromUrl;
  return allowed.has(candidate) ? candidate : DEFAULT_SETTINGS_TAB;
}
