// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue, IssueStatus } from "@multica/core/types";
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
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
const navigationState = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
  openInNewTab: vi.fn(),
  search: "",
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({
    push: navigationState.push,
    replace: navigationState.replace,
    openInNewTab: navigationState.openInNewTab,
    pathname: "/issues",
    searchParams: new URLSearchParams(navigationState.search),
  }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock workspace avatar
vi.mock("../../workspace/workspace-avatar", () => ({
  WorkspaceAvatar: ({ name }: { name: string }) => <span data-testid="workspace-avatar">{name.charAt(0)}</span>,
}));

// Mock api (queries use api internally)
const mockListIssues = vi.hoisted(() => vi.fn().mockResolvedValue({ issues: [], total: 0 }));
const mockGetChildIssueProgress = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ progress: [] }),
);
const mockGetIssueExecutionSummaries = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ summaries: [] }),
);
const mockGetIssue = vi.hoisted(() => vi.fn());
const mockListTimeline = vi.hoisted(() => vi.fn().mockResolvedValue([]));
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );

  return {
    ...actual,
    api: {
      listIssues: (...args: any[]) => mockListIssues(...args),
      getChildIssueProgress: (...args: any[]) => mockGetChildIssueProgress(...args),
      getIssueExecutionSummaries: (...args: any[]) =>
        mockGetIssueExecutionSummaries(...args),
      getIssue: (...args: any[]) => mockGetIssue(...args),
      listTimeline: (...args: any[]) => mockListTimeline(...args),
      getActiveTasksForIssue: vi.fn().mockResolvedValue({ tasks: [] }),
      createComment: vi.fn(),
      updateIssue: vi.fn(),
      listMembers: () => Promise.resolve([]),
      listAgents: () => Promise.resolve([]),
    },
    getApi: () => ({
      listIssues: (...args: any[]) => mockListIssues(...args),
      getChildIssueProgress: (...args: any[]) => mockGetChildIssueProgress(...args),
      getIssueExecutionSummaries: (...args: any[]) =>
        mockGetIssueExecutionSummaries(...args),
      getIssue: (...args: any[]) => mockGetIssue(...args),
      listTimeline: (...args: any[]) => mockListTimeline(...args),
      getActiveTasksForIssue: vi.fn().mockResolvedValue({ tasks: [] }),
      createComment: vi.fn(),
      updateIssue: vi.fn(),
      listMembers: () => Promise.resolve([]),
      listAgents: () => Promise.resolve([]),
    }),
    setApiInstance: vi.fn(),
  };
});

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
  viewMode: "board" as const,
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

vi.mock("@multica/core/issues/stores/issues-scope-store", () => ({
  useIssuesScopeStore: Object.assign(
    (selector?: any) => {
      const state = { scope: "all", setScope: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ scope: "all", setScope: vi.fn() }) },
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
      const state = { items: [], recordVisit: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ items: [], recordVisit: vi.fn() }) },
  ),
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
];

// ---------------------------------------------------------------------------
// Import component under test (after mocks)
// ---------------------------------------------------------------------------

