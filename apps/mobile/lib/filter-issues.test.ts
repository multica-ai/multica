// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { filterIssues, hideCompletedIssues } from "./filter-issues";

function issue(id: string, status: string, statusCategory?: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status,
    ...(statusCategory ? { status_category: statusCategory as Issue["status_category"] } : {}),
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "",
    updated_at: "",
  };
}

const ids = (issues: Issue[]) => issues.map((i) => i.id);

describe("hideCompletedIssues", () => {
  it("hides built-in done and cancelled issues", () => {
    const visible = hideCompletedIssues(
      [issue("a", "todo"), issue("b", "done"), issue("c", "cancelled"), issue("d", "blocked")],
      [],
    );
    expect(ids(visible)).toEqual(["a", "d"]);
  });

  it("hides custom statuses by their done/closed category", () => {
    const visible = hideCompletedIssues(
      [issue("a", "shipped", "done"), issue("b", "wontfix", "closed"), issue("c", "qa", "started")],
      [],
    );
    expect(ids(visible)).toEqual(["c"]);
  });

  it("keeps a completed status the user selected in the status filter", () => {
    const issues = [issue("a", "todo"), issue("b", "done"), issue("c", "cancelled")];
    const matching = filterIssues(issues, ["todo", "done"], []);
    expect(ids(hideCompletedIssues(matching, ["todo", "done"]))).toEqual(["a", "b"]);
  });

  it("keeps a custom status whose category is unknown", () => {
    expect(ids(hideCompletedIssues([issue("a", "mystery")], []))).toEqual(["a"]);
  });
});
