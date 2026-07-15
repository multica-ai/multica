import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { forwardRef, useEffect, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { renderWithI18n } from "../../test/i18n";

const uploadWithToast = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {},
  dispatchReasonCode: () => undefined,
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

// The trigger-preview chips have their own suite; inert here so this file
// stays about the edit gate.
vi.mock("../hooks/use-comment-trigger-preview", () => ({
  useCommentTriggerPreview: () => ({ agents: [], blocked: [] }),
}));

vi.mock("../../editor", async () => ({
  // Real submit gate (pure React) driven by the mock editor below.
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>(
    "../../editor/use-upload-gate",
  )),
  // The card nests a ReplyInput, which is readonly-first — real controller.
  ...(await vi.importActual<typeof import("../../editor/use-lazy-editor")>(
    "../../editor/use-lazy-editor",
  )),
  useEditorUpload: () => ({ uploadWithToast, upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  Attachment: () => null,
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      onUploadFile,
      onUploadingChange,
      onSubmit,
      placeholder,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      onUploadFile?: (file: File) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      onSubmit?: () => void;
      placeholder?: string;
    },
    ref: Ref<unknown>,
  ) {
    const valueRef = useRef(defaultValue ?? "");
    // Mirrors the real editor's `uploading` node attrs — see the sibling
    // composer suite for the same stand-in.
    const inFlightRef = useRef(0);
    // Mirrors the real editor publishing its current answer on subscribe: a
    // fresh instance owns no pending upload, so it reports "not uploading".
    useEffect(() => {
      onUploadingChange?.(inFlightRef.current > 0);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => { valueRef.current = ""; },
      focus: () => {},
      blur: () => {},
      uploadFile: async (file: File) => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          const result = await onUploadFile?.(file);
          if (!result) return;
          valueRef.current = `${valueRef.current}\n${result.url}`.trim();
          onUpdate?.(valueRef.current);
        } finally {
          inFlightRef.current -= 1;
          if (inFlightRef.current === 0) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => inFlightRef.current > 0,
    }));
    return (
      <textarea
        data-testid="editor"
        defaultValue={defaultValue}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") onSubmit?.();
        }}
      />
    );
  }),
}));

import { CommentCard } from "./comment-card";

const entry: TimelineEntry = {
  id: "comment-1",
  issue_id: "issue-1",
  parent_id: null,
  // `actor_*` is what the own-comment / can-edit rule reads.
  actor_type: "member",
  actor_id: "user-1",
  content: "Original body",
  type: "comment",
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
  attachments: [],
  reactions: [],
} as unknown as TimelineEntry;

function renderCard(onEdit = vi.fn().mockResolvedValue(undefined)) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        entry={entry}
        replies={[]}
        currentUserId="user-1"
        onReply={vi.fn().mockResolvedValue(true)}
        onEdit={onEdit}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />
    </QueryClientProvider>,
  );
  return { ...view, onEdit };
}

/** Open the row's ⋯ menu and click Edit. The trigger is icon-only with no
 *  accessible name, so it is addressed by its menu popup role. */
async function startEditing() {
  const trigger = document.querySelector('button[aria-haspopup="menu"]');
  if (!trigger) throw new Error("Expected the comment actions menu trigger");
  fireEvent.click(trigger);
  fireEvent.click(await screen.findByText("Edit"));
  await screen.findByTestId("editor");
}

function getSaveButton() {
  return screen.getByRole("button", { name: /Save|Uploading…/ });
}

beforeEach(() => {
  uploadWithToast.mockReset();
  useCommentDraftStore.setState({ drafts: {} });
});

// MUL-4808 — comment edit had no upload gate: saving mid-upload persisted the
// edit with the pending image stripped out of the body and its id unbound.
describe("comment edit — upload submit gate", () => {
  function startPendingUpload(container: HTMLElement) {
    let release!: (result: UploadResult | null) => void;
    uploadWithToast.mockImplementationOnce(
      () => new Promise<UploadResult | null>((resolve) => { release = resolve; }),
    );
    const input = container.querySelector('input[type="file"]');
    if (!input) throw new Error("Expected a file input to render");
    fireEvent.change(input, {
      target: { files: [new File(["x"], "shot.png", { type: "image/png" })] },
    });
    return { release: (result: UploadResult | null) => release(result) };
  }

  it("disables Save while an upload is in flight and re-enables once it settles", async () => {
    const { container } = renderCard();
    await startEditing();
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "Edited body" } });

    const pending = startPendingUpload(container);

    await waitFor(() => expect(getSaveButton()).toBeDisabled());
    expect(getSaveButton()).toHaveAttribute("aria-busy", "true");

    await act(async () => {
      pending.release({
        id: "att-1",
        url: "https://cdn.example/att-1.png",
        filename: "shot.png",
        link: "https://cdn.example/att-1.png",
        markdownLink: "https://cdn.example/att-1.png",
      } as unknown as UploadResult);
    });

    await waitFor(() => expect(getSaveButton()).not.toBeDisabled());
  });

  // The editor unmounts on Cancel, taking the pending upload's node with it,
  // while the gate state lives on the surviving card. Re-entering edit must
  // come back usable — not stuck on the dead editor's last answer.
  it("does not stay gated after cancelling edit mid-upload and re-entering", async () => {
    const { container } = renderCard();
    await startEditing();
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "Edited body" } });

    startPendingUpload(container);
    await waitFor(() => expect(getSaveButton()).toBeDisabled());

    // Cancel stays available during an upload, so this is reachable.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByTestId("editor")).toBeNull());

    await startEditing();
    await waitFor(() => expect(getSaveButton()).not.toBeDisabled());
    expect(getSaveButton()).not.toHaveAttribute("aria-busy");
  });

  it("blocks the Cmd+Enter save path while an upload is in flight", async () => {
    const { container, onEdit } = renderCard();
    await startEditing();
    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "Edited body" } });

    startPendingUpload(container);

    // Cmd+Enter reaches saveEdit without ever consulting the Save button.
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await Promise.resolve();
    expect(onEdit).not.toHaveBeenCalled();
  });
});
