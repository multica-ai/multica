import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import type { Attachment } from "@multica/core/types";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";

const mockFocus = vi.hoisted(() => vi.fn());
const mockSetContent = vi.hoisted(() => vi.fn());
const mockSetTextSelection = vi.hoisted(() => vi.fn());
const editorState = vi.hoisted(() => ({
  isFocused: false,
  isDestroyed: false,
  markdown: "",
}));

// Records the attachments[] prop the provider received on its most recent
// render. Content-editor merges its `attachments` prop with in-session
// upload results before passing them down — these tests assert that merged
// shape lands here.
const providerProps = vi.hoisted<{ attachments: Attachment[] | undefined }>(
  () => ({ attachments: undefined }),
);

const uploadAndInsertFileMock = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("./extensions", () => ({
  createEditorExtensions: () => [],
}));

vi.mock("./extensions/file-upload", () => ({
  uploadAndInsertFile: uploadAndInsertFileMock,
}));

vi.mock("./utils/preprocess", () => ({
  preprocessMarkdown: (value: string) => value,
}));

vi.mock("./bubble-menu", () => ({
  EditorBubbleMenu: () => null,
}));

vi.mock("./attachment-download-context", () => ({
  AttachmentDownloadProvider: ({
    attachments,
    children,
  }: {
    attachments?: Attachment[];
    children: React.ReactNode;
  }) => {
    providerProps.attachments = attachments;
    return <>{children}</>;
  },
}));

const editorRef = vi.hoisted<{ current: unknown }>(() => ({ current: null }));
const onCreateFired = vi.hoisted(() => ({ value: false }));
const latestEditorOptions = vi.hoisted<{
  current?: { onUpdate?: (args: { editor: unknown }) => void };
}>(() => ({}));

vi.mock("@tiptap/react", () => ({
  useEditor: (options: {
    onCreate?: (args: { editor: unknown }) => void;
    onUpdate?: (args: { editor: unknown }) => void;
  }) => {
    latestEditorOptions.current = options;
    if (!editorRef.current) {
      editorRef.current = {
        get isFocused() {
          return editorState.isFocused;
        },
        get isDestroyed() {
          return editorState.isDestroyed;
        },
        commands: {
          focus: mockFocus,
          clearContent: vi.fn(),
          setContent: mockSetContent,
          setTextSelection: mockSetTextSelection,
        },
        getMarkdown: () => editorState.markdown,
        state: {
          doc: { content: { size: 0 } },
          selection: { empty: true, from: 0, to: 0 },
        },
      };
    }
    if (!onCreateFired.value) {
      onCreateFired.value = true;
      options?.onCreate?.({ editor: editorRef.current });
    }
    return editorRef.current;
  },
  EditorContent: ({ className }: { className?: string }) => (
    <div className={className} data-testid="editor-content">
      <div className="ProseMirror rich-text-editor" data-testid="prosemirror" />
    </div>
  ),
}));

import { ContentEditor } from "./content-editor";

