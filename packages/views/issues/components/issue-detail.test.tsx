import { forwardRef, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { WorkspaceIdProvider } from "@multica/core/hooks";

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

// Mock @multica/core/workspace
vi.mock("@multica/core/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector?: any) => {
      const state = {
        workspace: { id: "ws-1", name: "Test WS", slug: "test" },
        agents: [],
        members: [],
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        workspace: { id: "ws-1", name: "Test WS", slug: "test" },
        agents: [],
        members: [],
      }),
    },
  ),
  registerWorkspaceStore: vi.fn(),
}));

// Mock @multica/core/workspace/hooks
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Test User" : "Unknown"),
    getAgentName: (id: string) => (id === "agent-1" ? "Claude Agent" : "Unknown Agent"),
    getActorName: (type: string, id: string) => {
      if (type === "member" && id === "user-1") return "Test User";
      if (type === "agent" && id === "agent-1") return "Claude Agent";
      return "Unknown";
    },
    getActorInitials: (type: string) => (type === "member" ? "TU" : "CA"),
    getActorAvatarUrl: () => null,
  }),
}));

// Mock workspace queries
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "members"],
    queryFn: () => Promise.resolve([{ user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" }]),
  }),
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
  assigneeFrequencyOptions: () => ({
    queryKey: ["workspaces", "ws-1", "assignee-frequency"],
    queryFn: () => Promise.resolve([]),
  }),
}));

// Mock navigation
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues/issue-1", getShareableUrl: undefined }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock editor components (Tiptap requires real DOM)
vi.mock("../../editor", () => ({
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: forwardRef(({ defaultValue, onUpdate, placeholder }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => { valueRef.current = ""; setValue(""); },
      focus: () => {},
      uploadFile: () => {},
    }));
    return (
      <textarea
        value={value}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
        placeholder={placeholder}
        data-testid="rich-text-editor"
      />
    );
  }),
  TitleEditor: forwardRef(({ defaultValue, placeholder, onBlur, onChange }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getText: () => valueRef.current,
      focus: () => {},
    }));
    return (
      <input
        value={value}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onChange?.(e.target.value);
        }}
        onBlur={() => onBlur?.(valueRef.current)}
        placeholder={placeholder}
        data-testid="title-editor"
      />
    );
  }),
}));

// Mock common components
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: any) => (
    <span data-testid="actor-avatar">
      {actorType}:{actorId}
    </span>
  ),
}));

vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: () => <span data-testid="project-picker">Project</span>,
}));

// Mock api
const mockApiObj = vi.hoisted(() => ({
  getIssue: vi.fn(),
  listTimeline: vi.fn().mockResolvedValue([]),
  listComments: vi.fn().mockResolvedValue([]),
  createComment: vi.fn(),
  updateComment: vi.fn(),
  deleteComment: vi.fn(),
  deleteIssue: vi.fn(),
  updateIssue: vi.fn(),
  listIssueSubscribers: vi.fn().mockResolvedValue([]),
  subscribeToIssue: vi.fn().mockResolvedValue(undefined),
  unsubscribeFromIssue: vi.fn().mockResolvedValue(undefined),
  getActiveTasksForIssue: vi.fn().mockResolvedValue({ tasks: [] }),
  listTasksByIssue: vi.fn().mockResolvedValue([]),
  listTaskMessages: vi.fn().mockResolvedValue([]),
  listChildIssues: vi.fn().mockResolvedValue({ issues: [] }),
  listIssues: vi.fn().mockResolvedValue({ issues: [], total: 0 }),
  uploadFile: vi.fn(),
  listIssueReactions: vi.fn().mockResolvedValue([]),
  addIssueReaction: vi.fn(),
  removeIssueReaction: vi.fn(),
  addCommentReaction: vi.fn(),
  removeCommentReaction: vi.fn(),
  listMembers: vi.fn().mockResolvedValue([{ user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" }]),
  listAgents: vi.fn().mockResolvedValue([]),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApiObj,
  getApi: () => mockApiObj,
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
    urgent: { label: "Urgent", bars: 4, color: "text-destructive", badgeBg: "bg-destructive/10", badgeText: "text-destructive" },
    high: { label: "High", bars: 3, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
    medium: { label: "Medium", bars: 2, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
    low: { label: "Low", bars: 1, color: "text-info", badgeBg: "bg-info/10", badgeText: "text-info" },
    none: { label: "No priority", bars: 0, color: "text-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  },
}));

// Mock recent issues store
const mockRecordVisit = vi.fn();
vi.mock("@multica/core/issues/stores", () => ({
  useRecentIssuesStore: Object.assign(
    (selector?: any) => {
      const state = { items: [], recordVisit: mockRecordVisit };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ items: [], recordVisit: mockRecordVisit }) },
  ),
}));

// Mock modals
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

// Mock core/utils
vi.mock("@multica/core/utils", () => ({
  timeAgo: () => "1d ago",
}));

// Mock core/hooks/use-file-upload
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn().mockResolvedValue("https://example.com/file.png") }),
}));

// Mock realtime
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
  useWS: () => ({ subscribe: vi.fn(() => () => {}), onReconnect: vi.fn(() => () => {}) }),
  WSProvider: ({ children }: { children: React.ReactNode }) => children,
  useRealtimeSync: () => {},
}));

