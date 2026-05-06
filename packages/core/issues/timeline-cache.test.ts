import { describe, expect, it } from "vitest";
import {
  mapAllEntries,
  filterAllEntries,
  prependToLatestPage,
  collectDeletedCommentIds,
  type TimelineCacheData,
} from "./timeline-cache";
import type { TimelineEntry } from "../types";

// Exercises the defensive guards added for upstream#2143 / #2147:
// `timeline.filter is not a function`. The Inbox page / issue detail reads
// the flattened timeline via a hook that iterates `data.pages[].entries`.
// A malformed cache entry (wrong shape from a persisted older build, a
// stale setQueryData caller in future code) used to white-screen the whole
// route. These helpers now tolerate non-array shapes and return the data
// unchanged, so the consumer falls back to "empty timeline" instead of
// crashing the React tree.

function entry(id: string, createdAt: string, parentId?: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "u",
    created_at: createdAt,
    ...(parentId ? { parent_id: parentId } : {}),
  };
}

function wellFormed(entries: TimelineEntry[]): TimelineCacheData {
  return {
    pages: [
      {
        entries,
        next_cursor: null,
        prev_cursor: null,
        has_more_before: false,
        has_more_after: false,
      },
    ],
    pageParams: [{ mode: "latest" }],
  };
}

