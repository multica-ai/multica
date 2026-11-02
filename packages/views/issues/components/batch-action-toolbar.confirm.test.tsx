import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { BatchActionToolbar } from "./batch-action-toolbar";

// MUL-4155: batch status changes must apply directly (no run-confirm modal),
// while agent/squad assignment still confirms and delete still confirms. These
// tests drive the pickers' onUpdate callbacks and assert which path is taken.

const selection = vi.hoisted(() => ({
  selectedIds: new Set<string>(),
  clear: vi.fn(),
  toggle: vi.fn(),
  select: vi.fn(),
  deselect: vi.fn(),
}));
vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector: (s: typeof selection) => unknown) => selector(selection),
}));

const batchUpdate = vi.hoisted(() => vi.fn().mockResolvedValue({ updated: 1 }));
const batchDelete = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: batchUpdate, isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: batchDelete, isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-workflows", () => ({
  useIssueWorkflows: () => ({ workflows: [] }),
  sharedWorkflowStatusKeys: () => null,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

const openModal = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/modals", () => ({
  useModalStore: (selector: (s: { open: typeof openModal }) => unknown) => selector({ open: openModal }),
}));

vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "label" }) }));
const toast = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

// Interactive picker stubs: each renders buttons that fire the real onUpdate the
// toolbar passes in, so we exercise handleBatchStatus / handleBatchAssignee.
const ACTIVE_STATUSES = ["todo", "in_progress", "in_review", "blocked"] as const;
const TERMINAL_STATUSES = ["done", "cancelled"] as const;
vi.mock("./pickers", () => ({
  StatusPicker: ({ onUpdate }: { onUpdate: (u: Partial<UpdateIssueRequest>) => void }) => (
    <div>
      {[...ACTIVE_STATUSES, ...TERMINAL_STATUSES, "backlog"].map((s) => (
        <button
          key={s}
          data-testid={`status-${s}`}
          onClick={() => onUpdate({ status: s as UpdateIssueRequest["status"] })}
        />
      ))}
    </div>
  ),
  PriorityPicker: () => <div data-testid="priority-picker" />,
  AssigneePicker: ({ onUpdate }: { onUpdate: (u: Partial<UpdateIssueRequest>) => void }) => (
    <div>
      <button
        data-testid="assign-agent"
        onClick={() => onUpdate({ assignee_type: "agent", assignee_id: "agent-1" })}
      />
      <button
        data-testid="assign-member"
        onClick={() => onUpdate({ assignee_type: "member", assignee_id: "user-1" })}
      />
    </div>
  ),
}));

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "a",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Issue",
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
  selection.selectedIds = new Set(["a"]);
  batchUpdate.mockClear();
  batchDelete.mockClear();
  openModal.mockClear();
  selection.clear.mockClear();
  toast.success.mockClear();
  toast.warning.mockClear();
});

describe("BatchActionToolbar status routing (MUL-4155)", () => {
  it("applies every status target directly, never opening the run-confirm modal", () => {
    for (const status of [...ACTIVE_STATUSES, ...TERMINAL_STATUSES, "backlog"]) {
      batchUpdate.mockClear();
      openModal.mockClear();
      // A backlog issue in the selection is the case that historically could
      // start a run — it still must not pop the confirm modal now.
      const { unmount } = render(<BatchActionToolbar issues={[makeIssue({ status: "backlog" })]} />);
      fireEvent.click(screen.getByTestId(`status-${status}`));
      expect(openModal).not.toHaveBeenCalled();
      expect(batchUpdate).toHaveBeenCalledWith({ ids: ["a"], updates: { status } });
      unmount();
    }
  });

  it("warns instead of reporting success when the server skips some issues", async () => {
    selection.selectedIds = new Set(["a", "b"]);
    batchUpdate.mockResolvedValueOnce({ updated: 1 });
    render(<BatchActionToolbar issues={[makeIssue({ id: "a" }), makeIssue({ id: "b" })]} />);
    fireEvent.click(screen.getByTestId("status-done"));
    await waitFor(() => expect(toast.warning).toHaveBeenCalled());
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("still routes agent assignment through the run-confirm modal", () => {
    render(<BatchActionToolbar issues={[makeIssue({ status: "todo" })]} />);
    fireEvent.click(screen.getByTestId("assign-agent"));
    expect(openModal).toHaveBeenCalledWith(
      "issue-run-confirm",
      expect.objectContaining({ issueIds: ["a"], mode: "assign", assigneeType: "agent", assigneeId: "agent-1" }),
    );
    expect(batchUpdate).not.toHaveBeenCalled();
  });

  it("applies member assignment directly (never starts a run)", () => {
    render(<BatchActionToolbar issues={[makeIssue({ status: "todo" })]} />);
    fireEvent.click(screen.getByTestId("assign-member"));
    expect(openModal).not.toHaveBeenCalled();
    expect(batchUpdate).toHaveBeenCalledWith({
      ids: ["a"],
      updates: { assignee_type: "member", assignee_id: "user-1" },
    });
  });

  it("opens the dedicated delete confirmation, not the run-confirm modal", () => {
    render(<BatchActionToolbar issues={[makeIssue({ status: "todo" })]} />);
    fireEvent.click(screen.getByText("label", { selector: "button.text-destructive" }));
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(openModal).not.toHaveBeenCalled();
    expect(batchDelete).not.toHaveBeenCalled();
  });
});

describe("BatchActionToolbar selection after apply (MUL-8179)", () => {
  it("keeps the selection after an update by default, so edits can be chained", async () => {
    render(<BatchActionToolbar issues={[makeIssue()]} />);
    fireEvent.click(screen.getByTestId("status-done"));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    expect(selection.clear).not.toHaveBeenCalled();
  });

  it("clears the selection once an update fully applies when opted in", async () => {
    render(<BatchActionToolbar issues={[makeIssue()]} clearSelectionOnApply />);
    fireEvent.click(screen.getByTestId("status-done"));
    await waitFor(() => expect(selection.clear).toHaveBeenCalledTimes(1));
  });

  it("keeps the selection after a partial update so it can be retried", async () => {
    selection.selectedIds = new Set(["a", "b"]);
    batchUpdate.mockResolvedValueOnce({ updated: 1 });
    render(
      <BatchActionToolbar
        issues={[makeIssue({ id: "a" }), makeIssue({ id: "b" })]}
        clearSelectionOnApply
      />,
    );
    fireEvent.click(screen.getByTestId("status-done"));
    await waitFor(() => expect(toast.warning).toHaveBeenCalled());
    expect(selection.clear).not.toHaveBeenCalled();
  });

  it("hands the run-confirm modal a clear callback instead of clearing up front", () => {
    render(<BatchActionToolbar issues={[makeIssue({ status: "todo" })]} clearSelectionOnApply />);
    fireEvent.click(screen.getByTestId("assign-agent"));
    expect(selection.clear).not.toHaveBeenCalled();
    const data = openModal.mock.calls[0]![1] as { onApplied?: () => void };
    data.onApplied?.();
    expect(selection.clear).toHaveBeenCalledTimes(1);
  });
});
