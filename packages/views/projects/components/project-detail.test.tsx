import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import type { Project } from "@multica/core/types";
import { ProjectDetail } from "./project-detail";

const mockViewport = vi.hoisted(() => ({ isMobile: false }));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => mockViewport.isMobile,
}));

const mockProject = {
  id: "project-1",
  workspace_id: "ws-1",
  title: "Launch Plan",
  description: "Ship the first version",
  icon: "🚀",
  status: "in_progress",
  priority: "high",
  lead_type: null,
  lead_id: null,
  created_at: "2026-06-01T00:00:00Z",
  updated_at: "2026-06-01T00:00:00Z",
  issue_count: 3,
  done_count: 1,
  resource_count: 0,
} satisfies Project;

const mocks = vi.hoisted(() => ({
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
  createPin: vi.fn(),
  deletePin: vi.fn(),
  recordVisit: vi.fn(),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );

  return {
    ...actual,
    useQuery: (options: { queryKey?: readonly unknown[] }) => {
      const key = options.queryKey?.[0];
      if (key === "project") {
        return { data: mockProject, isLoading: false };
      }
      if (key === "members") {
        return { data: [], isLoading: false };
      }
      if (key === "agents") {
        return { data: [], isLoading: false };
      }
      if (key === "pins") {
        return { data: [], isLoading: false };
      }
      if (key === "projects") {
        return { data: [], isLoading: false };
      }
      if (key === "issues") {
        return { data: [], isLoading: false };
      }
      return { data: [], isLoading: false };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectDetailOptions: () => ({ queryKey: ["project"] }),
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useUpdateProject: () => ({ mutate: mocks.updateProject }),
  useDeleteProject: () => ({ mutate: mocks.deleteProject }),
}));

vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"] }),
  useCreatePin: () => ({ mutate: mocks.createPin }),
  useDeletePin: () => ({ mutate: mocks.deletePin }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Unknown",
  }),
}));

vi.mock("@multica/core/chat", () => ({
  useRecentContextStore: (selector: (state: unknown) => unknown) =>
    selector({ recordVisit: mocks.recordVisit }),
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

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/test/projects/project-1",
    getShareableUrl: (p: string) => p,
  }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: vi.fn() }),
  },
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["agent-task-snapshot"] }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  myIssueAssigneeGroupsOptions: () => ({ queryKey: ["issues", "assignee-groups"] }),
  myIssueListOptions: () => ({ queryKey: ["issues"] }),
  projectGanttIssuesOptions: () => ({ queryKey: ["issues", "gantt"] }),
  childIssueProgressOptions: () => ({ queryKey: ["issues", "child-progress"] }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/issues/stores/view-store", () => ({
  createIssueViewStore: () => ({}),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({
      viewMode: "board",
      grouping: "status",
      sortBy: "position",
      sortDirection: "asc",
      statusFilters: [],
      priorityFilters: [],
      assigneeFilters: [],
      includeNoAssignee: false,
      creatorFilters: [],
      labelFilters: [],
      agentRunningFilter: "all",
    }),
}));

vi.mock("../../issues/utils/filter", () => ({
  filterIssues: (issues: unknown[]) => issues,
}));

vi.mock("./project-issue-metrics", () => ({
  getProjectIssueMetrics: () => ({
    totalCount: 0,
    completedCount: 0,
  }),
}));

vi.mock("./project-issue-filters", () => ({
  filterRunningAssigneeGroups: (groups: unknown[]) => groups,
}));

vi.mock("../../editor", () => ({
  TitleEditor: ({ defaultValue }: { defaultValue: string }) => <input value={defaultValue} readOnly />,
  ContentEditor: () => <div>Description editor</div>,
}));

vi.mock("./project-resources-section", () => ({
  ProjectResourcesSection: () => <div>Resources</div>,
}));

vi.mock("../../issues/components/issues-header", () => ({
  IssuesHeader: () => <div>Issues header</div>,
}));

