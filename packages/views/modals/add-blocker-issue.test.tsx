import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AddBlockerIssueModal } from "./add-blocker-issue";

type PickerProps = {
  excludeIds: string[];
  isSelectable: (issue: { parent_issue_id: string | null }) => boolean;
  onSelect: (issue: { id: string }) => void;
  error?: string;
  pending: boolean;
  closeOnSelect: boolean;
};

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  mutateBlocking: vi.fn(),
  mutation: { isError: false, isPending: false, error: null as Error | null },
  picker: null as PickerProps | null,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/blockers", () => ({
  useUpdateIssueBlockers: () => ({ ...mocks.mutation, mutate: mocks.mutate }),
  useUpdateIssueBlocking: () => ({ ...mocks.mutation, mutate: mocks.mutateBlocking }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_wsId: string, issueId: string) => ({ queryKey: ["issues", "detail", issueId] }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: {
      id: "issue-1",
      parent_issue_id: "parent-1",
      blocked_by: [
        { id: "manual-1", via: "manual" },
        { id: "stage-1", via: "stage" },
      ],
      blocking: [
        { id: "dependent-1", via: "manual" },
        { id: "later-stage-1", via: "stage" },
      ],
    },
  }),
}));
vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: (props: PickerProps) => {
    mocks.picker = props;
    return (
      <button type="button" onClick={() => props.onSelect({ id: "blocker-1" })}>
        Select issue
      </button>
    );
  },
}));
vi.mock("../i18n", () => ({
  useT: () => ({
    t: (selector: (labels: Record<string, Record<string, string>>) => string) =>
      selector({
        waiting: { picker_hint: "Pick a blocker" },
        relations: { wait_title: "Wait for another task", block_title: "Block another task", blocking_picker_hint: "Pick a dependent" },
      }),
  }),
}));

beforeEach(() => {
  mocks.mutate.mockReset();
  mocks.mutateBlocking.mockReset();
  mocks.mutation = { isError: false, isPending: false, error: null };
  mocks.picker = null;
});

describe("AddBlockerIssueModal", () => {
  it("excludes the issue, its parent and existing manual blockers, and its own sub-issues", () => {
    render(<AddBlockerIssueModal onClose={vi.fn()} data={{ issueId: "issue-1" }} />);
    expect(mocks.picker?.excludeIds).toEqual(["issue-1", "parent-1", "manual-1"]);
    expect(mocks.picker?.isSelectable({ parent_issue_id: "issue-1" })).toBe(false);
    expect(mocks.picker?.isSelectable({ parent_issue_id: null })).toBe(true);
  });

  it("adds the picked blocker and closes only once the write succeeds", () => {
    const onClose = vi.fn();
    render(<AddBlockerIssueModal onClose={onClose} data={{ issueId: "issue-1" }} />);
    expect(mocks.picker?.closeOnSelect).toBe(false);

    fireEvent.click(screen.getByText("Select issue"));
    expect(mocks.mutate).toHaveBeenCalledWith({ blocked_by: ["blocker-1"] }, { onSuccess: onClose });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("shows the server's rejection inside the picker", () => {
    mocks.mutation = { isError: true, isPending: false, error: new Error("would create a cycle") };
    render(<AddBlockerIssueModal onClose={vi.fn()} data={{ issueId: "issue-1" }} />);
    expect(mocks.picker?.error).toBe("would create a cycle");
  });

  it("blocks the picked issue by writing the edge on that issue", () => {
    const onClose = vi.fn();
    render(<AddBlockerIssueModal onClose={onClose} data={{ issueId: "issue-1" }} direction="blocking" />);
    expect(mocks.picker?.excludeIds).toEqual(["issue-1", "parent-1", "dependent-1"]);
    expect(mocks.picker?.isSelectable({ parent_issue_id: "issue-1" })).toBe(false);

    fireEvent.click(screen.getByText("Select issue"));
    expect(mocks.mutateBlocking).toHaveBeenCalledWith({ dependentId: "blocker-1" }, { onSuccess: onClose });
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
