import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NextIntlClientProvider } from "next-intl";
import type { Issue } from "@multica/core/types";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import enMessages from "@/messages/en.json";

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

// Mock @multica/core/auth (used by @multica/views pickers like AssigneePicker)
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

// Mock @multica/core/workspace (used by @multica/views components)
vi.mock("@multica/core/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector?: any) => {
      const state = { workspace: { id: "ws-1", name: "Test", slug: "test" }, agents: [], members: [] };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ workspace: { id: "ws-1", name: "Test", slug: "test" }, agents: [], members: [] }) },
  ),
  registerWorkspaceStore: vi.fn(),
}));

vi.mock("@/platform/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector?: any) => {
      const state = { workspace: { id: "ws-1", name: "Test", slug: "test" }, agents: [], members: [] };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ workspace: { id: "ws-1", name: "Test", slug: "test" }, agents: [], members: [] }) },
  ),
}));

// Mock @multica/views/navigation (AppLink used by views components)
vi.mock("@multica/views/navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => <a href={href} {...props}>{children}</a>,
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues" }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock @multica/views/workspace/workspace-avatar
vi.mock("@multica/views/workspace/workspace-avatar", () => ({
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

// Mock api (core queries/mutations use @multica/core/api)
const mockUpdateIssue = vi.fn();
const mockListIssues = vi.hoisted(() => vi.fn().mockResolvedValue({ issues: [], total: 0 }));

vi.mock("@multica/core/api", () => ({
  api: {
    listIssues: (...args: any[]) => mockListIssues(...args),
    updateIssue: (...args: any[]) => mockUpdateIssue(...args),
    listMembers: () => Promise.resolve([]),
    listAgents: () => Promise.resolve([]),
  },
  getApi: () => ({
    listIssues: (...args: any[]) => mockListIssues(...args),
    updateIssue: (...args: any[]) => mockUpdateIssue(...args),
    listMembers: () => Promise.resolve([]),
    listAgents: () => Promise.resolve([]),
  }),
  setApiInstance: vi.fn(),
}));

// Mock issue store — only client state remains
const mockIssueClientState = { activeIssueId: null, setActiveIssue: vi.fn() };
vi.mock("@multica/core/issues", () => ({
  useIssueStore: Object.assign(
    (selector?: any) => (selector ? selector(mockIssueClientState) : mockIssueClientState),
    { getState: () => mockIssueClientState },
  ),
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: Object.assign(
    (selector?: any) => (selector ? selector(mockIssueClientState) : mockIssueClientState),
    { getState: () => mockIssueClientState },
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

vi.mock("@multica/core/issues/stores/view-store", () => ({
  initFilterWorkspaceSync: vi.fn(),
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
  ],
}));

// Mock view store context (shared components read from context)
vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({ getState: () => mockViewState, setState: vi.fn(), subscribe: vi.fn() }),
}));

// Mock issues scope store
vi.mock("@multica/core/issues/stores/issues-scope-store", () => ({
  useIssuesScopeStore: Object.assign(
    (selector?: any) => {
      const state = { scope: "all", setScope: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ scope: "all", setScope: vi.fn() }) },
  ),
}));

// Mock selection store
vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: Object.assign(
    (selector?: any) => {
      const state = { selectedIds: new Set(), toggle: vi.fn(), clear: vi.fn(), setAll: vi.fn() };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ selectedIds: new Set(), toggle: vi.fn(), clear: vi.fn(), setAll: vi.fn() }) },
  ),
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

// Mock modals
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

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

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } });
  return render(
    <NextIntlClientProvider locale="en" messages={enMessages}>
      <QueryClientProvider client={qc}>
        <WorkspaceIdProvider wsId="ws-1">
          {ui}
        </WorkspaceIdProvider>
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("IssuesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListIssues.mockResolvedValue({ issues: [], total: 0 });
    mockViewState.viewMode = "board";
    mockViewState.statusFilters = [];
    mockViewState.priorityFilters = [];
  });

  it("shows loading state initially", () => {
    renderWithQuery(<IssuesPage />);
    expect(screen.getAllByRole("generic").some(el => el.getAttribute("data-slot") === "skeleton")).toBe(true);
  });

  it("renders issues in board view after loading", async () => {
    // issueListOptions makes 2 calls: open_only + closed page. Return issues for open, empty for closed.
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve(params?.open_only ? { issues: mockIssues, total: mockIssues.length } : { issues: [], total: 0 }),
    );

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Implement auth");
    expect(screen.getByText("Design landing page")).toBeInTheDocument();
    expect(screen.getByText("Write tests")).toBeInTheDocument();
  });

  it("renders board columns", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve(params?.open_only ? { issues: mockIssues, total: mockIssues.length } : { issues: [], total: 0 }),
    );

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Backlog");
    expect(screen.getAllByText("Todo").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("In Progress").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("In Review").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Done").length).toBeGreaterThanOrEqual(1);
  });

  it("shows workspace breadcrumb", async () => {
    renderWithQuery(<IssuesPage />);

    await screen.findByText("Issues");
  });

  it("shows scope buttons", async () => {
    renderWithQuery(<IssuesPage />);

    await screen.findByText("All");
    expect(screen.getByText("Members")).toBeInTheDocument();
    expect(screen.getByText("Agents")).toBeInTheDocument();
  });

  it("shows filter and display icon buttons", async () => {
    mockListIssues.mockImplementation((params: any) =>
      Promise.resolve(params?.open_only ? { issues: mockIssues, total: mockIssues.length } : { issues: [], total: 0 }),
    );

    renderWithQuery(<IssuesPage />);

    await screen.findByText("Implement auth");
    const buttons = screen.getAllByRole("button");
    expect(buttons.length).toBeGreaterThan(0);
  });

  it("shows empty board view when no issues exist", () => {
    renderWithQuery(<IssuesPage />);

    // Should still render the board/list view, not a "no issues" message
    expect(screen.queryByText("No matching issues")).not.toBeInTheDocument();
  });
});
