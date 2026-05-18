import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockClipboardWriteText = vi.hoisted(() => vi.fn());
const mockGetShareableUrl = vi.hoisted(() =>
  vi.fn((path: string) => `https://share.test${path}`),
);
const mockCopyMarkdown = vi.hoisted(() => vi.fn());
const mockRetryAgentComment = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Test User",
  }),
}));

vi.mock("@multica/core/utils", () => ({
  timeAgo: () => "1m ago",
}));

vi.mock("@multica/core/issues/stores", () => ({
  useCommentCollapseStore: (selector?: (state: {
    isCollapsed: (issueId: string, commentId: string) => boolean;
    toggle: (issueId: string, commentId: string) => void;
  }) => unknown) => {
    const state = {
      isCollapsed: () => false,
      toggle: () => {},
    };
    return selector ? selector(state) : state;
  },
  useCommentDraftStore: Object.assign(
    (selector?: (state: {
      getDraft: () => string | undefined;
      setDraft: () => void;
      clearDraft: () => void;
    }) => unknown) => {
      const state = {
        getDraft: () => undefined,
        setDraft: () => {},
        clearDraft: () => {},
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        getDraft: () => undefined,
        setDraft: () => {},
        clearDraft: () => {},
      }),
    },
  ),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";
const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    retryAgentComment: mockRetryAgentComment,
  },
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test/issues/issue-1",
    searchParams: new URLSearchParams(),
    getShareableUrl: mockGetShareableUrl,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="avatar" />,
}));

vi.mock("@multica/ui/components/common/reaction-bar", () => ({
  ReactionBar: () => null,
}));

vi.mock("@multica/ui/components/common/quick-emoji-picker", () => ({
  QuickEmojiPicker: () => null,
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => null,
}));

vi.mock("../../editor", () => ({
  copyMarkdown: mockCopyMarkdown,
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  ContentEditor: () => null,
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  useDownloadAttachment: () => vi.fn(),
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: vi.fn(), modal: null }),
}));

vi.mock("./reply-input", () => ({
  ReplyInput: () => null,
}));

vi.mock("@multica/ui/components/ui/card", () => ({
  Card: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, onClick, className }: { children: React.ReactNode; onClick?: () => void; className?: string }) => (
    <button type="button" onClick={onClick} className={className}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render?: React.ReactNode }) => render,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render?: React.ReactNode }) => render,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogAction: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  AlertDialogCancel: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
  AlertDialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/collapsible", () => ({
  Collapsible: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CollapsibleTrigger: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <button type="button" className={className}>
      {children}
    </button>
  ),
  CollapsibleContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import { CommentCard } from "./comment-card";

const entry: TimelineEntry = {
  type: "comment",
  id: "comment-1",
  actor_type: "member",
  actor_id: "user-1",
  content: "Started working on this",
  parent_id: null,
  created_at: "2026-01-16T00:00:00Z",
  updated_at: "2026-01-16T00:00:00Z",
  comment_type: "comment",
};

