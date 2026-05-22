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

// Mock view store
const mockViewState = {
  sortBy: "position" as const,
  sortDirection: "asc" as const,
  cardProperties: { priority: true, description: true, assignee: true, dueDate: true, project: true, childProgress: true, labels: true },
};

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

    // Click the Add Issue button inside SwimLaneCell
    const addButtons = screen.getAllByRole("button");
    const fullWidthAddButton = addButtons.find(btn => btn.classList.contains("w-full"));
    expect(fullWidthAddButton).toBeDefined();
    
    fireEvent.click(fullWidthAddButton!);
    expect(mockOpenModal).toHaveBeenCalledWith("create-issue", expect.any(Object));
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

    // Parent Issue 1 (id: "parent-1", status: "todo")
    // We simulate dragging "parent-1" over the "In Progress" column of the "No parent" swimlane
    // Cell ID format: `swim:${parentKey}:${status}`
    const targetCellId = "swim:parent:none:in_progress";

    act(() => {
      lastOnDragOver({
        active: { id: "parent-1" },
        over: { id: targetCellId },
      });
    });

    act(() => {
      lastOnDragEnd({
        active: { id: "parent-1" },
        over: { id: targetCellId },
      });
    });

    expect(mockOnMoveIssue).toHaveBeenCalledWith("parent-1", {
      parent_issue_id: null,
      status: "in_progress",
      position: 100, // maintains its position since it's the only item
    });
  });
});
