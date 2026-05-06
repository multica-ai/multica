// Regression tests for #BRY-53. The renderer used to call `.filter` on the
// timeline source unguarded; for issues with no activity, the source returned
// null/undefined and the entire detail screen white-screened. These tests
// mock the timeline hook to inject those non-array shapes and assert that
// the component still mounts.

import { forwardRef, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

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

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: () => "Test User",
    getAgentName: () => "Unknown Agent",
    getActorName: () => "Test User",
    getActorInitials: () => "TU",
    getActorAvatarUrl: () => null,
  }),
}));

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
  workspaceListOptions: () => ({
    queryKey: ["workspaces"],
    queryFn: () => Promise.resolve([{ id: "ws-1", name: "Test WS", slug: "test" }]),
  }),
}));

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

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues/issue-1", getShareableUrl: undefined }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("../../editor", () => ({
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: forwardRef(function MockContentEditor(
    { defaultValue, onUpdate, placeholder }: any,
    ref: any,
  ) {
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
  TitleEditor: forwardRef(function MockTitleEditor(
    { defaultValue, placeholder, onBlur, onChange }: any,
    ref: any,
  ) {
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

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: any) => (
    <span data-testid="actor-avatar">{actorType}:{actorId}</span>
  ),
}));

vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: () => <span data-testid="project-picker">Project</span>,
}));

const mockApiObj = vi.hoisted(() => ({
  getIssue: vi.fn(),
  listTimeline: vi.fn(),
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

const mockRecordVisit = vi.fn();
vi.mock("@multica/core/issues/stores", () => ({
  useRecentIssuesStore: Object.assign(
    (selector?: any) => {
      const state = { items: [], recordVisit: mockRecordVisit };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ items: [], recordVisit: mockRecordVisit }) },
  ),
  useCommentCollapseStore: (selector?: any) => {
    const state = { collapsedByIssue: {}, isCollapsed: () => false, toggle: () => {} };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/utils", () => ({
  timeAgo: () => "1d ago",
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn().mockResolvedValue("https://example.com/file.png") }),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
  useWS: () => ({ subscribe: vi.fn(() => () => {}), onReconnect: vi.fn(() => () => {}) }),
  WSProvider: ({ children }: { children: React.ReactNode }) => children,
  useRealtimeSync: () => {},
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("react-resizable-panels", () => ({
  Group: ({ children, ...props }: any) => <div data-testid="panel-group" {...props}>{children}</div>,
  Panel: ({ children, ...props }: any) => <div data-testid="panel" {...props}>{children}</div>,
  Separator: ({ children, ...props }: any) => <div data-testid="panel-handle" {...props}>{children}</div>,
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  usePanelRef: () => ({ current: { isCollapsed: () => false, expand: vi.fn(), collapse: vi.fn() } }),
}));

// The crash this file regresses: useIssueTimeline returned a non-array
// timeline (null/undefined) for zero-activity issues, and IssueDetail called
// .filter on it. Mock the hook directly so we can inject those shapes.
const timelineMock = vi.hoisted(() => ({
  timeline: null as unknown,
}));

vi.mock("../hooks/use-issue-timeline", () => ({
  useIssueTimeline: () => ({
    timeline: timelineMock.timeline,
    loading: false,
    submitting: false,
    submitComment: vi.fn(),
    submitReply: vi.fn(),
    editComment: vi.fn(),
    deleteComment: vi.fn(),
    toggleReaction: vi.fn(),
    hasMoreOlder: false,
    hasMoreNewer: false,
    isFetchingOlder: false,
    isFetchingNewer: false,
    fetchOlder: vi.fn(),
    fetchNewer: vi.fn(),
    jumpToLatest: vi.fn(),
    isAtLatest: true,
    newEntriesBelowCount: 0,
    targetFlatIndex: null,
  }),
}));

vi.mock("../hooks/use-issue-reactions", () => ({
  useIssueReactions: () => ({ reactions: [], toggleReaction: vi.fn() }),
}));

vi.mock("../hooks/use-issue-subscribers", () => ({
  useIssueSubscribers: () => ({
    subscribers: [],
    isSubscribed: false,
    toggleSubscribe: vi.fn(),
    toggleSubscriber: vi.fn(),
  }),
}));

// ---------------------------------------------------------------------------
// Test data
// ---------------------------------------------------------------------------

const mockIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Fresh issue with no activity",
  description: "",
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  due_date: null,
  created_at: "2026-05-06T10:00:00Z",
  updated_at: "2026-05-06T10:00:00Z",
};

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { IssueDetail } from "./issue-detail";

function renderIssueDetail() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <IssueDetail issueId="issue-1" />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("IssueDetail (empty-timeline guard)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockApiObj.getIssue.mockResolvedValue(mockIssue);
    mockApiObj.listChildIssues.mockResolvedValue({ issues: [] });
    mockApiObj.listIssues.mockResolvedValue({ issues: [], total: 0 });
    mockApiObj.listIssueSubscribers.mockResolvedValue([]);
    mockApiObj.listMembers.mockResolvedValue([
      { user_id: "user-1", name: "Test User", email: "test@test.com", role: "admin" },
    ]);
    mockApiObj.listAgents.mockResolvedValue([]);
  });

  it("renders an issue with timeline=null", async () => {
    timelineMock.timeline = null;

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Fresh issue with no activity")).toBeInTheDocument();
    });
  });

  it("renders an issue with timeline=undefined", async () => {
    timelineMock.timeline = undefined;

    renderIssueDetail();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Fresh issue with no activity")).toBeInTheDocument();
    });
  });
});
