import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Mocks (mirrors the pattern in issues-page.test.tsx — see that file's
// header for the rationale behind each block; copied here so the test is
// runnable in isolation.)
// ---------------------------------------------------------------------------

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => {
  const state = {
    user: { id: "user-1", email: "t@t.com", name: "T" },
    isAuthenticated: true,
  };
  return {
    useAuthStore: Object.assign(
      (selector?: any) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
    registerAuthStore: vi.fn(),
    createAuthStore: vi.fn(),
  };
});

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...rest }: any) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues" }),
  NavigationProvider: ({ children }: any) => children,
}));

const mockListIssues = vi.hoisted(() => vi.fn().mockResolvedValue({ issues: [], total: 0 }));
vi.mock("@multica/core/api", () => ({
  api: {
    listIssues: (...a: any[]) => mockListIssues(...a),
    listMembers: () => Promise.resolve([]),
    listAgents: () => Promise.resolve([]),
    listProjects: () => Promise.resolve({ projects: [], total: 0 }),
    getChildIssueProgress: () => Promise.resolve({ progress: [] }),
  },
  getApi: () => ({
    listIssues: (...a: any[]) => mockListIssues(...a),
    listMembers: () => Promise.resolve([]),
    listAgents: () => Promise.resolve([]),
    listProjects: () => Promise.resolve({ projects: [], total: 0 }),
    getChildIssueProgress: () => Promise.resolve({ progress: [] }),
  }),
  setApiInstance: vi.fn(),
}));

vi.mock("@multica/core/issues/config", () => ({
  ALL_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  BOARD_STATUSES: ["backlog", "todo", "in_progress", "in_review", "done", "blocked"],
  STATUS_CONFIG: {
    backlog: { label: "Backlog", iconColor: "", hoverBg: "" },
    todo: { label: "Todo", iconColor: "", hoverBg: "" },
    in_progress: { label: "In Progress", iconColor: "", hoverBg: "" },
    in_review: { label: "In Review", iconColor: "", hoverBg: "" },
    done: { label: "Done", iconColor: "", hoverBg: "" },
    blocked: { label: "Blocked", iconColor: "", hoverBg: "" },
    cancelled: { label: "Cancelled", iconColor: "", hoverBg: "" },
  },
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
  STATUS_ORDER: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  PRIORITY_CONFIG: {
    urgent: { label: "Urgent", bars: 4, color: "" },
    high: { label: "High", bars: 3, color: "" },
    medium: { label: "Medium", bars: 2, color: "" },
    low: { label: "Low", bars: 1, color: "" },
    none: { label: "No priority", bars: 0, color: "" },
  },
}));

const mockToggleParent = vi.fn();
const mockViewState = {
  viewMode: "list" as const,
  statusFilters: [] as string[],
  priorityFilters: [] as string[],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  sortBy: "position" as const,
  sortDirection: "asc" as const,
  cardProperties: {
    priority: true,
    description: true,
    assignee: true,
    dueDate: true,
    project: true,
    childProgress: true,
    labels: true,
  },
  listCollapsedStatuses: [] as string[],
  collapsedParentIds: [] as string[],
  toggleListCollapsed: vi.fn(),
  toggleParentCollapsed: mockToggleParent,
};

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: any) => children,
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({
    getState: () => mockViewState,
    setState: vi.fn(),
    subscribe: vi.fn(),
  }),
}));