// Mock sonner
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Mock react-resizable-panels (used by @multica/ui/components/ui/resizable)
vi.mock("react-resizable-panels", () => ({
  Group: ({ children, ...props }: any) => <div data-testid="panel-group" {...props}>{children}</div>,
  Panel: ({ children, ...props }: any) => <div data-testid="panel" {...props}>{children}</div>,
  Separator: ({ children, ...props }: any) => <div data-testid="panel-handle" {...props}>{children}</div>,
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  usePanelRef: () => ({ current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() } }),
}));

// ---------------------------------------------------------------------------
// Test data
// ---------------------------------------------------------------------------

const mockIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Implement authentication",
  description: "Add JWT auth to the backend",
  status: "in_progress",
  priority: "high",
  assignee_type: "member",
  assignee_id: "user-1",
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  due_date: "2026-06-01T00:00:00Z",
  created_at: "2026-01-15T00:00:00Z",
  updated_at: "2026-01-20T00:00:00Z",
};

const mockTimeline: TimelineEntry[] = [
  {
    type: "comment",
    id: "comment-1",
    actor_type: "member",
    actor_id: "user-1",
    content: "Started working on this",
    parent_id: null,
    created_at: "2026-01-16T00:00:00Z",
    updated_at: "2026-01-16T00:00:00Z",
    comment_type: "comment",
  },
  {
    type: "comment",
    id: "comment-2",
    actor_type: "agent",
    actor_id: "agent-1",
    content: "I can help with this",
    parent_id: null,
    created_at: "2026-01-17T00:00:00Z",
    updated_at: "2026-01-17T00:00:00Z",
    comment_type: "comment",
  },
];

// ---------------------------------------------------------------------------
// Import component under test (after mocks)
// ---------------------------------------------------------------------------

import { IssueDetail } from "./issue-detail";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderIssueDetail(issueId = "issue-1") {
  const queryClient = createTestQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceIdProvider wsId="ws-1">
        <IssueDetail issueId={issueId} />
      </WorkspaceIdProvider>
    </QueryClientProvider>,
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("IssueDetail (shared)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Default: issue loads successfully
    mockApiObj.getIssue.mockResolvedValue(mockIssue);
    mockApiObj.listTimeline.mockResolvedValue(mockTimeline);
    mockApiObj.listIssueReactions.mockResolvedValue([]);
    mockApiObj.listIssueSubscribers.mockResolvedValue([]);
    mockApiObj.listChildIssues.mockResolvedValue({ issues: [] });
    mockApiObj.listIssues.mockResolvedValue({ issues: [], total: 0 });
    mockApiObj.getActiveTasksForIssue.mockResolvedValue({ tasks: [] });
    mockApiObj.listTasksByIssue.mockResolvedValue([]);
    mockApiObj.listMembers.mockResolvedValue([
      { user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" },
    ]);
    mockApiObj.listAgents.mockResolvedValue([]);
  });

  it("shows loading skeleton while data is loading", () => {
    // Make the API hang to keep loading state
    mockApiObj.getIssue.mockReturnValue(new Promise(() => {}));
    renderIssueDetail();

    expect(
      screen.getAllByRole("generic").some((el) => el.getAttribute("data-slot") === "skeleton"),
    ).toBe(true);
  });

  it("renders issue title and description after loading", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Implement authentication")).toBeInTheDocument();
    });

    expect(screen.getByDisplayValue("Add JWT auth to the backend")).toBeInTheDocument();
  });

  it("renders issue identifier in the breadcrumb", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("TES-1")).toBeInTheDocument();
    });
  });

  it("renders workspace name as breadcrumb link", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Test WS")).toBeInTheDocument();
    });

    const wsLink = screen.getByText("Test WS");
    expect(wsLink.closest("a")).toHaveAttribute("href", "/issues");
  });

  it("renders properties sidebar with status, priority, assignee, due date", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Properties")).toBeInTheDocument();
    });

    expect(screen.getByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Priority")).toBeInTheDocument();
    expect(screen.getByText("Assignee")).toBeInTheDocument();
    expect(screen.getByText("Due date")).toBeInTheDocument();
  });

  it("renders Details section with Created by and dates", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Details")).toBeInTheDocument();
    });

    expect(screen.getByText("Created by")).toBeInTheDocument();
    expect(screen.getByText("Created")).toBeInTheDocument();
    expect(screen.getByText("Updated")).toBeInTheDocument();
  });

  it("shows 'not found' message when issue does not exist", async () => {
    mockApiObj.getIssue.mockRejectedValue(new Error("Not found"));

    renderIssueDetail("nonexistent-id");

    await waitFor(() => {
      expect(
        screen.getByText("This issue does not exist or has been deleted in this workspace."),
      ).toBeInTheDocument();
    });
  });

  it("shows 'Back to Issues' button when issue is not found and no onDelete prop", async () => {
    mockApiObj.getIssue.mockRejectedValue(new Error("Not found"));

    renderIssueDetail("nonexistent-id");

    await waitFor(() => {
      expect(screen.getByText("Back to Issues")).toBeInTheDocument();
    });
  });

  it("renders Activity section header", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getAllByText("Activity").length).toBeGreaterThanOrEqual(1);
    });
  });

  it("renders comments from timeline", async () => {
    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByText("Started working on this")).toBeInTheDocument();
    });

    expect(screen.getByText("I can help with this")).toBeInTheDocument();
  });
});
