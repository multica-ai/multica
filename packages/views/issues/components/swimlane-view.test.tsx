import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SwimLaneView } from "./swimlane-view";
import type { Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// Mock hooks
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Mock the API so childrenByParentsOptions doesn't fire real HTTP.
// Individual tests can override listChildrenByParents via mockResolvedValueOnce.
const mockListChildrenByParents = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ issues: [] }),
);
const mockGetAgentTaskSnapshot = vi.hoisted(() =>
  vi.fn().mockResolvedValue([]),
);
vi.mock("@multica/core/api", () => ({
  api: {
    listChildrenByParents: mockListChildrenByParents,
    getAgentTaskSnapshot: mockGetAgentTaskSnapshot,
  },
  getApi: () => ({
    listChildrenByParents: mockListChildrenByParents,
    getAgentTaskSnapshot: mockGetAgentTaskSnapshot,
  }),
  setApiInstance: vi.fn(),
}));

// Mock paths
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
    useRequiredWorkspaceSlug: () => "acme",
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

// Stub backend-bound queries that the swimlane invokes for project /
// assignee groupings. The hook MUST return a stable reference each call
// — production `useActorName` wraps its returns in `useMemo`, and the
// swimlane feeds the result into a `useMemo(..., [getActorName, ...])`
// that then drives a `useEffect(setLocalCells, [cells])` chain. A fresh
// object per render therefore loops the effect indefinitely.
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (_wsId: string) => ({
    queryKey: ["projects", _wsId, "list"],
    queryFn: () => Promise.resolve([]),
  }),
}));
const { mockActorNameResult } = vi.hoisted(() => ({
  mockActorNameResult: {
    getActorName: (_type: string, _id: string) => "Mock Actor",
    getActorInitials: () => "MA",
    getActorAvatarUrl: () => null,
    getMemberName: () => "Mock Member",
    getAgentName: () => "Mock Agent",
    getSquadName: () => "Mock Squad",
  },
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => mockActorNameResult,
}));

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

// Mock navigation
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues" }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
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

// Default mock returns hasMore=false so the load-more sentinels render
// as no-op divs and don't pull IntersectionObserver into JSDOM.
const mockLoadMore = vi.fn();
const useLoadMoreByStatusMock = vi.fn(
  (_status: string, _opts?: unknown, _sort?: unknown) => ({
    total: 0,
    loaded: 0,
    hasMore: false,
    isLoading: false,
    loadMore: mockLoadMore,
  }),
);
vi.mock("@multica/core/issues/mutations", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/issues/mutations")>();
  return {
    ...actual,
    useLoadMoreByStatus: (status: string, opts?: unknown, sort?: unknown) =>
      useLoadMoreByStatusMock(status, opts, sort),
  };
});

type SwimlaneGroupingMock = "parent" | "project" | "assignee";

// Mock view store. The lane order and collapsed-lane fields are mutable
// records on the captured object so tests can simulate persisted state
// (per grouping) and assert that `setSwimlaneOrder` was called by drag-end
// handlers. The store actions operate on `swimlaneGrouping` — tests that
// flip grouping must set both `swimlaneGrouping` and the matching slice
// in `swimlaneOrders` / `collapsedSwimlanes`.
const mockViewState: {
  sortBy: "position";
  sortDirection: "asc";
  cardProperties: Record<string, boolean>;
  swimlaneGrouping: SwimlaneGroupingMock;
  swimlaneOrders: Record<SwimlaneGroupingMock, string[]>;
  collapsedSwimlanes: Record<SwimlaneGroupingMock, string[]>;
  setSwimlaneGrouping: (g: SwimlaneGroupingMock) => void;
  setSwimlaneOrder: (order: string[]) => void;
  toggleSwimlaneCollapsed: (key: string) => void;
  hideStatus: (s: string) => void;
  showStatus: (s: string) => void;
  priorityFilters?: string[];
  assigneeFilters?: any[];
  includeNoAssignee?: boolean;
  creatorFilters?: any[];
  projectFilters?: string[];
  includeNoProject?: boolean;
  labelFilters?: string[];
  agentRunningFilter?: boolean;
} = {
  sortBy: "position",
  sortDirection: "asc",
  cardProperties: { priority: true, description: true, assignee: true, dueDate: true, project: true, childProgress: true, labels: true },
  swimlaneGrouping: "parent",
  swimlaneOrders: { parent: [], project: [], assignee: [] },
  collapsedSwimlanes: { parent: [], project: [], assignee: [] },
  setSwimlaneGrouping: vi.fn(),
  setSwimlaneOrder: vi.fn(),
  toggleSwimlaneCollapsed: vi.fn(),
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  agentRunningFilter: false,
};
const mockSetSwimlaneOrder = mockViewState.setSwimlaneOrder as ReturnType<typeof vi.fn>;
const mockToggleSwimlaneCollapsed = mockViewState.toggleSwimlaneCollapsed as ReturnType<typeof vi.fn>;

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({ getState: () => mockViewState, setState: vi.fn(), subscribe: vi.fn() }),
}));

