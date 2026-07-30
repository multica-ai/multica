// @vitest-environment jsdom

// FIR-4139 — the comment composer must float next to the text you marked
// instead of sitting at the bottom of the comments rail.

import "@testing-library/jest-dom/vitest";

import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/api", () => ({ ApiError: class extends Error {} }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: () => [] }),
}));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => false,
}));
vi.mock("./note-mention-scope", () => ({
  useEnsureNoteMentionScope: () => undefined,
}));
vi.mock("./note-couple-and-send", () => ({ NoteCoupleAndSend: () => null }));
vi.mock("./note-suggest-issue-reference", () => ({
  NoteSuggestIssueReference: () => null,
}));
vi.mock("./note-comment-create-issue-dialog", () => ({
  NoteCommentCreateIssueDialog: () => null,
}));
vi.mock("./note-comment-issue-link", () => ({
  NoteCommentIssueLink: () => null,
}));
vi.mock("./use-comment-anchors", () => ({ useCommentAnchors: () => undefined }));

vi.mock("../core", () => ({
  useNoteComments: () => ({ data: [] }),
  useCreateNoteComment: () => ({ mutate: vi.fn(), isPending: false }),
  useResolveNoteComment: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteNoteComment: () => ({ mutate: vi.fn(), isPending: false }),
  useDecideNoteSuggestion: () => ({ mutate: vi.fn(), isPending: false }),
  useSendNoteComments: () => ({ mutate: vi.fn(), isPending: false }),
  useNoteReferences: () => ({ data: [] }),
  noteDetailOptions: () => ({ queryKey: ["note"], queryFn: () => null }),
  isUnsentToAgent: () => false,
  parseIssueMentions: () => [],
  buildThreads: () => [],
  noteKeys: { all: () => ["notes"] },
  extractMemberMentions: () => [],
  checkMentionAccess: () => Promise.resolve([]),
  grantMentionAccess: () => Promise.resolve(),
}));

vi.mock("@multica/views/editor", () => ({
  ContentEditor: React.forwardRef(
    (
      props: { defaultValue?: string; placeholder?: string },
      ref: React.ForwardedRef<{
        getMarkdown: () => string;
        clearContent: () => void;
      }>,
    ) => {
      React.useImperativeHandle(ref, () => ({
        getMarkdown: () => props.defaultValue ?? "",
        clearContent: () => undefined,
      }));
      return <textarea readOnly placeholder={props.placeholder} />;
    },
  ),
  ReadonlyContent: () => null,
}));

import { NoteCommentsPanel } from "./note-comments-panel";
import { DRAFT_ANCHOR_ID } from "./comment-anchor-plugin";

function renderPanel(draftQuote: string | null) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <NoteCommentsPanel
        noteId="note-1"
        noteBody="the quick brown fox"
        isOwner
        draftQuote={draftQuote}
        activeAnchorId={null}
        onClearDraft={() => undefined}
        onSelectThread={() => undefined}
        onClose={() => undefined}
      />
    </QueryClientProvider>,
  );
}

// The marked span painted by comment-anchor-plugin — what the floating
// composer anchors to.
function paintDraftAnchor(): { getRect: ReturnType<typeof vi.fn> } {
  const span = document.createElement("span");
  span.setAttribute("data-anchor-id", DRAFT_ANCHOR_ID);
  span.textContent = "quick brown";
  const getRect = vi.fn(() => new DOMRect(120, 240, 90, 18));
  span.getBoundingClientRect = getRect;
  document.body.appendChild(span);
  return { getRect };
}

beforeEach(() => {
  // Desktop viewport — useIsMobile reads matchMedia + innerWidth.
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  document.body.querySelectorAll("[data-anchor-id]").forEach((n) => n.remove());
});

describe("note comment composer placement", () => {
  it("floats the composer and anchors it to the marked text", async () => {
    const { getRect } = paintDraftAnchor();
    renderPanel("quick brown");

    const card = await screen.findByTestId("floating-comment-composer");
    expect(card).toHaveStyle({ position: "fixed" });
    // The marked span drives the position — not the rail, not the viewport.
    await waitFor(() => expect(getRect).toHaveBeenCalled());
    // The quote stays visible in the floating card.
    expect(within(card).getByText(/quick brown/)).toBeInTheDocument();
  });

  it("keeps the composer docked when no text is marked", () => {
    renderPanel(null);

    expect(
      screen.queryByTestId("floating-comment-composer"),
    ).not.toBeInTheDocument();
    expect(
      screen.getByPlaceholderText(/Write a comment/),
    ).toBeInTheDocument();
  });

  it("keeps the composer docked on mobile", () => {
    window.innerWidth = 500;
    paintDraftAnchor();
    renderPanel("quick brown");

    expect(
      screen.queryByTestId("floating-comment-composer"),
    ).not.toBeInTheDocument();
    window.innerWidth = 1024;
  });
});