describe("ContentEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    editorState.isFocused = false;
    editorState.isDestroyed = false;
    editorState.markdown = "";
    editorRef.current = null;
    onCreateFired.value = false;
    latestEditorOptions.current = undefined;
    providerProps.attachments = undefined;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("focuses the editor when clicking the empty container area", () => {
    render(<ContentEditor placeholder="Add description..." />);

    const shell = screen.getByTestId("editor-content").parentElement;
    expect(shell).not.toBeNull();

    fireEvent.mouseDown(shell!);

    expect(mockFocus).toHaveBeenCalledWith("end");
  });

  it("does not hijack clicks that land inside the ProseMirror node", () => {
    render(<ContentEditor placeholder="Add description..." />);

    fireEvent.mouseDown(screen.getByTestId("prosemirror"));

    expect(mockFocus).not.toHaveBeenCalled();
  });

  it("syncs editor content when defaultValue changes externally and editor is unfocused", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor defaultValue="old content" />);

    expect(mockSetContent).not.toHaveBeenCalled();

    // Editor still holds the old, in-sync content; external value changes.
    editorState.markdown = "old content";
    rerender(<ContentEditor defaultValue="new content from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      "new content from server",
      expect.objectContaining({ emitUpdate: false, contentType: "markdown" }),
    );
  });

  it("does not sync when editor is focused and has unsaved local edits", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor defaultValue="old content" />);

    // User is typing — focused AND dirty (markdown diverges from
    // lastEmittedRef, which was seeded with "old content" by onCreate).
    editorState.isFocused = true;
    editorState.markdown = "user-typed-content";

    rerender(<ContentEditor defaultValue="incoming external change" />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("syncs even when editor is focused, as long as it is clean (focused-but-clean must not be permanently dropped)", () => {
    // This case is the regression test for the focused-but-clean hole:
    // user clicks into the editor (focused = true) but types nothing
    // (markdown still equals lastEmittedRef). An external update arrives.
    // With an unconditional `if (isFocused) return`, this sync would be lost
    // forever because onBlur has no replay path.
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor defaultValue="old content" />);

    editorState.isFocused = true;
    editorState.markdown = "old content"; // clean — no typing happened

    rerender(<ContentEditor defaultValue="new content from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      "new content from server",
      expect.objectContaining({ emitUpdate: false, contentType: "markdown" }),
    );
  });

  it("does not sync when editor is unfocused but has unsaved local edits (blur-before-debounce window)", () => {
    editorState.markdown = "old content";
    const { rerender } = render(
      <ContentEditor defaultValue="old content" onUpdate={() => {}} />,
    );

    // User typed locally, then blurred. Debounce hasn't flushed yet so
    // lastEmittedRef inside the component still reflects "old content".
    editorState.isFocused = false;
    editorState.markdown = "user typed but unsaved";

    rerender(
      <ContentEditor
        defaultValue="external update from another agent"
        onUpdate={() => {}}
      />,
    );

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("does not sync when defaultValue normalizes to the current editor markdown", () => {
    editorState.markdown = "same content";
    const { rerender } = render(<ContentEditor defaultValue="same content" />);

    // Different `defaultValue` string forces the effect to re-run (the dep
    // array sees a new value), but the trailing whitespace normalises away
    // via `trimEnd()`, so `setContent` must still short-circuit.
    rerender(<ContentEditor defaultValue={"same content\n"} />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("flushes a pending debounced update on unmount when flushPendingOnUnmount is set", () => {
    vi.useFakeTimers();
    const onUpdate = vi.fn();
    editorState.markdown = "old content";
    const { unmount } = render(
      <ContentEditor
        defaultValue="old content"
        onUpdate={onUpdate}
        debounceMs={1500}
        flushPendingOnUnmount
      />,
    );

    editorState.markdown = "old content\n\n![shot](/api/attachments/att-1/download)";
    act(() => {
      latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
    });

    expect(onUpdate).not.toHaveBeenCalled();

    // The flush must emit the copy cached at onUpdate time — by cleanup time
    // Tiptap may already have torn the instance down, so reading the editor
    // during unmount is not an option.
    editorState.isDestroyed = true;
    editorState.markdown = "";

    unmount();

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate).toHaveBeenCalledWith(
      "old content\n\n![shot](/api/attachments/att-1/download)",
    );
    act(() => {
      vi.advanceTimersByTime(1500);
    });
    expect(onUpdate).toHaveBeenCalledTimes(1);
  });

  it("drops a pending debounced update on unmount by default", () => {
    // Regression guard for draft resurrection: composers like comment edit
    // cancel `clearDraft()` and then unmount this editor. A default unmount
    // flush would re-emit the discarded markdown into onUpdate, which writes
    // it straight back into the draft store.
    vi.useFakeTimers();
    const onUpdate = vi.fn();
    editorState.markdown = "edit draft the user cancelled";
    const { unmount } = render(
      <ContentEditor
        defaultValue=""
        onUpdate={onUpdate}
        debounceMs={300}
      />,
    );

    act(() => {
      latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
    });
    expect(onUpdate).not.toHaveBeenCalled();

    unmount();

    expect(onUpdate).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(onUpdate).not.toHaveBeenCalled();
  });

  it("does not re-emit on unmount when the debounce already fired", () => {
    vi.useFakeTimers();
    const onUpdate = vi.fn();
    const { unmount } = render(
      <ContentEditor
        defaultValue=""
        onUpdate={onUpdate}
        debounceMs={1500}
        flushPendingOnUnmount
      />,
    );

    editorState.markdown = "typed content";
    act(() => {
      latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
      vi.advanceTimersByTime(1500);
    });
    expect(onUpdate).toHaveBeenCalledTimes(1);

    unmount();

    expect(onUpdate).toHaveBeenCalledTimes(1);
  });
});

function makeAttachment(id: string, overrides: Partial<Attachment> = {}): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u-1",
    filename: `${id}.png`,
    url: `/uploads/${id}.png`,
    download_url: `/api/attachments/${id}/download`,
    markdown_url: `https://api.multica.test/api/attachments/${id}/download`,
    content_type: "image/png",
    size_bytes: 1,
    created_at: "2026-06-10T00:00:00Z",
    ...overrides,
  };
}

