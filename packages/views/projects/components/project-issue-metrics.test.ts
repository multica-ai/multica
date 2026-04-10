import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { getProjectIssueMetrics } from "./project-issue-metrics";

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Test issue",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: "project-1",
    position: 0,
    due_date: null,
    created_at: "2026-04-10T00:00:00Z",
    updated_at: "2026-04-10T00:00:00Z",
    ...overrides,
  };
}

describe("getProjectIssueMetrics", () => {
  it("uses project totals for progress and project-local done issues for the kanban done count", () => {
    const metrics = getProjectIssueMetrics(
      { issue_count: 9, done_count: 5 },
      [
        makeIssue({ id: "issue-1", status: "done" }),
        makeIssue({ id: "issue-2", status: "done" }),
        makeIssue({ id: "issue-3", status: "cancelled" }),
        makeIssue({ id: "issue-4", status: "todo" }),
      ],
    );

    expect(metrics).toEqual({
      totalCount: 9,
      completedCount: 5,
      doneColumnCount: 2,
    });
  });
});
