import { describe, expect, it, vi } from "vitest";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// MUL-9426: the timeline rebuilds an agent reply's `commentProps` on every
// render. A sidebar toggle re-renders the visible rows each animation frame,
// and every agent reply on screen used to re-render with them.

const runStateCalls = vi.hoisted(() => ({ count: 0 }));

vi.mock("./inline-comment-run", async () => {
  const actual = await vi.importActual<typeof import("./inline-comment-run")>("./inline-comment-run");
  return {
    ...actual,
    // The run's own activity block is not under test and needs a workspace.
    InlineCommentRun: () => null,
    PlacedInlineCommentRun: () => null,
    useInlineCommentRunState: () => {
      runStateCalls.count += 1;
      return actual.useInlineCommentRunState();
    },
  };
});

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

// The composer asks whether the viewer is a guest (MUL-9420); these tests
// render as a member without an auth store.
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useIsGuest: () => false,
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

import { AgentRunComment } from "./comment-card";

const reply: TimelineEntry = {
  type: "comment",
  id: "reply-1",
  actor_type: "agent",
  actor_id: "agent-1",
  content: "Agent report",
  parent_id: null,
  comment_type: "comment",
  reactions: [],
  attachments: [],
  created_at: "2026-09-11T07:00:00Z",
  updated_at: "2026-09-11T07:00:00Z",
  revision: 1,
};

const run = {
  task: {
    id: "task-1",
    agent_id: "agent-1",
    issue_id: "issue-1",
    status: "completed",
    created_at: "2026-09-11T06:59:00Z",
    started_at: "2026-09-11T06:59:00Z",
    completed_at: "2026-09-11T07:00:00Z",
  } as AgentTask,
  commentId: reply.id,
  hasReply: true,
};

const handlers = {
  onReply: vi.fn().mockResolvedValue(true),
  onEdit: vi.fn().mockResolvedValue(undefined),
  onDelete: vi.fn(),
  onToggleReaction: vi.fn(),
  onCopyLink: vi.fn(),
  onCreateSubIssue: vi.fn(),
};
const replies: TimelineEntry[] = [];

function renderRun() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // A fresh `commentProps` object every time, as the timeline's renderItem builds it.
  const view = (entry: TimelineEntry) => (
    <QueryClientProvider client={qc}>
      <AgentRunComment run={run} standalone commentProps={{
        issueId: "issue-1", entry, replies, currentUserId: "user-1", ...handlers,
      }} />
    </QueryClientProvider>
  );
  const result = renderWithI18n(view(reply));
  return { ...result, update: (entry: TimelineEntry) => result.rerender(view(entry)) };
}

describe("AgentRunComment memoization", () => {
  it("skips re-rendering when the rebuilt commentProps carry the same values", () => {
    const { update, getByText } = renderRun();
    expect(getByText("Agent report")).toBeInTheDocument();
    const rendered = runStateCalls.count;

    update(reply);
    update(reply);
    expect(runStateCalls.count).toBe(rendered);
  });

  it("re-renders when a commentProps value changes", () => {
    const { update, getByText } = renderRun();
    const rendered = runStateCalls.count;

    update({ ...reply, content: "Edited report" });
    expect(runStateCalls.count).toBe(rendered + 1);
    expect(getByText("Edited report")).toBeInTheDocument();
  });
});
