import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import {
  activeSessionId,
  defaultSession,
  groupTimelineByThread,
  sessionForTime,
  sessionIdForComment,
} from "./grouping";
import type { Session } from "./types";

const entry = (
  id: string,
  created_at: string,
  resolved_at: string | null = null,
  parent_id: string | null = null,
): TimelineEntry => ({
  type: "comment",
  id,
  actor_type: "member",
  actor_id: "u1",
  content: id,
  parent_id,
  created_at,
  resolved_at,
});

const commentGroup = (id: string, created_at: string, resolved_at: string | null = null) => ({
  type: "comment" as const,
  entries: [entry(id, created_at, resolved_at)],
});

const activityGroup = (id: string, created_at: string) => ({
  type: "activities" as const,
  entries: [{ ...entry(id, created_at), type: "activity" as const }],
});

const row = (root_comment_id: string, name: string, handoff: Session["handoff"] = null): Session => ({
  id: "row-" + root_comment_id,
  issue_id: "issue-1",
  root_comment_id,
  position: 0,
  name,
  phase: null,
  handoff,
  created_at: "2026-06-21T00:00:00Z",
  updated_at: "2026-06-21T00:00:00Z",
});

describe("groupTimelineByThread", () => {
  it("makes each comment thread its own session box keyed by the thread root id", () => {
    const groups = groupTimelineByThread("issue-1", [], [
      commentGroup("a", "2026-06-21T00:00:00Z"),
      commentGroup("b", "2026-06-21T01:00:00Z"),
    ]);
    expect(groups).toHaveLength(2);
    expect(groups.map((g) => g.session.id)).toEqual(["a", "b"]);
    expect(groups.map((g) => g.session.name)).toEqual(["Session 1", "Session 2"]);
  });

  it("derives open/closed from the thread root's resolved_at", () => {
    const groups = groupTimelineByThread("issue-1", [], [
      commentGroup("a", "2026-06-21T00:00:00Z", "2026-06-21T05:00:00Z"), // resolved
      commentGroup("b", "2026-06-21T01:00:00Z"), // open
    ]);
    expect(groups[0]!.resolved).toBe(true);
    expect(groups[1]!.resolved).toBe(false);
  });

  it("attaches an activity block to the thread that precedes it", () => {
    const groups = groupTimelineByThread("issue-1", [], [
      commentGroup("a", "2026-06-21T00:00:00Z"),
      activityGroup("act", "2026-06-21T00:30:00Z"),
      commentGroup("b", "2026-06-21T01:00:00Z"),
    ]);
    expect(groups).toHaveLength(2);
    expect(groups[0]!.groups.map((g) => g.type)).toEqual(["comment", "activities"]);
    expect(groups[1]!.groups.map((g) => g.type)).toEqual(["comment"]);
  });

  it("applies session-row metadata (name + handoff) by root_comment_id", () => {
    const handoff = { summary: "did work", done: [], remaining: [], plan_ref: null };
    const groups = groupTimelineByThread("issue-1", [row("a", "Login fix", handoff)], [
      commentGroup("a", "2026-06-21T00:00:00Z"),
    ]);
    expect(groups[0]!.session.name).toBe("Login fix");
    expect(groups[0]!.session.handoff).toEqual(handoff);
  });

  it("falls back to one implicit Session 1 when there are no threads (only activity)", () => {
    const groups = groupTimelineByThread("issue-1", [], [
      activityGroup("act", "2026-06-21T00:00:00Z"),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0]!.session.id).toBe("default");
    expect(groups[0]!.session.name).toBe("Session 1");
  });

  it("copies the workflow phase from the session row onto the built session", () => {
    const phasedRow: Session = { ...row("a", "Plan it"), phase: "plan" };
    const groups = groupTimelineByThread("issue-1", [phasedRow], [
      commentGroup("a", "2026-06-21T00:00:00Z"),
      commentGroup("b", "2026-06-21T01:00:00Z"),
    ]);
    // Row carries a phase → it flows through; a thread with no row → null.
    expect(groups[0]!.session.phase).toBe("plan");
    expect(groups[1]!.session.phase).toBeNull();
  });
});

describe("defaultSession", () => {
  it("has a null phase", () => {
    expect(defaultSession("issue-1").phase).toBeNull();
  });
});

describe("activeSessionId", () => {
  it("returns the latest OPEN thread", () => {
    const groups = groupTimelineByThread("issue-1", [], [
      commentGroup("a", "2026-06-21T00:00:00Z"),
      commentGroup("b", "2026-06-21T01:00:00Z", "2026-06-21T05:00:00Z"), // resolved
    ]);
    expect(activeSessionId(groups)).toBe("a"); // b is resolved, a is the latest open
  });

  it("falls back to the latest thread when all are resolved", () => {
    const groups = groupTimelineByThread("issue-1", [], [
      commentGroup("a", "2026-06-21T00:00:00Z", "2026-06-21T05:00:00Z"),
      commentGroup("b", "2026-06-21T01:00:00Z", "2026-06-21T06:00:00Z"),
    ]);
    expect(activeSessionId(groups)).toBe("b");
  });

  it("returns 'default' when there are no sessions", () => {
    expect(activeSessionId([])).toBe("default");
  });
});

describe("sessionForTime", () => {
  it("returns null when there are no session rows or none precede the time", () => {
    expect(sessionForTime([], "2026-06-21T00:00:00Z")).toBeNull();
    expect(sessionForTime(undefined, "2026-06-21T00:00:00Z")).toBeNull();
    expect(sessionForTime([row("a", "x")], "2020-01-01T00:00:00Z")).toBeNull();
  });

  it("picks the latest row whose created_at is at or before the time", () => {
    const rows: Session[] = [
      { ...row("a", "A"), created_at: "2026-06-21T00:00:00Z", position: 1 },
      { ...row("b", "B"), created_at: "2026-06-21T02:00:00Z", position: 2 },
    ];
    expect(sessionForTime(rows, "2026-06-21T01:00:00Z")?.name).toBe("A");
    expect(sessionForTime(rows, "2026-06-21T05:00:00Z")?.name).toBe("B");
  });
});

describe("sessionIdForComment", () => {
  it("resolves a root comment to its own id", () => {
    const timeline = [entry("a", "2026-06-21T01:00:00Z"), entry("b", "2026-06-21T03:00:00Z")];
    expect(sessionIdForComment(timeline, undefined, "a")).toBe("a");
    expect(sessionIdForComment(timeline, undefined, "b")).toBe("b");
  });

  it("walks a reply up to its thread root", () => {
    const timeline = [
      entry("a", "2026-06-21T01:00:00Z"),
      entry("r", "2026-06-21T03:30:00Z", null, "a"),
    ];
    expect(sessionIdForComment(timeline, undefined, "r")).toBe("a");
  });

  it("returns null when the comment isn't in the timeline", () => {
    const timeline = [entry("a", "2026-06-21T01:00:00Z")];
    expect(sessionIdForComment(timeline, undefined, "missing")).toBeNull();
  });
});