import { IssuesPage } from "./issues-page";
import { BoardView } from "./board-view";

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
    <QueryClientProvider client={qc}>
      {ui}
    </QueryClientProvider>,
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("IssuesPage (shared)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: true,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    });
    Object.defineProperty(HTMLElement.prototype, "scrollTo", {
      configurable: true,
      value: vi.fn(),
    });
    Object.defineProperty(HTMLElement.prototype, "getAnimations", {
      configurable: true,
      value: vi.fn(() => []),
    });
    navigationState.search = "";
    mockListIssues.mockResolvedValue({ issues: [], total: 0 });
    mockGetChildIssueProgress.mockResolvedValue({ progress: [] });
    mockGetIssueExecutionSummaries.mockResolvedValue({ summaries: [] });
    mockGetIssue.mockImplementation((id: string) =>
      Promise.resolve(mockIssues.find((issue) => issue.id === id) ?? mockIssues[0]),
    );
    mockListTimeline.mockResolvedValue([]);
    mockViewState.viewMode = "board";
    mockViewState.statusFilters = [];
    mockViewState.priorityFilters = [];
  });

  afterEach(() => {
    cleanup();
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

  it("opens preview via peek query when clicking an issue card", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );

    renderWithQuery(<IssuesPage />);

    const title = await screen.findByText("Implement auth");
    const link = title.closest("a");
    expect(link).toBeTruthy();
    fireEvent.click(link!);

    expect(navigationState.replace).toHaveBeenCalledWith("/test/issues?peek=issue-1");
  });

  it("navigates the preview with J/K and arrow keys within the current lane", async () => {
    const laneIssues: Issue[] = [
      { ...mockIssues[0]!, position: 0 },
      {
        ...mockIssues[0]!,
        id: "issue-4",
        number: 4,
        identifier: "TES-4",
        title: "Second todo",
        position: 1,
      },
    ];
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: laneIssues.filter((i) => i.status === params?.status),
        total: laneIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    mockGetIssue.mockImplementation((id: string) =>
      Promise.resolve(laneIssues.find((issue) => issue.id === id) ?? laneIssues[0]),
    );
    navigationState.search = "?peek=issue-1";

    renderWithQuery(<IssuesPage />);

    await screen.findAllByText("Second todo");
    navigationState.replace.mockClear();

    fireEvent.keyDown(window, { key: "j" });
    expect(navigationState.replace).toHaveBeenCalledWith("/test/issues?peek=issue-4");

    navigationState.replace.mockClear();
    fireEvent.keyDown(window, { key: "ArrowDown" });
    expect(navigationState.replace).toHaveBeenCalledWith("/test/issues?peek=issue-4");
  });

  it("navigates to the previous preview item with K and ArrowUp", async () => {
    const laneIssues: Issue[] = [
      { ...mockIssues[0]!, position: 0 },
      {
        ...mockIssues[0]!,
        id: "issue-4",
        number: 4,
        identifier: "TES-4",
        title: "Second todo",
        position: 1,
      },
    ];
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: laneIssues.filter((i) => i.status === params?.status),
        total: laneIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    mockGetIssue.mockImplementation((id: string) =>
      Promise.resolve(laneIssues.find((issue) => issue.id === id) ?? laneIssues[1]),
    );
    navigationState.search = "?peek=issue-4";

    renderWithQuery(<IssuesPage />);

    await screen.findAllByText("Second todo");
    navigationState.replace.mockClear();

    fireEvent.keyDown(window, { key: "k" });
    expect(navigationState.replace).toHaveBeenCalledWith("/test/issues?peek=issue-1");

    navigationState.replace.mockClear();
    fireEvent.keyDown(window, { key: "ArrowUp" });
    expect(navigationState.replace).toHaveBeenCalledWith("/test/issues?peek=issue-1");
  });

  it("closes preview with Escape without resetting workbench state", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    navigationState.search = "?peek=issue-1";

    renderWithQuery(<IssuesPage />);

    await screen.findAllByText("Implement auth");
    navigationState.replace.mockClear();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(navigationState.replace).toHaveBeenCalledWith("/test/issues");
  });

  it("does not trigger preview shortcuts while typing in quick comment", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    navigationState.search = "?peek=issue-1";

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Quick comment");
    const textbox = document.querySelector(
      "[contenteditable='true'], [contenteditable='plaintext-only']",
    ) as HTMLElement | null;
    expect(textbox).toBeTruthy();
    navigationState.replace.mockClear();

    fireEvent.keyDown(textbox!, { key: "j" });
    fireEvent.keyDown(textbox!, { key: "ArrowDown" });
    fireEvent.keyDown(textbox!, { key: "Escape" });

    expect(navigationState.replace).not.toHaveBeenCalled();
  });

  it("does not steal arrow or escape keys from open picker/listbox popovers", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve({
        issues: mockIssues.filter((i) => i.status === params?.status),
        total: mockIssues.filter((i) => i.status === params?.status).length,
      }),
    );
    navigationState.search = "?peek=issue-1";

    renderWithQuery(<IssuesPage />);

    await screen.findAllByText("Implement auth");
    navigationState.replace.mockClear();

    const listbox = document.createElement("div");
    listbox.setAttribute("role", "listbox");
    const option = document.createElement("button");
    listbox.appendChild(option);
    document.body.appendChild(listbox);
    try {
      fireEvent.keyDown(option, { key: "ArrowDown" });
      fireEvent.keyDown(option, { key: "Escape" });
    } finally {
      document.body.removeChild(listbox);
    }

    expect(navigationState.replace).not.toHaveBeenCalled();
  });

  it("scrolls the selected board card into the safe preview viewport", async () => {
    const scrollTo = vi.fn();
    const originalScrollTo = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "scrollTo",
    );
    const originalClientWidth = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "clientWidth",
    );
    const originalOffsetLeft = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "offsetLeft",
    );
    const originalOffsetWidth = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "offsetWidth",
    );
    const rectSpy = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function getTestRect(this: HTMLElement) {
        if (this.getAttribute("data-testid") === "issues-board-scroll-container") {
          return {
            x: 0,
            y: 0,
            width: 320,
            height: 800,
            top: 0,
            right: 320,
            bottom: 800,
            left: 0,
            toJSON: () => {},
          };
        }
        if (this.getAttribute("data-issue-card-id") === "issue-2") {
          return {
            x: 620,
            y: 0,
            width: 220,
            height: 120,
            top: 0,
            right: 840,
            bottom: 120,
            left: 620,
            toJSON: () => {},
          };
        }
        return {
          x: 0,
          y: 0,
          width: 0,
          height: 0,
          top: 0,
          right: 0,
          bottom: 0,
          left: 0,
          toJSON: () => {},
        };
      });

    Object.defineProperty(HTMLElement.prototype, "scrollTo", {
      configurable: true,
      value: scrollTo,
    });
    Object.defineProperty(HTMLElement.prototype, "clientWidth", {
      configurable: true,
      get() {
        return this.getAttribute("data-testid") === "issues-board-scroll-container"
          ? 320
          : 0;
      },
    });
    Object.defineProperty(HTMLElement.prototype, "offsetLeft", {
      configurable: true,
      get() {
        return this.getAttribute("data-issue-card-id") === "issue-2" ? 900 : 0;
      },
    });
    Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
      configurable: true,
      get() {
        return this.getAttribute("data-issue-card-id") === "issue-2" ? 220 : 0;
      },
    });

    try {
      const statuses: IssueStatus[] = [
        "backlog",
        "todo",
        "in_progress",
        "in_review",
        "done",
        "blocked",
      ];

      renderWithQuery(
        <BoardView
          issues={mockIssues}
          visibleStatuses={statuses}
          hiddenStatuses={[]}
          onMoveIssue={vi.fn()}
          selectedIssueId="issue-2"
        />,
      );

      await waitFor(() => {
        expect(scrollTo).toHaveBeenCalledWith({
          left: 850,
          behavior: "smooth",
        });
      });
    } finally {
      rectSpy.mockRestore();
      if (originalScrollTo) {
        Object.defineProperty(HTMLElement.prototype, "scrollTo", originalScrollTo);
      } else {
        delete (HTMLElement.prototype as unknown as { scrollTo?: unknown }).scrollTo;
      }
      if (originalClientWidth) {
        Object.defineProperty(HTMLElement.prototype, "clientWidth", originalClientWidth);
      }
      if (originalOffsetLeft) {
        Object.defineProperty(HTMLElement.prototype, "offsetLeft", originalOffsetLeft);
      }
      if (originalOffsetWidth) {
        Object.defineProperty(HTMLElement.prototype, "offsetWidth", originalOffsetWidth);
      }
    }
  });
});
