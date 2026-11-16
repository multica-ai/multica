import { useState, type ComponentType } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AddChildIssueModal } from "./add-child-issue";
import { AddProjectIssueModal } from "./add-project-issue";
import { SetParentIssueModal } from "./set-parent-issue";

const mocks = vi.hoisted(() => ({
  mutateAsync: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutateAsync: mocks.mutateAsync }),
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
// Like the real picker, picking an issue closes the modal straight away.
vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: ({
    onSelect,
    onOpenChange,
  }: {
    onSelect: (issue: { id: string; identifier: string; revision: number }) => void;
    onOpenChange: (open: boolean) => void;
  }) => (
    <button
      type="button"
      onClick={() => {
        onSelect({ id: "selected-1", identifier: "MUL-2", revision: 5 });
        onOpenChange(false);
      }}
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
        add_project_issue: {
          title: "Add to project",
          description: "Choose issue",
          toast_success: `Moved ${String(vars?.identifier ?? "")}`,
          toast_failed: "Move failed",
        },
      }),
  }),
}));

type ModalComponent = ComponentType<{ onClose: () => void; data: Record<string, unknown> | null }>;

/** The modal host unmounts a modal as soon as it closes. */
function Host({ Component, data }: { Component: ModalComponent; data: Record<string, unknown> }) {
  const [open, setOpen] = useState(true);
  return open ? <Component onClose={() => setOpen(false)} data={data} /> : <span>Closed</span>;
}

function deferred() {
  let settle!: { resolve: () => void; reject: (err: Error) => void };
  mocks.mutateAsync.mockImplementation(
    () => new Promise<void>((resolve, reject) => (settle = { resolve, reject })),
  );
  return {
    resolve: () => act(async () => settle.resolve()),
    reject: (err: Error) => act(async () => settle.reject(err)),
  };
}

describe.each([
  {
    name: "AddChildIssueModal",
    Component: AddChildIssueModal,
    data: { issueId: "issue-1" },
    expectedMutation: { id: "selected-1", parent_issue_id: "issue-1" },
    success: "Added MUL-2",
  },
  {
    name: "SetParentIssueModal",
    Component: SetParentIssueModal,
    data: { issueId: "issue-1" },
    expectedMutation: { id: "issue-1", parent_issue_id: "selected-1" },
    success: "Set MUL-2",
  },
  {
    name: "AddProjectIssueModal",
    Component: AddProjectIssueModal,
    data: { projectId: "project-1" },
    expectedMutation: { id: "selected-1", project_id: "project-1" },
    success: "Moved MUL-2",
  },
])("$name", ({ Component, data, expectedMutation, success }) => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows success once the update saves, after the modal has closed", async () => {
    const request = deferred();
    render(<Host Component={Component} data={data} />);
    fireEvent.click(screen.getByRole("button", { name: "Select issue" }));

    expect(screen.getByText("Closed")).toBeInTheDocument();
    expect(mocks.mutateAsync).toHaveBeenCalledWith(expectedMutation);
    expect(mocks.toast.success).not.toHaveBeenCalled();

    await request.resolve();
    expect(mocks.toast.success).toHaveBeenCalledWith(success);
    expect(mocks.toast.error).not.toHaveBeenCalled();
  });

  it("reports a failure, not success, after the modal has closed", async () => {
    const request = deferred();
    render(<Host Component={Component} data={data} />);
    fireEvent.click(screen.getByRole("button", { name: "Select issue" }));

    expect(screen.getByText("Closed")).toBeInTheDocument();
    await request.reject(new Error("revision conflict"));
    expect(mocks.toast.success).not.toHaveBeenCalled();
    expect(mocks.toast.error).toHaveBeenCalledWith("revision conflict");
  });
});
