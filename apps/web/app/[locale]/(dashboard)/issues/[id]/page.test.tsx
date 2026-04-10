import { Suspense, forwardRef, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NextIntlClientProvider } from "next-intl";
import type { Issue, Comment, TimelineEntry } from "@multica/core/types";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import enMessages from "@/messages/en.json";

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/issues/issue-1",
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

// Mock auth store
vi.mock("@/platform/auth", () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({
      user: { id: "user-1", name: "Test User", email: "test@multica.ai" },
      isLoading: false,
    }),
}));

// Mock @multica/core/workspace (used by @multica/views components)
vi.mock("@multica/core/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector: (s: any) => any) =>
      selector({
        workspace: { id: "ws-1", name: "Test WS" },
        workspaces: [{ id: "ws-1", name: "Test WS" }],
        members: [{ user_id: "user-1", name: "Test User", email: "test@multica.ai" }],
        agents: [{ id: "agent-1", name: "Claude Agent" }],
      }),
    { getState: () => ({
        workspace: { id: "ws-1", name: "Test WS" },
        workspaces: [{ id: "ws-1", name: "Test WS" }],
        members: [{ user_id: "user-1", name: "Test User", email: "test@multica.ai" }],
        agents: [{ id: "agent-1", name: "Claude Agent" }],
      }),
    },
  ),
  registerWorkspaceStore: vi.fn(),
}));

// Mock @multica/core/auth (used by @multica/views components)
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: any) => any) =>
      selector({
        user: { id: "user-1", name: "Test User", email: "test@multica.ai" },
        isLoading: false,
      }),
    { getState: () => ({
        user: { id: "user-1", name: "Test User", email: "test@multica.ai" },
        isLoading: false,
      }),
    },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

// Mock @multica/views/navigation (AppLink used by views components)
vi.mock("@multica/views/navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => <a href={href} {...props}>{children}</a>,
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues/issue-1" }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock @multica/views/editor (ContentEditor, TitleEditor used by IssueDetail)
vi.mock("@multica/views/editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: forwardRef(({ defaultValue, onUpdate, placeholder, onSubmit }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => { valueRef.current = ""; setValue(""); },
      focus: () => {},
    }));
    return (
      <textarea
        value={value}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
            onSubmit?.();
          }
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

// Mock @multica/views/workspace/workspace-avatar
vi.mock("@multica/views/workspace/workspace-avatar", () => ({
  WorkspaceAvatar: ({ name }: { name: string }) => <span>{name.charAt(0)}</span>,
}));

// Mock @multica/views/common/actor-avatar
vi.mock("@multica/views/common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: any) => <span data-testid="actor-avatar">{actorType}:{actorId}</span>,
}));

// Mock @multica/views/common/markdown
vi.mock("@multica/views/common/markdown", () => ({
  Markdown: ({ children }: { children: string }) => <div>{children}</div>,
}));

// Mock workspace feature
vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      workspace: { id: "ws-1", name: "Test WS" },
      workspaces: [{ id: "ws-1", name: "Test WS" }],
      members: [{ user_id: "user-1", name: "Test User", email: "test@multica.ai" }],
      agents: [{ id: "agent-1", name: "Claude Agent" }],
    }),
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Test User" : "Unknown"),
    getAgentName: (id: string) => (id === "agent-1" ? "Claude Agent" : "Unknown Agent"),
    getActorName: (type: string, id: string) => {
      if (type === "member" && id === "user-1") return "Test User";
      if (type === "agent" && id === "agent-1") return "Claude Agent";
      return "Unknown";
    },
    getActorInitials: (type: string, id: string) => {
      if (type === "member") return "TU";
      if (type === "agent") return "CA";
      return "??";
    },
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@/platform/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      workspace: { id: "ws-1", name: "Test WS" },
      workspaces: [{ id: "ws-1", name: "Test WS" }],
      members: [{ user_id: "user-1", name: "Test User", email: "test@multica.ai" }],
      agents: [{ id: "agent-1", name: "Claude Agent" }],
    }),
}));

