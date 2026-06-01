// Mirror of the server-side parser in
// server/internal/cerebro/search/filters.go (FIR-2595). Same syntax, same
// known-filter set, so what the user sees in chips matches what the SQL
// actually applies.

export const FILTER_KEYS = [
  "from",
  "assignee",
  "status",
  "project",
  "has",
] as const;

export type FilterKey = (typeof FILTER_KEYS)[number];

export interface ParsedFilter {
  key: FilterKey;
  value: string;
}

export interface ParsedSearchQuery {
  /** Free-text portion that drives the FTS query. */
  text: string;
  /** Inline filters in the order they appeared. */
  filters: ParsedFilter[];
}

/**
 * Backend echo: `{ key, value, match }` where match is "ok" when the value
 * resolved to at least one row, "miss" when nothing matched. Surfaced as a
 * subtle warning on the chip so the user understands why their search
 * returns no results.
 */
export interface ActiveFilter {
  key: string;
  value: string;
  match: "ok" | "miss";
}

const FILTER_SET = new Set<string>(FILTER_KEYS);

export function isKnownFilter(key: string): key is FilterKey {
  return FILTER_SET.has(key.toLowerCase());
}

/**
 * Extract inline `key:value` filters from a search query. Quoted values
 * (`project:"my project"`) carry through spaces; unknown prefixes stay in
 * the free-text portion so a typo or a real `http:...` substring still
 * searches.
 *
 *   parseSearchQuery("login bug from:jesper status:todo")
 *     → { text: "login bug",
 *         filters: [{key:"from", value:"jesper"}, {key:"status", value:"todo"}] }
 */
export function parseSearchQuery(raw: string): ParsedSearchQuery {
  const filters: ParsedFilter[] = [];
  const textParts: string[] = [];
  const n = raw.length;
  let i = 0;

  // Indexing a string in TS with noUncheckedIndexedAccess returns string|undefined.
  // charAt() always returns "" past the end, which we already guard with i<n.
  const at = (idx: number): string => raw.charAt(idx);
  const isSpace = (ch: string) => /\s/.test(ch);

  while (i < n) {
    if (isSpace(at(i))) {
      i++;
      continue;
    }

    // Look for a `key:` prefix at the current cursor.
    let colonAt = -1;
    for (let j = i; j < n && !isSpace(at(j)); j++) {
      if (at(j) === ":") {
        colonAt = j;
        break;
      }
    }

    if (colonAt > i) {
      const keyStr = raw.slice(i, colonAt).toLowerCase();
      if (isKnownFilter(keyStr)) {
        const valStart = colonAt + 1;
        let value = "";
        const first = at(valStart);
        if (valStart < n && (first === '"' || first === "'")) {
          const quote = first;
          let end = valStart + 1;
          while (end < n && at(end) !== quote) end++;
          value = raw.slice(valStart + 1, end);
          i = end < n ? end + 1 : end;
        } else {
          let end = valStart;
          while (end < n && !isSpace(at(end))) end++;
          value = raw.slice(valStart, end);
          i = end;
        }
        const trimmed = value.trim();
        if (trimmed.length > 0) {
          filters.push({ key: keyStr as FilterKey, value: trimmed });
        }
        continue;
      }
    }

    // Plain token — copy verbatim to the free-text buffer.
    const start = i;
    while (i < n && !isSpace(at(i))) i++;
    textParts.push(raw.slice(start, i));
  }

  return {
    text: textParts.join(" ").trim(),
    filters,
  };
}
