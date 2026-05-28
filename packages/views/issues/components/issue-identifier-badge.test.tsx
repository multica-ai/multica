import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { IssueIdentifierBadge } from "./issue-identifier-badge";
import type { Issue } from "@multica/core/types";

const mockIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Example",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  due_date: null,
  project_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Issue;

describe("IssueIdentifierBadge", () => {
  it("renders the identifier with status icon", () => {
    render(<IssueIdentifierBadge issue={mockIssue} onCopy={vi.fn()} />);

    expect(screen.getByText("TES-1")).toBeInTheDocument();
    expect(screen.getByRole("button")).toBeInTheDocument();
  });

  it("calls onCopy when clicked", async () => {
    const onCopy = vi.fn();
    render(<IssueIdentifierBadge issue={mockIssue} onCopy={onCopy} />);

    await userEvent.click(screen.getByRole("button"));

    expect(onCopy).toHaveBeenCalledTimes(1);
  });

  it("shows the identifier in the title attribute", () => {
    render(<IssueIdentifierBadge issue={mockIssue} onCopy={vi.fn()} />);

    expect(screen.getByRole("button")).toHaveAttribute("title", "TES-1");
  });
});