// Mock workspace hooks from core
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Test User" : "Unknown"),
    getAgentName: (id: string) => (id === "agent-1" ? "Claude Agent" : "Unknown Agent"),
    getActorName: (type: string, id: string) => {
      if (type === "member" && id === "user-1") return "Test User";
      if (type === "agent" && id === "agent-1") return "Claude Agent";
      return "Unknown";
    },
    getActorInitials: (type: string, id: string) => {
      if (type === "member") return "TU";
      if (type === "agent") return "CA";
      return "??";
    },
    getActorAvatarUrl: () => null,
  }),
}));

// Mock issue store — only client state remains (activeIssueId)
vi.mock("@/features/issues", () => ({
  useIssueStore: Object.assign(
    (selector: (s: any) => any) => selector({ activeIssueId: null }),
    { getState: () => ({ activeIssueId: null, setActiveIssue: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/issues", () => ({
  useIssueStore: Object.assign(
    (selector: (s: any) => any) => selector({ activeIssueId: null }),
    { getState: () => ({ activeIssueId: null, setActiveIssue: vi.fn() }) },
  ),
}));

// Mock ws-context
vi.mock("@/features/realtime", () => ({
  useWSEvent: () => {},
  useWSReconnect: () => {},
}));

// Mock core realtime (hooks now import from @multica/core/realtime)
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: () => {},
  useWSReconnect: () => {},
  useWS: () => ({ subscribe: vi.fn(() => () => {}), onReconnect: vi.fn(() => () => {}) }),
  WSProvider: ({ children }: { children: React.ReactNode }) => children,
  useRealtimeSync: () => {},
}));

// Mock calendar (react-day-picker needs browser APIs)
vi.mock("@/components/ui/calendar", () => ({
  Calendar: () => null,
}));

// Mock ContentEditor (Tiptap needs real DOM)
vi.mock("@/features/editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: forwardRef(({ defaultValue, onUpdate, placeholder, onSubmit }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => { valueRef.current = ""; setValue(""); },
      focus: () => {},
    }));
    return (
      <textarea
        value={value}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
            onSubmit?.();
          }
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

// Mock Markdown renderer
vi.mock("@/components/markdown", () => ({
  Markdown: ({ children }: { children: string }) => <div>{children}</div>,
}));

// Mock api (core queries/mutations use @multica/core/api, some components use @/platform/api)

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
}));

vi.mock("@multica/core/api", () => ({
  api: mockApiObj,
  getApi: () => mockApiObj,
  setApiInstance: vi.fn(),
}));

vi.mock("@/platform/api", () => ({
  api: mockApiObj,
}));

// Mock issue config from core
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

// Mock utils
vi.mock("@multica/core/utils", () => ({
  timeAgo: (date: string) => "1d ago",
}));

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

import IssueDetailPage from "./page";

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

// React 19 use(Promise) needs the promise to resolve within act + Suspense
async function renderPage(id = "issue-1") {
  const queryClient = createTestQueryClient();
  let result: ReturnType<typeof render>;
  await act(async () => {
    result = render(
      <NextIntlClientProvider locale="en" messages={enMessages}>
        <QueryClientProvider client={queryClient}>
          <WorkspaceIdProvider wsId="ws-1">
            <Suspense fallback={<div>Suspense loading...</div>}>
              <IssueDetailPage params={Promise.resolve({ id })} />
            </Suspense>
          </WorkspaceIdProvider>
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );
  });
  return result!;
}

