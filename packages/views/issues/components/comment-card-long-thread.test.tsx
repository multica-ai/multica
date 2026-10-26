import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

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

function comment(id: string, parentId: string | null, minute: number): TimelineEntry {
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
    created_at: `2026-10-23T07:${String(minute).padStart(2, "0")}:00Z`,
    updated_at: "2026-10-23T07:00:00Z",
    revision: 1,
  };
}

const root = comment("root", null, 0);
const replies = Array.from({ length: 10 }, (_, i) => comment(`r${i + 1}`, "root", i + 1));

function renderThread(props: { foldLongThread?: boolean; onExpandLongThread?: (id: string) => void }) {
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
        {...props}
      />
    </QueryClientProvider>,
  );
}

const shownReplies = () =>
  replies.filter((reply) => document.getElementById(`comment-${reply.id}`)).map((reply) => reply.id);

describe("CommentCard — long thread fold", () => {
  it("mounts the first reply and the latest three, with the rest behind a bar", () => {
    const onExpand = vi.fn();
    renderThread({ foldLongThread: true, onExpandLongThread: onExpand });

    expect(shownReplies()).toEqual(["r1", "r8", "r9", "r10"]);
    fireEvent.click(screen.getByText("6 comments from Ada"));
    expect(onExpand).toHaveBeenCalledWith("root");
  });

  it("mounts every reply when the parent does not ask for the fold", () => {
    renderThread({ foldLongThread: false, onExpandLongThread: vi.fn() });

    expect(shownReplies()).toHaveLength(10);
    expect(screen.queryByText(/comments from Ada/)).toBeNull();
  });
});
