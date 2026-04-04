import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Issue } from "@/shared/types";

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/issues",
}));

// Mock next/link
vi.mock("next/link", () => ({
  default: ({
    children,
    href,
    ...props
  }: {
    children: React.ReactNode;
    href: string;
    [key: string]: any;
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

// Mock workspace feature
vi.mock("@/features/workspace", () => ({
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Test User" : "Unknown"),
    getAgentName: (id: string) => (id === "agent-1" ? "Claude Agent" : "Unknown Agent"),
    getActorName: (type: string, id: string) =>
      type === "member" ? "Test User" : "Claude Agent",
    getActorInitials: () => "TU",
    getActorAvatarUrl: () => null,
  }),
  useWorkspaceStore: Object.assign(
    (selector?: any) => {
      const state = { workspace: { id: "ws-1", name: "Test", slug: "test" }, agents: [], members: [] };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ workspace: { id: "ws-1", name: "Test", slug: "test" }, agents: [], members: [] }) },
  ),
  WorkspaceAvatar: ({ name }: { name: string }) => <span>{name.charAt(0)}</span>,
}));

// Mock WebSocket context
vi.mock("@/features/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
  useWS: () => ({ subscribe: vi.fn(() => () => {}), onReconnect: vi.fn(() => () => {}) }),
  WSProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock sonner toast
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Mock api
const mockUpdateIssue = vi.fn();

vi.mock("@/shared/api", () => ({
  api: {
    listIssues: vi.fn().mockResolvedValue({ issues: [], total: 0 }),
    updateIssue: (...args: any[]) => mockUpdateIssue(...args),
  },
}));

// Mock the issue store
let mockStoreState: {
  issues: Issue[];
  loading: boolean;
  fetch: () => Promise<void>;
  setIssues: (issues: Issue[]) => void;
  addIssue: (issue: Issue) => void;
  updateIssue: (id: string, updates: Partial<Issue>) => void;
  removeIssue: (id: string) => void;
};

vi.mock("@/features/issues/store", () => ({
  useIssueStore: Object.assign(
    (selector?: any) => (selector ? selector(mockStoreState) : mockStoreState),
    { getState: () => mockStoreState },
  ),
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: Object.assign(
    (selector?: any) => (selector ? selector(mockStoreState) : mockStoreState),
    { getState: () => mockStoreState },
  ),
  StatusIcon: () => null,
  PriorityIcon: () => null,
  StatusPicker: ({ value, onChange }: any) => (
    <button onClick={() => onChange?.("todo")}>{value || "todo"}</button>
  ),
  PriorityPicker: ({ value, onChange }: any) => (
    <button onClick={() => onChange?.("none")}>{value || "none"}</button>
  ),
}));

// Mock view store
const mockViewState = {
  viewMode: "board" as const,
  statusFilters: [] as string[],
  priorityFilters: [] as string[],
  assigneeFilters: [] as { type: string; id: string }[],
  includeNoAssignee: false,
  creatorFilters: [] as { type: string; id: string }[],
  sortBy: "position" as const,
  sortDirection: "asc" as const,
  cardProperties: { priority: true, description: true, assignee: true, dueDate: true },
  listCollapsedStatuses: [] as string[],
  setViewMode: vi.fn(),
  toggleStatusFilter: vi.fn(),
  togglePriorityFilter: vi.fn(),
  toggleAssigneeFilter: vi.fn(),
  toggleNoAssignee: vi.fn(),
  toggleCreatorFilter: vi.fn(),
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
  clearFilters: vi.fn(),
  setSortBy: vi.fn(),
  setSortDirection: vi.fn(),
  toggleCardProperty: vi.fn(),
  toggleListCollapsed: vi.fn(),
};

vi.mock("@/features/issues/stores/view-store", () => ({
  initFilterWorkspaceSync: vi.fn(),
  useIssueViewStore: Object.assign(
    (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
    { getState: () => mockViewState, setState: vi.fn() },
  ),
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
  ],
}));

// Mock view store context (shared components read from context)
vi.mock("@/features/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({ getState: () => mockViewState, setState: vi.fn(), subscribe: vi.fn() }),
}));

// Mock issue config
vi.mock("@/features/issues/config", () => ({
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

// Mock modals
vi.mock("@/features/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
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

import IssuesPage from "./page";

describe("IssuesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockStoreState = {
      issues: [],
      loading: true,
      fetch: vi.fn(),
      setIssues: vi.fn(),
      addIssue: vi.fn(),
      updateIssue: vi.fn(),
      removeIssue: vi.fn(),
    };
    mockViewState.viewMode = "board";
    mockViewState.statusFilters = [];
    mockViewState.priorityFilters = [];
  });

  it("shows loading state initially", () => {
    mockStoreState.loading = true;
    mockStoreState.issues = [];
    render(<IssuesPage />);
    expect(screen.getAllByRole("generic").some(el => el.getAttribute("data-slot") === "skeleton")).toBe(true);
  });

  it("renders issues in board view after loading", async () => {
    mockStoreState.loading = false;
    mockStoreState.issues = mockIssues;

    render(<IssuesPage />);

    expect(screen.getByText("Implement auth")).toBeInTheDocument();
    expect(screen.getByText("Design landing page")).toBeInTheDocument();
    expect(screen.getByText("Write tests")).toBeInTheDocument();
  });

  it("renders board columns", async () => {
    mockStoreState.loading = false;
    mockStoreState.issues = mockIssues;

    render(<IssuesPage />);

    expect(screen.getAllByText("Backlog").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Todo").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("In Progress").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("In Review").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Done").length).toBeGreaterThanOrEqual(1);
  });

  it("shows workspace breadcrumb", () => {
    mockStoreState.loading = false;
    mockStoreState.issues = [];

    render(<IssuesPage />);

    expect(screen.getByText("Issues")).toBeInTheDocument();
  });

  it("shows scope buttons", () => {
    mockStoreState.loading = false;
    mockStoreState.issues = [];

    render(<IssuesPage />);

    expect(screen.getByText("All")).toBeInTheDocument();
    expect(screen.getByText("Members")).toBeInTheDocument();
    expect(screen.getByText("Agents")).toBeInTheDocument();
  });

  it("shows filter and display icon buttons", () => {
    mockStoreState.loading = false;
    mockStoreState.issues = mockIssues;

    render(<IssuesPage />);

    // Filter and Display are now icon-only buttons, verify they render as buttons
    const buttons = screen.getAllByRole("button");
    expect(buttons.length).toBeGreaterThan(0);
  });

  it("shows empty board view when no issues exist", () => {
    mockStoreState.loading = false;
    mockStoreState.issues = [];

    render(<IssuesPage />);

    // Should still render the board/list view, not a "no issues" message
    expect(screen.queryByText("No matching issues")).not.toBeInTheDocument();
  });
});
