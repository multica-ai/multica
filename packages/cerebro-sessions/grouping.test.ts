import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { groupTimelineBySession } from "./grouping";
import type { Session } from "./types";

const entry = (id: string, created_at: string): TimelineEntry => ({
  type: "comment",
  id,
  actor_type: "member",
  actor_id: "u1",
  content: id,
  parent_id: null,
  created_at,
});

const commentGroup = (id: string, created_at: string) => ({
  type: "comment" as const,
  entries: [entry(id, created_at)],
});

const session = (id: string, position: number, created_at: string, status: Session["status"] = "in_progress"): Session => ({
  id,
  issue_id: "issue-1",
  position,
  name: `Session ${position}`,
  status,
  handoff: null,
  created_at,
  updated_at: created_at,
});

describe("groupTimelineBySession", () => {
  it("renders one implicit Session 1 when there are no session markers", () => {
    const groups = groupTimelineBySession("issue-1", [], [
      commentGroup("a", "2026-06-21T00:00:00Z"),
      commentGroup("b", "2026-06-21T01:00:00Z"),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0]!.session.id).toBe("default");
    expect(groups[0]!.session.name).toBe("Session 1");
    expect(groups[0]!.groups.map((g) => g.entries[0]!.id)).toEqual(["a", "b"]);
  });

  it("assigns each entry to the marker that precedes it in time", () => {
    const sessions = [
      session("s1", 1, "2026-06-21T00:00:00Z", "done"),
      session("s2", 2, "2026-06-21T02:00:00Z"),
    ];
    const groups = groupTimelineBySession("issue-1", sessions, [
      commentGroup("before-2", "2026-06-21T01:00:00Z"), // belongs to s1
      commentGroup("after-2", "2026-06-21T03:00:00Z"), // belongs to s2
    ]);
    expect(groups.map((g) => g.session.id)).toEqual(["s1", "s2"]);
    expect(groups[0]!.groups.map((g) => g.entries[0]!.id)).toEqual(["before-2"]);
    expect(groups[1]!.groups.map((g) => g.entries[0]!.id)).toEqual(["after-2"]);
  });

  it("never orphans entries that predate the first marker", () => {
    const sessions = [session("s1", 1, "2026-06-21T05:00:00Z")];
    const groups = groupTimelineBySession("issue-1", sessions, [
      commentGroup("old", "2026-06-21T00:00:00Z"),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0]!.session.id).toBe("s1");
    expect(groups[0]!.groups.map((g) => g.entries[0]!.id)).toEqual(["old"]);
  });

  it("keeps an empty active session so its header still shows", () => {
    const sessions = [
      session("s1", 1, "2026-06-21T00:00:00Z", "done"),
      session("s2", 2, "2026-06-21T02:00:00Z"),
    ];
    const groups = groupTimelineBySession("issue-1", sessions, [
      commentGroup("only", "2026-06-21T01:00:00Z"),
    ]);
    expect(groups.map((g) => g.session.id)).toEqual(["s1", "s2"]);
    expect(groups[1]!.groups).toHaveLength(0);
  });
});
