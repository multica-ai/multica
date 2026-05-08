import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Issue, IssueStatus } from "@multica/core/types";

const mockBoardView = vi.hoisted(() => vi.fn());
const mockListView = vi.hoisted(() => vi.fn());
const viewState = vi.hoisted(() => ({
  viewMode: "board" as "board" | "list",
  statusFilters: [] as IssueStatus[],
  priorityFilters: [],
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

vi.mock("../../issues/utils/filter", () => ({
  filterIssues: (issues: Issue[]) => issues,
}));

vi.mock("../../issues/components/board-view", () => ({
  BoardView: (props: { projectId?: string | null }) => {
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

const issues = [
  {
    id: "issue-1",
    identifier: "MUL-1",
    title: "Ship regression fix",
    status: "todo",
    priority: "none",
    position: 0,
  },
] as unknown as Issue[];

describe("ProjectIssuesContent", () => {
  beforeEach(() => {
    viewState.viewMode = "board";
    mockBoardView.mockClear();
    mockListView.mockClear();
  });

  it("passes the current project to board view create affordances", () => {
    render(
      <ProjectIssuesContent
        projectId="project-1"
        projectIssues={issues}
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
});
