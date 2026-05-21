import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  UNPARENTED_SWIMLANE_ID,
  buildIssueSwimlanes,
  buildIssueTreeRows,
} from "./issue-hierarchy";

const baseIssue: Omit<Issue, "id" | "number" | "identifier" | "title" | "status" | "parent_issue_id"> = {
  workspace_id: "ws-1",
  description: null,
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  project_id: null,
  position: 0,
  start_date: null,
  due_date: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function issue(overrides: Pick<Issue, "id" | "number" | "identifier" | "title" | "status"> & Partial<Issue>): Issue {
  return {
    ...baseIssue,
    parent_issue_id: null,
    ...overrides,
  };
}

describe("issue hierarchy utilities", () => {
  it("nests same-status children under their visible parent", () => {
    const parent = issue({
      id: "parent",
      number: 1,
      identifier: "TES-1",
      title: "Parent",
      status: "todo",
    });
    const child = issue({
      id: "child",
      number: 2,
      identifier: "TES-2",
      title: "Child",
      status: "todo",
      parent_issue_id: parent.id,
    });

    const rows = buildIssueTreeRows([parent, child], [parent, child]);

    expect(rows.map((row) => [row.issue.id, row.depth])).toEqual([
      ["parent", 0],
      ["child", 1],
    ]);
    expect(rows[1]!.parentIssue?.id).toBe(parent.id);
  });

  it("keeps a cross-status child visible with a parent reference", () => {
    const parent = issue({
      id: "parent",
      number: 1,
      identifier: "TES-1",
      title: "Parent",
      status: "todo",
    });
    const child = issue({
      id: "child",
      number: 2,
      identifier: "TES-2",
      title: "Child",
      status: "in_progress",
      parent_issue_id: parent.id,
    });

    const rows = buildIssueTreeRows([child], [parent, child]);

    expect(rows).toHaveLength(1);
    expect(rows[0]!.depth).toBe(1);
    expect(rows[0]!.parentIssue?.identifier).toBe("TES-1");
  });

  it("builds parent swimlanes while keeping standalone issues visible", () => {
    const parent = issue({
      id: "parent",
      number: 1,
      identifier: "TES-1",
      title: "Parent",
      status: "todo",
    });
    const todoChild = issue({
      id: "todo-child",
      number: 2,
      identifier: "TES-2",
      title: "Todo child",
      status: "todo",
      parent_issue_id: parent.id,
    });
    const reviewChild = issue({
      id: "review-child",
      number: 3,
      identifier: "TES-3",
      title: "Review child",
      status: "in_review",
      parent_issue_id: parent.id,
    });
    const standalone = issue({
      id: "standalone",
      number: 4,
      identifier: "TES-4",
      title: "Standalone",
      status: "todo",
    });

    const lanes = buildIssueSwimlanes(
      [parent, todoChild, reviewChild, standalone],
      [parent, todoChild, reviewChild, standalone],
      ["todo", "in_review"],
    );

    expect(lanes).toHaveLength(2);
    expect(lanes[0]!.parentIssue?.id).toBe(parent.id);
    expect(lanes[0]!.issueIdsByStatus.todo).toEqual(["todo-child"]);
    expect(lanes[0]!.issueIdsByStatus.in_review).toEqual(["review-child"]);
    expect(lanes[1]!.id).toBe(UNPARENTED_SWIMLANE_ID);
    expect(lanes[1]!.issueIdsByStatus.todo).toEqual(["standalone"]);
  });
});
