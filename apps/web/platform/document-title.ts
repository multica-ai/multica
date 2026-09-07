/**
 * The browser tab title contract for the web app.
 *
 * Two writers produce titles and they must agree: Next.js metadata renders the
 * `<title>` for statically known routes (landing, auth), and
 * `WorkspaceDocumentTitle` sets `document.title` on workspace routes, whose
 * names only exist in the query cache. Both go through the constants here so
 * `Issues | Multica` and `Changelog | Multica` can never drift into two
 * different separators.
 *
 * Pure and React-free on purpose: the root layout is a server component and
 * imports `SITE_TITLE` / `TITLE_TEMPLATE` for its metadata export.
 */

/** Root fallback — the title of a page that has nothing more specific to say. */
export const SITE_TITLE = "Multica — Project Management for Human + Agent Teams";

/** Appended to every page-specific title. */
export const TITLE_SUFFIX = " | Multica";

/** Next.js `metadata.title.template`; see apps/web/app/layout.tsx. */
export const TITLE_TEMPLATE = `%s${TITLE_SUFFIX}`;

/**
 * Longest page name we put in front of the suffix, counted in code points.
 *
 * Issue titles are unbounded server-side, and the whole string ends up in the
 * window title, history entries and bookmarks, not just the tab — a 600-char
 * title makes all three unreadable. Browsers truncate the tab itself far
 * earlier than this, so the cap only exists to keep those secondary surfaces
 * sane; the discriminating part of a title (the issue key) leads, so it always
 * survives both truncations.
 */
export const MAX_PAGE_TITLE_LENGTH = 120;

/**
 * Clip to {@link MAX_PAGE_TITLE_LENGTH} code points.
 *
 * Iterating code points rather than slicing UTF-16 units keeps an emoji or any
 * other astral character from being cut in half into a replacement glyph — a
 * real case here, since issue titles routinely open with one.
 */
function clipTitle(title: string): string {
  const codePoints = Array.from(title);
  if (codePoints.length <= MAX_PAGE_TITLE_LENGTH) return title;
  return `${codePoints.slice(0, MAX_PAGE_TITLE_LENGTH).join("").trimEnd()}…`;
}

/**
 * Build the full document title for a page name, e.g.
 * `MUL-123: Fix login` → `MUL-123: Fix login | Multica`.
 *
 * An empty or whitespace-only name falls back to {@link SITE_TITLE} rather than
 * rendering a bare ` | Multica`.
 */
export function formatDocumentTitle(pageTitle: string | null | undefined): string {
  const trimmed = pageTitle?.trim();
  if (!trimmed) return SITE_TITLE;
  return `${clipTitle(trimmed)}${TITLE_SUFFIX}`;
}
