import { describe, expect, it } from "vitest";
import {
  ALL_STATUSES,
  BUILT_IN_STATUS_CATEGORY,
  BUILT_IN_STATUS_ORDER,
  STATUS_ORDER,
} from "./status";

describe("LifeOS issue workflow", () => {
  it("uses the current four lifecycle categories", () => {
    expect(STATUS_ORDER).toEqual([
      "unstarted",
      "started",
      "done",
      "closed",
    ]);
    expect(ALL_STATUSES).toEqual(STATUS_ORDER);
  });

  it("keeps the installed LifeOS statuses mapped to those categories", () => {
    expect(BUILT_IN_STATUS_ORDER).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "blocked",
      "done",
      "cancelled",
    ]);
    expect(BUILT_IN_STATUS_CATEGORY.blocked).toBe("started");
    expect(BUILT_IN_STATUS_CATEGORY.cancelled).toBe("closed");
  });
});
