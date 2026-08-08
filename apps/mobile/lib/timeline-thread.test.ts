import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { buildTimelineRows } from "./timeline-thread";

function comment(id: string, created_at: string, parent_id: string | null = null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "u-1",
    created_at,
    parent_id,
    content: `body-${id}`,
  };
}

function activity(id: string, created_at: string): TimelineEntry {
  return {
    type: "activity",
    id,
    actor_type: "member",
    actor_id: "u-1",
    created_at,
    action: "status_changed",
  };
}

describe("buildTimelineRows", () => {
  it("folds direct replies into their parent row in ASC order", () => {
    const a = comment("a", "2025-01-01T00:00:00Z");
    const b = comment("b", "2025-01-02T00:00:00Z", "a");
    const c = comment("c", "2025-01-03T00:00:00Z", "a");
    const rows = buildTimelineRows([a, b, c]);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.entry.id).toBe("a");
    expect(rows[0]!.replies.map((r) => r.id)).toEqual(["b", "c"]);
  });

  it("flattens reply-of-reply chains into the root row", () => {
    const a = comment("a", "2025-01-01T00:00:00Z");
    const b = comment("b", "2025-01-02T00:00:00Z", "a");
    const c = comment("c", "2025-01-03T00:00:00Z", "b");
    const rows = buildTimelineRows([a, b, c]);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.replies.map((r) => r.id)).toEqual(["b", "c"]);
  });

  it("T-A3: cross-branch reply ordering is chronological across BFS levels", () => {
    // Root A; branch 1 has B (late) → B2 (earlier than C? no); the specific
    // bug: a direct reply C can have an earlier timestamp than a nested reply
    // B→B2 and still must appear first.
    const a = comment("a", "2025-01-01T00:00:00Z");
    const c = comment("c", "2025-01-02T00:00:00Z", "a");
    const b = comment("b", "2025-01-03T00:00:00Z", "a");
    const b2 = comment("b2", "2025-01-04T00:00:00Z", "b");
    // Input order is deliberately scrambled (BFS would give b, c, b2).
    const rows = buildTimelineRows([a, b, c, b2]);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.replies.map((r) => r.id)).toEqual(["c", "b", "b2"]);
  });

  it("T-A4: orphan replies (parent not in batch) are promoted to top-level", () => {
    const a = comment("a", "2025-01-01T00:00:00Z");
    // b replies to a missing parent "ghost" — must NOT vanish.
    const b = comment("b", "2025-01-02T00:00:00Z", "ghost");
    const rows = buildTimelineRows([a, b]);
    expect(rows.map((r) => r.entry.id).sort()).toEqual(["a", "b"]);
    const bRow = rows.find((r) => r.entry.id === "b")!;
    expect(bRow.replies).toEqual([]);
  });

  it("T-A5: does not mutate the input array", () => {
    const a = comment("a", "2025-01-03T00:00:00Z");
    const b = comment("b", "2025-01-01T00:00:00Z");
    const c = comment("c", "2025-01-02T00:00:00Z");
    const input = [a, b, c];
    const snapshot = input.map((e) => e.id);
    buildTimelineRows(input, "newest");
    expect(input.map((e) => e.id)).toEqual(snapshot);
  });

  it("top-level direction reverses row order but leaves replies ASC", () => {
    const a = comment("a", "2025-01-01T00:00:00Z");
    const a1 = comment("a1", "2025-01-02T00:00:00Z", "a");
    const b = comment("b", "2025-01-03T00:00:00Z");
    const b1 = comment("b1", "2025-01-04T00:00:00Z", "b");
    const rows = buildTimelineRows([a, a1, b, b1], "newest");
    expect(rows.map((r) => r.entry.id)).toEqual(["b", "a"]);
    expect(rows[0]!.replies.map((r) => r.id)).toEqual(["b1"]);
    expect(rows[1]!.replies.map((r) => r.id)).toEqual(["a1"]);
  });

  it("activities stay top-level and keep direction", () => {
    const act = activity("ev-1", "2025-01-01T00:00:00Z");
    const c = comment("c", "2025-01-02T00:00:00Z");
    const oldest = buildTimelineRows([act, c], "oldest");
    const newest = buildTimelineRows([act, c], "newest");
    expect(oldest.map((r) => r.entry.id)).toEqual(["ev-1", "c"]);
    expect(newest.map((r) => r.entry.id)).toEqual(["c", "ev-1"]);
    // Activity rows have no replies.
    expect(oldest[0]!.replies).toEqual([]);
  });
});
