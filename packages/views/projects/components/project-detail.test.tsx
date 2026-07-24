import React from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Project } from "@multica/core/types";

import { ProjectDetail } from "./project-detail";

const mocks = vi.hoisted(() => ({
  isMobile: true,
  project: {
    id: "project-1",
    workspace_id: "workspace-1",
    title: "Launch Plan",
    description: null,
    icon: null,
    status: "in_progress",
    priority: "high",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-06-01T00:00:00Z",
    updated_at: "2026-06-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
  } as Project,
  onLayoutChanged: vi.fn(),
  panelGroupProps: [] as any[],
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
  createPin: vi.fn(),
  deletePin: vi.fn(),
  recordVisit: vi.fn(),
}));

vi.mock("react-resizable-panels", () => ({
  Group: ({ children, ...props }: any) => {
    mocks.panelGroupProps.push(props);
    return <div data-testid="panel-group">{children}</div>;
  },
  Panel: ({ children, ...props }: any) => (
    <div data-testid="panel" {...props}>
      {children}
    </div>
  ),
  Separator: ({ children, ...props }: any) => (
    <div data-testid="panel-handle" {...props}>
      {children}
    </div>
  ),
  useDefaultLayout: () => ({
    defaultLayout: { content: 70, sidebar: 30 },
    onLayoutChanged: mocks.onLayoutChanged,
  }),
  usePanelRef: () => ({
    current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() },
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    if (options.queryKey?.[0] === "project") {
      return { data: mocks.project, isLoading: false };
    }
    return { data: [], isLoading: false };
  },
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

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projects: () => "/test-workspace/projects",
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Test Lead" }),
}));

vi.mock("@multica/core/chat", () => ({
  useRecentContextStore: (selector: (state: unknown) => unknown) =>
    selector({ recordVisit: mocks.recordVisit }),
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => mocks.isMobile,
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "label" }),
}));

vi.mock("../../layout/breadcrumb-header", () => ({
  BreadcrumbHeader: ({
    leaf,
    actions,
  }: {
    leaf: React.ReactNode;
    actions?: React.ReactNode;
  }) => (
    <header>
      {leaf}
      {actions}
    </header>
  ),
}));

vi.mock("../../layout/animated-right-sidebar", () => ({
  AnimatedRightSidebar: ({ children }: { children: React.ReactNode }) => (
    <aside>{children}</aside>
  ),
  getAnimatedRightSidebarInitialOpen: () => true,
  rightSidebarPanelMotionProps: {},
  useAnimatedRightSidebarState: () => ({
    open: true,
    visualOpen: true,
    motionEnabled: false,
    beginToggle: vi.fn(),
    handleResize: vi.fn(),
  }),
}));

vi.mock("../../editor", () => ({
  ContentEditor: () => <div data-testid="content-editor" />,
  TitleEditor: ({ defaultValue }: { defaultValue: string }) => (
    <span>{defaultValue}</span>
  ),
}));

vi.mock("../../issues/surface/issue-surface", () => ({
  IssueSurface: () => <div data-testid="issue-surface" />,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("./project-resources-section", () => ({
  ProjectResourcesSection: () => <div data-testid="project-resources" />,
}));

vi.mock("./project-start-date-picker", () => ({
  ProjectStartDatePicker: () => <button type="button">start date</button>,
}));

vi.mock("./project-due-date-picker", () => ({
  ProjectDueDatePicker: () => <button type="button">due date</button>,
}));

vi.mock("./priority-icon", () => ({
  PriorityIcon: () => <span data-testid="priority-icon" />,
}));

vi.mock("./labels", () => ({
  useProjectStatusLabels: () => ({ in_progress: "In Progress" }),
  useProjectPriorityLabels: () => ({ high: "High" }),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
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
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/sheet", () => ({
  Sheet: ({
    open,
    children,
  }: {
    open: boolean;
    children: React.ReactNode;
  }) => (open ? <div data-testid="sheet">{children}</div> : null),
  SheetContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/common/emoji-picker", () => ({
  EmojiPicker: () => <div data-testid="emoji-picker" />,
}));

beforeEach(() => {
  mocks.isMobile = true;
  mocks.onLayoutChanged.mockClear();
  mocks.panelGroupProps = [];
});

describe("ProjectDetail responsive layout", () => {
  it("does not mount a resizable panel group on mobile when a desktop layout was restored", () => {
    render(<ProjectDetail projectId="project-1" />);

    expect(screen.getByText("Launch Plan")).toBeInTheDocument();
    expect(screen.getByTestId("issue-surface")).toBeInTheDocument();
    expect(screen.queryByTestId("panel-group")).not.toBeInTheDocument();
    expect(mocks.onLayoutChanged).not.toHaveBeenCalled();
  });

  it("keeps the persisted split-panel group on desktop", () => {
    mocks.isMobile = false;

    render(<ProjectDetail projectId="project-1" />);

    expect(mocks.panelGroupProps[0]?.defaultLayout).toEqual({
      content: 70,
      sidebar: 30,
    });
    expect(screen.getAllByTestId("panel")).toHaveLength(2);
  });
});
