import { describe, expect, it } from "vitest";
import { getProjectIssueMetrics } from "./project-issue-metrics";

describe("getProjectIssueMetrics", () => {
  it("surfaces project-level totals from the project record", () => {
    const metrics = getProjectIssueMetrics({ issue_count: 9, done_count: 5 });

    expect(metrics).toEqual({
      totalCount: 9,
      completedCount: 5,
    });
  });
});
