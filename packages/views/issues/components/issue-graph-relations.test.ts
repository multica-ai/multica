import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { traceIssueRelationships } from "./issue-graph-relations";

function issue(id: string, parentIssueId: string | null = null): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: id,
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: parentIssueId,
    project_id: null,
    workflow_id: null,
    workflow_run_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("traceIssueRelationships", () => {
  const issues = [
    issue("root"),
    issue("parent", "root"),
    issue("selected", "parent"),
    issue("child-a", "selected"),
    issue("child-b", "selected"),
    issue("grandchild", "child-a"),
    issue("unrelated"),
  ];

  it("returns the complete ancestor chain and descendant tree", () => {
    const trace = traceIssueRelationships(issues, "selected");
    expect([...trace.ancestors].sort()).toEqual(["parent", "root"]);
    expect([...trace.descendants].sort()).toEqual([
      "child-a",
      "child-b",
      "grandchild",
    ]);
  });

  it("returns empty sets without a selection", () => {
    const trace = traceIssueRelationships(issues, null);
    expect(trace.ancestors.size).toBe(0);
    expect(trace.descendants.size).toBe(0);
  });

  it("terminates safely when legacy data contains a cycle", () => {
    const trace = traceIssueRelationships(
      [issue("a", "b"), issue("b", "a")],
      "a",
    );
    expect([...trace.ancestors]).toEqual(["b"]);
    expect([...trace.descendants]).toEqual(["b"]);
  });
});
