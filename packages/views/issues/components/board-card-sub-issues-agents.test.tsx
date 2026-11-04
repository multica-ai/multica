import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "@multica/core/types";
import { BoardCardContent } from "./board-card";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/properties", () => ({
  propertyListOptions: () => ({ queryKey: ["properties"] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Actor",
    getActorInitials: () => "AC",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("../../i18n", () => ({
  useLocale: () => "en",
  useT: () => ({ t: () => "Translated" }),
  useTimeAgo: () => () => "now",
}));

vi.mock("./issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: () => null,
}));

vi.mock("./custom-status-chip", () => ({
  CustomStatusChip: () => null,
  useIsCustomStatus: () => false,
}));

vi.mock("./sub-issues-agent-working-chip", () => ({
  SubIssuesAgentWorkingChip: ({
    parentIssueId,
    align,
  }: {
    parentIssueId: string;
    align?: string;
  }) => (
    <div
      data-testid="sub-issues-agent-working-chip"
      data-parent-id={parentIssueId}
      data-align={align}
    >
      2 agents working
    </div>
  ),
}));

const viewState = vi.hoisted(() => ({
  viewMode: "board",
  grouping: "status",
  swimlaneGrouping: "assignee",
  showSubIssues: false,
  cardProperties: {
    priority: false,
    description: false,
    assignee: false,
    startDate: false,
    dueDate: false,
    project: false,
    childProgress: false,
    labels: false,
  },
  cardPropertyIds: [],
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: typeof viewState) => unknown) => selector(viewState),
}));

const testIssue = {
  id: "parent-issue-1",
  identifier: "MUL-100",
  title: "Parent task with subtasks",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  labels: [],
  properties: {},
  start_date: null,
  due_date: null,
  updated_at: "2026-09-28T00:00:00Z",
} as unknown as Issue;

describe("BoardCardContent sub-issues agent activity indicator", () => {
  beforeEach(() => {
    viewState.showSubIssues = false;
  });

  it("renders SubIssuesAgentWorkingChip when subtasks are hidden and childProgress exists", () => {
    viewState.showSubIssues = false;

    render(
      <BoardCardContent
        issue={testIssue}
        childProgress={{ done: 1, total: 3 }}
      />,
    );

    const chip = screen.getByTestId("sub-issues-agent-working-chip");
    expect(chip).toBeDefined();
    expect(chip.getAttribute("data-parent-id")).toBe("parent-issue-1");
    expect(chip.getAttribute("data-align")).toBe("end");
  });

  it("does not render SubIssuesAgentWorkingChip when subtasks are shown", () => {
    viewState.showSubIssues = true;

    render(
      <BoardCardContent
        issue={testIssue}
        childProgress={{ done: 1, total: 3 }}
      />,
    );

    expect(screen.queryByTestId("sub-issues-agent-working-chip")).toBeNull();
  });

  it("does not render SubIssuesAgentWorkingChip when childProgress is absent", () => {
    viewState.showSubIssues = false;

    render(
      <BoardCardContent
        issue={testIssue}
        childProgress={undefined}
      />,
    );

    expect(screen.queryByTestId("sub-issues-agent-working-chip")).toBeNull();
  });

  it("stops click and pointerdown events from bubbling through PickerWrapper", () => {
    viewState.showSubIssues = false;
    const parentClick = vi.fn();
    const parentPointerDown = vi.fn();

    render(
      <div
        onClick={parentClick}
        onPointerDown={parentPointerDown}
      >
        <BoardCardContent
          issue={testIssue}
          childProgress={{ done: 1, total: 3 }}
        />
      </div>,
    );

    const chip = screen.getByTestId("sub-issues-agent-working-chip");
    fireEvent.click(chip);
    expect(parentClick).not.toHaveBeenCalled();

    fireEvent.pointerDown(chip);
    expect(parentPointerDown).not.toHaveBeenCalled();
  });
});
