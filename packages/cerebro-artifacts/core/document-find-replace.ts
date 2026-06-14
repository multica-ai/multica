// TECH-3529 bølge 2: pure helpers for in-document find & replace. Operates on
// the note's markdown text so search & replace works directly on the inline
// document (no separate edit field). Matching is case-insensitive literal text.

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function buildMatcher(query: string): RegExp | null {
  if (query === "") return null;
  return new RegExp(escapeRegExp(query), "gi");
}

/** Number of (case-insensitive) occurrences of query in body. */
export function countMatches(body: string, query: string): number {
  const re = buildMatcher(query);
  if (!re) return 0;
  const m = body.match(re);
  return m ? m.length : 0;
}

/**
 * Replaces every occurrence of query with replacement (literal — `$` in the
 * replacement is inserted verbatim, not treated as a backreference). Returns the
 * new body and how many replacements were made.
 */
export function replaceAll(
  body: string,
  query: string,
  replacement: string,
): { body: string; count: number } {
  const re = buildMatcher(query);
  if (!re) return { body, count: 0 };
  let count = 0;
  const next = body.replace(re, () => {
    count++;
    return replacement;
  });
  return { body: next, count };
}

/**
 * Replaces only the first occurrence (case-insensitive) of query. Returns the
 * new body and whether a replacement happened.
 */
export function replaceFirst(
  body: string,
  query: string,
  replacement: string,
): { body: string; replaced: boolean } {
  const re = buildMatcher(query);
  if (!re) return { body, replaced: false };
  let replaced = false;
  const next = body.replace(re, (match) => {
    if (replaced) return match;
    replaced = true;
    return replacement;
  });
  return { body: next, replaced };
}
