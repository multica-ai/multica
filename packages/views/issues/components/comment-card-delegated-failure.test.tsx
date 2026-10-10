import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { forwardRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The platform writes its "delegated task failed" signal as one English
// sentence for the coordinating agent. People see a localized line, with the
// original kept under "Technical details". Parsing is covered in
// delegated-failure-comment.test.ts; this file covers the rendering.

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

const SIGNAL =
  "Delegated task `01a11236-22a3-7b36-b9e6-88df82d742a0` ended in a final failure (`idle_watchdog`) and no automatic retry is pending. " +
  "Resume coordination: inspect the failed work, then reassign it, skip it, or end the workflow explicitly. " +
  "Source coordinator task: `01a1122b-c71f-751c-a930-6f197a99d3b8`.";

function entry(extra: Partial<TimelineEntry>): TimelineEntry {
  return {
    type: "comment",
    id: "c-1",
    actor_type: "system",
    actor_id: "",
    content: SIGNAL,
    parent_id: null,
    comment_type: "progress_update",
    source_task_id: "01a1122b-c71f-751c-a930-6f197a99d3b8",
    reactions: [],
    attachments: [],
    created_at: "2026-10-06T17:00:00Z",
    updated_at: "2026-10-06T17:00:00Z",
    revision: 1,
    ...extra,
  };
}

function renderCard(root: TimelineEntry) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        entry={root}
        replies={[]}
        currentUserId="user-1"
        onReply={vi.fn().mockResolvedValue(true)}
        onEdit={vi.fn().mockResolvedValue(undefined)}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />
    </QueryClientProvider>,
  );
}

describe("CommentCard — delegated failure signal", () => {
  it("shows a localized line with the translated reason, and keeps the original as detail", () => {
    renderCard(entry({}));
    expect(
      screen.getByText(
        "A delegated run failed for good: Agent stopped after inactivity. No automatic retry is pending, so the coordinating agent takes the work back.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Technical details")).toBeInTheDocument();
    expect(screen.getByText(SIGNAL)).toBeInTheDocument();
  });

  it("renders an ordinary comment unchanged", () => {
    renderCard(entry({ actor_type: "member", actor_id: "user-1", comment_type: "comment", source_task_id: null, content: "Looks good" }));
    expect(screen.getByText("Looks good")).toBeInTheDocument();
    expect(screen.queryByText("Technical details")).not.toBeInTheDocument();
  });
});
