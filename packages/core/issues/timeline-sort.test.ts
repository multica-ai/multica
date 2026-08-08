import { describe, it, expect } from "vitest";
import type { TimelineEntry } from "../types";
import {
  compareTimelineEntriesAsc,
  sortTimelineEntries,
  sortTimelineEntriesAsc,
} from "./timeline-sort";

function entry(id: string, created_at: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "u-1",
    created_at,
  };
}

describe("compareTimelineEntriesAsc", () => {
  it("orders by created_at ascending", () => {
    const a = entry("a", "2025-01-01T00:00:00Z");
    const b = entry("b", "2025-01-02T00:00:00Z");
    expect(compareTimelineEntriesAsc(a, b)).toBe(-1);
    expect(compareTimelineEntriesAsc(b, a)).toBe(1);
  });

  it("ties break on id for a deterministic order", () => {
    const a = entry("a", "2025-01-01T00:00:00Z");
    const b = entry("b", "2025-01-01T00:00:00Z");
    expect(compareTimelineEntriesAsc(a, b)).toBe(-1);
    expect(compareTimelineEntriesAsc(b, a)).toBe(1);
    expect(compareTimelineEntriesAsc(a, a)).toBe(0);
  });
});

describe("sortTimelineEntriesAsc", () => {
  it("sorts in place and returns the same reference", () => {
    const arr = [
      entry("c", "2025-01-03T00:00:00Z"),
      entry("a", "2025-01-01T00:00:00Z"),
      entry("b", "2025-01-02T00:00:00Z"),
    ];
    const out = sortTimelineEntriesAsc(arr);
    expect(out).toBe(arr);
    expect(arr.map((e) => e.id)).toEqual(["a", "b", "c"]);
  });
});

describe("sortTimelineEntries (display layer)", () => {
  const input = [
    entry("c", "2025-01-03T00:00:00Z"),
    entry("a", "2025-01-01T00:00:00Z"),
    entry("b", "2025-01-02T00:00:00Z"),
  ];

  it("oldest returns ascending order on a copy", () => {
    const out = sortTimelineEntries(input, "oldest");
    expect(out.map((e) => e.id)).toEqual(["a", "b", "c"]);
    expect(out).not.toBe(input);
    // Input is untouched — safe for React Query cache references.
    expect(input.map((e) => e.id)).toEqual(["c", "a", "b"]);
  });

  it("newest returns descending order on a copy", () => {
    const out = sortTimelineEntries(input, "newest");
    expect(out.map((e) => e.id)).toEqual(["c", "b", "a"]);
    expect(out).not.toBe(input);
    expect(input.map((e) => e.id)).toEqual(["c", "a", "b"]);
  });

  it("accepts readonly arrays", () => {
    const frozen: readonly TimelineEntry[] = [
      entry("a", "2025-01-01T00:00:00Z"),
      entry("b", "2025-01-02T00:00:00Z"),
    ];
    expect(() => sortTimelineEntries(frozen, "newest")).not.toThrow();
  });
});
