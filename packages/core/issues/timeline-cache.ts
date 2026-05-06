import type { InfiniteData } from "@tanstack/react-query";
import type {
  TimelineEntry,
  TimelinePage,
  TimelinePageParam,
} from "../types";

/** Shape of the cursor-paginated timeline cache. Exported so consumers (the
 *  hook, mutations, tests) all reference the same type. */
export type TimelineCacheData = InfiniteData<TimelinePage, TimelinePageParam>;

/** Map fn over every entry across every page, preserving page identity for
 *  any page whose entries don't change so React.memo on CommentCard isn't
 *  defeated by gratuitous reference churn.
 *
 *  Defensive on shape: pages / entries may be non-arrays if a malformed
 *  entry was ever written to the cache. Return unchanged in that case so
 *  downstream consumers don't inherit a corrupted cache. */
export function mapAllEntries(
  data: TimelineCacheData | undefined,
  fn: (e: TimelineEntry) => TimelineEntry,
): TimelineCacheData | undefined {
  if (!data || !Array.isArray(data.pages)) return data;
  let pagesChanged = false;
  const pages = data.pages.map((page) => {
    if (!page || !Array.isArray(page.entries)) return page;
    let entriesChanged = false;
    const entries = page.entries.map((e) => {
      const next = fn(e);
      if (next !== e) entriesChanged = true;
      return next;
    });
    if (!entriesChanged) return page;
    pagesChanged = true;
    return { ...page, entries };
  });
  if (!pagesChanged) return data;
  return { ...data, pages };
}

/** Filter out entries matching the predicate from every page. */
export function filterAllEntries(
  data: TimelineCacheData | undefined,
  predicate: (e: TimelineEntry) => boolean,
): TimelineCacheData | undefined {
  if (!data || !Array.isArray(data.pages)) return data;
  let pagesChanged = false;
  const pages = data.pages.map((page) => {
    if (!page || !Array.isArray(page.entries)) return page;
    const entries = page.entries.filter((e) => !predicate(e));
    if (entries.length === page.entries.length) return page;
    pagesChanged = true;
    return { ...page, entries };
  });
  if (!pagesChanged) return data;
  return { ...data, pages };
}

/** Prepend a new entry to the latest page (pages[0]). Caller must verify
 *  the cache is at-latest before calling — otherwise the entry is hidden
 *  behind a "show newer" gap and shouldn't be injected. Returns the data
 *  unchanged if the cache is not at-latest or the entry already exists. */
export function prependToLatestPage(
  data: TimelineCacheData | undefined,
  entry: TimelineEntry,
): TimelineCacheData | undefined {
  if (!data || !Array.isArray(data.pages) || data.pages.length === 0) return data;
  const first = data.pages[0];
  if (!first || !Array.isArray(first.entries)) return data;
  if (first.has_more_after) return data; // not at latest; skip silently
  if (first.entries.some((e) => e.id === entry.id)) return data;
  return {
    ...data,
    pages: [
      { ...first, entries: [entry, ...first.entries] },
      ...data.pages.slice(1),
    ],
  };
}