describe("IssueDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders issue details after loading", async () => {
    mockApiObj.getIssue.mockResolvedValueOnce(mockIssue);
    mockApiObj.listTimeline.mockResolvedValueOnce(mockTimeline);
    await renderPage();

    await waitFor(() => {
      expect(
        screen.getAllByText("Implement authentication").length,
      ).toBeGreaterThanOrEqual(1);
    });

    expect(
      screen.getByText("Add JWT auth to the backend"),
    ).toBeInTheDocument();
  });

  it("renders issue properties sidebar", async () => {
    mockApiObj.getIssue.mockResolvedValueOnce(mockIssue);
    mockApiObj.listTimeline.mockResolvedValueOnce(mockTimeline);
    await renderPage();

    await waitFor(() => {
      expect(screen.getByText("Properties")).toBeInTheDocument();
    });

    expect(screen.getByText("In Progress")).toBeInTheDocument();
    expect(screen.getByText("High")).toBeInTheDocument();
  });

  it("renders comments", async () => {
    mockApiObj.getIssue.mockResolvedValueOnce(mockIssue);
    mockApiObj.listTimeline.mockResolvedValueOnce(mockTimeline);
    await renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Started working on this"),
      ).toBeInTheDocument();
    });

    expect(screen.getByText("I can help with this")).toBeInTheDocument();
    expect(screen.getAllByText("Activity").length).toBeGreaterThanOrEqual(1);
  });

  it("shows 'Issue not found' for missing issue", async () => {
    // issue-detail fetches getIssue, useIssueReactions also fetches getIssue
    mockApiObj.getIssue.mockRejectedValue(new Error("Not found"));
    mockApiObj.listTimeline.mockRejectedValue(new Error("Not found"));
    await renderPage("nonexistent-id");

    await waitFor(() => {
      expect(screen.getByText("This issue does not exist or has been deleted in this workspace.")).toBeInTheDocument();
    });
  });

  it("submits a new comment", async () => {
    mockApiObj.getIssue.mockResolvedValueOnce(mockIssue);
    mockApiObj.listTimeline.mockResolvedValueOnce(mockTimeline);

    const newComment: Comment = {
      id: "comment-3",
      issue_id: "issue-1",
      content: "New test comment",
      type: "comment",
      author_type: "member",
      author_id: "user-1",
      parent_id: null,
      reactions: [],
      attachments: [],
      created_at: "2026-01-18T00:00:00Z",
      updated_at: "2026-01-18T00:00:00Z",
    };
    mockApiObj.createComment.mockResolvedValueOnce(newComment);

    const user = userEvent.setup();
    await renderPage();

    await waitFor(() => {
      expect(
        screen.getByPlaceholderText("Leave a comment..."),
      ).toBeInTheDocument();
    });

    const commentInput = screen.getByPlaceholderText("Leave a comment...");

    // Use fireEvent to update the textarea value and trigger onUpdate
    await act(async () => {
      fireEvent.change(commentInput, { target: { value: "New test comment" } });
    });

    // Find the submit button associated with the "Leave a comment..." input.
    // Multiple ArrowUp buttons exist (one per ReplyInput), so we find the
    // button within the same ReplyInput container as our textarea.
    const allArrowUpBtns = screen.getAllByRole("button").filter(
      (btn) => btn.querySelector(".lucide-arrow-up") !== null,
    );
    // The bottom "Leave a comment..." ReplyInput renders last, so its button is last
    const submitBtn = allArrowUpBtns[allArrowUpBtns.length - 1]!;
    await waitFor(() => {
      expect(submitBtn).not.toBeDisabled();
    });
    await user.click(submitBtn);

    await waitFor(() => {
      expect(mockApiObj.createComment).toHaveBeenCalled();
      const [issueId, content] = mockApiObj.createComment.mock.calls[0]!;
      expect(issueId).toBe("issue-1");
      expect(content).toBe("New test comment");
    });

    await waitFor(() => {
      expect(screen.getByText("New test comment")).toBeInTheDocument();
    });
  });

  it("renders breadcrumb navigation", async () => {
    mockApiObj.getIssue.mockResolvedValueOnce(mockIssue);
    mockApiObj.listTimeline.mockResolvedValueOnce(mockTimeline);
    await renderPage();

    await waitFor(() => {
      expect(screen.getByText("Test WS")).toBeInTheDocument();
    });

    const wsLink = screen.getByText("Test WS");
    expect(wsLink.closest("a")).toHaveAttribute("href", "/issues");
  });
});