function renderCommentCard({
  cardEntry = entry,
  commentById = new Map<string, TimelineEntry>([[cardEntry.id, cardEntry]]),
  agents = [],
  issueOpen = true,
  currentUserId = "user-1",
}: {
  cardEntry?: TimelineEntry;
  commentById?: Map<string, TimelineEntry>;
  agents?: Array<{ id: string; owner_id?: string | null }>;
  issueOpen?: boolean;
  currentUserId?: string;
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <CommentCard
          issueId="issue-1"
          entry={cardEntry}
          replies={[]}
          commentById={commentById}
          agents={agents as never[]}
          issueOpen={issueOpen}
          currentUserId={currentUserId}
          onReply={async () => {}}
          onEdit={async () => {}}
          onDelete={() => {}}
          onToggleReaction={() => {}}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("CommentCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockRetryAgentComment.mockResolvedValue(undefined);
    Object.assign(navigator, {
      clipboard: {
        writeText: mockClipboardWriteText,
      },
    });
  });

  it("copies a shareable comment permalink from the menu", async () => {
    renderCommentCard();

    await userEvent.click(screen.getByText("Copy link"));

    expect(mockGetShareableUrl).toHaveBeenCalledWith(
      "/test/issues/issue-1?comment=comment-1",
    );
    expect(mockClipboardWriteText).toHaveBeenCalledWith(
      "https://share.test/test/issues/issue-1?comment=comment-1",
    );
    expect(mockToastSuccess).toHaveBeenCalled();
  });

  it("shows retry for a task-run system comment and calls the retry API", async () => {
    const systemComment: TimelineEntry = {
      ...entry,
      id: "agent-system-1",
      actor_type: "agent",
      actor_id: "agent-1",
      comment_type: "system",
    };

    renderCommentCard({
      cardEntry: systemComment,
      commentById: new Map([[systemComment.id, systemComment]]),
      agents: [{ id: "agent-1", owner_id: "user-1" }],
    });

    await userEvent.click(screen.getByText("Retry"));

    await waitFor(() => {
      expect(mockRetryAgentComment).toHaveBeenCalledWith("agent-system-1");
    });
    expect(mockToastSuccess).toHaveBeenCalledWith("Agent run queued");
  });

  it("shows retry for any agent comment (not just system)", async () => {
    const agentComment: TimelineEntry = {
      ...entry,
      id: "agent-comment-1",
      actor_type: "agent",
      actor_id: "agent-1",
      comment_type: "comment",
    };

    renderCommentCard({
      cardEntry: agentComment,
      commentById: new Map([[agentComment.id, agentComment]]),
      agents: [{ id: "agent-1", owner_id: "user-1" }],
    });

    await userEvent.click(screen.getByText("Retry"));

    await waitFor(() => {
      expect(mockRetryAgentComment).toHaveBeenCalledWith("agent-comment-1");
    });
  });

  it("does not show retry for a member comment", () => {
    renderCommentCard();

    expect(screen.queryByText("Retry")).toBeNull();
  });

  it("shows retry for an agent reply when the current user authored the trigger comment", () => {
    const memberThreadRoot: TimelineEntry = {
      ...entry,
      id: "member-root-1",
      actor_type: "member",
      actor_id: "user-1",
      comment_type: "comment",
      parent_id: null,
    };
    const agentReply: TimelineEntry = {
      ...entry,
      id: "agent-reply-1",
      actor_type: "agent",
      actor_id: "agent-2",
      comment_type: "comment",
      parent_id: "member-root-1",
    };

    renderCommentCard({
      cardEntry: agentReply,
      commentById: new Map([
        [memberThreadRoot.id, memberThreadRoot],
        [agentReply.id, agentReply],
      ]),
      agents: [{ id: "agent-2", owner_id: "someone-else" }],
    });

    expect(screen.getByText("Retry")).toBeTruthy();
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: vi.fn(),
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
import { AttachmentList } from "./comment-card";
function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());
describe("AttachmentList — standalone HTML attachment routes through AttachmentBlock", () => {
  // Regression pin for comment-card.tsx:152. This is the entry point
  // MUL-2330 originally regressed on: standalone HTML attachments (not
  // referenced inline in the markdown body) MUST render through
  // <AttachmentBlock> so the html+attachmentId dispatch fires. Reverting to
  // <AttachmentCard> here re-introduces the "report.html shows as a bare
  // file card row instead of the rendered chart" bug.
  it("renders an iframe (no file-card chrome) for a standalone HTML attachment", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    const attachment = {
      id: "att-1",
      url: "/uploads/report.html",
      filename: "report.html",
      content_type: "text/html",
      size_bytes: 0,
    } as any;
    renderWithQuery(<AttachmentList attachments={[attachment]} content="" />);
    const frame = await waitFor(() => {
      const f = document.querySelector("iframe") as HTMLIFrameElement | null;
      expect(f).toBeTruthy();
      return f!;
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<p>chart</p>");
    // AttachmentCard chrome would render the filename as visible <p> text;
    // HtmlAttachmentPreview replaces the row entirely.
    expect(screen.queryByText("report.html")).toBeNull();
  });
});
