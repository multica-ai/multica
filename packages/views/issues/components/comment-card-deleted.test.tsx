import { describe, expect, it, onTestFinished, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// #8296: deleting a comment keeps its replies. A comment deleted while it had
// replies arrives as a tombstone (`deleted_at` set, empty body) and renders as
// a placeholder with no actions. The cache rules live in
// packages/core/issues/comment-deletion.test.ts.

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

import { configStore } from "@multica/core/config";
import { CommentCard } from "./comment-card";

const DELETED_AT = "2026-09-11T08:00:00Z";

function comment(id: string, parentId: string | null, extra: Partial<TimelineEntry> = {}): TimelineEntry {
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
    created_at: `2026-09-11T07:0${id.length}:00Z`,
    updated_at: "2026-09-11T07:00:00Z",
    revision: 1,
    ...extra,
  };
}

const tombstone = (id: string, parentId: string | null) =>
  comment(id, parentId, { content: "", deleted_at: DELETED_AT });

function renderThread(root: TimelineEntry, replies: TimelineEntry[]) {
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
      />
    </QueryClientProvider>,
  );
}

const actionMenus = () => screen.queryAllByRole("button", { name: "Comment actions" });

describe("CommentCard — deleted comments", () => {
  it("renders a deleted reply as a placeholder and keeps the replies to it", () => {
    renderThread(comment("a", null), [tombstone("bb", "a"), comment("ccc", "bb")]);

    expect(screen.getByText("This comment was deleted")).toBeTruthy();
    expect(screen.getByText("body ccc")).toBeTruthy();
    // Root and the live reply keep their actions; the placeholder has none.
    expect(actionMenus()).toHaveLength(2);
  });

  it("renders a deleted root as a placeholder and keeps the thread open for replies", () => {
    renderThread(tombstone("a", null), [comment("bb", "a")]);

    expect(screen.getByText("This comment was deleted")).toBeTruthy();
    expect(screen.getByText("body bb")).toBeTruthy();
    expect(actionMenus()).toHaveLength(1);
    expect(screen.getByText("Leave a reply...")).toBeTruthy();
  });

  it("tells the user the replies are kept when the server keeps them", async () => {
    configStore.getState().setCommentDeleteKeepRepliesSupported(true);
    onTestFinished(() => configStore.getState().setCommentDeleteKeepRepliesSupported(false));
    renderThread(comment("a", null), [comment("bb", "a")]);

    fireEvent.click(actionMenus()[0]!);
    fireEvent.click(await screen.findByText("Delete"));

    expect(await screen.findByText(/Its replies stay in the thread/)).toBeTruthy();
  });

  // An older server deletes the replies with the comment; the copy must not
  // promise otherwise.
  it("warns that the replies go too when the server has not declared it keeps them", async () => {
    renderThread(comment("a", null), [comment("bb", "a")]);

    fireEvent.click(actionMenus()[0]!);
    fireEvent.click(await screen.findByText("Delete"));

    expect(await screen.findByText(/and all its replies will be permanently deleted/)).toBeTruthy();
  });
});
