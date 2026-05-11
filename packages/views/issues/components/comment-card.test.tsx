import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {},
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

function renderCommentCard() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CommentCard
        issueId="issue-1"
        entry={entry}
        replies={[]}
        currentUserId="user-1"
        onReply={async () => {}}
        onEdit={async () => {}}
        onDelete={() => {}}
        onToggleReaction={() => {}}
      />
    </I18nProvider>,
  );
}

describe("CommentCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
});
