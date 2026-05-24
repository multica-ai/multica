import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
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

// Mock view store. `swimlaneOrder` is mutable on the captured object so
// tests can simulate persisted lane order and assert that
// `setSwimlaneOrder` was called by drag-end handlers.
const mockViewState: {
  sortBy: "position";
  sortDirection: "asc";
  cardProperties: Record<string, boolean>;
  swimlaneOrder: string[];
  collapsedSwimlanes: string[];
  setSwimlaneOrder: (order: string[]) => void;
  toggleSwimlaneCollapsed: (key: string) => void;
  hideStatus: (s: string) => void;
  showStatus: (s: string) => void;
} = {
  sortBy: "position",
  sortDirection: "asc",
  cardProperties: { priority: true, description: true, assignee: true, dueDate: true, project: true, childProgress: true, labels: true },
  swimlaneOrder: [],
  collapsedSwimlanes: [],
  setSwimlaneOrder: vi.fn(),
  toggleSwimlaneCollapsed: vi.fn(),
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
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
    // Reset mutable mock state between tests so stored lane order /
    // collapsed state from one test doesn't leak into the next.
    mockViewState.swimlaneOrder = [];
    mockViewState.collapsedSwimlanes = [];
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

    // No parent (orphan swimlane)
    expect(screen.getAllByText("No parent").length).toBeGreaterThanOrEqual(1);

    // Parent Issue 1 swimlane
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

    // Orphan Issue 1 is in "No parent" + Backlog
    expect(screen.getByText("Orphan Issue 1")).toBeInTheDocument();

    // Parent Issue 1 is in "No parent" + Todo
    expect(screen.getAllByText("Parent Issue 1").length).toBeGreaterThanOrEqual(1);

    // Child Issue 1 is in "Parent Issue 1" + In Progress
    expect(screen.getByText("Child Issue 1")).toBeInTheDocument();
  });

  it("triggers modal open when add button is clicked", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    // Find the per-cell "+" button by its aria-label (set to the
    // `board.add_issue_tooltip` translation).
    const addButtons = screen.getAllByRole("button", { name: /add issue/i });
    expect(addButtons.length).toBeGreaterThan(0);

    fireEvent.click(addButtons[0]!);
    expect(mockOpenModal).toHaveBeenCalledWith("create-issue", expect.any(Object));
  });

  it("renders an open-parent link for lanes with a real parent", () => {
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );

    // The pencil link uses aria-label "Open parent issue" and href to /issues/<id>.
    // Only the "Parent Issue 1" lane (parent-1) has a parent issue id; the
    // orphan lane ("No parent") must not render this link.
    const links = screen.getAllByRole("link", { name: "Open parent issue" });
    expect(links).toHaveLength(1);
    expect(links[0]).toHaveAttribute("href", expect.stringContaining("parent-1"));
  });

  it("renders HiddenColumnsPanel only when hiddenStatuses is non-empty", () => {
    // Case 1: no hidden statuses → panel is absent.
    const { unmount } = renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        onMoveIssue={vi.fn()}
      />,
    );
    expect(screen.queryByText("Hidden columns")).not.toBeInTheDocument();
    unmount();

    // Case 2: hide a status → panel shows the localized status name + count.
    // "cancelled" is excluded from BOARD_STATUSES, so we pass "blocked" as hidden.
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

    expect(lastOnDragOver).toBeDefined();
    expect(lastOnDragEnd).toBeDefined();

    // Drag orphan-1 (status: backlog, parent: null, in "No parent" lane)
    // to the "in_progress" column of the same "No parent" lane.
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

    expect(mockOnMoveIssue).toHaveBeenCalledWith("orphan-1", {
      parent_issue_id: null,
      status: "in_progress",
      position: 300, // maintains its position since it's the only item in target
    });
  });

  it("does not call onMoveIssue when drop target equals source cell (no-op)", () => {
    // Drop "parent-1" (status: todo, parent_issue_id: null) onto its own
    // current cell. The handler must detect the no-op and skip the mutation
    // so the network and the optimistic cache aren't churned.
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
    // Drag "orphan-1" (parent_issue_id: null, status: backlog) into the
    // "Parent Issue 1" lane's `todo` column. The contract: payload must
    // carry the new parent_issue_id (parent-1) and the new status (todo).
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
    );
  });

  it("renders count for hidden statuses based on in-memory issues", () => {
    // statusTotals must count every issue's status, including statuses that
    // are not currently rendered as columns. mockIssues has 1 `backlog` and
    // 0 `blocked`. Hide both and assert the panel shows the right counts.
    renderWithI18n(
      <SwimLaneView
        issues={mockIssues}
        visibleStatuses={["todo", "in_progress", "in_review", "done"]}
        hiddenStatuses={["backlog", "blocked"]}
        onMoveIssue={vi.fn()}
      />,
    );

    const panel = screen.getByText("Hidden columns").parentElement!.parentElement!;
    // Backlog row should show "1"; Blocked row should show "0".
    expect(panel).toHaveTextContent("Backlog");
    expect(panel).toHaveTextContent("Blocked");
    expect(panel).toHaveTextContent("1");
    expect(panel).toHaveTextContent("0");
  });

  // ------------------------------------------------------------------
  // Lane reordering via drag-and-drop
  //
  // Two parent fixtures (parent-1, parent-2) so we can test cross-lane
  // reordering.  Each needs a child issue so a swimlane is actually created
  // (parentGroups skips parents with no children loaded).
  // ------------------------------------------------------------------
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

    // Drag lane parent-1 onto lane parent-2 — expect order to become
    // [parent-2, parent-1].
    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent-1" },
        over: { id: "lane:parent-2" },
      });
    });

    expect(mockSetSwimlaneOrder).toHaveBeenCalledWith(["parent-2", "parent-1"]);
  });

  it("does not call setSwimlaneOrder when a lane is dropped onto itself", () => {
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={vi.fn()} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent-1" },
        over: { id: "lane:parent-1" },
      });
    });

    expect(mockSetSwimlaneOrder).not.toHaveBeenCalled();
  });

  it("does not call onMoveIssue when a lane drag ends (no card mutation)", () => {
    // Lane drags must not accidentally trigger card-position mutations.
    const mockOnMoveIssue = vi.fn();
    renderWithI18n(
      <SwimLaneView issues={multiParentIssues} onMoveIssue={mockOnMoveIssue} />,
    );

    act(() => {
      lastOnDragEnd({
        active: { id: "lane:parent-1" },
        over: { id: "lane:parent-2" },
      });
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });

  it("renders parent lanes in stored swimlaneOrder when set", () => {
    // Set persisted order to put parent-2 first, then parent-1.
    mockViewState.swimlaneOrder = ["parent-2", "parent-1"];

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
    // Even if the user could somehow include a synthetic "no parent" key,
    // the No-parent lane is rendered outside the SortableContext and
    // always appears first.
    mockViewState.swimlaneOrder = ["parent-2", "parent-1"];

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
    // Pre-seed the store as if the lane had been collapsed in a prior
    // session. Child cards inside that lane must not render.
    mockViewState.collapsedSwimlanes = ["parent-1"];

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
    // Sentinel key "none" represents the No-parent lane. mockIssues
    // contains the orphan "Orphan Issue 1" — when collapsed, its card
    // must be absent from the DOM.
    mockViewState.collapsedSwimlanes = ["none"];

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

    // The Parent A lane header is the <button> containing the text "Parent A".
    const parentAHeader = screen.getByText("Parent A").closest("button");
    expect(parentAHeader).not.toBeNull();
    fireEvent.click(parentAHeader!);

    expect(mockToggleSwimlaneCollapsed).toHaveBeenCalledWith("parent-1");
  });

  it("calls toggleSwimlaneCollapsed with 'none' when the No-parent lane header is clicked", () => {
    renderWithI18n(
      <SwimLaneView issues={mockIssues} onMoveIssue={vi.fn()} />,
    );

    // mockIssues' orphan card has description "No parent", so the literal
    // appears twice in the DOM (lane title + card description). The lane
    // title is the one inside a <button> — disambiguate by closest button.
    const matches = screen.getAllByText("No parent");
    const noParentHeader = matches.map((m) => m.closest("button")).find(Boolean);
    expect(noParentHeader).toBeDefined();
    fireEvent.click(noParentHeader!);

    expect(mockToggleSwimlaneCollapsed).toHaveBeenCalledWith("none");
  });
});
