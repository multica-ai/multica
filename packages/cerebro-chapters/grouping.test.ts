import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { groupTimelineByChapter } from "./grouping";
import type { Chapter } from "./types";

const entry = (id: string, chapter_id?: string | null): TimelineEntry => ({
  type: "comment",
  id,
  actor_type: "member",
  actor_id: "u1",
  content: id,
  parent_id: null,
  chapter_id,
  created_at: "2026-06-21T00:00:00Z",
});

const chapter = (id: string, position: number): Chapter => ({
  id,
  issue_id: "issue-1",
  name: `Session ${position}`,
  status: position === 1 ? "done" : "in_progress",
  position,
  handoff_summary: "",
  handoff_done: [],
  handoff_remaining: [],
  plan_ref: null,
  created_at: "",
  updated_at: "",
});

describe("groupTimelineByChapter", () => {
  it("keeps null chapter roots in the default chapter", () => {
    const groups = groupTimelineByChapter("issue-1", [chapter("c1", 1)], [
      { type: "comment", entries: [entry("root-a", null)] },
    ]);
    expect(groups).toHaveLength(2);
    expect(groups[1]!.chapter.id).toBe("default");
    expect(groups[1]!.entries.map((e) => e.id)).toEqual(["root-a"]);
  });

  it("orders concrete chapters by position", () => {
    const groups = groupTimelineByChapter("issue-1", [chapter("c2", 2), chapter("c1", 1)], [
      { type: "comment", entries: [entry("root-b", "c2")] },
      { type: "comment", entries: [entry("root-a", "c1")] },
    ]);
    expect(groups.map((g) => g.chapter.id)).toEqual(["c1", "c2"]);
    expect(groups.map((g) => g.entries[0]!.id)).toEqual(["root-a", "root-b"]);
  });
});
