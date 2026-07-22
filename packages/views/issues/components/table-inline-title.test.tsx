/**
 * @vitest-environment jsdom
 */
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { useState } from "react";

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

const baseProps = {
  onUpdate: vi.fn(),
  onOpen: vi.fn(),
  onCreateSubIssue: vi.fn(),
  onToggleParent: vi.fn(),
  toggleLabel: "Toggle sub-issues",
  renameLabel: "Rename issue",
  createSubIssueLabel: "Create sub-issue",
};

/** Editing state lives in the table (one editor at a time); mirror that. */
function Harness({
  title,
  onOpen,
  onUpdate,
  onEditingChange,
  onCreateSubIssue,
}: {
  title: string;
  onOpen?: () => void;
  onUpdate?: (updates: unknown) => void;
  onEditingChange?: (editing: boolean) => void;
  onCreateSubIssue?: () => void;
}) {
  const [editing, setEditing] = useState(false);
  return (
    <InlineTitle
      {...baseProps}
      row={makeRow(title)}
      editing={editing}
      onEditingChange={(next) => {
        setEditing(next);
        onEditingChange?.(next);
      }}
      onOpen={onOpen ?? baseProps.onOpen}
      onUpdate={onUpdate ?? baseProps.onUpdate}
      onCreateSubIssue={onCreateSubIssue ?? baseProps.onCreateSubIssue}
    />
  );
}

describe("InlineTitle", () => {
  it("opens the issue when the title is clicked instead of entering edit mode", () => {
    const onOpen = vi.fn();
    const rowClick = vi.fn();
    render(
      // The title's click must not also bubble into the row, which would be
      // a second, duplicate navigation through onRowClick.
      <div onClick={rowClick}>
        <Harness title="Original" onOpen={onOpen} />
      </div>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Original" }));

    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(rowClick).not.toHaveBeenCalled();
  });

  it("enters edit mode from the rename affordance and commits on Enter", () => {
    const onUpdate = vi.fn();
    render(<Harness title="Original" onUpdate={onUpdate} />);

    fireEvent.click(screen.getByRole("button", { name: "Rename issue" }));
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Renamed" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onUpdate).toHaveBeenCalledWith({ title: "Renamed" });
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("opens sub-issue creation without also navigating into the issue", () => {
    const onCreateSubIssue = vi.fn();
    const rowClick = vi.fn();
    render(
      <div onClick={rowClick}>
        <Harness title="Original" onCreateSubIssue={onCreateSubIssue} />
      </div>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Create sub-issue" }),
    );

    expect(onCreateSubIssue).toHaveBeenCalledTimes(1);
    expect(rowClick).not.toHaveBeenCalled();
  });

  it("commits on click-away without also navigating into the issue", async () => {
    const user = userEvent.setup({ delay: null });
    const onUpdate = vi.fn();
    const rowClick = vi.fn();
    render(
      // The row's onClick is the navigation handler. Committing a rename by
      // clicking away (which blurs the input) must not bubble into it.
      <div onClick={rowClick}>
        <Harness title="Original" onUpdate={onUpdate} />
      </div>,
    );

    await user.click(screen.getByRole("button", { name: "Rename issue" }));
    const input = screen.getByRole("textbox");
    await user.clear(input);
    await user.type(input, "Renamed");
    // The identifier is inside the title cell and not focusable, so clicking
    // it blurs the input (→ commit) and is the click that previously leaked
    // into row navigation.
    await user.click(screen.getByText("MUL-1"));

    expect(onUpdate).toHaveBeenCalledWith({ title: "Renamed" });
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(rowClick).not.toHaveBeenCalled();
  });

  it("preserves an active draft when a realtime title snapshot arrives", () => {
    function SnapshotHarness({ title }: { title: string }) {
      const [editing, setEditing] = useState(false);
      return (
        <InlineTitle
          {...baseProps}
          row={makeRow(title)}
          editing={editing}
          onEditingChange={setEditing}
        />
      );
    }
    const view = render(<SnapshotHarness title="Original" />);

    fireEvent.click(screen.getByRole("button", { name: "Rename issue" }));
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Local draft" } });

    view.rerender(<SnapshotHarness title="Remote title" />);

    expect(screen.getByRole("textbox")).toHaveValue("Local draft");
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    expect(screen.getByRole("button", { name: "Remote title" })).toBeTruthy();
  });
});
