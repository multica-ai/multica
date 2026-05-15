import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock @multica/core/auth
const mockAuthUser = { id: "user-1", email: "test@test.com", name: "Test User" };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

// Mock @multica/core/paths — after the URL-driven workspace refactor,
// useCurrentWorkspace derives from the workspace slug in URL Context. Tests
// don't mount a real route, so we short-circuit to a fixed fixture.
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test WS", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

// Mock @multica/views/navigation (AppLink + useNavigation)
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues" }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock workspace avatar
vi.mock("../../workspace/workspace-avatar", () => ({
  WorkspaceAvatar: ({ name }: { name: string }) => <span data-testid="workspace-avatar">{name.charAt(0)}</span>,
}));

// Mock api (queries use api internally)
const mockListIssues = vi.hoisted(() => vi.fn().mockResolvedValue({ issues: [], total: 0 }));
vi.mock("@multica/core/api", () => ({
  api: {
    listIssues: (...args: any[]) => mockListIssues(...args),
    updateIssue: vi.fn(),
    listMembers: () => Promise.resolve([]),
    listAgents: () => Promise.resolve([]),
  },
  getApi: () => ({
    listIssues: (...args: any[]) => mockListIssues(...args),
    updateIssue: vi.fn(),
    listMembers: () => Promise.resolve([]),
    listAgents: () => Promise.resolve([]),
  }),
  setApiInstance: vi.fn(),
}));

// Mock issue config
vi.mock("@multica/core/issues/config", () => ({
  ALL_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  BOARD_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked"],
  STATUS_ORDER: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  STATUS_CONFIG: {
    backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
    in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10" },
    in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10" },
    done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10" },
    blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10" },
    cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent" },
  },
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
  PRIORITY_CONFIG: {
    urgent: { label: "Urgent", bars: 4, color: "text-destructive" },
    high: { label: "High", bars: 3, color: "text-warning" },
    medium: { label: "Medium", bars: 2, color: "text-warning" },
    low: { label: "Low", bars: 1, color: "text-info" },
    none: { label: "No priority", bars: 0, color: "text-muted-foreground" },
  },
}));

// Mock view store
const mockViewState = {
  viewMode: "board" as "board" | "list",
  statusFilters: [] as string[],
  priorityFilters: [] as string[],
  assigneeFilters: [] as { type: string; id: string }[],
  includeNoAssignee: false,
  creatorFilters: [] as { type: string; id: string }[],
  projectFilters: [] as string[],
  includeNoProject: false,
  labelFilters: [] as string[],
  sortBy: "position" as const,
  sortDirection: "asc" as const,
  cardProperties: { priority: true, description: true, assignee: true, dueDate: true, project: true, childProgress: true, labels: true },
  listCollapsedStatuses: [] as string[],
  setViewMode: vi.fn(),
  toggleStatusFilter: vi.fn(),
  togglePriorityFilter: vi.fn(),
  toggleAssigneeFilter: vi.fn(),
  toggleNoAssignee: vi.fn(),
  toggleCreatorFilter: vi.fn(),
  toggleProjectFilter: vi.fn(),
  toggleNoProject: vi.fn(),
  toggleLabelFilter: vi.fn(),
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
  clearFilters: vi.fn(),
  setSortBy: vi.fn(),
  setSortDirection: vi.fn(),
  toggleCardProperty: vi.fn(),
  toggleListCollapsed: vi.fn(),
};