// Mock modal store
const mockOpenModal = vi.fn();
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: mockOpenModal }),
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

// Mock dnd-kit
let lastOnDragEnd: any = null;
let lastOnDragOver: any = null;

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children, onDragEnd, onDragOver }: any) => {
    lastOnDragEnd = onDragEnd;
    lastOnDragOver = onDragOver;
    return children;
  },
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
  // Real arrayMove implementation — the production code uses this both for
  // card reordering and lane reordering, so returning undefined would break
  // every reorder assertion.
  arrayMove: <T,>(arr: T[], from: number, to: number): T[] => {
    const copy = arr.slice();
    const [item] = copy.splice(from, 1);
    copy.splice(to, 0, item!);
    return copy;
  },
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

const mockIssues: Issue[] = [
  {
    id: "parent-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "PROJ-1",
    title: "Parent Issue 1",
    description: "Parent description",
    status: "todo",
    priority: "high",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 100,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "child-1",
    workspace_id: "ws-1",
    number: 2,
    identifier: "PROJ-2",
    title: "Child Issue 1",
    description: "Child description",
    status: "in_progress",
    priority: "medium",
    assignee_type: "member",
    assignee_id: "user-1",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: "parent-1",
    project_id: null,
    position: 200,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "orphan-1",
    workspace_id: "ws-1",
    number: 3,
    identifier: "PROJ-3",
    title: "Orphan Issue 1",
    description: "No parent",
    status: "backlog",
    priority: "low",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 300,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
];

function renderWithI18n(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        {ui}
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("SwimLaneView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockViewState.swimlaneGrouping = "parent";
    mockViewState.swimlaneOrders = { parent: [], project: [], assignee: [] };
    mockViewState.collapsedSwimlanes = { parent: [], project: [], assignee: [] };
    mockViewState.priorityFilters = [];
    mockViewState.assigneeFilters = [];
    mockViewState.includeNoAssignee = false;
    mockViewState.creatorFilters = [];
    mockViewState.projectFilters = [];
    mockViewState.includeNoProject = false;
    mockViewState.labelFilters = [];
    mockViewState.agentRunningFilter = false;
    mockListChildrenByParents.mockResolvedValue({ issues: [] });
    mockGetAgentTaskSnapshot.mockResolvedValue([]);
    useLoadMoreByStatusMock.mockImplementation(() => ({
      total: 0,
      loaded: 0,
      hasMore: false,
      isLoading: false,
      loadMore: mockLoadMore,
    }));
  });

  it("renders status columns as headers", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    expect(screen.getByText("Backlog")).toBeInTheDocument();
    expect(screen.getByText("Todo")).toBeInTheDocument();
    expect(screen.getByText("In Progress")).toBeInTheDocument();
  });

  it("renders parent swimlanes and orphans section", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    expect(screen.getAllByText("No parent").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Parent Issue 1").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("PROJ-1").length).toBeGreaterThanOrEqual(1);
  });

  it("renders cards in their corresponding cell", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    expect(screen.getByText("Orphan Issue 1")).toBeInTheDocument();

    // parent-1 is promoted to a lane header — must not also appear as a card.
    const parentTitleMatches = screen.getAllByText("Parent Issue 1");
    expect(parentTitleMatches).toHaveLength(1);
    expect(parentTitleMatches[0]!.closest("div")?.textContent).toContain("PROJ-1");

    expect(screen.getByText("Child Issue 1")).toBeInTheDocument();
  });

  it("triggers modal open when add button is clicked", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    const addButtons = screen.getAllByRole("button", { name: /add issue/i });
    expect(addButtons.length).toBeGreaterThan(0);

    fireEvent.click(addButtons[0]!);
    expect(mockOpenModal).toHaveBeenCalledWith("create-issue", expect.any(Object));
  });

  it("includes project_id in the create payload when projectId prop is set", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
        projectId="proj-42"
      />,
    );

    const addButtons = screen.getAllByRole("button", { name: /add issue/i });
    fireEvent.click(addButtons[0]!);

    expect(mockOpenModal).toHaveBeenCalledWith(
      "create-issue",
      expect.objectContaining({ project_id: "proj-42" }),
    );
  });

  // A child whose parent isn't in the loaded set — lands in "Other parents".
  const orphanChild: Issue = {
    id: "lonely-child",
    workspace_id: "ws-1",
    number: 99,
    identifier: "PROJ-99",
    title: "Lonely Child",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: "member",
    assignee_id: "user-1",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: "missing-parent",
    project_id: null,
    position: 400,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };

  it("renders children whose parent is not in the loaded set under an 'Other parents' fallback lane", () => {
    renderWithI18n(
      <SwimLaneView issues={[orphanChild]} onMoveIssue={vi.fn()} />,
    );

    expect(screen.getByText("Other parents")).toBeInTheDocument();
    expect(screen.getByText("Lonely Child")).toBeInTheDocument();
  });

  it("does not render the add-issue button inside 'Other parents' cells", () => {
    renderWithI18n(
      <SwimLaneView
        issues={[...mockIssues, orphanChild]}
        onMoveIssue={vi.fn()}
      />,
    );

    // No parent + Parent Issue 1 each have one + per visible status column.
    // The Other parents lane must add zero.
    const realLaneCount = 2;
    const visibleStatusCount = 6; // BOARD_STATUSES default
    expect(
      screen.getAllByRole("button", { name: /add issue/i }).length,
    ).toBe(realLaneCount * visibleStatusCount);
  });

  it("does not call onMoveIssue when a card is dropped onto the empty whitespace of an 'Other parents' cell", () => {
    // `over.id` is the orphan cell id — what dnd-kit emits when the
    // pointer lands on the cell's empty area.
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView
        issues={[...mockIssues, orphanChild]}
        onMoveIssue={mockOnMoveIssue}
      />,
    );

    act(() => {
      lastOnDragOver({
        active: { id: "child-1" },
        over: { id: "swim:parent:__orphans__:todo" },
      });
    });
    act(() => {
      lastOnDragEnd({
        active: { id: "child-1" },
        over: { id: "swim:parent:__orphans__:todo" },
      });
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });

  it("hides a parent lane when all its children are in hidden statuses", () => {
    // child-1 (in_progress) is not in the visible set — parent-1's lane
    // is dropped. parent-1 itself falls into the No-parent lane as a card.
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues.filter((i) => i.status === "todo")}
        unfilteredIssues={mockIssues}
        visibleStatuses={["todo"]}
        hiddenStatuses={["backlog", "in_progress", "in_review", "done", "blocked"]}
        onMoveIssue={vi.fn()}
      />,
    );

    expect(screen.queryByText("Child Issue 1")).not.toBeInTheDocument();
    expect(screen.getAllByText("Parent Issue 1")).toHaveLength(1);
  });

  it("does not call onMoveIssue when a card is dragged out of 'Other parents'", () => {
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView
        issues={[...mockIssues, orphanChild]}
        onMoveIssue={mockOnMoveIssue}
      />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lonely-child" },
        over: { id: "swim:parent:parent-1:in_progress" },
      });
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });

  it("renders an open-parent link for lanes with a real parent", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    const links = screen.getAllByRole("link", { name: "Open parent issue" });
    expect(links).toHaveLength(1);
    expect(links[0]).toHaveAttribute("href", expect.stringContaining("parent-1"));
  });

  it("renders HiddenColumnsPanel only when hiddenStatuses is non-empty", () => {
    const { unmount } = renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );
    expect(screen.queryByText("Hidden columns")).not.toBeInTheDocument();
    unmount();

    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        visibleStatuses={["backlog", "todo", "in_progress", "in_review", "done"]}
        hiddenStatuses={["blocked"]}
        onMoveIssue={vi.fn()}
      />,
    );
    expect(screen.getByText("Hidden columns")).toBeInTheDocument();
    expect(screen.getByText("Blocked")).toBeInTheDocument();
  });

  it("calls onMoveIssue on drag-and-drop end", () => {
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={mockOnMoveIssue}
      />,
    );

    const targetCellId = "swim:parent:none:in_progress";

    act(() => {
      lastOnDragOver({
        active: { id: "orphan-1" },
        over: { id: targetCellId },
      });
    });

    act(() => {
      lastOnDragEnd({
        active: { id: "orphan-1" },
        over: { id: targetCellId },
      });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "orphan-1",
      { parent_issue_id: null, status: "in_progress", position: 300 },
      expect.any(Function),
    );
  });

  it("passes a settle callback that releases the lock without error", () => {
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    const targetCellId = "swim:parent:none:in_progress";
    act(() => {
      lastOnDragOver({ active: { id: "orphan-1" }, over: { id: targetCellId } });
    });
    act(() => {
      lastOnDragEnd({ active: { id: "orphan-1" }, over: { id: targetCellId } });
    });

    // The move carries a settle callback (held from drop until the mutation
    // settles); invoking it releases the lock and re-syncs from the cache.
    const onSettled = mockOnMoveIssue.mock.calls[0]?.[2] as
      | (() => void)
      | undefined;
    expect(typeof onSettled).toBe("function");
    expect(() => act(() => onSettled?.())).not.toThrow();
  });

  it("does not call onMoveIssue when drop target equals source cell (no-op)", () => {
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "parent-1" },
        over: { id: "swim:parent:none:todo" },
      });
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });

  it("emits parent_issue_id when dragging from orphan into a parent lane", () => {
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    const target = "swim:parent:parent-1:todo";
    act(() => {
      lastOnDragOver({
        active: { id: "orphan-1" },
        over: { id: target },
      });
    });
    act(() => {
      lastOnDragEnd({
        active: { id: "orphan-1" },
        over: { id: target },
      });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "orphan-1",
      expect.objectContaining({
        parent_issue_id: "parent-1",
        status: "todo",
      }),
      expect.any(Function),
    );
  });

  it("renders count for hidden statuses from in-memory statusTotals", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        visibleStatuses={["todo", "in_progress", "in_review", "done"]}
        hiddenStatuses={["backlog", "blocked"]}
        onMoveIssue={vi.fn()}
      />,
    );

    const panel = screen.getByText("Hidden columns").parentElement!.parentElement!;
    expect(panel).toHaveTextContent("Backlog");
    expect(panel).toHaveTextContent("Blocked");
    expect(panel).toHaveTextContent("1");
    expect(panel).toHaveTextContent("0");
  });

  it("hidden-column totals come from unfilteredIssues when provided", () => {
    const unfiltered: Issue[] = [
      ...mockIssues,
      {
        ...mockIssues[2]!,
        id: "blocked-1",
        identifier: "PROJ-99",
        title: "Blocked Issue",
        status: "blocked",
        position: 500,
      },
    ];

    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        unfilteredIssues={unfiltered}
        visibleStatuses={["todo", "in_progress", "in_review", "done"]}
        hiddenStatuses={["backlog", "blocked"]}
        onMoveIssue={vi.fn()}
      />,
    );

    const panel = screen.getByText("Hidden columns").parentElement!.parentElement!;
    expect(panel).toHaveTextContent("Backlog");
    expect(panel).toHaveTextContent("Blocked");
    const counts = [...panel.querySelectorAll("span")].map((el) => el.textContent);
    expect(counts.filter((c) => c === "1").length).toBeGreaterThanOrEqual(2);
  });

  const multiParentIssues: Issue[] = [
    {
      id: "parent-1",
      workspace_id: "ws-1",
      number: 1,
      identifier: "PROJ-1",
      title: "Parent A",
      description: null,
      status: "todo",
      priority: "high",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 100,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "parent-2",
      workspace_id: "ws-1",
      number: 2,
      identifier: "PROJ-10",
      title: "Parent B",
      description: null,
      status: "todo",
      priority: "high",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 200,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "child-of-1",
      workspace_id: "ws-1",
      number: 3,
      identifier: "PROJ-2",
      title: "Child of A",
      description: null,
      status: "in_progress",
      priority: "medium",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: "parent-1",
      project_id: null,
      position: 300,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "child-of-2",
      workspace_id: "ws-1",
      number: 4,
      identifier: "PROJ-11",
      title: "Child of B",
      description: null,
      status: "in_progress",
      priority: "medium",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: "parent-2",
      project_id: null,
      position: 400,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ];

  it("persists lane order via setSwimlaneOrder when a lane is dragged onto another", () => {
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent:parent-1" },
        over: { id: "lane:parent:parent-2" },
      });
    });

    expect(mockSetSwimlaneOrder).toHaveBeenCalledWith(["parent-2", "parent-1"]);
  });

  it("appends newly-visible parents to the persisted order on first reorder", () => {
    mockViewState.swimlaneOrders = {
      ...mockViewState.swimlaneOrders,
      parent: ["parent-1"],
    };

    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent:parent-1" },
        over: { id: "lane:parent:parent-2" },
      });
    });

    expect(mockSetSwimlaneOrder).toHaveBeenCalledWith(["parent-2", "parent-1"]);
  });

  it("preserves persisted entries that aren't currently visible during a reorder", () => {
    mockViewState.swimlaneOrders = {
      ...mockViewState.swimlaneOrders,
      parent: ["filtered-a", "parent-1", "filtered-b", "parent-2"],
    };

    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent:parent-1" },
        over: { id: "lane:parent:parent-2" },
      });
    });

    expect(mockSetSwimlaneOrder).toHaveBeenCalledWith([
      "filtered-a",
      "parent-2",
      "filtered-b",
      "parent-1",
    ]);
  });

  it("does not call setSwimlaneOrder when a lane is dropped onto itself", () => {
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent:parent-1" },
        over: { id: "lane:parent:parent-1" },
      });
    });

    expect(mockSetSwimlaneOrder).not.toHaveBeenCalled();
  });

  it("does not call onMoveIssue when a lane drag ends (no card mutation)", () => {
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent:parent-1" },
        over: { id: "lane:parent:parent-2" },
      });
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });

  it("renders parent lanes in stored swimlaneOrder when set", () => {
    mockViewState.swimlaneOrders = {
      ...mockViewState.swimlaneOrders,
      parent: ["parent-2", "parent-1"],
    };

    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    const parentA = screen.getByText("Parent A");
    const parentB = screen.getByText("Parent B");
    // DOM order: "Parent B" must precede "Parent A".
    // compareDocumentPosition: bitmask, DOCUMENT_POSITION_FOLLOWING = 4
    expect(parentB.compareDocumentPosition(parentA) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("keeps 'No parent' lane pinned at top regardless of stored order", () => {
    mockViewState.swimlaneOrders = {
      ...mockViewState.swimlaneOrders,
      parent: ["parent-2", "parent-1"],
    };

    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    const noParent = screen.getByText("No parent");
    const parentB = screen.getByText("Parent B");
    expect(noParent.compareDocumentPosition(parentB) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  // ------------------------------------------------------------------
  // Persisted collapsed-lane state
  // ------------------------------------------------------------------

  it("collapses a parent lane when its id is in stored collapsedSwimlanes", () => {
    mockViewState.collapsedSwimlanes = {
      ...mockViewState.collapsedSwimlanes,
      parent: ["parent-1"],
    };

    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    // The lane HEADER for Parent A is still visible…
    expect(screen.getByText("Parent A")).toBeInTheDocument();
    // …but its child card is not.
    expect(screen.queryByText("Child of A")).not.toBeInTheDocument();
    // Parent B (not collapsed) still shows its child.
    expect(screen.getByText("Child of B")).toBeInTheDocument();
  });

  it("collapses the 'No parent' lane when 'none' is in stored collapsedSwimlanes", () => {
    mockViewState.collapsedSwimlanes = {
      ...mockViewState.collapsedSwimlanes,
      parent: ["none"],
    };

    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={vi.fn()} />,
    );

    expect(screen.getByText("No parent")).toBeInTheDocument();
    expect(screen.queryByText("Orphan Issue 1")).not.toBeInTheDocument();
  });

  it("calls toggleSwimlaneCollapsed with the raw parent id when a lane header is clicked", () => {
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    const parentAHeader = screen.getByText("Parent A").closest("button");
    expect(parentAHeader).not.toBeNull();
    fireEvent.click(parentAHeader!);

    expect(mockToggleSwimlaneCollapsed).toHaveBeenCalledWith("parent-1");
  });

  it("calls toggleSwimlaneCollapsed with 'none' when the No-parent lane header is clicked", () => {
    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={vi.fn()} />,
    );

    // "No parent" appears twice (lane title + orphan-1's card description);
    // find the one inside a button.
    const matches = screen.getAllByText("No parent");
    const noParentHeader = matches.map((m) => m.closest("button")).find(Boolean);
    expect(noParentHeader).toBeDefined();
    fireEvent.click(noParentHeader!);

    expect(mockToggleSwimlaneCollapsed).toHaveBeenCalledWith("none");
  });

  // ------------------------------------------------------------------
  // Project grouping
  // ------------------------------------------------------------------

  const projectIssues: Issue[] = [
    {
      ...mockIssues[0]!,
      id: "issue-a",
      identifier: "PROJ-100",
      title: "Issue A",
      project_id: "proj-1",
      parent_issue_id: null,
      status: "todo",
    },
    {
      ...mockIssues[0]!,
      id: "issue-b",
      identifier: "PROJ-101",
      title: "Issue B",
      project_id: "proj-2",
      parent_issue_id: null,
      status: "in_progress",
    },
    {
      ...mockIssues[0]!,
      id: "issue-c",
      identifier: "PROJ-102",
      title: "Issue C",
      project_id: null,
      parent_issue_id: null,
      status: "todo",
    },
  ];

  it("groups by project when swimlaneGrouping is 'project'", () => {
    mockViewState.swimlaneGrouping = "project";

    renderWithI18n(
      <SwimLaneView issues={projectIssues} onMoveIssue={vi.fn()} />,
    );

    // No-project pinned lane is always present.
    expect(screen.getAllByText("No project").length).toBeGreaterThanOrEqual(1);
    // Both issue cards from real projects render — production fetches
    // project titles from the API; in tests the mocked listProjects
    // returns [] so the lane headers fall back to an empty title and
    // we assert on card visibility, not lane title text.
    expect(screen.getByText("Issue A")).toBeInTheDocument();
    expect(screen.getByText("Issue B")).toBeInTheDocument();
    expect(screen.getByText("Issue C")).toBeInTheDocument();
  });

  it("emits project_id when a card is dropped into a project lane", () => {
    mockViewState.swimlaneGrouping = "project";
    const mockOnMoveIssue = vi.fn();

    renderWithI18n(
      <SwimLaneView issues={projectIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    // Drop "issue-c" (no project) into proj-1's todo cell.
    const target = "swim:project:proj-1:todo";
    act(() => {
      lastOnDragOver({ active: { id: "issue-c" }, over: { id: target } });
    });
    act(() => {
      lastOnDragEnd({ active: { id: "issue-c" }, over: { id: target } });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "issue-c",
      expect.objectContaining({ project_id: "proj-1", status: "todo" }),
      expect.any(Function),
    );
  });

  it("emits null project_id when a card is dropped into the 'No project' lane", () => {
    mockViewState.swimlaneGrouping = "project";
    const mockOnMoveIssue = vi.fn();

    renderWithI18n(
      <SwimLaneView issues={projectIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    const target = "swim:project:none:in_review";
    act(() => {
      lastOnDragOver({ active: { id: "issue-a" }, over: { id: target } });
    });
    act(() => {
      lastOnDragEnd({ active: { id: "issue-a" }, over: { id: target } });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "issue-a",
      expect.objectContaining({ project_id: null, status: "in_review" }),
      expect.any(Function),
    );
  });

  // ------------------------------------------------------------------
  // Assignee grouping
  // ------------------------------------------------------------------

  const assigneeIssues: Issue[] = [
    {
      ...mockIssues[0]!,
      id: "issue-x",
      identifier: "PROJ-200",
      title: "Issue X",
      assignee_type: "member",
      assignee_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      status: "todo",
    },
    {
      ...mockIssues[0]!,
      id: "issue-y",
      identifier: "PROJ-201",
      title: "Issue Y",
      assignee_type: "agent",
      assignee_id: "agent-1",
      parent_issue_id: null,
      project_id: null,
      status: "in_progress",
    },
    {
      ...mockIssues[0]!,
      id: "issue-z",
      identifier: "PROJ-202",
      title: "Issue Z",
      assignee_type: null,
      assignee_id: null,
      parent_issue_id: null,
      project_id: null,
      status: "todo",
    },
  ];

  it("groups by assignee when swimlaneGrouping is 'assignee'", () => {
    mockViewState.swimlaneGrouping = "assignee";

    renderWithI18n(
      <SwimLaneView issues={assigneeIssues} onMoveIssue={vi.fn()} />,
    );

    // Unassigned pinned lane is always rendered.
    expect(screen.getAllByText("Unassigned").length).toBeGreaterThanOrEqual(1);
    // Mock actor name fallback for both member and agent.
    expect(screen.getAllByText("Mock Actor").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("Issue X")).toBeInTheDocument();
    expect(screen.getByText("Issue Y")).toBeInTheDocument();
    expect(screen.getByText("Issue Z")).toBeInTheDocument();
  });

  it("emits assignee_type + assignee_id when a card is dropped into an actor lane", () => {
    mockViewState.swimlaneGrouping = "assignee";
    const mockOnMoveIssue = vi.fn();

    renderWithI18n(
      <SwimLaneView issues={assigneeIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    const target = "swim:assignee:member:user-1:in_review";
    act(() => {
      lastOnDragOver({ active: { id: "issue-z" }, over: { id: target } });
    });
    act(() => {
      lastOnDragEnd({ active: { id: "issue-z" }, over: { id: target } });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "issue-z",
      expect.objectContaining({
        assignee_type: "member",
        assignee_id: "user-1",
        status: "in_review",
      }),
      expect.any(Function),
    );
  });

  it("emits null assignee when a card is dropped into the 'Unassigned' lane", () => {
    mockViewState.swimlaneGrouping = "assignee";
    const mockOnMoveIssue = vi.fn();

    renderWithI18n(
      <SwimLaneView issues={assigneeIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    const target = "swim:assignee:none:done";
    act(() => {
      lastOnDragOver({ active: { id: "issue-x" }, over: { id: target } });
    });
    act(() => {
      lastOnDragEnd({ active: { id: "issue-x" }, over: { id: target } });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "issue-x",
      expect.objectContaining({
        assignee_type: null,
        assignee_id: null,
        status: "done",
      }),
      expect.any(Function),
    );
  });

  // ------------------------------------------------------------------
  // Batched children fetch (childrenByParentsOptions)
  // ------------------------------------------------------------------

  it("fires listChildrenByParents once with all visible parent ids on mount", async () => {
    // multiParentIssues has parent-1 (Child of A) and parent-2 (Child of B) as
    // visible parent lanes. Both ids should appear in one batched call.
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    await waitFor(() => {
      expect(mockListChildrenByParents).toHaveBeenCalledTimes(1);
    });
    const [calledIds] = mockListChildrenByParents.mock.calls[0] as [string[]];
    expect(calledIds.sort()).toEqual(["parent-1", "parent-2"].sort());
  });

  it("does not fire listChildrenByParents when there are no parent lanes", async () => {
    // All issues are top-level — no parent lanes, no batch request.
    const flatIssues = mockIssues.filter((i) => i.parent_issue_id === null);
    renderWithI18n(
      <SwimLaneView issues={flatIssues} onMoveIssue={vi.fn()} />,
    );

    await act(async () => {});
    expect(mockListChildrenByParents).not.toHaveBeenCalled();
  });

  it("merges batch-fetched children into parent lanes so previously-empty cells populate", async () => {
    // Scenario: grandparent G → parent P (loaded, becomes a lane header) →
    // grandchild GC (NOT in the initial `issues` set, returned only by the
    // batch fetch). P's lane should show GC after the batch resolves.
    //
    // For the batch to include P.id, the caller must pass childProgressMap
    // signaling that P has children — without it, batchParentIds only sees
    // GP.id (from parent.parent_issue_id) and GC is never fetched.
    const grandparent: Issue = {
      id: "gp-1",
      workspace_id: "ws-1",
      number: 10,
      identifier: "PROJ-10",
      title: "Grandparent",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 10,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const parent: Issue = {
      ...grandparent,
      id: "p-1",
      number: 11,
      identifier: "PROJ-11",
      title: "Parent",
      parent_issue_id: "gp-1",
      position: 11,
    };
    const grandchild: Issue = {
      ...grandparent,
      id: "gc-1",
      number: 12,
      identifier: "PROJ-12",
      title: "Grandchild (batch only)",
      status: "in_progress",
      parent_issue_id: "p-1",
      position: 12,
    };

    mockListChildrenByParents.mockResolvedValueOnce({ issues: [grandchild] });
    const childProgressMap = new Map<string, { done: number; total: number }>([
      ["p-1", { done: 0, total: 1 }],
    ]);

    renderWithI18n(
      <SwimLaneView
        issues={[grandparent, parent]}
        childProgressMap={childProgressMap}
        onMoveIssue={vi.fn()}
      />,
    );

    // Assert the batch request actually included p-1 — without this the
    // mock would happily return GC for any request and the merge would
    // appear to work without exercising the real path.
    await waitFor(() => {
      expect(mockListChildrenByParents).toHaveBeenCalled();
    });
    const [calledIds] = mockListChildrenByParents.mock.calls[0] as [string[]];
    expect(calledIds).toEqual(expect.arrayContaining(["p-1"]));

    await waitFor(() => {
      expect(screen.getByText("Grandchild (batch only)")).toBeInTheDocument();
    });
  });

  it("includes visible parents with children (via childProgressMap) in the batch request", async () => {
    // Even without any loaded child pointing at a parent, if childProgressMap
    // says the parent has children we should query it so deep-nested
    // grandchildren are discoverable.
    const parentWithUnloadedChildren: Issue = {
      id: "p-only",
      workspace_id: "ws-1",
      number: 50,
      identifier: "PROJ-50",
      title: "Standalone parent",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 50,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const childProgressMap = new Map<string, { done: number; total: number }>([
      ["p-only", { done: 0, total: 3 }],
    ]);

    renderWithI18n(
      <SwimLaneView
        issues={[parentWithUnloadedChildren]}
        childProgressMap={childProgressMap}
        onMoveIssue={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(mockListChildrenByParents).toHaveBeenCalled();
    });
    const [calledIds] = mockListChildrenByParents.mock.calls[0] as [string[]];
    expect(calledIds).toEqual(expect.arrayContaining(["p-only"]));
  });

  it("does not fire listChildrenByParents when swimlaneGrouping is not parent", async () => {
    mockViewState.swimlaneGrouping = "project";

    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    await act(async () => {});
    expect(mockListChildrenByParents).not.toHaveBeenCalled();
  });

  it("does not call onMoveIssue when dropping a card into a lane whose header is that card", () => {
    // parent-1 (a lane-header card in the No-parent lane) dropped onto a
    // cell inside its own lane (`swim:parent:parent-1:in_progress`) would
    // be a self-cycle. The client guard refuses before reaching the API.
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    act(() => {
      lastOnDragOver({
        active: { id: "parent-1" },
        over: { id: "swim:parent:parent-1:in_progress" },
      });
    });
    act(() => {
      lastOnDragEnd({
        active: { id: "parent-1" },
        over: { id: "swim:parent:parent-1:in_progress" },
      });
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });

  it("filters batch-fetched children using active filters", async () => {
    mockViewState.swimlaneGrouping = "parent";

    const grandparent: Issue = {
      id: "gp-2",
      workspace_id: "ws-1",
      number: 20,
      identifier: "PROJ-20",
      title: "Grandparent 2",
      description: null,
      status: "todo",
      priority: "high",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 10,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const parent: Issue = {
      ...grandparent,
      id: "p-2",
      number: 21,
      identifier: "PROJ-21",
      title: "Parent 2",
      parent_issue_id: "gp-2",
      position: 11,
    };
    const matchingGrandchild: Issue = {
      ...grandparent,
      id: "gc-matching",
      number: 22,
      identifier: "PROJ-22",
      title: "Matching Child (High Priority)",
      status: "in_progress",
      priority: "high",
      parent_issue_id: "p-2",
      position: 12,
    };
    const nonMatchingGrandchild: Issue = {
      ...grandparent,
      id: "gc-non-matching",
      number: 23,
      identifier: "PROJ-23",
      title: "Non-matching Child (Low Priority)",
      status: "in_progress",
      priority: "low",
      parent_issue_id: "p-2",
      position: 13,
    };

    mockListChildrenByParents.mockResolvedValueOnce({
      issues: [matchingGrandchild, nonMatchingGrandchild],
    });

    const childProgressMap = new Map<string, { done: number; total: number }>([
      ["p-2", { done: 0, total: 2 }],
    ]);

    renderWithI18n(
      <SwimLaneView
        issues={[grandparent, parent]}
        activeFilters={{
          priorityFilters: ["high"],
          assigneeFilters: [],
          includeNoAssignee: false,
          creatorFilters: [],
          projectFilters: [],
          includeNoProject: false,
          labelFilters: [],
          agentRunningFilter: false,
        }}
        childProgressMap={childProgressMap}
        onMoveIssue={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(mockListChildrenByParents).toHaveBeenCalled();
    });

    await waitFor(() => {
      expect(screen.getByText("Matching Child (High Priority)")).toBeInTheDocument();
      expect(screen.queryByText("Non-matching Child (Low Priority)")).toBeNull();
    });
  });

  it("filters batch-fetched children using working filter", async () => {
    mockViewState.swimlaneGrouping = "parent";

    const grandparent: Issue = {
      id: "gp-3",
      workspace_id: "ws-1",
      number: 30,
      identifier: "PROJ-30",
      title: "Grandparent 3",
      description: null,
      status: "todo",
      priority: "medium",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 10,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const parent: Issue = {
      ...grandparent,
      id: "p-3",
      number: 31,
      identifier: "PROJ-31",
      title: "Parent 3",
      parent_issue_id: "gp-3",
      position: 11,
    };
    const runningGrandchild: Issue = {
      ...grandparent,
      id: "gc-running",
      number: 32,
      identifier: "PROJ-32",
      title: "Running Child",
      status: "in_progress",
      parent_issue_id: "p-3",
      position: 12,
    };
    const nonRunningGrandchild: Issue = {
      ...grandparent,
      id: "gc-non-running",
      number: 33,
      identifier: "PROJ-33",
      title: "Non-running Child",
      status: "in_progress",
      parent_issue_id: "p-3",
      position: 13,
    };

    mockGetAgentTaskSnapshot.mockResolvedValueOnce([
      { id: "task-1", status: "running", issue_id: "gc-running" },
    ]);

    mockListChildrenByParents.mockResolvedValueOnce({
      issues: [runningGrandchild, nonRunningGrandchild],
    });

    const childProgressMap = new Map<string, { done: number; total: number }>([
      ["p-3", { done: 0, total: 2 }],
    ]);

    renderWithI18n(
      <SwimLaneView
        issues={[grandparent, parent]}
        activeFilters={{
          priorityFilters: [],
          assigneeFilters: [],
          includeNoAssignee: false,
          creatorFilters: [],
          projectFilters: [],
          includeNoProject: false,
          labelFilters: [],
          agentRunningFilter: true,
        }}
        childProgressMap={childProgressMap}
        onMoveIssue={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(mockListChildrenByParents).toHaveBeenCalled();
    });

    await waitFor(() => {
      expect(screen.getByText("Running Child")).toBeInTheDocument();
      expect(screen.queryByText("Non-running Child")).toBeNull();
    });
  });

  it("hides batch-fetched children when 'Show sub-issues' is off", async () => {
    mockViewState.swimlaneGrouping = "parent";

    const grandparent: Issue = {
      id: "gp-4",
      workspace_id: "ws-1",
      number: 40,
      identifier: "PROJ-40",
      title: "Grandparent 4",
      description: null,
      status: "todo",
      priority: "medium",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 10,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const parent: Issue = {
      ...grandparent,
      id: "p-4",
      number: 41,
      identifier: "PROJ-41",
      title: "Parent 4",
      parent_issue_id: "gp-4",
      position: 11,
    };
    // Returned only by the batch fetch (not in the initial `issues` set). It is
    // a sub-issue, so with showSubIssues off it must not be merged back in.
    const batchOnlyChild: Issue = {
      ...grandparent,
      id: "gc-hidden",
      number: 42,
      identifier: "PROJ-42",
      title: "Batch Sub-issue",
      status: "in_progress",
      parent_issue_id: "p-4",
      position: 12,
    };

    mockListChildrenByParents.mockResolvedValueOnce({ issues: [batchOnlyChild] });

    const childProgressMap = new Map<string, { done: number; total: number }>([
      ["p-4", { done: 0, total: 1 }],
    ]);

    renderWithI18n(
      <SwimLaneView
        issues={[grandparent, parent]}
        activeFilters={{
          priorityFilters: [],
          assigneeFilters: [],
          includeNoAssignee: false,
          creatorFilters: [],
          projectFilters: [],
          includeNoProject: false,
          labelFilters: [],
          agentRunningFilter: false,
          showSubIssues: false,
        }}
        childProgressMap={childProgressMap}
        onMoveIssue={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(mockListChildrenByParents).toHaveBeenCalled();
    });

    // Guard against a false pass: the batch request must have targeted p-4.
    const [calledIds] = mockListChildrenByParents.mock.calls[0] as [string[]];
    expect(calledIds).toEqual(expect.arrayContaining(["p-4"]));

    // Give the merge effect a chance to run, then assert the sub-issue stays hidden.
    await act(async () => {});
    expect(screen.queryByText("Batch Sub-issue")).toBeNull();
  });
});
