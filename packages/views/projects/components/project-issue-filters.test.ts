import { describe, expect, it } from "vitest";
import type { Issue, IssueAssigneeGroup } from "@multica/core/types";
import { filterRunningAssigneeGroups } from "./project-issue-filters";

function issue(id: string): Issue {
  return {
    id,
    title: `Issue ${id}`,
    identifier: id,
    number: Number(id.replace(/\D/g, "")) || 0,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    workspace_id: "ws-1",
    project_id: "project-1",
    parent_issue_id: null,
    position: 0,
    due_date: null,
    start_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as Issue;
}

function group(id: string, issues: Issue[]): IssueAssigneeGroup {
  return {
    id,
    assignee_type: id === "none" ? null : "agent",
    assignee_id: id === "none" ? null : id,
    issues,
    total: issues.length,
  };
}

describe("filterRunningAssigneeGroups", () => {
  it("returns the original groups when the agent running filter is off", () => {
    const groups = [group("agent-1", [issue("issue-1"), issue("issue-2")])];

    expect(filterRunningAssigneeGroups(groups, false, new Set(["issue-1"]))).toBe(groups);
  });

  it("keeps only running issues and removes empty assignee groups", () => {
    const groups = [
      group("agent-1", [issue("issue-1"), issue("issue-2")]),
      group("agent-2", [issue("issue-3")]),
      group("none", [issue("issue-4")]),
    ];

    const result = filterRunningAssigneeGroups(groups, true, new Set(["issue-2", "issue-4"]));

    expect(result!.map((g) => ({ id: g.id, issueIds: g.issues.map((i) => i.id), total: g.total }))).toEqual([
      { id: "agent-1", issueIds: ["issue-2"], total: 1 },
      { id: "none", issueIds: ["issue-4"], total: 1 },
    ]);
  });
});
