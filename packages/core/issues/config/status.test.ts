import { describe, expect, it } from "vitest";
import {
  ALL_STATUSES,
  LIFEOS_BOARD_STATUSES,
  STATUS_ORDER,
} from "./status";

describe("LifeOS issue workflow", () => {
  it("puts chairman attention before review and completion", () => {
    expect(STATUS_ORDER).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "blocked",
      "in_review",
      "done",
      "cancelled",
    ]);
    expect(ALL_STATUSES).toEqual(STATUS_ORDER);
  });

  it("keeps cancelled recoverable but off the default board", () => {
    expect(LIFEOS_BOARD_STATUSES).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "blocked",
      "in_review",
      "done",
    ]);
    expect(ALL_STATUSES).toContain("cancelled");
  });
});
