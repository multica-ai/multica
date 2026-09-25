import { describe, it, expect } from "vitest";
import type { Issue } from "../types";
import { commonIssueFields } from "./batch";

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Issue 1",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
  properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("commonIssueFields", () => {
  it("returns all-null for an empty selection (nothing to reflect)", () => {
    expect(commonIssueFields([])).toEqual({
      status: null,
      priority: null,
      assignee: null,
    });
  });

  it("reflects a single issue's own fields", () => {
    const common = commonIssueFields([
      makeIssue({ status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
    ]);
    expect(common.status).toBe("in_progress");
    expect(common.priority).toBe("high");
    expect(common.assignee).toEqual({ type: "member", id: "u-1" });
  });

  it("returns the shared status when every issue agrees, not a hardcoded default", () => {
    // Regression for MUL-3510: the batch picker used to assert "todo"
    // regardless of the selection.
    const common = commonIssueFields([
      makeIssue({ id: "a", status: "in_review" }),
      makeIssue({ id: "b", status: "in_review" }),
    ]);
    expect(common.status).toBe("in_review");
  });

  it("returns null status when the selection spans different statuses (mixed)", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", status: "todo" }),
      makeIssue({ id: "b", status: "done" }),
    ]);
    expect(common.status).toBeNull();
  });

  it("derives each field independently — shared status, mixed priority", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", status: "blocked", priority: "urgent" }),
      makeIssue({ id: "b", status: "blocked", priority: "low" }),
    ]);
    expect(common.status).toBe("blocked");
    expect(common.priority).toBeNull();
  });

  it("treats an all-unassigned selection as a real shared value, not mixed", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", assignee_type: null, assignee_id: null }),
      makeIssue({ id: "b", assignee_type: null, assignee_id: null }),
    ]);
    expect(common.assignee).toEqual({ type: null, id: null });
  });

  it("returns the shared assignee when every issue points at the same actor", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", assignee_type: "agent", assignee_id: "agent-1" }),
      makeIssue({ id: "b", assignee_type: "agent", assignee_id: "agent-1" }),
    ]);
    expect(common.assignee).toEqual({ type: "agent", id: "agent-1" });
  });

  it("returns null assignee when actors differ", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", assignee_type: "member", assignee_id: "u-2" }),
    ]);
    expect(common.assignee).toBeNull();
  });

  it("returns null assignee when some are assigned and some are unassigned", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", assignee_type: null, assignee_id: null }),
    ]);
    expect(common.assignee).toBeNull();
  });

  it("distinguishes assignees of the same id but different type", () => {
    const common = commonIssueFields([
      makeIssue({ id: "a", assignee_type: "member", assignee_id: "x" }),
      makeIssue({ id: "b", assignee_type: "agent", assignee_id: "x" }),
    ]);
    expect(common.assignee).toBeNull();
  });
});
