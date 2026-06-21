import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import type { TimelineGroup } from "./grouping";
import { partitionSessionGroups, countActivityEntries } from "./partition";
import { estimateSessionTokens, estimateSessionContextFraction } from "./context-estimate";

const entry = (id: string, content: string | undefined = id): TimelineEntry => ({
  type: "comment",
  id,
  actor_type: "member",
  actor_id: "u1",
  content,
  parent_id: null,
  created_at: "2026-06-21T00:00:00Z",
});

const commentGroup = (id: string): TimelineGroup => ({ type: "comment", entries: [entry(id)] });
const activityGroup = (...ids: string[]): TimelineGroup => ({
  type: "activities",
  entries: ids.map((i) => entry(i, undefined)),
});

describe("partitionSessionGroups", () => {
  it("splits comment threads from activity blocks, preserving order", () => {
    const groups: TimelineGroup[] = [
      commentGroup("c1"),
      activityGroup("a1", "a2"),
      commentGroup("c2"),
      activityGroup("a3"),
    ];
    const { commentGroups, activityGroups } = partitionSessionGroups(groups);
    expect(commentGroups.map((g) => g.entries[0]!.id)).toEqual(["c1", "c2"]);
    expect(activityGroups).toHaveLength(2);
    expect(countActivityEntries(activityGroups)).toBe(3);
  });

  it("handles a session with no activity", () => {
    const { activityGroups } = partitionSessionGroups([commentGroup("c1")]);
    expect(activityGroups).toHaveLength(0);
    expect(countActivityEntries(activityGroups)).toBe(0);
  });
});

describe("estimateSessionContextFraction", () => {
  it("counts only entry content characters", () => {
    const groups: TimelineGroup[] = [
      { type: "comment", entries: [entry("c1", "x".repeat(33))] }, // ~10 tokens
    ];
    expect(estimateSessionTokens(groups)).toBe(10);
  });

  it("clamps the fraction to 0..1", () => {
    const huge = "x".repeat(2_000_000);
    const groups: TimelineGroup[] = [{ type: "comment", entries: [entry("c1", huge)] }];
    expect(estimateSessionContextFraction(groups)).toBe(1);
    expect(estimateSessionContextFraction([])).toBe(0);
  });
});
