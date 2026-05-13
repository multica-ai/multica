import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { TimelineEntry } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";
import { CommentCard } from "./comment-card";

const TEST_RESOURCES = { en: { common: enCommon, editor: enEditor, issues: enIssues } };
const previewAttachmentMarkdown = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Claude Agent",
    getActorInitials: () => "CA",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/issues/stores", () => ({
  useCommentCollapseStore: (selector: (state: { isCollapsed: () => boolean; toggle: () => void }) => unknown) =>
    selector({ isCollapsed: () => false, toggle: vi.fn() }),
  useCommentDraftStore: Object.assign(
    (selector?: any) => {
      const state = {
        drafts: {},
        getDraft: () => undefined,
        setDraft: vi.fn(),
        clearDraft: vi.fn(),
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        drafts: {},
        getDraft: () => undefined,
        setDraft: vi.fn(),
        clearDraft: vi.fn(),
      }),
    },
  ),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    previewAttachmentMarkdown,
  },
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "test",
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
  ContentEditor: () => <textarea data-testid="rich-text-editor" />,
  FileDropOverlay: () => null,
  copyMarkdown: vi.fn(),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  useDownloadAttachment: () => vi.fn(),
  useAttachmentPreview: () => ({
    tryOpen: vi.fn(() => true),
    modal: null,
  }),
  isPreviewable: () => false,
}));

vi.mock("./reply-input", () => ({
  ReplyInput: () => <div data-testid="reply-input" />,
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => <button type="button">Upload</button>,
}));

vi.mock("@multica/ui/components/common/quick-emoji-picker", () => ({
  QuickEmojiPicker: () => <button type="button">React</button>,
}));

vi.mock("@multica/ui/components/common/reaction-bar", () => ({
  ReactionBar: () => null,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/collapsible", () => ({
  Collapsible: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  CollapsibleTrigger: ({ children }: { children: ReactNode }) => <button type="button">{children}</button>,
  CollapsibleContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) => (open ? <div>{children}</div> : null),
  DialogClose: ({ children, render }: { children: ReactNode; render?: ReactNode }) =>
    render ? <button type="button">{children}</button> : <button type="button">{children}</button>,
  DialogContent: ({ children }: { children: ReactNode }) => <div role="dialog">{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
}));

function renderComment(entry: TimelineEntry) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CommentCard
        issueId="issue-1"
        entry={entry}
        replies={[]}
        currentUserId="user-1"
        onReply={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />
    </I18nProvider>,
  );
}

describe("CommentCard attachments", () => {
  beforeEach(() => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title\n\nGenerated markdown body");
  });

  it("previews standalone markdown attachments before the download action", async () => {
    const user = userEvent.setup();
    const entry = {
      id: "comment-1",
      type: "comment",
      actor_type: "agent",
      actor_id: "agent-1",
      content: "Generated a file.",
      created_at: "2026-05-07T08:00:00Z",
      attachments: [
        {
          id: "attachment-1",
          workspace_id: "ws-1",
          issue_id: "issue-1",
          comment_id: "comment-1",
          uploader_type: "agent",
          uploader_id: "agent-1",
          filename: "result.md",
          url: "https://cdn.example.com/result.md",
          download_url: "https://cdn.example.com/result.md?download=1",
          content_type: "text/markdown",
          size_bytes: 128,
          created_at: "2026-05-07T08:00:00Z",
          chat_session_id: null,
          chat_message_id: null,
        },
      ],
    } satisfies TimelineEntry;

    renderComment(entry);

    const previewButton = screen.getByRole("button", { name: "Preview result.md" });
    const downloadButton = screen.getByRole("button", { name: "Download" });
    expect(previewButton.compareDocumentPosition(downloadButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    await user.click(previewButton);

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith("https://cdn.example.com/result.md?download=1"),
    );
    expect(await screen.findByRole("dialog")).toHaveTextContent("Generated markdown body");
  });
});
