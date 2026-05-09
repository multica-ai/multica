import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "../types";
import {
  filterTimelineEntries,
  getTimelineEntries,
  mapTimelineEntries,
  prependTimelineEntry,
  type TimelineCacheData,
} from "./timeline-cache";

function entry(id: string, parentId: string | null = null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content: id,
    parent_id: parentId,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function infiniteTimeline(entries: TimelineEntry[]): TimelineCacheData {
  return {
    pageParams: [{ mode: "latest" }],
    pages: [{
      entries,
      next_cursor: null,
      prev_cursor: null,
      has_more_before: false,
      has_more_after: false,
    }],
  };
}

describe("timeline cache helpers", () => {
  it("prepends entries into mobile flat timeline caches", () => {
    const existing = entry("existing");
    const created = entry("created");

    expect(prependTimelineEntry([existing], created)).toEqual([created, existing]);
    expect(prependTimelineEntry([created, existing], created)).toEqual([created, existing]);
  });

  it("updates flat and infinite timeline cache entries", () => {
    const updatedFlat = mapTimelineEntries([entry("a")], (item) =>
      item.id === "a" ? { ...item, content: "updated" } : item,
    );
    const updatedInfinite = mapTimelineEntries(infiniteTimeline([entry("a")]), (item) =>
      item.id === "a" ? { ...item, content: "updated" } : item,
    );

    expect(getTimelineEntries(updatedFlat)[0]?.content).toBe("updated");
    expect(getTimelineEntries(updatedInfinite)[0]?.content).toBe("updated");
  });

  it("filters flat and infinite timeline cache entries", () => {
    const flat = filterTimelineEntries([entry("a"), entry("b")], (item) => item.id === "a");
    const infinite = filterTimelineEntries(
      infiniteTimeline([entry("a"), entry("b")]),
      (item) => item.id === "a",
    );

    expect(getTimelineEntries(flat).map((item) => item.id)).toEqual(["b"]);
    expect(getTimelineEntries(infinite).map((item) => item.id)).toEqual(["b"]);
  });
});