vi.mock("@multica/core/issues/stores/selection-store", () => {
  const state = {
    selectedIds: new Set<string>(),
    toggle: vi.fn(),
    clear: vi.fn(),
    select: vi.fn(),
    deselect: vi.fn(),
  };
  return {
    useIssueSelectionStore: Object.assign(
      (selector?: any) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

// useLoadMoreByStatus is a TanStack-Query-touching hook that pulls from the
// real cache; stub it so the component can render without a populated
// per-status cache.
vi.mock("@multica/core/issues/mutations", () => ({
  useLoadMoreByStatus: () => ({
    loadMore: vi.fn(),
    hasMore: false,
    isLoading: false,
    total: 0,
  }),
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

// Capture the handlers DndContext is mounted with so tests can fire
// synthetic drag events directly. PointerSensor + jsdom doesn't have
// real pointer events, and dragging in a unit test is too brittle —
// invoking onDragEnd with a synthetic event proves the wiring without
// depending on the sensor.
type CapturedHandlers = {
  onDragEnd: ((e: any) => void) | null;
  onDragStart: ((e: any) => void) | null;
};
const dndHandlers: CapturedHandlers = vi.hoisted(() => ({
  onDragEnd: null,
  onDragStart: null,
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children, onDragEnd, onDragStart }: any) => {
    dndHandlers.onDragEnd = onDragEnd ?? null;
    dndHandlers.onDragStart = onDragStart ?? null;
    return <div data-testid="dnd-context">{children}</div>;
  },
  DragOverlay: ({ children }: any) => <div data-testid="drag-overlay">{children}</div>,
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

// Render Accordion as plain divs so the panel content always shows
// (the real impl gates on aria-expanded which depends on real DOM events).
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
  workspace_id: "ws-1",
  description: null,
  priority: "medium" as const,
  assignee_type: null,
  assignee_id: null,
  creator_type: "member" as const,
  creator_id: "user-1",
  due_date: null,
  project_id: null,
  parent_issue_id: null,
  position: 0,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const parent: Issue = {
  ...issueDefaults,
  id: "issue-parent",
  number: 1,
  identifier: "TES-1",
  title: "Parent issue",
  status: "todo",
};

const child1: Issue = {
  ...issueDefaults,
  id: "issue-child-1",
  number: 2,
  identifier: "TES-2",
  title: "First child",
  status: "todo",
  parent_issue_id: "issue-parent",
};

const child2: Issue = {
  ...issueDefaults,
  id: "issue-child-2",
  number: 3,
  identifier: "TES-3",
  title: "Second child",
  status: "todo",
  parent_issue_id: "issue-parent",
};

const orphan: Issue = {
  ...issueDefaults,
  id: "issue-orphan",
  number: 4,
  identifier: "TES-4",
  title: "Orphan child",
  status: "todo",
  parent_issue_id: "issue-not-in-list", // parent excluded by current filters
};

// ---------------------------------------------------------------------------
// Imports under test (after mocks)
// ---------------------------------------------------------------------------

import { ListView } from "./list-view";

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  mockViewState.collapsedParentIds = [];
  mockViewState.listCollapsedStatuses = [];
  dndHandlers.onDragEnd = null;
  dndHandlers.onDragStart = null;
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("ListView — nested sub-issues", () => {
  it("renders children indented below their parent and not as siblings", () => {
    renderWithQuery(
      <ListView issues={[parent, child1, child2]} visibleStatuses={["todo"]} />,
    );

    // All three render
    expect(screen.getByText("Parent issue")).toBeInTheDocument();
    expect(screen.getByText("First child")).toBeInTheDocument();
    expect(screen.getByText("Second child")).toBeInTheDocument();

    // Children live inside the parent's role=group container — proves
    // they are rendered as nested sub-issues, not flat siblings.
    const subGroup = screen.getByRole("group", { name: /sub-issues of TES-1/i });
    expect(within(subGroup).getByText("First child")).toBeInTheDocument();
    expect(within(subGroup).getByText("Second child")).toBeInTheDocument();
    expect(within(subGroup).queryByText("Parent issue")).not.toBeInTheDocument();
  });

  it("hides children when the parent is collapsed and toggle calls the store", () => {
    mockViewState.collapsedParentIds = ["issue-parent"];

    renderWithQuery(
      <ListView issues={[parent, child1, child2]} visibleStatuses={["todo"]} />,
    );

    // Parent still visible; children hidden.
    expect(screen.getByText("Parent issue")).toBeInTheDocument();
    expect(screen.queryByText("First child")).not.toBeInTheDocument();
    expect(screen.queryByText("Second child")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("group", { name: /sub-issues of TES-1/i }),
    ).not.toBeInTheDocument();

    // Click expand chevron — wired to toggleParentCollapsed.
    const expandBtn = screen.getByRole("button", { name: /expand sub-issues/i });
    fireEvent.click(expandBtn);
    expect(mockToggleParent).toHaveBeenCalledWith("issue-parent");
  });

  it("renders an orphan child at the top level (parent filtered out)", () => {
    renderWithQuery(
      <ListView issues={[parent, child1, orphan]} visibleStatuses={["todo"]} />,
    );

    // Orphan renders even though its parent isn't in the list.
    expect(screen.getByText("Orphan child")).toBeInTheDocument();

    // Orphan is NOT inside the visible parent's sub-issues group — it's
    // surfaced at the top level instead.
    const subGroup = screen.getByRole("group", { name: /sub-issues of TES-1/i });
    expect(within(subGroup).queryByText("Orphan child")).not.toBeInTheDocument();
  });

  it("does not show a chevron on rows with no children", () => {
    renderWithQuery(
      <ListView issues={[parent, child1]} visibleStatuses={["todo"]} />,
    );

    // The parent (which has children) should expose a collapse button;
    // the child should not.
    const buttons = screen.getAllByRole("button");
    const collapseLabels = buttons
      .map((b) => b.getAttribute("aria-label"))
      .filter(Boolean);
    expect(collapseLabels).toContain("Collapse sub-issues");
    // Only one parent → only one such control.
    expect(
      collapseLabels.filter((l) => l && /sub-issues/i.test(l)).length,
    ).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// Drag-to-move tests
// ---------------------------------------------------------------------------

const todoIssue: Issue = {
  ...issueDefaults,
  id: "issue-todo",
  number: 10,
  identifier: "TES-10",
  title: "Todo item",
  status: "todo",
  position: 1,
};

const inProgressIssue: Issue = {
  ...issueDefaults,
  id: "issue-inprog",
  number: 11,
  identifier: "TES-11",
  title: "In progress item",
  status: "in_progress",
  position: 1,
};

describe("ListView — drag to move", () => {
  it("does NOT mount DndContext when onMoveIssue is not provided (read-only)", () => {
    renderWithQuery(
      <ListView issues={[todoIssue]} visibleStatuses={["todo"]} />,
    );
    expect(screen.queryByTestId("dnd-context")).not.toBeInTheDocument();
  });

  it("mounts DndContext when onMoveIssue is provided", () => {
    const onMove = vi.fn();
    renderWithQuery(
      <ListView
        issues={[todoIssue]}
        visibleStatuses={["todo"]}
        onMoveIssue={onMove}
      />,
    );
    expect(screen.getByTestId("dnd-context")).toBeInTheDocument();
  });

  it("calls onMoveIssue with new status when dropped on another status section", () => {
    const onMove = vi.fn();
    renderWithQuery(
      <ListView
        issues={[todoIssue, inProgressIssue]}
        visibleStatuses={["todo", "in_progress"]}
        onMoveIssue={onMove}
      />,
    );

    // Drop the todo issue on the in_progress status section.
    dndHandlers.onDragEnd?.({
      active: { id: "issue-todo" },
      over: { id: "list-status:in_progress" },
    });

    expect(onMove).toHaveBeenCalledTimes(1);
    expect(onMove).toHaveBeenCalledWith(
      "issue-todo",
      "in_progress",
      expect.any(Number),
    );
  });

  it("calls onMoveIssue with target status when dropped on a row in another status", () => {
    const onMove = vi.fn();
    renderWithQuery(
      <ListView
        issues={[todoIssue, inProgressIssue]}
        visibleStatuses={["todo", "in_progress"]}
        onMoveIssue={onMove}
      />,
    );

    // Drop the todo issue ON another row that's in in_progress — target
    // status is derived from the over-row's status.
    dndHandlers.onDragEnd?.({
      active: { id: "issue-todo" },
      over: { id: "issue-inprog" },
    });

    expect(onMove).toHaveBeenCalledWith(
      "issue-todo",
      "in_progress",
      expect.any(Number),
    );
  });

  it("does NOT call onMoveIssue when status + position would be unchanged", () => {
    const onMove = vi.fn();
    renderWithQuery(
      <ListView
        issues={[todoIssue]}
        visibleStatuses={["todo"]}
        onMoveIssue={onMove}
      />,
    );

    // "Drop" on the same item — same status, same position computed.
    dndHandlers.onDragEnd?.({
      active: { id: "issue-todo" },
      over: { id: "issue-todo" },
    });

    expect(onMove).not.toHaveBeenCalled();
  });

  it("does NOT call onMoveIssue when there's no drop target (drag cancelled)", () => {
    const onMove = vi.fn();
    renderWithQuery(
      <ListView
        issues={[todoIssue]}
        visibleStatuses={["todo"]}
        onMoveIssue={onMove}
      />,
    );

    dndHandlers.onDragEnd?.({
      active: { id: "issue-todo" },
      over: null,
    });

    expect(onMove).not.toHaveBeenCalled();
  });
});
