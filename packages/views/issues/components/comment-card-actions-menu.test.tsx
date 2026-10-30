import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Comment "more actions" menu layout, in three groups: act on the comment
// (Edit, then Resolve), take it elsewhere (Copy, Copy link, Create sub-issue),
// and Delete alone at the bottom. Edit only shows on comments the viewer may
// edit, so on anyone else's comment Resolve leads the menu.

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

function comment(
  id: string,
  parentId: string | null,
  author: Pick<TimelineEntry, "actor_type" | "actor_id"> = { actor_type: "member", actor_id: "user-1" },
): TimelineEntry {
  return {
    type: "comment",
    id,
    ...author,
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
  { resolvable = true }: { resolvable?: boolean } = {},
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
        onCopyLink={vi.fn()}
        onCreateSubIssue={vi.fn()}
        onResolveToggle={resolvable ? vi.fn() : undefined}
      />
    </QueryClientProvider>,
  );
}

const SEPARATOR = "---";

// Menu items and separators in document order, separators as "---".
async function openMenuLayout(index: number): Promise<string[]> {
  fireEvent.click(screen.getAllByRole("button", { name: "Comment actions" })[index]!);
  const menu = await screen.findByRole("menu");
  return Array.from(
    menu.querySelectorAll('[role="menuitem"], [data-slot="dropdown-menu-separator"]'),
    (el) => (el.getAttribute("data-slot") === "dropdown-menu-separator" ? SEPARATOR : el.textContent?.trim() ?? ""),
  );
}

const agentAuthor = { actor_type: "agent", actor_id: "agent-1" } as const;

describe("CommentCard — actions menu layout", () => {
  it("groups the author's root menu as edit/resolve, copy/derive, delete", async () => {
    renderThread(comment("root", null), []);

    expect(await openMenuLayout(0)).toEqual([
      "Edit",
      "Resolve thread",
      SEPARATOR,
      "Copy",
      "Copy link",
      "Create sub-issue from here",
      SEPARATOR,
      "Delete",
    ]);
  });

  it("offers Resolve thread with comment on the author's reply", async () => {
    renderThread(comment("root", null), [comment("reply", "root")]);

    // Menus render in order: root first, then the reply.
    expect(await openMenuLayout(1)).toEqual([
      "Edit",
      "Resolve thread with comment",
      SEPARATOR,
      "Copy",
      "Copy link",
      "Create sub-issue from here",
      SEPARATOR,
      "Delete",
    ]);
  });

  it("leads with Resolve on someone else's comment", async () => {
    renderThread(comment("root", null, agentAuthor), []);

    expect(await openMenuLayout(0)).toEqual([
      "Resolve thread",
      SEPARATOR,
      "Copy",
      "Copy link",
      "Create sub-issue from here",
    ]);
  });

  it("drops the leading separator when there is nothing to edit or resolve", async () => {
    renderThread(comment("root", null, agentAuthor), [], { resolvable: false });

    expect(await openMenuLayout(0)).toEqual(["Copy", "Copy link", "Create sub-issue from here"]);
  });
});
