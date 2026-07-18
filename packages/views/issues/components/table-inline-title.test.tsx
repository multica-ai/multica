/**
 * @vitest-environment jsdom
 */
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Issue } from "@multica/core/types";
import { InlineTitle } from "./table-view";
import type { IssueTableDisplayRow } from "./table-view-model";

function makeIssue(title: string): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function makeRow(title: string): Extract<
  IssueTableDisplayRow,
  { kind: "issue" }
> {
  return {
    kind: "issue",
    key: "issue:issue-1",
    issue: makeIssue(title),
    depth: 0,
    hasChildren: false,
    collapsed: false,
  };
}

describe("InlineTitle", () => {
  it("preserves an active draft when a realtime title snapshot arrives", () => {
    const props = {
      onUpdate: vi.fn(),
      onToggleParent: vi.fn(),
      toggleLabel: "Toggle sub-issues",
    };
    const view = render(<InlineTitle row={makeRow("Original")} {...props} />);

    fireEvent.click(screen.getByRole("button", { name: "Original" }));
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Local draft" } });

    view.rerender(<InlineTitle row={makeRow("Remote title")} {...props} />);

    expect(screen.getByRole("textbox")).toHaveValue("Local draft");
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    expect(screen.getByRole("button", { name: "Remote title" })).toBeTruthy();
  });
});
