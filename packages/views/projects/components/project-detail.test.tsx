import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";

const mockBoardView = vi.hoisted(() => vi.fn());
const mockListView = vi.hoisted(() => vi.fn());
const viewState = vi.hoisted(() => ({
  viewMode: "board" as "board" | "list",
  statusFilters: [] as IssueStatus[],
  priorityFilters: [] as IssuePriority[],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  labelFilters: [],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: new Map() }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/issues/queries", () => ({
  myIssueListOptions: vi.fn(),
  childIssueProgressOptions: vi.fn(),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: ReactNode }) => children,
  useViewStore: (selector?: (state: typeof viewState) => unknown) =>
    (selector ? selector(viewState) : viewState),
}));

vi.mock("../../issues/utils/filter", async () => {
  const actual = await vi.importActual<typeof import("../../issues/utils/filter")>(
    "../../issues/utils/filter",
  );
  return actual;
});

vi.mock("../../issues/components/board-view", () => ({
  BoardView: (props: {
    columnCounts?: Partial<Record<IssueStatus, number>>;
    projectId?: string | null;
  }) => {
    mockBoardView(props);
    return <div data-testid="board-view">{props.projectId ?? "missing"}</div>;
  },
}));

vi.mock("../../issues/components/list-view", () => ({
  ListView: (props: { projectId?: string | null }) => {
    mockListView(props);
    return <div data-testid="list-view">{props.projectId ?? "missing"}</div>;
  },
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

import { ProjectIssuesContent } from "./project-detail";

function issue({
  id,
  status,
  title,
  ...overrides
}: Partial<Issue> & Pick<Issue, "id" | "status" | "title">): Issue {
  return {
    id,
    workspace_id: "ws-test",
    number: 1,
    identifier: id.toUpperCase(),
    title,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "creator-1",
    parent_issue_id: null,
    project_id: "project-1",
    position: 0,
    due_date: null,
    start_date: null,
    labels: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const issues: Issue[] = [
  issue({
    id: "issue-1",
    title: "Ship regression fix",
    status: "todo",
    priority: "none",
  }),
];

describe("ProjectIssuesContent", () => {
  beforeEach(() => {
    viewState.viewMode = "board";
    viewState.statusFilters = [];
    viewState.priorityFilters = [];
    viewState.assigneeFilters = [];
    viewState.includeNoAssignee = false;
    viewState.creatorFilters = [];
    viewState.labelFilters = [];
    mockBoardView.mockClear();
    mockListView.mockClear();
  });

  it("passes the current project to board view create affordances", () => {
    render(
      <ProjectIssuesContent
        projectId="project-1"
        projectIssues={issues}
        ganttIssues={[]}
        scope="project:project-1"
        filter={{ project_id: "project-1" }}
      />,
    );

    expect(screen.getByTestId("board-view").textContent).toBe("project-1");
    expect(mockBoardView).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: "project-1" }),
    );
    expect(mockListView).not.toHaveBeenCalled();
  });

  it("passes the current project to list view create affordances", () => {
    viewState.viewMode = "list";

    render(
      <ProjectIssuesContent
        projectId="project-1"
        projectIssues={issues}
        ganttIssues={[]}
        scope="project:project-1"
        filter={{ project_id: "project-1" }}
      />,
    );

    expect(screen.getByTestId("list-view").textContent).toBe("project-1");
    expect(mockListView).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: "project-1" }),
    );
    expect(mockBoardView).not.toHaveBeenCalled();
  });

  it("overrides board column counts when project issues are filtered by priority", () => {
    viewState.priorityFilters = ["high"];

    render(
      <ProjectIssuesContent
        projectId="project-1"
        projectIssues={[
          issue({
            id: "issue-1",
            title: "High priority todo",
            status: "todo",
            priority: "high",
          }),
          issue({
            id: "issue-2",
            title: "Medium priority todo",
            status: "todo",
            priority: "medium",
          }),
          issue({
            id: "issue-3",
            title: "Medium priority done",
            status: "done",
            priority: "medium",
          }),
        ]}
        ganttIssues={[]}
        scope="project:project-1"
        filter={{ project_id: "project-1" }}
      />,
    );

    expect(mockBoardView).toHaveBeenCalledWith(
      expect.objectContaining({
        issues: expect.arrayContaining([
          expect.objectContaining({ id: "issue-1" }),
        ]),
        columnCounts: expect.objectContaining({
          todo: 1,
          done: 0,
        }),
      }),
    );
  });
});
