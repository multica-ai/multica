import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { BatchActionToolbar } from "./batch-action-toolbar";

// Mutable selection state shared with the store mock below. The real toolbar
// derives the pickers' current value from the issues it can resolve out of
// this selection, so each test sets `selection.selectedIds` before rendering.
const selection = vi.hoisted(() => ({
  selectedIds: new Set<string>(),
  clear: () => {},
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector: (s: typeof selection) => unknown) =>
    selector(selection),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "label" }),
}));

// Render each picker as a probe that surfaces the value the toolbar passed in,
// so the test asserts the wiring (real `commonIssueFields` runs underneath).
vi.mock("./pickers", () => ({
  StatusPicker: ({ status }: { status: string | null }) => (
    <div data-testid="status-picker" data-status={status ?? "__none__"} />
  ),
  PriorityPicker: ({ priority }: { priority: string | null }) => (
    <div data-testid="priority-picker" data-priority={priority ?? "__none__"} />
  ),
  AssigneePicker: ({
    assigneeType,
    assigneeId,
    mixed,
  }: {
    assigneeType: string | null;
    assigneeId: string | null;
    mixed?: boolean;
  }) => (
    <div
      data-testid="assignee-picker"
      data-assignee-type={assigneeType ?? "__null__"}
      data-assignee-id={assigneeId ?? "__null__"}
      data-mixed={String(Boolean(mixed))}
    />
  ),
}));

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

beforeEach(() => {
  selection.selectedIds = new Set();
});

describe("BatchActionToolbar picker wiring", () => {
  it("reflects the shared status / priority / assignee of the selected issues", () => {
    const issues = [
      makeIssue({ id: "a", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "in_progress");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "high");
    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-assignee-type", "member");
    expect(assignee).toHaveAttribute("data-assignee-id", "u-1");
    expect(assignee).toHaveAttribute("data-mixed", "false");
  });

  it("falls back to an empty (no-checkmark) state when the selection is mixed", () => {
    const issues = [
      makeIssue({ id: "a", status: "todo", priority: "none", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", status: "done", priority: "urgent", assignee_type: "agent", assignee_id: "ag-1" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "__none__");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "__none__");
    expect(screen.getByTestId("assignee-picker")).toHaveAttribute("data-mixed", "true");
  });

  it("treats an all-unassigned selection as unassigned, not mixed", () => {
    const issues = [
      makeIssue({ id: "a", assignee_type: null, assignee_id: null }),
      makeIssue({ id: "b", assignee_type: null, assignee_id: null }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-mixed", "false");
    expect(assignee).toHaveAttribute("data-assignee-type", "__null__");
    expect(assignee).toHaveAttribute("data-assignee-id", "__null__");
  });

  it("renders nothing when nothing is selected", () => {
    render(<BatchActionToolbar issues={[makeIssue({ id: "a" })]} />);
    expect(screen.queryByTestId("status-picker")).toBeNull();
  });
});
