import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { groupTimelineBySession, sessionForTime, sessionIdForComment } from "./grouping";
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

describe("sessionForTime", () => {
  it("returns null when there are no real sessions", () => {
    expect(sessionForTime([], "2026-06-21T00:00:00Z")).toBeNull();
    expect(sessionForTime(undefined, "2026-06-21T00:00:00Z")).toBeNull();
  });

  it("picks the latest session whose start is at or before the time", () => {
    const sessions = [
      session("s1", 1, "2026-06-21T00:00:00Z"),
      session("s2", 2, "2026-06-21T02:00:00Z"),
    ];
    expect(sessionForTime(sessions, "2026-06-21T01:00:00Z")?.id).toBe("s1");
    expect(sessionForTime(sessions, "2026-06-21T02:00:00Z")?.id).toBe("s2");
    expect(sessionForTime(sessions, "2026-06-21T05:00:00Z")?.id).toBe("s2");
  });

  it("falls into the earliest session for times before the first marker", () => {
    const sessions = [
      session("s1", 1, "2026-06-21T01:00:00Z"),
      session("s2", 2, "2026-06-21T03:00:00Z"),
    ];
    expect(sessionForTime(sessions, "2026-06-21T00:00:00Z")?.id).toBe("s1");
  });

  it("ignores input order — sorts by created_at then position", () => {
    const sessions = [
      session("s2", 2, "2026-06-21T02:00:00Z"),
      session("s1", 1, "2026-06-21T00:00:00Z"),
    ];
    expect(sessionForTime(sessions, "2026-06-21T02:30:00Z")?.id).toBe("s2");
  });
});

describe("sessionIdForComment (FIR-1839 point 8)", () => {
  const reply = (id: string, parent_id: string, created_at: string): TimelineEntry => ({
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "u1",
    content: id,
    parent_id,
    created_at,
  });

  const sessions = [
    session("s1", 1, "2026-06-21T00:00:00Z"),
    session("s2", 2, "2026-06-21T02:00:00Z"),
  ];

  it("resolves a root comment to the session that owns its time", () => {
    const timeline = [entry("a", "2026-06-21T01:00:00Z"), entry("b", "2026-06-21T03:00:00Z")];
    expect(sessionIdForComment(timeline, sessions, "a")).toBe("s1");
    expect(sessionIdForComment(timeline, sessions, "b")).toBe("s2");
  });

  it("walks a reply up to its thread root, so a late reply stays in the root's session", () => {
    // Root 'a' lives in s1; a reply made at 03:00 (in s2's window) must still
    // resolve to s1 because its thread root belongs to s1.
    const timeline = [
      entry("a", "2026-06-21T01:00:00Z"),
      reply("r", "a", "2026-06-21T03:30:00Z"),
    ];
    expect(sessionIdForComment(timeline, sessions, "r")).toBe("s1");
  });

  it("returns null when the comment isn't in the timeline or there are no sessions", () => {
    const timeline = [entry("a", "2026-06-21T01:00:00Z")];
    expect(sessionIdForComment(timeline, sessions, "missing")).toBeNull();
    expect(sessionIdForComment(timeline, [], "a")).toBeNull();
    expect(sessionIdForComment(timeline, undefined, "a")).toBeNull();
  });
});