function asUploadResult(att: Attachment): UploadResult {
  return { ...att, link: att.url, markdownLink: `/api/attachments/${att.id}/download` };
}

// MUL-3192 — surfaces like the quick-create modal upload images through the
// editor without a server-supplied `attachments` prop. Without in-session
// tracking, the AttachmentDownloadProvider had nothing to resolve the
// freshly-inserted /api/attachments/<id>/download URL against, so
// Attachment.normalize() couldn't swap it for a freshly-loadable URL — the
// <img> rendered broken on Desktop where the renderer's origin doesn't
// proxy /api to the API host. ContentEditor now wraps onUploadFile so the
// successful UploadResult lands in the provider as a tracked record.
describe("ContentEditor — in-session attachment tracking (MUL-3192)", () => {
  it("seeds the AttachmentDownloadProvider with the caller-supplied attachments prop", () => {
    const att = makeAttachment("seed-1");
    render(<ContentEditor attachments={[att]} />);
    expect(providerProps.attachments).toEqual([att]);
  });

  it("appends a successful upload result to the provider's attachments list", async () => {
    const onUploadFile = vi.fn(async (_file: File) =>
      asUploadResult(makeAttachment("uploaded-1")),
    );
    // Capture the wrapped uploader the editor hands to uploadAndInsertFile,
    // then invoke it the same way the file-upload extension would.
    let capturedHandler:
      | ((file: File) => Promise<UploadResult | null>)
      | undefined;
    uploadAndInsertFileMock.mockImplementation(
      async (_editor: unknown, file: File, handler: typeof capturedHandler) => {
        capturedHandler = handler;
        await handler?.(file);
      },
    );

    let imperativeRef: { uploadFile: (file: File) => void } | null = null;
    render(
      <ContentEditor
        onUploadFile={onUploadFile}
        ref={(r) => {
          imperativeRef = r;
        }}
      />,
    );

    expect(providerProps.attachments).toBeUndefined();

    await act(async () => {
      imperativeRef?.uploadFile(new File(["payload"], "shot.png", { type: "image/png" }));
    });

    // The wrapper (not the raw caller-supplied uploader) is what reaches
    // the file-upload extension — that's the layer that captures successful
    // results into the provider.
    expect(capturedHandler).toBeTypeOf("function");
    expect(capturedHandler).not.toBe(onUploadFile);
    expect(onUploadFile).toHaveBeenCalledTimes(1);

    expect(providerProps.attachments).toHaveLength(1);
    expect(providerProps.attachments?.[0]?.id).toBe("uploaded-1");
  });

  it("merges in-session uploads with the caller's attachments prop, preferring the prop on id collision", async () => {
    // The pre-loaded record carries a freshly-signed download_url; the
    // upload result for the same id has an older download_url. After merge
    // the provider should still expose the prop's record so the editor's
    // resolveAttachment lookup hands back the freshest data.
    const seeded = makeAttachment("shared-1", {
      download_url: "https://cdn.example/freshly-signed.png?Signature=fresh",
    });
    const collision = makeAttachment("shared-1", {
      download_url: "https://cdn.example/freshly-signed.png?Signature=stale",
    });
    const onUploadFile = vi.fn(async () => asUploadResult(collision));
    uploadAndInsertFileMock.mockImplementation(
      async (_e: unknown, file: File, handler: (f: File) => Promise<unknown>) => {
        await handler(file);
      },
    );

    let imperativeRef: { uploadFile: (file: File) => void } | null = null;
    render(
      <ContentEditor
        attachments={[seeded]}
        onUploadFile={onUploadFile}
        ref={(r) => {
          imperativeRef = r;
        }}
      />,
    );

    await act(async () => {
      imperativeRef?.uploadFile(new File(["x"], "shared.png", { type: "image/png" }));
    });

    expect(providerProps.attachments).toHaveLength(1);
    expect(providerProps.attachments?.[0]?.download_url).toContain("Signature=fresh");
  });

  it("backfills an empty caller download_url from the session upload on id collision", async () => {
    // The create-issue draft persists attachment records with download_url
    // stripped (the signed URL is response-scoped). While the upload session
    // is still alive, the provider should hand back the signed URL so the
    // just-pasted image first-paints from it instead of detouring through
    // markdown_url.
    const draftRecord = makeAttachment("draft-1", { download_url: "" });
    const uploaded = makeAttachment("draft-1", {
      download_url: "https://cdn.example/draft-1.png?Signature=fresh",
    });
    const onUploadFile = vi.fn(async () => asUploadResult(uploaded));
    uploadAndInsertFileMock.mockImplementation(
      async (_e: unknown, file: File, handler: (f: File) => Promise<unknown>) => {
        await handler(file);
      },
    );

    let imperativeRef: { uploadFile: (file: File) => void } | null = null;
    render(
      <ContentEditor
        attachments={[draftRecord]}
        onUploadFile={onUploadFile}
        ref={(r) => {
          imperativeRef = r;
        }}
      />,
    );

    await act(async () => {
      imperativeRef?.uploadFile(new File(["x"], "draft-1.png", { type: "image/png" }));
    });

    expect(providerProps.attachments).toHaveLength(1);
    expect(providerProps.attachments?.[0]?.download_url).toContain("Signature=fresh");
    // Everything except the backfilled field still comes from the caller copy.
    expect(providerProps.attachments?.[0]?.filename).toBe(draftRecord.filename);
  });

  it("does not append a duplicate when the same upload result returns twice (paste-then-drop the same blob)", async () => {
    const result = asUploadResult(makeAttachment("dedup-1"));
    const onUploadFile = vi.fn(async () => result);
    uploadAndInsertFileMock.mockImplementation(
      async (_e: unknown, file: File, handler: (f: File) => Promise<unknown>) => {
        await handler(file);
      },
    );

    let imperativeRef: { uploadFile: (file: File) => void } | null = null;
    render(
      <ContentEditor
        onUploadFile={onUploadFile}
        ref={(r) => {
          imperativeRef = r;
        }}
      />,
    );

    await act(async () => {
      imperativeRef?.uploadFile(new File(["a"], "a.png", { type: "image/png" }));
    });
    await act(async () => {
      imperativeRef?.uploadFile(new File(["b"], "b.png", { type: "image/png" }));
    });

    expect(providerProps.attachments).toHaveLength(1);
    expect(providerProps.attachments?.[0]?.id).toBe("dedup-1");
  });
});