describe("timeline-cache helpers — malformed-shape tolerance", () => {
  it("mapAllEntries returns data unchanged when pages is not an array", () => {
    const bad = { pages: null as unknown, pageParams: [] } as unknown as TimelineCacheData;
    const out = mapAllEntries(bad, (e) => ({ ...e, content: "modified" }));
    expect(out).toBe(bad);
  });

  it("mapAllEntries returns data unchanged when pages is an object (e.g. { entries: [...] } accidentally stored)", () => {
    const bad = { pages: { entries: [] } as unknown, pageParams: [] } as unknown as TimelineCacheData;
    const out = mapAllEntries(bad, (e) => e);
    expect(out).toBe(bad);
  });

  it("mapAllEntries skips pages whose entries is not an array, keeps others", () => {
    const mixed = {
      pages: [
        { entries: null, next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
        { entries: [entry("c1", "2026-05-06T01:00:00Z")], next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
      ],
      pageParams: [{ mode: "latest" }, { mode: "before", cursor: "x" }],
    } as unknown as TimelineCacheData;
    const out = mapAllEntries(mixed, (e) => ({ ...e, content: "x" }));
    // First page (malformed) returned verbatim; second page rebuilt with mapped entries.
    expect(out).toBeDefined();
    expect(out!.pages[0]).toBe(mixed.pages[0]);
    expect(out!.pages[1]!.entries[0]).toMatchObject({ id: "c1", content: "x" });
  });

  it("filterAllEntries returns data unchanged when pages is not an array", () => {
    const bad = { pages: undefined as unknown, pageParams: [] } as unknown as TimelineCacheData;
    const out = filterAllEntries(bad, () => true);
    expect(out).toBe(bad);
  });

  it("filterAllEntries skips pages with non-array entries", () => {
    const mixed = {
      pages: [
        { entries: "not-an-array" as unknown, next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
        { entries: [entry("c1", "t1"), entry("c2", "t2")], next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
      ],
      pageParams: [{ mode: "latest" }, { mode: "before", cursor: "x" }],
    } as unknown as TimelineCacheData;
    const out = filterAllEntries(mixed, (e) => e.id === "c1");
    expect(out).toBeDefined();
    expect(out!.pages[0]).toBe(mixed.pages[0]);
    expect(out!.pages[1]!.entries.map((e) => e.id)).toEqual(["c2"]);
  });

  it("prependToLatestPage returns data unchanged when pages is not an array", () => {
    const bad = { pages: {} as unknown, pageParams: [] } as unknown as TimelineCacheData;
    const out = prependToLatestPage(bad, entry("new", "t"));
    expect(out).toBe(bad);
  });

  it("prependToLatestPage returns data unchanged when first page entries is not an array", () => {
    const bad = {
      pages: [
        { entries: null as unknown, next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
      ],
      pageParams: [{ mode: "latest" }],
    } as unknown as TimelineCacheData;
    const out = prependToLatestPage(bad, entry("new", "t"));
    expect(out).toBe(bad);
  });

  // Sanity: helpers still work on well-formed data.
  it("mapAllEntries maps entries on well-formed data", () => {
    const data = wellFormed([entry("c1", "t1"), entry("c2", "t2")]);
    const out = mapAllEntries(data, (e) =>
      e.id === "c1" ? { ...e, content: "changed" } : e,
    );
    expect(out).toBeDefined();
    expect(out!.pages[0]!.entries[0]).toMatchObject({ id: "c1", content: "changed" });
    // Identity preserved for unchanged entry.
    expect(out!.pages[0]!.entries[1]).toBe(data.pages[0]!.entries[1]);
  });
});

// useDeleteComment.onMutate uses this helper to walk every open timeline
// window (latest + around-mode caches) and collect the root comment plus all
// descendants before calling filterAllEntries. Before this helper existed,
// the walk ran inline in mutations.ts and read `data.pages` / `page.entries`
// unguarded — the same shape that white-screened the render side in
// upstream#2143 / #2147 could have crashed the delete path too.
describe("collectDeletedCommentIds", () => {
  it("returns a set containing only the root when snapshots are empty", () => {
    const out = collectDeletedCommentIds([], "c1");
    expect(out).toEqual(new Set(["c1"]));
  });

  it("cascades through direct children on a single snapshot", () => {
    const snap: [unknown, TimelineCacheData] = [
      ["issues", "timeline", "i1"],
      wellFormed([
        entry("c1", "t1"),
        entry("c2", "t2", "c1"),
        entry("c3", "t3", "c1"),
      ]),
    ];
    const out = collectDeletedCommentIds([snap], "c1");
    expect(out).toEqual(new Set(["c1", "c2", "c3"]));
  });

  it("cascades through multi-level nesting (grandchildren)", () => {
    const snap: [unknown, TimelineCacheData] = [
      ["k"],
      wellFormed([
        entry("c1", "t1"),
        entry("c2", "t2", "c1"),
        entry("c3", "t3", "c2"),
        entry("unrelated", "t4"),
      ]),
    ];
    const out = collectDeletedCommentIds([snap], "c1");
    expect(out).toEqual(new Set(["c1", "c2", "c3"]));
    expect(out.has("unrelated")).toBe(false);
  });

  it("merges descendants found across multiple snapshots (latest + around window)", () => {
    // Parent on latest page, reply on an older around-mode window.
    const latest: [unknown, TimelineCacheData] = [
      ["k1"],
      wellFormed([entry("c1", "t1")]),
    ];
    const around: [unknown, TimelineCacheData] = [
      ["k2"],
      wellFormed([entry("c2", "t2", "c1")]),
    ];
    const out = collectDeletedCommentIds([latest, around], "c1");
    expect(out).toEqual(new Set(["c1", "c2"]));
  });

  it("skips snapshots whose data.pages is not an array (no throw)", () => {
    const bad: [unknown, TimelineCacheData] = [
      ["k"],
      { pages: null as unknown, pageParams: [] } as unknown as TimelineCacheData,
    ];
    const good: [unknown, TimelineCacheData] = [
      ["k2"],
      wellFormed([entry("c1", "t1"), entry("c2", "t2", "c1")]),
    ];
    expect(() => collectDeletedCommentIds([bad, good], "c1")).not.toThrow();
    const out = collectDeletedCommentIds([bad, good], "c1");
    // Good snapshot still processed.
    expect(out).toEqual(new Set(["c1", "c2"]));
  });

  it("skips pages whose entries is not an array (no throw, other pages processed)", () => {
    const snap: [unknown, TimelineCacheData] = [
      ["k"],
      {
        pages: [
          { entries: null, next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
          { entries: [entry("c2", "t2", "c1")], next_cursor: null, prev_cursor: null, has_more_before: false, has_more_after: false },
        ],
        pageParams: [{ mode: "latest" }, { mode: "before", cursor: "x" }],
      } as unknown as TimelineCacheData,
    ];
    expect(() => collectDeletedCommentIds([snap], "c1")).not.toThrow();
    const out = collectDeletedCommentIds([snap], "c1");
    expect(out).toEqual(new Set(["c1", "c2"]));
  });

  it("tolerates undefined data in a snapshot", () => {
    const missing: [unknown, TimelineCacheData | undefined] = [["k"], undefined];
    expect(() => collectDeletedCommentIds([missing], "c1")).not.toThrow();
    expect(collectDeletedCommentIds([missing], "c1")).toEqual(new Set(["c1"]));
  });
});
