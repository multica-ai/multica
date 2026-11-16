import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Comment "more actions" menu layout, in four groups: respond (Reply, Add
// reaction, Edit), act on the thread (Resolve, Create sub-issue), share (Copy,
// Copy link), and Delete alone at the bottom. Edit and Delete only show on
// comments the viewer may change.

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
import { findContextMenuScope } from "../../common/context-menu-scope";

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
  it("groups the author's root menu as respond, act, share, delete", async () => {
    renderThread(comment("root", null), []);

    expect(await openMenuLayout(0)).toEqual([
      "Reply",
      "Add reaction",
      "Edit",
      SEPARATOR,
      "Resolve thread",
      "Create sub-issue from here",
      SEPARATOR,
      "Copy",
      "Copy link",
      SEPARATOR,
      "Delete",
    ]);
  });

  it("offers Resolve thread with comment on the author's reply", async () => {
    renderThread(comment("root", null), [comment("reply", "root")]);

    // Menus render in order: root first, then the reply.
    expect(await openMenuLayout(1)).toEqual([
      "Reply",
      "Add reaction",
      "Edit",
      SEPARATOR,
      "Resolve thread with comment",
      "Create sub-issue from here",
      SEPARATOR,
      "Copy",
      "Copy link",
      SEPARATOR,
      "Delete",
    ]);
  });

  it("offers no Edit or Delete on someone else's comment", async () => {
    renderThread(comment("root", null, agentAuthor), []);

    expect(await openMenuLayout(0)).toEqual([
      "Reply",
      "Add reaction",
      SEPARATOR,
      "Resolve thread",
      "Create sub-issue from here",
      SEPARATOR,
      "Copy",
      "Copy link",
    ]);
  });

  it("keeps Create sub-issue when the thread cannot be resolved", async () => {
    renderThread(comment("root", null, agentAuthor), [], { resolvable: false });

    expect(await openMenuLayout(0)).toEqual([
      "Reply",
      "Add reaction",
      SEPARATOR,
      "Create sub-issue from here",
      SEPARATOR,
      "Copy",
      "Copy link",
    ]);
  });
});

describe("CommentCard — right-click", () => {
  function menuItems(menu: HTMLElement) {
    return Array.from(menu.querySelectorAll('[role="menuitem"]'), (el) => el.textContent?.trim() ?? "");
  }

  it("opens the same menu as the ⋯ button at the cursor", async () => {
    renderThread(comment("root", null), []);

    fireEvent.contextMenu(screen.getByText("body root"), { clientX: 40, clientY: 60 });
    const menu = await screen.findByRole("menu");
    expect(menuItems(menu)).toEqual([
      "Reply",
      "Add reaction",
      "Edit",
      "Resolve thread",
      "Create sub-issue from here",
      "Copy",
      "Copy link",
      "Delete",
    ]);
  });

  it("leaves a right-click something inside already handled", () => {
    renderThread(comment("root", null), []);

    const body = screen.getByText("body root");
    body.addEventListener("contextmenu", (event) => event.preventDefault());
    fireEvent.contextMenu(body);
    expect(screen.queryByRole("menu")).toBeNull();
  });
});

describe("CommentCard — menus opened inside a comment", () => {
  it("offers the comment's actions as the submenu and adds selection entries", () => {
    renderThread(comment("root", null, agentAuthor), [comment("reply", "root")]);

    const scope = findContextMenuScope(screen.getByText("body reply"));
    expect(scope.submenu?.label).toBe("Comment actions");
    expect(scope.submenu?.groups.flat().map((action) => action.label)).toEqual([
      "Reply",
      "Add reaction",
      "Edit",
      "Resolve thread with comment",
      "Create sub-issue from here",
      "Copy",
      "Copy link",
      "Delete",
    ]);
    expect(scope.selectionActions("quoted").map((action) => action.label)).toEqual([
      "Add to reply",
      "Create sub-issue from selection",
    ]);
  });
});
