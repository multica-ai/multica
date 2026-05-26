import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";

const mockBoardView = vi.hoisted(() => vi.fn());
const mockListView = vi.hoisted(() => vi.fn());
const mockMyIssueListOptions = vi.hoisted(() => vi.fn());
const mockMyIssueAssigneeGroupsOptions = vi.hoisted(() => vi.fn());
const mockProjectGanttIssuesOptions = vi.hoisted(() => vi.fn());
const mockSetViewState = vi.hoisted(() => vi.fn());
const mockUseQuery = vi.hoisted(() => vi.fn());
const viewState = vi.hoisted(() => ({
  viewMode: "board" as "board" | "list",
  grouping: "status" as "status" | "assignee",
  statusFilters: [] as IssueStatus[],
  priorityFilters: [] as IssuePriority[],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  labelFilters: [] as string[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options?: { queryKey?: readonly unknown[] }) => mockUseQuery(options),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/issues/queries", () => ({
  myIssueListOptions: (wsId: string, scope: string, filter: unknown) =>
    mockMyIssueListOptions(wsId, scope, filter),
  myIssueAssigneeGroupsOptions: (wsId: string, scope: string, filter: unknown) =>
    mockMyIssueAssigneeGroupsOptions(wsId, scope, filter),
  projectGanttIssuesOptions: (wsId: string, projectId: string) =>
    mockProjectGanttIssuesOptions(wsId, projectId),
  childIssueProgressOptions: () => ({ queryKey: ["child-progress"] }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: ReactNode }) => children,
  useViewStore: (selector?: (state: typeof viewState) => unknown) =>
    (selector ? selector(viewState) : viewState),
  useViewStoreApi: () => ({ getState: () => viewState, setState: mockSetViewState }),
}));

vi.mock("@multica/core/labels/queries", () => ({
  labelListOptions: (_wsId: string, scope?: { projectId?: string | null }) => ({
    queryKey: ["labels", scope?.projectId ?? null],
  }),
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

vi.mock("../../issues/components/issues-header", () => ({
  IssuesHeader: () => <div data-testid="issues-header" />,
}));

vi.mock("../../issues/components/batch-action-toolbar", () => ({
  BatchActionToolbar: () => <div data-testid="batch-action-toolbar" />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

import { ProjectIssuesContent, ProjectIssuesSurface, pruneLabelFiltersToVisibleProjectLabels } from "./project-detail";

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
    metadata: {},
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
    viewState.grouping = "status";
    viewState.statusFilters = [];
    viewState.priorityFilters = [];
    viewState.assigneeFilters = [];
    viewState.includeNoAssignee = false;
    viewState.creatorFilters = [];
    viewState.labelFilters = [];
    mockBoardView.mockClear();
    mockListView.mockClear();
    mockMyIssueListOptions.mockClear();
    mockMyIssueAssigneeGroupsOptions.mockClear();
    mockProjectGanttIssuesOptions.mockClear();
    mockSetViewState.mockClear();
    mockUseQuery.mockReset();
    mockUseQuery.mockImplementation((options?: { queryKey?: readonly unknown[] }) => {
      if (options?.queryKey?.[0] === "labels") {
        return {
          data: [
            { id: "label-global", project_id: null },
            { id: "label-project-b", project_id: "project-b" },
          ],
          isSuccess: true,
        };
      }
      if (options?.queryKey?.[0] === "my-issues") return { data: [], isSuccess: true };
      if (options?.queryKey?.[0] === "my-assignee-groups") return { data: { groups: [] }, isSuccess: true };
      if (options?.queryKey?.[0] === "project-gantt") return { data: [], isSuccess: true };
      return { data: new Map(), isSuccess: true };
    });
    mockMyIssueListOptions.mockReturnValue({
      queryKey: ["my-issues"],
      queryFn: vi.fn(),
    });
    mockMyIssueAssigneeGroupsOptions.mockReturnValue({
      queryKey: ["my-assignee-groups"],
      queryFn: vi.fn(),
    });
    mockProjectGanttIssuesOptions.mockReturnValue({
      queryKey: ["project-gantt"],
      queryFn: vi.fn(),
    });
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

  it("passes server-filtered project issues through without local priority filtering", () => {
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

    const props = mockBoardView.mock.calls[0]?.[0];
    expect(props.columnCounts).toBeUndefined();
    expect(props.issues.map((i: Issue) => i.id)).toEqual(
      expect.arrayContaining(["issue-1", "issue-2", "issue-3"]),
    );
  });

  it("prunes project label filters to labels visible in the current project scope", () => {
    expect(
      pruneLabelFiltersToVisibleProjectLabels(
        ["label-project-a", "label-global", "label-project-b"],
        [{ id: "label-global" }, { id: "label-project-b" }],
      ),
    ).toEqual(["label-global", "label-project-b"]);
  });

  it("does not send stale project label filters after switching projects", () => {
    viewState.labelFilters = ["label-project-a", "label-global", "label-project-b"];

    render(
      <ProjectIssuesSurface
        projectId="project-b"
        scope="project:project-b"
        filter={{ project_id: "project-b" }}
      />,
    );

    expect(mockMyIssueListOptions).toHaveBeenCalledWith(
      "ws-test",
      "project:project-b",
      expect.objectContaining({
        project_id: "project-b",
        label_ids: ["label-global", "label-project-b"],
      }),
    );
    expect(mockSetViewState).toHaveBeenCalledWith({
      labelFilters: ["label-global", "label-project-b"],
    });
  });
});
