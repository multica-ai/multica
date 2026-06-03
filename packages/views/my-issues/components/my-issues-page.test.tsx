// @vitest-environment jsdom

import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";

const mockBoardView = vi.hoisted(() => vi.fn());
const mockListView = vi.hoisted(() => vi.fn());
const mockSwimlaneView = vi.hoisted(() => vi.fn());
const mockMyIssueListOptions = vi.hoisted(() => vi.fn());
const mockMyIssueAssigneeGroupsOptions = vi.hoisted(() => vi.fn());
const mockUseQuery = vi.hoisted(() => vi.fn());

const viewState = vi.hoisted(() => ({
  viewMode: "board" as "board" | "list" | "swimlane",
  grouping: "status" as "status" | "assignee",
  scope: "assigned" as "all" | "assigned" | "created" | "agents",
  statusFilters: [] as IssueStatus[],
  priorityFilters: [] as IssuePriority[],
  assigneeFilters: [] as { type: string; id: string }[],
  includeNoAssignee: false,
  creatorFilters: [] as { type: string; id: string }[],
  projectFilters: [] as string[],
  includeNoProject: false,
  labelFilters: [] as string[],
  sortBy: "position" as const,
  sortDirection: "asc" as const,
  agentRunningFilter: false,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options?: { queryKey?: readonly unknown[] }) => mockUseQuery(options),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (state: { user: { id: string } | null }) => unknown) => {
    const state = { user: { id: "user-1" } };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/issues/stores/view-store", () => ({
  useClearFiltersOnWorkspaceChange: () => {},
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: {
    getState: () => ({ clear: vi.fn() }),
  },
}));

vi.mock("@multica/core/issues/stores/my-issues-view-store", () => ({
  myIssuesViewStore: {
    getState: () => viewState,
    setState: vi.fn(),
    subscribe: () => () => {},
  },
}));

vi.mock("@multica/core/issues/config", () => ({
  BOARD_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked"],
}));

