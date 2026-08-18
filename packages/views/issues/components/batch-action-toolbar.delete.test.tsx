import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { BatchActionToolbar } from "./batch-action-toolbar";

// Batch delete is the one action whose server response carries a count: the
// handler deletes what it can, skips what it cannot, and answers 200 either
// way. These tests pin the toolbar to that count instead of to "the promise
// resolved", and pin the confirm button's pending state.

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

const batchDelete = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: batchDelete, isPending: false }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: (selector: (s: { open: () => void }) => unknown) => selector({ open: vi.fn() }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

// Echo the key so assertions can tell "deleted them all" from "some failed"
// apart, and interpolate the counts the way i18next would.
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (
      sel: (x: Record<string, Record<string, string>>) => string,
      vars?: Record<string, unknown>,
    ) =>
      sel({
        batch: new Proxy({} as Record<string, string>, {
          get: (_t, key: string) =>
            `${key}${vars ? `:${JSON.stringify(vars)}` : ""}`,
        }),
      }),
  }),
}));

vi.mock("./pickers", () => ({
  StatusPicker: () => <div />,
  PriorityPicker: () => <div />,
  AssigneePicker: () => <div />,
}));

function makeIssue(id: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Issue ${id}`,
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
  } as Issue;
}

const issues = [makeIssue("a"), makeIssue("b"), makeIssue("c")];

function openDeleteDialog() {
  render(<BatchActionToolbar issues={issues} />);
  fireEvent.click(screen.getByText(/^delete/, { selector: "button.text-destructive" }));
  return screen.getByRole("alertdialog");
}

function confirmDelete(dialog: HTMLElement) {
  const button = Array.from(dialog.querySelectorAll("button")).find((b) =>
    b.textContent?.startsWith("delete"),
  );
  fireEvent.click(button!);
  return button!;
}

beforeEach(() => {
  vi.clearAllMocks();
  selection.selectedIds = new Set(["a", "b", "c"]);
});

describe("BatchActionToolbar delete", () => {
  it("reports success and clears the selection when the server deleted every issue", async () => {
    batchDelete.mockResolvedValue({ deleted: 3 });

    confirmDelete(openDeleteDialog());

    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    expect(String(toast.success.mock.calls[0]?.[0])).toContain("delete_success");
    expect(selection.clear).toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  // The regression this guards: the server skips what it cannot delete and
  // still answers 200, so a resolved promise used to be reported as "deleted
  // 3 issues" while the survivors reappeared underneath the toast.
  it("reports the shortfall and keeps the selection when only some were deleted", async () => {
    batchDelete.mockResolvedValue({ deleted: 1 });

    confirmDelete(openDeleteDialog());

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    const message = String(toast.error.mock.calls[0]?.[0]);
    expect(message).toContain("delete_partial");
    expect(message).toContain('"deleted":1');
    expect(message).toContain('"failed":2');
    expect(toast.success).not.toHaveBeenCalled();
    // The survivors are still selected, which is what lets the user retry.
    expect(selection.clear).not.toHaveBeenCalled();
  });

  it("treats a zero count as a failure, not a silent success", async () => {
    batchDelete.mockResolvedValue({ deleted: 0 });

    confirmDelete(openDeleteDialog());

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("shows a pending confirm button and ignores a second click while in flight", async () => {
    let resolveDelete!: (value: { deleted: number }) => void;
    batchDelete.mockImplementation(
      () => new Promise<{ deleted: number }>((res) => { resolveDelete = res; }),
    );

    const dialog = openDeleteDialog();
    const button = confirmDelete(dialog);

    await waitFor(() => expect(button.textContent).toContain("deleting"));
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(batchDelete).toHaveBeenCalledTimes(1);

    resolveDelete({ deleted: 3 });
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
  });

  it("reports a rejected request as a failure", async () => {
    batchDelete.mockRejectedValue(new Error("network down"));

    confirmDelete(openDeleteDialog());

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("network down"));
    expect(toast.success).not.toHaveBeenCalled();
    expect(selection.clear).not.toHaveBeenCalled();
  });
});
