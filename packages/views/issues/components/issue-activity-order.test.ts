// @vitest-environment node
import { describe, expect, it } from "vitest";
import { orderActivitySlots } from "./issue-activity-order";

type Entry = {
  id: string;
  type: "activity" | "comment" | "run";
};

const isActivity = (entry: Entry) => entry.type === "activity";
const ids = (entries: readonly Entry[]) => entries.map((entry) => entry.id);

describe("orderActivitySlots", () => {
  const timeline: Entry[] = [
    { id: "activity-1", type: "activity" },
    { id: "comment-1", type: "comment" },
    { id: "activity-2", type: "activity" },
    { id: "run-1", type: "run" },
    { id: "activity-3", type: "activity" },
    { id: "comment-2", type: "comment" },
  ];

  it("keeps the default chronological order", () => {
    expect(ids(orderActivitySlots(timeline, "oldest", isActivity))).toEqual(
      ids(timeline),
    );
  });

  it("reverses activities without moving comments or runs", () => {
    expect(ids(orderActivitySlots(timeline, "newest", isActivity))).toEqual([
      "activity-3",
      "comment-1",
      "activity-2",
      "run-1",
      "activity-1",
      "comment-2",
    ]);
  });

  it("places a newly appended realtime activity in the first activity slot", () => {
    const updated = [
      ...timeline,
      { id: "activity-4", type: "activity" as const },
    ];

    expect(ids(orderActivitySlots(updated, "newest", isActivity))).toEqual([
      "activity-4",
      "comment-1",
      "activity-3",
      "run-1",
      "activity-2",
      "comment-2",
      "activity-1",
    ]);
  });
});
