/**
 * One searchable setting. Titles and descriptions are resolved copy, so the
 * search matches what the user reads in the current language. User-entered
 * values (a workspace name, a repository URL) are deliberately not indexed:
 * the search finds where a setting lives, not what it currently holds.
 */
export interface SettingsSearchEntry {
  /** Settings page (`?tab=`) the entry opens. */
  tab: string;
  /** In-page anchor (`?section=`); omitted for page-level entries. */
  anchor?: string;
  /** Detail page within Channels (`?integration=`). */
  integration?: string;
  title: string;
  description?: string;
}

export interface SettingsSearchResult extends SettingsSearchEntry {
  /** The description window shown under the title, trimmed around the match. */
  snippet?: string;
}

// Sized for the settings nav column: about 16 CJK characters fit on a line.
const SNIPPET_LEAD = 6;
const SNIPPET_LENGTH = 16;
const ELLIPSIS = "...";

/** Case-insensitive, whitespace-trimmed needle; empty means "no search". */
export function normalizeSettingsQuery(query: string): string {
  return query.trim().toLocaleLowerCase();
}

/** Keep the match visible in a one-line description preview. */
export function descriptionSnippet(description: string, needle: string): string {
  const at = description.toLocaleLowerCase().indexOf(needle);
  if (at < 0 || at + needle.length <= SNIPPET_LENGTH) return description;
  const start = Math.max(0, at - SNIPPET_LEAD);
  return `${ELLIPSIS}${description.slice(start)}`;
}

/**
 * Rank entries whose title or description contains the query: title matches
 * first (page-level before in-page), then description-only matches, each in
 * index order so results read in the same order as the navigation.
 */
export function searchSettings(
  entries: readonly SettingsSearchEntry[],
  query: string,
): SettingsSearchResult[] {
  const needle = normalizeSettingsQuery(query);
  if (!needle) return [];
  const titleHits: SettingsSearchResult[] = [];
  const descriptionHits: SettingsSearchResult[] = [];
  const seen = new Set<string>();
  for (const entry of entries) {
    const key = `${entry.tab}#${entry.anchor ?? ""}#${entry.integration ?? ""}`;
    if (seen.has(key)) continue;
    const inTitle = entry.title.toLocaleLowerCase().includes(needle);
    const inDescription =
      !!entry.description &&
      entry.description.toLocaleLowerCase().includes(needle);
    if (!inTitle && !inDescription) continue;
    seen.add(key);
    const result: SettingsSearchResult = {
      ...entry,
      snippet: entry.description
        ? inDescription
          ? descriptionSnippet(entry.description, needle)
          : entry.description
        : undefined,
    };
    (inTitle ? titleHits : descriptionHits).push(result);
  }
  return [...titleHits, ...descriptionHits];
}
