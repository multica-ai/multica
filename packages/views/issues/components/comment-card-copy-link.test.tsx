import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Per-comment copy link: the menu must offer a link to the single comment
// (`#comment-<id>`), not just the comment body text. The URL is built by
// `useIssueActions.copyCommentLink`; this suite pins that the card routes each
// row's own id to the handler and hides the item when no handler is wired.

vi.mock("@multica/core/api", () => ({
  api: { uploadFile: vi.fn() },
  dispatchReasonCode: () => undefined,
  errorCode: () => undefined,
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/acme/issues",
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada" }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../hooks/use-comment-trigger-preview", () => ({
  useCommentTriggerPreview: () => ({ agents: [], blocked: [] }),
}));

vi.mock("../../editor", async () => ({
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>("../../editor/use-upload-gate")),
  ...(await vi.importActual<typeof import("../../editor/use-lazy-editor")>("../../editor/use-lazy-editor")),
  ...(await vi.importActual<typeof import("../../editor/use-composer-submit")>("../../editor/use-composer-submit")),
  useEditorUpload: () => ({ uploadWithToast: vi.fn(), upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  Attachment: () => null,
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  ContentEditor: forwardRef(function MockContentEditor() {
    return <textarea data-testid="editor" />;
  }),
}));

import { CommentCard } from "./comment-card";

function comment(id: string, parentId: string | null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content: `body ${id}`,
    parent_id: parentId,
    comment_type: "comment",
    reactions: [],
    attachments: [],
    created_at: "2026-09-11T07:00:00Z",
    updated_at: "2026-09-11T07:00:00Z",
    revision: 1,
  };
}

function renderThread(
  root: TimelineEntry,
  replies: TimelineEntry[],
  onCopyLink?: (commentId: string) => void,
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        entry={root}
        replies={replies}
        currentUserId="user-1"
        onReply={vi.fn().mockResolvedValue(true)}
        onEdit={vi.fn().mockResolvedValue(undefined)}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
        onCopyLink={onCopyLink}
      />
    </QueryClientProvider>,
  );
}

const actionMenus = () => screen.queryAllByRole("button", { name: "Comment actions" });

describe("CommentCard — copy comment link", () => {
  it("copies the root comment's own deep link", async () => {
    const onCopyLink = vi.fn();
    renderThread(comment("root", null), [], onCopyLink);

    fireEvent.click(actionMenus()[0]!);
    fireEvent.click(await screen.findByText("Copy link"));

    expect(onCopyLink).toHaveBeenCalledWith("root");
  });

  it("copies a reply's own deep link, not the thread root's", async () => {
    const onCopyLink = vi.fn();
    renderThread(comment("root", null), [comment("reply", "root")], onCopyLink);

    // Menus render in order: root first, then the reply.
    fireEvent.click(actionMenus()[1]!);
    fireEvent.click(await screen.findByText("Copy link"));

    expect(onCopyLink).toHaveBeenCalledWith("reply");
  });

  it("omits the item when no handler is wired", async () => {
    renderThread(comment("root", null), []);

    fireEvent.click(actionMenus()[0]!);

    await screen.findByText("Copy");
    expect(screen.queryByText("Copy link")).toBeNull();
  });
});