vi.mock("../../issues/components/board-view", () => ({
  BoardView: () => <div>Board view</div>,
}));

vi.mock("../../issues/components/list-view", () => ({
  ListView: () => <div>List view</div>,
}));

vi.mock("../../issues/components/gantt-view", () => ({
  GanttView: () => <div>Gantt view</div>,
}));

vi.mock("../../issues/components/swimlane-view", () => ({
  SwimLaneView: () => <div>Swimlane view</div>,
}));

vi.mock("../../issues/components/batch-action-toolbar", () => ({
  BatchActionToolbar: () => null,
}));

vi.mock("../../layout/breadcrumb-header", () => ({
  BreadcrumbHeader: ({ leaf, actions }: { leaf: React.ReactNode; actions: React.ReactNode }) => (
    <div>
      <div>{leaf}</div>
      <div>{actions}</div>
    </div>
  ),
}));

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="panel-group">{children}</div>
  ),
  ResizablePanel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizableHandle: () => <div data-testid="panel-handle" />,
}));

vi.mock("@multica/ui/components/ui/sheet", () => ({
  Sheet: ({
    open,
    children,
  }: {
    open: boolean;
    children: React.ReactNode;
  }) => <div data-testid="sheet" data-open={String(open)}>{children}</div>,
  SheetContent: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="sheet-content">{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/common/emoji-picker", () => ({
  EmojiPicker: () => <div>Emoji picker</div>,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  AlertDialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogCancel: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
  AlertDialogAction: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: () => <div>Loading</div>,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>Avatar</div>,
}));

vi.mock("../../issues/components/priority-icon", () => ({
  PriorityIcon: () => <div>Priority</div>,
}));

vi.mock("./labels", () => ({
  useProjectStatusLabels: () => ({
    in_progress: "In Progress",
    planned: "Planned",
    done: "Done",
    cancelled: "Cancelled",
  }),
  useProjectPriorityLabels: () => ({
    high: "High",
    medium: "Medium",
    low: "Low",
    none: "None",
  }),
}));

vi.mock("../../editor/extensions/pinyin-match", () => ({
  matchesPinyin: () => false,
}));

function renderProjectDetail() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return renderWithI18n(
    <QueryClientProvider client={client}>
      <ProjectDetail projectId="project-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockViewport.isMobile = false;
  mocks.updateProject.mockClear();
  mocks.deleteProject.mockClear();
  mocks.createPin.mockClear();
  mocks.deletePin.mockClear();
  mocks.recordVisit.mockClear();
});

describe("ProjectDetail mobile layout", () => {
  it("uses a non-resizable layout with the sidebar sheet closed by default on mobile", () => {
    mockViewport.isMobile = true;

    renderProjectDetail();

    expect(document.querySelector('[data-testid="panel-group"]')).not.toBeInTheDocument();
    expect(document.querySelector('[data-testid="panel-handle"]')).not.toBeInTheDocument();
    expect(document.querySelector('[data-testid="sheet"]')).toHaveAttribute("data-open", "false");
  });

  it("opens the mobile sidebar sheet when the sidebar action is clicked", () => {
    mockViewport.isMobile = true;

    renderProjectDetail();

    const sidebarButton = document
      .querySelector("svg.lucide-panel-right")
      ?.closest("button");

    expect(sidebarButton).toBeInTheDocument();

    fireEvent.click(sidebarButton!);

    expect(document.querySelector('[data-testid="sheet"]')).toHaveAttribute("data-open", "true");
  });

  it("keeps the resizable layout on desktop", () => {
    mockViewport.isMobile = false;

    renderProjectDetail();

    expect(document.querySelector('[data-testid="panel-group"]')).toBeInTheDocument();
    expect(document.querySelector('[data-testid="panel-handle"]')).toBeInTheDocument();
    expect(document.querySelector('[data-testid="sheet"]')).not.toBeInTheDocument();
  });
});