vi.mock("@multica/core/issues/queries", () => ({
  myIssueListOptions: (
    wsId: string,
    scope: string,
    filter: unknown,
    userId?: string,
    sort?: unknown,
  ) => {
    mockMyIssueListOptions(wsId, scope, filter, userId, sort);
    return { queryKey: ["my-issues", scope, filter], queryFn: vi.fn() };
  },
  myIssueAssigneeGroupsOptions: (
    wsId: string,
    scope: string,
    filter: unknown,
    userId?: string,
    sort?: unknown,
  ) => {
    mockMyIssueAssigneeGroupsOptions(wsId, scope, filter, userId, sort);
    return { queryKey: ["my-assignee-groups", scope, filter], queryFn: vi.fn() };
  },
  childIssueProgressOptions: () => ({ queryKey: ["child-progress"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["agent-task-snapshot"], queryFn: vi.fn() }),
}));

vi.mock("../../issues/components/board-view", () => ({
  BoardView: (props: unknown) => {
    mockBoardView(props);
    return <div data-testid="board-view" />;
  },
}));

vi.mock("../../issues/components/list-view", () => ({
  ListView: (props: unknown) => {
    mockListView(props);
    return <div data-testid="list-view" />;
  },
}));

vi.mock("../../issues/components/swimlane-view", () => ({
  SwimLaneView: (props: unknown) => {
    mockSwimlaneView(props);
    return <div data-testid="swimlane-view" />;
  },
}));

vi.mock("../../issues/components/batch-action-toolbar", () => ({
  BatchActionToolbar: () => null,
}));

vi.mock("../../layout/page-header", () => ({
  PageHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("./my-issues-header", () => ({
  MyIssuesHeader: () => <div data-testid="my-issues-header" />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

import { MyIssuesPage } from "./my-issues-page";

function makeIssue({
  id,
  status,
  project_id,
}: Pick<Issue, "id" | "status" | "project_id">): Issue {
  return {
    id,
    workspace_id: "ws-test",
    number: Number(id.replace(/\D/g, "")) || 1,
    identifier: id.toUpperCase(),
    title: id,
    description: null,
    status,
    priority: "none",
    assignee_type: "member",
    assignee_id: "user-1",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id,
    position: 0,
    due_date: null,
    start_date: null,
    labels: [],
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const mixedIssues: Issue[] = [
  makeIssue({ id: "issue-1", status: "todo", project_id: "project-1" }),
  makeIssue({ id: "issue-2", status: "todo", project_id: "project-2" }),
  makeIssue({ id: "issue-3", status: "done", project_id: "project-1" }),
];

const groupedIssues = {
  groups: [
    {
      id: "member:user-1",
      assignee_type: "member",
      assignee_id: "user-1",
      issues: mixedIssues,
      total: mixedIssues.length,
    },
  ],
};

describe("MyIssuesPage", () => {
  beforeEach(() => {
    viewState.viewMode = "board";
    viewState.grouping = "status";
    viewState.scope = "assigned";
    viewState.statusFilters = [];
    viewState.priorityFilters = [];
    viewState.assigneeFilters = [];
    viewState.includeNoAssignee = false;
    viewState.creatorFilters = [];
    viewState.projectFilters = [];
    viewState.includeNoProject = false;
    viewState.labelFilters = [];
    viewState.sortBy = "position";
    viewState.sortDirection = "asc";
    viewState.agentRunningFilter = false;

    mockBoardView.mockClear();
    mockListView.mockClear();
    mockSwimlaneView.mockClear();
    mockMyIssueListOptions.mockClear();
    mockMyIssueAssigneeGroupsOptions.mockClear();
    mockUseQuery.mockReset();
    mockUseQuery.mockImplementation((options?: { queryKey?: readonly unknown[] }) => {
      const key = options?.queryKey?.[0];
      if (key === "my-issues") return { data: mixedIssues, isLoading: false };
      if (key === "my-assignee-groups") return { data: groupedIssues, isLoading: false };
      if (key === "agent-task-snapshot") return { data: [], isLoading: false };
      if (key === "child-progress") return { data: new Map(), isLoading: false };
      return { data: [], isLoading: false };
    });
  });

  it("sends a single project filter through the my-issues query and board pagination target", () => {
    viewState.statusFilters = ["todo"];
    viewState.projectFilters = ["project-1"];

    render(<MyIssuesPage />);

    expect(mockMyIssueListOptions).toHaveBeenCalledWith(
      "ws-test",
      "assigned",
      expect.objectContaining({
        assignee_id: "user-1",
        statuses: ["todo"],
        project_ids: ["project-1"],
      }),
      "user-1",
      expect.any(Object),
    );
    expect(mockBoardView).toHaveBeenCalledWith(
      expect.objectContaining({
        myIssuesScope: "assigned",
        myIssuesFilter: expect.objectContaining({
          assignee_id: "user-1",
          statuses: ["todo"],
          project_ids: ["project-1"],
        }),
        issues: [mixedIssues[0]],
      }),
    );
  });

  it("passes multi-project filters into the assignee-grouped board query", () => {
    viewState.grouping = "assignee";
    viewState.projectFilters = ["project-1", "project-2"];
    viewState.statusFilters = ["todo", "done"];

    render(<MyIssuesPage />);

    expect(mockMyIssueAssigneeGroupsOptions).toHaveBeenCalledWith(
      "ws-test",
      "assigned",
      expect.objectContaining({
        assignee_id: "user-1",
        statuses: ["todo", "done"],
        project_ids: ["project-1", "project-2"],
      }),
      "user-1",
      expect.any(Object),
    );
    expect(mockBoardView).toHaveBeenCalledWith(
      expect.objectContaining({
        assigneeGroupFilter: expect.objectContaining({
          assignee_id: "user-1",
          statuses: ["todo", "done"],
          project_ids: ["project-1", "project-2"],
        }),
      }),
    );
  });

  it("applies project + status filters to the rendered issue set", () => {
    viewState.viewMode = "list";
    viewState.statusFilters = ["todo"];
    viewState.projectFilters = ["project-1"];

    render(<MyIssuesPage />);

    expect(mockListView).toHaveBeenCalledWith(
      expect.objectContaining({
        issues: [mixedIssues[0]],
        myIssuesFilter: expect.objectContaining({
          statuses: ["todo"],
          project_ids: ["project-1"],
        }),
      }),
    );
  });

  it("clearing project filters restores all matching statuses and drops project_ids from the query", () => {
    viewState.viewMode = "list";
    viewState.statusFilters = ["todo"];

    render(<MyIssuesPage />);

    expect(mockMyIssueListOptions).toHaveBeenCalledWith(
      "ws-test",
      "assigned",
      expect.not.objectContaining({
        project_ids: expect.anything(),
      }),
      "user-1",
      expect.any(Object),
    );
    expect(mockListView).toHaveBeenCalledWith(
      expect.objectContaining({
        issues: [mixedIssues[0], mixedIssues[1]],
        myIssuesFilter: expect.not.objectContaining({
          project_ids: expect.anything(),
        }),
      }),
    );
  });
});
