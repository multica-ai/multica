import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { BoardCardContent } from "./board-card";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({ data: [] }),
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

const mockViewState = {
  cardProperties: {
    priority: false,
    description: false,
    assignee: false,
    startDate: false,
    dueDate: false,
    project: false,
    labels: true,
    childProgress: false,
  },
  swimlaneGrouping: "assignee",
};

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector?: any) => {
    return selector ? selector(mockViewState) : mockViewState;
  },
}));

vi.mock("@multica/core/issues/config", () => ({
  BOARD_STATUSES: ["todo", "done"],
  PRIORITY_CONFIG: {
    high: { badgeBg: "", badgeText: "", label: "High" },
  },
}));

vi.mock("../actions", () => ({
  IssueActionsContextMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("../../i18n", () => ({
  useTimeAgo: () => (date: string) => date,
  useT: () => ({
    t: (selector: any) =>
      selector({
        card: { update_failed: "Update failed" },
        priority: { high: "High" },
        pickers: {
          label: { trigger_label: "Add label" },
          assignee: { trigger_unassigned: "Unassigned" },
        },
      }),
  }),
}));

vi.mock("./pickers", async () => {
  const { createPortal } = await import("react-dom");
  const { Plus } = await import("lucide-react");

  return {
    LabelPicker: ({
      issueId,
      labels,
      appendAddTrigger,
      addTriggerLabel,
    }: {
      issueId: string;
      labels?: unknown[];
      appendAddTrigger?: boolean;
      addTriggerLabel?: string;
    }) => (
      <>
        <div data-testid="label-picker">
          {issueId}:{labels?.length ?? 0}
          {appendAddTrigger ? (
            labels?.length ? (
              <span data-testid="label-picker-add-trigger">{addTriggerLabel}</span>
            ) : (
              <button type="button" aria-label={addTriggerLabel} className="text-[10px]" data-testid="label-picker-add-trigger">
                <Plus aria-hidden className="h-3 w-3" />
                <span>{addTriggerLabel}</span>
              </button>
            )
          ) : null}
        </div>
        {createPortal(<input data-testid="portal-label-input" />, document.body)}
      </>
    ),
    PriorityPicker: () => null,
    AssigneePicker: () => null,
    StartDatePicker: () => null,
    DueDatePicker: () => null,
  };
});

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-test",
    number: 1,
    identifier: "OPE-1",
    title: "Board card issue",
    description: null,
    status: "todo",
    priority: "high",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "creator-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    due_date: null,
    start_date: null,
    metadata: {},
    labels: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("BoardCardContent labels", () => {
  it("renders an editable label picker with existing labels", () => {
    render(
      <BoardCardContent
        editable
        issue={makeIssue({
          labels: [
            {
              id: "label-1",
              workspace_id: "ws-test",
              project_id: null,
              name: "Bug",
              color: "#ef4444",
              created_at: "2026-01-01T00:00:00Z",
              updated_at: "2026-01-01T00:00:00Z",
            },
          ],
        })}
      />,
    );

    expect(screen.getByTestId("label-picker")).toHaveTextContent("issue-1:1");
    expect(screen.getByTestId("label-picker-add-trigger")).toHaveTextContent("Add label");
  });

  it("keeps the editable label picker visible when no labels are set", () => {
    render(<BoardCardContent editable issue={makeIssue()} />);

    expect(screen.getByTestId("label-picker")).toHaveTextContent("issue-1:0");
    expect(screen.getByTestId("label-picker-add-trigger")).toHaveAccessibleName("Add label");
    expect(screen.getByTestId("label-picker-add-trigger")).toHaveTextContent("Add label");
    expect(screen.getByTestId("label-picker-add-trigger")).toHaveClass("text-[10px]");
  });

  it("does not prevent default events from portaled label management inputs", () => {
    render(<BoardCardContent editable issue={makeIssue()} />);

    const mouseDown = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
    screen.getByTestId("portal-label-input").dispatchEvent(mouseDown);

    expect(mouseDown.defaultPrevented).toBe(false);
  });
});

describe("BoardCardContent sub-issue indicator", () => {
  it("renders CornerDownRight icon when parent_issue_id is present and grouping is not parent", () => {
    mockViewState.swimlaneGrouping = "assignee";
    const { container } = render(<BoardCardContent issue={makeIssue({ parent_issue_id: "parent-1" })} />);
    const icon = container.querySelector(".lucide-corner-down-right");
    expect(icon).not.toBeNull();
    expect(icon).toHaveAttribute("aria-hidden", "true");
  });

  it("does not render CornerDownRight icon when parent_issue_id is present but grouping is parent", () => {
    mockViewState.swimlaneGrouping = "parent";
    const { container } = render(<BoardCardContent issue={makeIssue({ parent_issue_id: "parent-1" })} />);
    const icon = container.querySelector(".lucide-corner-down-right");
    expect(icon).toBeNull();
  });
});
