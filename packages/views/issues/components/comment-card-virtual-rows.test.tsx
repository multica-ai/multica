import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Which threads mount their replies on demand (MUL-9409). The timeline's
// Virtuoso sees a thread as one item, so a thread of a few very long replies
// used to mount all of them at once; it now virtualizes by content as well as
// by row count.

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
import { resetRememberedThreadRowHeights } from "./virtual-thread-row";

beforeEach(() => {
  // Nothing intersects: every virtualized row stays a placeholder.
  vi.stubGlobal("IntersectionObserver", class {
    observe() {}
    disconnect() {}
  });
});
afterEach(() => { vi.unstubAllGlobals(); resetRememberedThreadRowHeights(); });

function comment(id: string, parentId: string | null, content: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "agent",
    actor_id: "agent-1",
    content,
    parent_id: parentId,
    comment_type: "comment",
    reactions: [],
    attachments: [],
    created_at: "2026-09-11T07:00:00Z",
    updated_at: "2026-09-11T07:00:00Z",
    revision: 1,
  };
}

function renderThread(replies: TimelineEntry[], replyScrollParent: HTMLElement | null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        replyScrollParent={replyScrollParent}
        entry={comment("root", null, "root body")}
        replies={replies}
        currentUserId="user-1"
        onReply={vi.fn().mockResolvedValue(true)}
        onEdit={vi.fn().mockResolvedValue(undefined)}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
        onCopyLink={vi.fn()}
        onCreateSubIssue={vi.fn()}
      />
    </QueryClientProvider>,
  );
}

const longReplies = () => [1, 2, 3].map((n) => comment(`reply-${n}`, "root", `report ${n} ${"x".repeat(5_000)}`));

describe("CommentCard — reply virtualization", () => {
  it("mounts a short thread of long replies on demand", () => {
    const { container } = renderThread(longReplies(), document.body);
    const rows = container.querySelectorAll("[data-virtual-thread-row]");
    expect(rows).toHaveLength(3);
    expect([...rows].every((row) => row.getAttribute("data-virtual-thread-mounted") === "false")).toBe(true);
    expect(container.querySelector("#comment-reply-2")).not.toBeNull();
    expect(screen.queryByText(/^report 2/)).toBeNull();
  });

  it("keeps a short thread of short replies inline", () => {
    const { container } = renderThread([1, 2, 3].map((n) => comment(`reply-${n}`, "root", `short ${n}`)), document.body);
    expect(container.querySelector("[data-virtual-thread-row]")).toBeNull();
    expect(screen.getByText("short 2")).toBeInTheDocument();
  });

  it("renders every reply without a scroll parent, for in-page find", () => {
    const { container } = renderThread(longReplies(), null);
    expect(container.querySelector("[data-virtual-thread-row]")).toBeNull();
    expect(screen.getByText(/^report 2/)).toBeInTheDocument();
  });
});
