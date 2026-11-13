import { matchesPinyin } from "../editor/extensions/pinyin-match";

// Row matcher shared by pages, commands and submenu options.
//
// Keywords match by prefix, not substring. They are invisible synonyms, so a
// substring hit reads as a bug from the outside: "s" used to surface Inbox
// (via "notifications") and "i" surfaced nearly every page. The visible label
// still matches anywhere, because there the user can see what they hit.
//
// Pinyin is included for the same reason member search has it: under a Chinese
// UI the localized label is the only thing the user can aim at, so typing
// "renwu" has to reach "任务".
export function matchesRow(label: string, keywords: readonly string[], query: string) {
  return scoreRow(label, keywords, query) > 0;
}

const WORD_SPLIT = /[\s\-_/·.:()]+/;

/**
 * How well `text` answers `query` (already lower-cased and trimmed), from 0
 * (no match) to 100 (exact). The tiers are what ranking across object types
 * relies on: an exact agent name must beat an issue whose title merely
 * contains it.
 */
export function scoreText(text: string, query: string): number {
  if (!query || !text) return 0;
  const t = text.toLowerCase();
  if (t === query) return 100;
  if (t.startsWith(query)) return 80;
  if (t.split(WORD_SPLIT).some((word) => word.startsWith(query))) return 60;
  if (t.includes(query)) return 40;
  if (matchesPinyin(text, query)) return 30;
  return 0;
}

export function scoreRow(label: string, keywords: readonly string[], query: string): number {
  const labelScore = scoreText(label, query);
  if (labelScore > 0) return labelScore;
  return keywords.some((kw) => kw.startsWith(query)) ? 25 : 0;
}

/** "Assign to..." → "Assign to", for breadcrumbs. */
export function stripEllipsis(label: string) {
  return label.replace(/\s*(\.\.\.|…)$/, "");
}

export type SearchScope = "all" | "commands" | "issues" | "people";

const SCOPE_PREFIXES: Record<string, SearchScope> = {
  ">": "commands",
  "#": "issues",
  "@": "people",
};

/**
 * `>` limits the palette to commands, `#` to issues, `@` to people and
 * agents. The prefix only counts as the first character; the rest is the
 * query.
 */
export function parseScope(raw: string): { scope: SearchScope; text: string } {
  const scope = SCOPE_PREFIXES[raw.charAt(0)];
  if (!scope) return { scope: "all", text: raw.trim() };
  return { scope, text: raw.slice(1).trim() };
}
