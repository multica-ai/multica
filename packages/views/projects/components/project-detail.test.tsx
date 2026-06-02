import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore, type StoreApi } from "zustand/vanilla";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue, Project } from "@multica/core/types";
import type { IssueViewState } from "@multica/core/issues/stores/view-store";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import enProjects from "../../locales/en/projects.json";
import { ProjectDetail } from "./project-detail";

const apiMocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  listIssues: vi.fn(),
  listGroupedIssues: vi.fn(),
  getAgentTaskSnapshot: vi.fn(),
  getChildIssueProgress: vi.fn(),
  listMembers: vi.fn(),
  listAgents: vi.fn(),
  listProjects: vi.fn(),
  listPins: vi.fn(),
  updateIssue: vi.fn(),
}));

const projectStoreRef = vi.hoisted(() => ({
  store: null as StoreApi<IssueViewState> | null,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getProject: (...args: any[]) => apiMocks.getProject(...args),
    listIssues: (...args: any[]) => apiMocks.listIssues(...args),
    listGroupedIssues: (...args: any[]) =>
      apiMocks.listGroupedIssues(...args),
    getAgentTaskSnapshot: (...args: any[]) =>
      apiMocks.getAgentTaskSnapshot(...args),
    getChildIssueProgress: (...args: any[]) =>
      apiMocks.getChildIssueProgress(...args),
    listMembers: (...args: any[]) => apiMocks.listMembers(...args),
    listAgents: (...args: any[]) => apiMocks.listAgents(...args),
    listProjects: (...args: any[]) => apiMocks.listProjects(...args),
    listPins: (...args: any[]) => apiMocks.listPins(...args),
    updateIssue: (...args: any[]) => apiMocks.updateIssue(...args),
  },
}));

vi.mock("@multica/core/issues/stores/view-store", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/issues/stores/view-store")>(
      "@multica/core/issues/stores/view-store",
    );

  return {
    ...actual,
    createIssueViewStore: () => {
      const store = createStore<IssueViewState>()((set) =>
        actual.viewStoreSlice(set),
      );
      projectStoreRef.store = store;
      return store;
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: any) => selector({ user: null }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), pathname: "/test/projects/proj-1" }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Someone" }),
}));

vi.mock("../../layout/breadcrumb-header", () => ({
  BreadcrumbHeader: ({ leaf, actions }: any) => (
    <div>
      {leaf}
      {actions}
    </div>
  ),
}));

vi.mock("../../editor", () => ({
  TitleEditor: ({ defaultValue }: any) => (
    <div data-testid="title-editor">{defaultValue}</div>
  ),
  ContentEditor: () => null,
}));

vi.mock("./project-resources-section", () => ({
  ProjectResourcesSection: () => null,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../issues/components/issues-header", () => ({
  IssuesHeader: () => null,
}));

vi.mock("../../issues/components/board-view", () => ({
  BoardView: ({ issues, assigneeGroups }: any) => {
    const renderedIssues = assigneeGroups
      ? assigneeGroups.flatMap((group: any) => group.issues)
      : issues;

    return (
      <div data-testid="board-view">
        <div data-testid="board-rendered-issues">
          {renderedIssues.map((issue: Issue) => issue.title).join(",")}
        </div>
        {assigneeGroups?.map((group: any) => (
          <div key={group.id} data-testid="assignee-group">
            {group.issues.map((issue: Issue) => issue.title).join(",")}
          </div>
        ))}
      </div>
    );
  },
}));

vi.mock("../../issues/components/list-view", () => ({
  ListView: () => null,
}));

vi.mock("../../issues/components/gantt-view", () => ({
  GanttView: () => null,
}));

vi.mock("../../issues/components/swimlane-view", () => ({
  SwimLaneView: () => null,
}));

vi.mock("../../issues/components/batch-action-toolbar", () => ({
  BatchActionToolbar: () => null,
}));

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: any) => <div>{children}</div>,
  ResizablePanel: ({ children }: any) => <div>{children}</div>,
  ResizableHandle: () => null,
}));

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({
    defaultLayout: undefined,
    onLayoutChanged: vi.fn(),
  }),
  usePanelRef: () => ({
    current: {
      isCollapsed: () => false,
      collapse: vi.fn(),
      expand: vi.fn(),
    },
  }),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const TEST_RESOURCES = {
  en: { common: enCommon, issues: enIssues, projects: enProjects },
};

const project: Project = {
  id: "proj-1",
  workspace_id: "ws-1",
  title: "Project One",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "medium",
  lead_type: null,
  lead_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  issue_count: 2,
  done_count: 0,
  resource_count: 0,
};

function makeIssue(id: string, title: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: id.toUpperCase(),
    title,
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: "agent",
    assignee_id: "agent-1",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: "proj-1",
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function renderProjectDetail() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ProjectDetail projectId="proj-1" />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("ProjectDetail", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    const runningIssue = makeIssue("issue-running", "Running issue");
    const idleIssue = makeIssue("issue-idle", "Idle issue");

    apiMocks.getProject.mockResolvedValue(project);
    apiMocks.listIssues.mockResolvedValue({ issues: [], total: 0 });
    apiMocks.listGroupedIssues.mockResolvedValue({
      groups: [
        {
          id: "assignee:agent:agent-1",
          assignee_type: "agent",
          assignee_id: "agent-1",
          issues: [runningIssue, idleIssue],
          total: 2,
        },
      ],
    });
    apiMocks.getAgentTaskSnapshot.mockResolvedValue([
      {
        id: "task-1",
        agent_id: "agent-1",
        runtime_id: "runtime-1",
        issue_id: "issue-running",
        status: "running",
      },
    ]);
    apiMocks.getChildIssueProgress.mockResolvedValue({ progress: [] });
    apiMocks.listMembers.mockResolvedValue([]);
    apiMocks.listAgents.mockResolvedValue([]);
    apiMocks.listProjects.mockResolvedValue({ projects: [], total: 0 });
    apiMocks.listPins.mockResolvedValue([]);

    projectStoreRef.store?.setState({
      viewMode: "board",
      grouping: "assignee",
      agentRunningFilter: true,
      statusFilters: [],
      priorityFilters: [],
      assigneeFilters: [],
      includeNoAssignee: false,
      creatorFilters: [],
      labelFilters: [],
    });
  });

  it("filters assignee-grouped project boards by running issues", async () => {
    renderProjectDetail();

    await screen.findByTestId("board-view");

    expect(screen.getByTestId("board-rendered-issues")).toHaveTextContent(
      "Running issue",
    );
    expect(screen.getByTestId("board-rendered-issues")).not.toHaveTextContent(
      "Idle issue",
    );
    expect(screen.getByTestId("assignee-group")).toHaveTextContent(
      "Running issue",
    );
    expect(screen.getByTestId("assignee-group")).not.toHaveTextContent(
      "Idle issue",
    );
  });
});