vi.mock("@multica/core/issues/stores/view-store", () => ({
  useClearFiltersOnWorkspaceChange: () => {},
  viewStorePersistOptions: () => ({ name: "test", storage: undefined, partialize: (s: any) => s }),
  mergeViewStatePersisted: (_p: unknown, c: any) => c,
  viewStoreSlice: vi.fn(),
  useIssueViewStore: Object.assign(
    (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
    { getState: () => mockViewState, setState: vi.fn() },
  ),
  createIssueViewStore: () => ({
    getState: () => mockViewState,
    setState: vi.fn(),
    subscribe: vi.fn(),
  }),
  SORT_OPTIONS: [
    { value: "position", label: "Manual" },
    { value: "priority", label: "Priority" },
    { value: "due_date", label: "Due date" },
    { value: "created_at", label: "Created date" },
    { value: "title", label: "Title" },
  ],
  CARD_PROPERTY_OPTIONS: [
    { key: "priority", label: "Priority" },
    { key: "description", label: "Description" },
    { key: "assignee", label: "Assignee" },
    { key: "dueDate", label: "Due date" },
    { key: "project", label: "Project" },
    { key: "labels", label: "Labels" },
    { key: "childProgress", label: "Sub-issue progress" },
  ],
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({ getState: () => mockViewState, setState: vi.fn(), subscribe: vi.fn() }),
}));

let mockScope = "all";

vi.mock("@multica/core/issues/stores/issues-scope-store", () => ({
  useIssuesScopeStore: Object.assign(
    (selector?: any) => {
      const state = { scope: mockScope, setScope: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ scope: mockScope, setScope: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: Object.assign(
    (selector?: any) => {
      const state = { selectedIds: new Set(), toggle: vi.fn(), clear: vi.fn(), setAll: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ selectedIds: new Set(), toggle: vi.fn(), clear: vi.fn(), setAll: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/issues/stores/recent-issues-store", () => ({
  useRecentIssuesStore: Object.assign(
    (selector?: any) => {
      const state = { byWorkspace: {}, recordVisit: vi.fn(), pruneWorkspaces: vi.fn() };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        byWorkspace: {},
        recordVisit: vi.fn(),
        pruneWorkspaces: vi.fn(),
      }),
    },
  ),
  selectRecentIssues: () => () => [],
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

// Mock sonner toast
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Mock dnd-kit
vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: any) => children,
  DragOverlay: () => null,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
  pointerWithin: vi.fn(),
  closestCenter: vi.fn(),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: any) => children,
  verticalListSortingStrategy: {},
  arrayMove: vi.fn(),
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
}));

vi.mock("@dnd-kit/utilities", () => ({
  CSS: { Transform: { toString: () => undefined } },
}));

// Mock @base-ui/react/accordion (used by ListView)
vi.mock("@base-ui/react/accordion", () => ({
  Accordion: Object.assign(
    ({ children }: any) => <div>{children}</div>,
    {
      Root: ({ children }: any) => <div>{children}</div>,
      Item: ({ children }: any) => <div>{children}</div>,
      Header: ({ children }: any) => <div>{children}</div>,
      Trigger: ({ children }: any) => <button>{children}</button>,
      Panel: ({ children }: any) => <div>{children}</div>,
    },
  ),
}));

// ---------------------------------------------------------------------------
// Test data
// ---------------------------------------------------------------------------

const issueDefaults = {
  parent_issue_id: null,
  project_id: null,
  position: 0,
};

const mockIssues: Issue[] = [
  {
    ...issueDefaults,
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "TES-1",
    title: "Implement auth",
    description: "Add JWT authentication",
    status: "todo",
    priority: "high",
    assignee_type: "member",
    assignee_id: "user-1",
    creator_type: "member",
    creator_id: "user-1",
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    ...issueDefaults,
    id: "issue-2",
    workspace_id: "ws-1",
    number: 2,
    identifier: "TES-2",
    title: "Design landing page",
    description: null,
    status: "in_progress",
    priority: "medium",
    assignee_type: "agent",
    assignee_id: "agent-1",
    creator_type: "member",
    creator_id: "user-1",
    due_date: "2026-02-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    ...issueDefaults,
    id: "issue-3",
    workspace_id: "ws-1",
    number: 3,
    identifier: "TES-3",
    title: "Write tests",
    description: null,
    status: "backlog",
    priority: "low",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    ...issueDefaults,
    id: "issue-4",
    workspace_id: "ws-1",
    number: 4,
    identifier: "TES-4",
    title: "Squad task",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: "squad",
    assignee_id: "squad-1",
    creator_type: "member",
    creator_id: "user-1",
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
];

// ---------------------------------------------------------------------------
// Import component under test (after mocks)
// ---------------------------------------------------------------------------

import { IssuesPage } from "./issues-page";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        {ui}
      </QueryClientProvider>
    </I18nProvider>,
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("IssuesPage (shared)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListIssues.mockResolvedValue({ issues: [], total: 0 });
    mockViewState.viewMode = "board";
    mockViewState.statusFilters = [];
    mockViewState.priorityFilters = [];
    mockScope = "all";
  });

  it("shows loading skeletons initially", () => {
    renderWithQuery(<IssuesPage />);
    expect(
      screen.getAllByRole("generic").some((el) => el.getAttribute("data-slot") === "skeleton"),
    ).toBe(true);
  });

  it("renders issue titles after data loads", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Implement auth");
    expect(screen.getByText("Design landing page")).toBeInTheDocument();
    expect(screen.getByText("Write tests")).toBeInTheDocument();
  });

  it("renders board column headers", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Backlog");
    expect(screen.getAllByText("Todo").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("In Progress").length).toBeGreaterThanOrEqual(1);
  });

  it("shows workspace breadcrumb with 'Issues' label", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Issues");
    expect(screen.getByText("Test WS")).toBeInTheDocument();
  });

  it("shows empty state when there are no issues", async () => {
    mockListIssues.mockResolvedValue({ issues: [], total: 0 });

    renderWithQuery(<IssuesPage />);

    await screen.findByText("No issues yet");
    expect(screen.getByText("Create an issue to get started.")).toBeInTheDocument();
  });

  it("shows scope tab buttons", async () => {
    renderWithQuery(<IssuesPage />);

    await screen.findByText("All");
    expect(screen.getByText("Members")).toBeInTheDocument();
    expect(screen.getByText("Agents")).toBeInTheDocument();
  });

  it("agents scope includes squad-assigned issues", async () => {
    mockScope = "agents";
    mockViewState.viewMode = "list";
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    renderWithQuery(<IssuesPage />);

    // Squad task and agent task should be visible
    await screen.findByText("Design landing page");
    expect(screen.getByText("Squad task")).toBeInTheDocument();
    // Member task should NOT be visible
    expect(screen.queryByText("Implement auth")).not.toBeInTheDocument();
  });

  it("members scope excludes squad-assigned issues", async () => {
    mockScope = "members";
    mockViewState.viewMode = "list";
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    renderWithQuery(<IssuesPage />);

    await screen.findByText("Implement auth");
    expect(screen.queryByText("Squad task")).not.toBeInTheDocument();
    expect(screen.queryByText("Design landing page")).not.toBeInTheDocument();
  });
});
