"use client";

import { useMemo } from "react";
import {
  SearchDialog,
  SearchDialogClose,
  SearchDialogContent,
  SearchDialogHeader,
  SearchDialogIcon,
  SearchDialogInput,
  SearchDialogList,
  SearchDialogListItem,
  SearchDialogOverlay,
  type SearchItemType,
} from "fumadocs-ui/components/dialog/search";
import { useI18n } from "fumadocs-ui/contexts/i18n";
import type { SharedProps } from "fumadocs-ui/contexts/search";
import { useFetchSearch, type SearchResultItem } from "fumadocs-core/search/client";

// Results read like DocSearch and Pagefind: the page and section titles, then
// at most a few one-line excerpts around the match. Fumadocs's default dialog
// renders every matching paragraph and table in full, which buries the titles.

/** Matches shown under one page; Pagefind caps its sub-results at three too. */
const MAX_MATCHES_PER_PAGE = 3;
/** Characters of an excerpt: about one line of CJK, or 12–15 English words. */
const EXCERPT_LENGTH = 80;
/** Characters kept before the match, so it reads in context. */
const EXCERPT_LEAD = 20;

interface HastNode {
  type: string;
  tagName?: string;
  value?: string;
  children?: HastNode[];
}

/** The text of a parsed result; table cells are joined into one line. */
function textOf(node: HastNode): string {
  if (node.type === "text") return node.value ?? "";
  const parts = (node.children ?? []).map(textOf);
  if (node.tagName === "tr") return parts.filter((p) => p.trim()).join(" · ");
  return parts.join(node.tagName === "table" || node.tagName === "tbody" ? " " : "");
}

/** Cuts `text` to a window around the first place it matches `query`. */
function excerpt(text: string, query: string): string {
  const flat = text.replace(/\s+/g, " ").trim();
  if (flat.length <= EXCERPT_LENGTH) return flat;
  const lower = flat.toLowerCase();
  const terms = [query, ...query.split(/\s+/)].map((t) => t.trim().toLowerCase()).filter(Boolean);
  const hit = terms.map((t) => lower.indexOf(t)).find((i) => i >= 0) ?? 0;
  let start = Math.max(0, Math.min(hit - EXCERPT_LEAD, flat.length - EXCERPT_LENGTH));
  let end = start + EXCERPT_LENGTH;
  // Don't cut Latin words in half; CJK text has no spaces to snap to.
  const space = (from: number, to: number) => flat.slice(from, to).search(/\s/);
  if (start > 0 && /\w/.test(flat[start - 1] ?? "")) {
    const s = space(start, Math.min(hit, start + 15));
    if (s >= 0) start += s + 1;
  }
  if (end < flat.length && /\w/.test(flat[end] ?? "")) {
    const tail = flat.slice(end - 15, end);
    const s = tail.lastIndexOf(" ");
    if (s >= 0) end -= 15 - s;
  }
  return `${start > 0 ? "…" : ""}${flat.slice(start, end).trim()}${end < flat.length ? "…" : ""}`;
}

/** Flattens tables, caps the matches per page and shortens each one. */
function compact(items: SearchResultItem[], query: string): SearchItemType[] {
  const out: SearchItemType[] = [];
  let matches = 0;
  const rows = items.flatMap((item) => (item.type === "table" ? item.rows : [item]));
  for (const row of rows) {
    if (row.type === "page") {
      matches = 0;
      out.push({ id: row.id, url: row.url, type: "page", content: row.content, breadcrumbs: row.breadcrumbs });
      continue;
    }
    if (matches >= MAX_MATCHES_PER_PAGE) continue;
    matches += 1;
    const text = textOf(row.hastContent as HastNode);
    out.push({
      id: row.id,
      url: row.url,
      type: row.type,
      content: row.type === "heading" ? text : excerpt(text, query),
      breadcrumbs: row.breadcrumbs,
    });
  }
  return out;
}

export function DocsSearchDialog({ api, ...props }: SharedProps & { api?: string }) {
  const { locale } = useI18n();
  const search = useFetchSearch({ api, locale });
  const items = useMemo(
    () => (search.data ? compact(search.data.items, search.data.query) : null),
    [search.data],
  );
  return (
    <SearchDialog {...search} {...props}>
      <SearchDialogOverlay />
      <SearchDialogContent>
        <SearchDialogHeader>
          <SearchDialogIcon />
          <SearchDialogInput />
          <SearchDialogClose />
        </SearchDialogHeader>
        <SearchDialogList
          items={items}
          // Excerpts stay within two lines even on a phone.
          Item={(p) => <SearchDialogListItem {...p} className="[&_.prose]:line-clamp-2" />}
        />
      </SearchDialogContent>
    </SearchDialog>
  );
}
