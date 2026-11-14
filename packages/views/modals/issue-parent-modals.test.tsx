import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AddChildIssueModal } from "./add-child-issue";
import { SetParentIssueModal } from "./set-parent-issue";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  mutateAsync: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@multica/core/issues/use-parent-issue-recents", () => ({
  useParentIssueRecents: () => [],
}));
vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: mocks.mutate, mutateAsync: mocks.mutateAsync }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_wsId: string, issueId: string) => ({
    queryKey: ["issues", "detail", issueId],
  }),
  childIssuesOptions: (_wsId: string, issueId: string) => ({
    queryKey: ["issues", "children", issueId],
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) =>
    queryKey[1] === "detail"
      ? { data: { id: queryKey[2], revision: 7, parent_issue_id: null } }
      : { data: [] },
}));
vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: ({
    onSelect,
  }: {
    onSelect: (issue: { id: string; identifier: string; revision: number }) => void;
  }) => (
    <button
      type="button"
      onClick={() =>
        onSelect({ id: "selected-1", identifier: "MUL-2", revision: 5 })
      }
    >
      Select issue
    </button>
  ),
}));
vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      selector: (labels: Record<string, Record<string, string>>) => string,
      vars?: Record<string, unknown>,
    ) =>
      selector({
        issue_picker: { suggested_recent: "Recently viewed" },
        add_child: {
          title: "Add child",
          description: "Choose child",
          toast_success: `Added ${String(vars?.identifier ?? "")}`,
          toast_failed: "Add failed",
        },
        set_parent: {
          title: "Set parent",
          description: "Choose parent",
          toast_success: `Set ${String(vars?.identifier ?? "")}`,
          toast_failed: "Set failed",
        },
      }),
  }),
}));

describe.each([
  {
    name: "AddChildIssueModal",
    asyncMutation: false,
    Component: AddChildIssueModal,
    expectedMutation: {
      id: "selected-1",
      parent_issue_id: "issue-1",
    },
  },
  {
    name: "SetParentIssueModal",
    asyncMutation: true,
    Component: SetParentIssueModal,
    expectedMutation: {
      id: "issue-1",
      parent_issue_id: "selected-1",
    },
  },
])("$name", ({ Component, expectedMutation, asyncMutation }) => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows success only after the mutation succeeds", async () => {
    mocks.mutateAsync.mockResolvedValue({});
    mocks.mutate.mockImplementation((_variables, callbacks) => {
      callbacks.onSuccess();
    });

    render(<Component onClose={vi.fn()} data={{ issueId: "issue-1" }} />);
    fireEvent.click(screen.getByRole("button", { name: "Select issue" }));

    if (asyncMutation) expect(mocks.mutateAsync).toHaveBeenCalledWith(expectedMutation);
    else expect(mocks.mutate).toHaveBeenCalledWith(
      expectedMutation,
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledTimes(1));
    expect(mocks.toast.error).not.toHaveBeenCalled();
  });

  it("does not report success when the mutation fails", async () => {
    mocks.mutateAsync.mockRejectedValue(new Error("revision conflict"));
    mocks.mutate.mockImplementation((_variables, callbacks) => {
      callbacks.onError(new Error("revision conflict"));
    });

    render(<Component onClose={vi.fn()} data={{ issueId: "issue-1" }} />);
    fireEvent.click(screen.getByRole("button", { name: "Select issue" }));

    expect(mocks.toast.success).not.toHaveBeenCalled();
    await waitFor(() => expect(mocks.toast.error).toHaveBeenCalledWith("revision conflict"));
  });
});
