import { Suspense, forwardRef, useRef, useState, useImperativeHandle } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Issue, Comment, TimelineEntry } from "@/shared/types";

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
vi.mock("@/features/auth", () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({
      user: { id: "user-1", name: "Test User", email: "test@multica.ai" },
      isLoading: false,
    }),
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

// Mock issue store — supply a stable full issue object so storeIssue
// doesn't create a new reference each render (avoids infinite effect loop)
// and has all required fields for rendering.
const stableStoreIssues = vi.hoisted(() => [
  {
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
  },
]);
vi.mock("@/features/issues", () => ({
  useIssueStore: Object.assign(
    (selector: (s: any) => any) => selector({ issues: stableStoreIssues }),
    { getState: () => ({ issues: stableStoreIssues, addIssue: vi.fn(), updateIssue: vi.fn(), removeIssue: vi.fn() }) },
  ),
}));

// Mock ws-context
vi.mock("@/features/realtime", () => ({
  useWSEvent: () => {},
  useWSReconnect: () => {},
}));

// Mock calendar (react-day-picker needs browser APIs)
vi.mock("@/components/ui/calendar", () => ({
  Calendar: () => null,
}));

// Mock ContentEditor (Tiptap needs real DOM)
vi.mock("@/features/editor", () => ({
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

// Mock api
const mockGetIssue = vi.hoisted(() => vi.fn());
const mockListTimeline = vi.hoisted(() => vi.fn());
const mockCreateComment = vi.hoisted(() => vi.fn());
const mockUpdateComment = vi.hoisted(() => vi.fn());
const mockDeleteComment = vi.hoisted(() => vi.fn());
const mockDeleteIssue = vi.hoisted(() => vi.fn());
const mockUpdateIssue = vi.hoisted(() => vi.fn());

vi.mock("@/shared/api", () => ({
  api: {
    getIssue: (...args: any[]) => mockGetIssue(...args),
    listTimeline: (...args: any[]) => mockListTimeline(...args),
    listComments: vi.fn().mockResolvedValue([]),
    createComment: (...args: any[]) => mockCreateComment(...args),
    updateComment: (...args: any[]) => mockUpdateComment(...args),
    deleteComment: (...args: any[]) => mockDeleteComment(...args),
    deleteIssue: (...args: any[]) => mockDeleteIssue(...args),
    updateIssue: (...args: any[]) => mockUpdateIssue(...args),
    listIssueSubscribers: vi.fn().mockResolvedValue([]),
    subscribeToIssue: vi.fn().mockResolvedValue(undefined),
    unsubscribeFromIssue: vi.fn().mockResolvedValue(undefined),
    getActiveTaskForIssue: vi.fn().mockResolvedValue({ task: null }),
    listTasksByIssue: vi.fn().mockResolvedValue([]),
    listTaskMessages: vi.fn().mockResolvedValue([]),
  },
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

// React 19 use(Promise) needs the promise to resolve within act + Suspense
async function renderPage(id = "issue-1") {
  let result: ReturnType<typeof render>;
  await act(async () => {
    result = render(
      <Suspense fallback={<div>Suspense loading...</div>}>
        <IssueDetailPage params={Promise.resolve({ id })} />
      </Suspense>,
    );
  });
  return result!;
}

describe("IssueDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders issue details after loading", async () => {
    mockGetIssue.mockResolvedValueOnce(mockIssue);
    mockListTimeline.mockResolvedValueOnce(mockTimeline);
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
    mockGetIssue.mockResolvedValueOnce(mockIssue);
    mockListTimeline.mockResolvedValueOnce(mockTimeline);
    await renderPage();

    await waitFor(() => {
      expect(screen.getByText("Properties")).toBeInTheDocument();
    });

    expect(screen.getByText("In Progress")).toBeInTheDocument();
    expect(screen.getByText("High")).toBeInTheDocument();
  });

  it("renders comments", async () => {
    mockGetIssue.mockResolvedValueOnce(mockIssue);
    mockListTimeline.mockResolvedValueOnce(mockTimeline);
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
    mockGetIssue.mockRejectedValue(new Error("Not found"));
    mockListTimeline.mockRejectedValue(new Error("Not found"));
    await renderPage("nonexistent-id");

    await waitFor(() => {
      expect(screen.getByText("This issue does not exist or has been deleted in this workspace.")).toBeInTheDocument();
    });
  });

  it("submits a new comment", async () => {
    mockGetIssue.mockResolvedValueOnce(mockIssue);
    mockListTimeline.mockResolvedValueOnce(mockTimeline);

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
    mockCreateComment.mockResolvedValueOnce(newComment);

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
      expect(mockCreateComment).toHaveBeenCalled();
      const [issueId, content] = mockCreateComment.mock.calls[0]!;
      expect(issueId).toBe("issue-1");
      expect(content).toBe("New test comment");
    });

    await waitFor(() => {
      expect(screen.getByText("New test comment")).toBeInTheDocument();
    });
  });

  it("renders breadcrumb navigation", async () => {
    mockGetIssue.mockResolvedValueOnce(mockIssue);
    mockListTimeline.mockResolvedValueOnce(mockTimeline);
    await renderPage();

    await waitFor(() => {
      expect(screen.getByText("Test WS")).toBeInTheDocument();
    });

    const wsLink = screen.getByText("Test WS");
    expect(wsLink.closest("a")).toHaveAttribute("href", "/issues");
  });
});
