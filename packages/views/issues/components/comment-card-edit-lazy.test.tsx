// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const triggerPreviewMock = vi.hoisted(() =>
  vi.fn((_args?: { editingCommentId?: string }) => ({ agents: [], blocked: [], hasAllMembersMention: false })),
);

vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useIsGuest: () => false,
}));
vi.mock("@multica/core/api", () => ({
  api: { uploadFile: vi.fn() },
  dispatchReasonCode: () => undefined,
  errorCode: () => undefined,
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada" }),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/acme/issues",
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));
vi.mock("../hooks/use-comment-trigger-preview", () => ({
  useCommentTriggerPreview: triggerPreviewMock,
}));
vi.mock("../hooks/use-recipient-actions", () => ({
  useRecipientActions: () => ({
    recipients: [],
    routing: { suppressAgentIds: [], steerAgentIds: [] },
    setAction: vi.fn(),
    reset: vi.fn(),
  }),
}));
vi.mock("./reply-input", () => ({
  ReplyInput: () => <div data-testid="reply-input" />,
}));
vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  ContentEditor: ({ defaultValue }: { defaultValue?: string }) => (
    <textarea aria-label="Edit comment editor" defaultValue={defaultValue} />
  ),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  Attachment: () => null,
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useUploadGate: () => ({ uploading: false, onUploadingChange: vi.fn(), isUploading: () => false }),
  useComposerSubmit: () => ({ submitting: false, submit: vi.fn() }),
}));

import { CommentCard } from "./comment-card";

function makeComment(id: string, parentId?: string): TimelineEntry {
  return {
    id,
    type: "comment",
    issue_id: "issue-1",
    actor_type: "member",
    actor_id: "user-1",
    actor_name: "Ada",
    content: `Body of ${id}`,
    parent_id: parentId ?? null,
    created_at: "2026-09-07T00:00:00Z",
    updated_at: "2026-09-07T00:00:00Z",
    reactions: [],
    attachments: [],
    revision: 1,
  } as unknown as TimelineEntry;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("CommentCard lazy edit form", () => {
  it("does not mount edit hooks for read-only root or replies until Edit is clicked", async () => {
    const client = new QueryClient();
    const root = makeComment("root-1");
    const replies = [makeComment("reply-1", "root-1"), makeComment("reply-2", "root-1")];

    renderWithI18n(
      <QueryClientProvider client={client}>
        <CommentCard
          issueId="issue-1"
          entry={root}
          replies={replies}
          currentUserId="user-1"
          onReply={vi.fn(async () => true)}
          onEdit={vi.fn(async () => {})}
          onDelete={vi.fn()}
          onToggleReaction={vi.fn()}
        />
      </QueryClientProvider>,
    );

    expect(screen.getByText("Body of root-1")).toBeInTheDocument();
    expect(screen.getByText("Body of reply-1")).toBeInTheDocument();
    expect(screen.getByText("Body of reply-2")).toBeInTheDocument();

    const editCallsBefore = triggerPreviewMock.mock.calls.filter(
      ([args]) => Boolean(args?.editingCommentId),
    );
    expect(editCallsBefore).toHaveLength(0);

    // Open the actions menu on the first reply and start editing it.
    const actionButtons = screen.getAllByRole("button", { name: /comment actions/i });
    fireEvent.click(actionButtons[1]!);
    const editItem = await screen.findByRole("menuitem", { name: /^edit$/i });
    fireEvent.click(editItem);

    expect(await screen.findByLabelText("Edit comment editor")).toBeInTheDocument();
    const editCallsAfter = triggerPreviewMock.mock.calls.filter(
      ([args]) => args?.editingCommentId === "reply-1",
    );
    expect(editCallsAfter.length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    expect(screen.queryByLabelText("Edit comment editor")).not.toBeInTheDocument();
  });
});
