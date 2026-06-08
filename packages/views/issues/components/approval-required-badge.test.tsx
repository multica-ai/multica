import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { ApprovalRequiredBadge } from "./approval-required-badge";

function issueWithStatus(status: Issue["status"]): Issue {
  return {
    id: "issue-1",
    workspace_id: "workspace-1",
    number: 1,
    identifier: "VEN-1",
    title: "Needs review",
    description: "",
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    project_id: null,
    parent_issue_id: null,
    position: 1,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-06-08T00:00:00Z",
    updated_at: "2026-06-08T00:00:00Z",
    labels: [],
  } as Issue;
}

describe("ApprovalRequiredBadge", () => {
  it("marks in-review tasks as requiring approval", () => {
    render(<ApprovalRequiredBadge issue={issueWithStatus("in_review")} />);

    expect(screen.getByText("Approval")).toBeInTheDocument();
  });

  it("does not mark tasks outside review", () => {
    render(<ApprovalRequiredBadge issue={issueWithStatus("todo")} />);

    expect(screen.queryByText("Approval")).not.toBeInTheDocument();
  });
});
