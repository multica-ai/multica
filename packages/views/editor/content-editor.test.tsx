import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { createRef, useState } from "react";
import type { Attachment } from "@multica/core/types";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";

const mockFocus = vi.hoisted(() => vi.fn());
const mockSetContent = vi.hoisted(() => vi.fn());
const mockSetTextSelection = vi.hoisted(() => vi.fn());
const mockDispatch = vi.hoisted(() => vi.fn());
const emptyTr = vi.hoisted(() => ({ __emptyTransaction: true }));
// Captures the options ContentEditor hands to createEditorExtensions on its
// most recent render — lets the placeholder tests assert the getter it wires
// into the Placeholder extension reads the live value.
const capturedExtOptions = vi.hoisted<{
  current: { placeholder?: string | (() => string) } | undefined;
}>(() => ({ current: undefined }));
const editorState = vi.hoisted(() => ({
  isFocused: false,
  isDestroyed: false,
  markdown: "",
  // Nodes the mocked doc reports via `descendants`. The content-sync effect
  // walks these to detect in-flight uploads; default empty = nothing uploading.
  uploadingNodes: [] as Array<{ attrs: { uploading?: boolean } }>,
}));

// Records the attachments[] prop the provider received on its most recent
// render. Content-editor merges its `attachments` prop with in-session
// upload results before passing them down — these tests assert that merged
// shape lands here.
const providerProps = vi.hoisted<{ attachments: Attachment[] | undefined }>(
  () => ({ attachments: undefined }),
);

const uploadAndInsertFileMock = vi.hoisted(() => vi.fn());
const preprocessMarkdownMock = vi.hoisted(() => vi.fn((value: string) => value));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("./extensions", () => ({
  createEditorExtensions: (options: {
    placeholder?: string | (() => string);
  }) => {
    capturedExtOptions.current = options;
    return [];
  },
}));

vi.mock("./extensions/file-upload", () => ({
  uploadAndInsertFile: uploadAndInsertFileMock,
}));

vi.mock("./utils/preprocess", () => ({
  preprocessMarkdown: preprocessMarkdownMock,
}));

// Empty-list repair needs a live ProseMirror doc (covered by
// repair-list-items.test.ts against the real editor). Here it is a no-op so the
// mocked editor's sync path exercises the normal (non-repair) branch.
vi.mock("./utils/repair-list-items", () => ({
  repairEmptyListItems: vi.fn(() => false),
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
// Transaction listeners registered via `editor.on("transaction", …)`. The
// upload-state publisher subscribes here; `emitTransaction` stands in for
// ProseMirror dispatching a doc change.
const transactionListeners = vi.hoisted(() => ({ current: [] as Array<() => void> }));
const emitTransaction = () => {
  for (const listener of [...transactionListeners.current]) listener();
};
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
        on: (event: string, cb: () => void) => {
          if (event === "transaction") transactionListeners.current.push(cb);
        },
        off: (event: string, cb: () => void) => {
          if (event !== "transaction") return;
          transactionListeners.current = transactionListeners.current.filter(
            (listener) => listener !== cb,
          );
        },
        view: { dispatch: mockDispatch },
        state: {
          get tr() {
            return emptyTr;
          },
          doc: {
            content: { size: 0 },
            descendants: (cb: (node: { attrs: { uploading?: boolean } }) => boolean | void) => {
              for (const node of editorState.uploadingNodes) {
                if (cb(node) === false) break;
              }
            },
          },
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

import { ContentEditor, type ContentEditorRef } from "./content-editor";

describe("ContentEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    editorState.isFocused = false;
    editorState.isDestroyed = false;
    editorState.markdown = "";
    editorState.uploadingNodes = [];
    editorRef.current = null;
    transactionListeners.current = [];
    onCreateFired.value = false;
    latestEditorOptions.current = undefined;
    providerProps.attachments = undefined;
    capturedExtOptions.current = undefined;
    preprocessMarkdownMock.mockImplementation((value: string) => value);
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

  it("syncs editor content when value changes externally and editor is unfocused", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor value="old content" />);

    expect(mockSetContent).not.toHaveBeenCalled();

    // Editor still holds the old, in-sync content; external value changes.
    editorState.markdown = "old content";
    rerender(<ContentEditor value="new content from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      "new content from server",
      expect.objectContaining({ emitUpdate: false, contentType: "markdown" }),
    );
  });

  it("treats defaultValue as mount-only", () => {
    editorState.markdown = "initial draft";
    const { rerender } = render(<ContentEditor defaultValue="initial draft" />);

    rerender(<ContentEditor defaultValue="draft store echo" />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("does not parse the initial defaultValue twice when markdown round-trip canonicalizes it", () => {
    // useEditor already parsed defaultValue through its `content` option. The
    // editor's canonical Markdown can legitimately differ from the source, so
    // that difference alone must not trigger an immediate second setContent.
    editorState.markdown = "- [ ] canonical task";

    render(
      <ContentEditor defaultValue={"- [ ] source task\n\ncontinuation"} />,
    );

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("does not feed a locally emitted draft back through preprocessing", () => {
    vi.useFakeTimers();
    preprocessMarkdownMock.mockImplementation((value: string) =>
      value === "dev.de" ? "[dev.de](http://dev.de)" : value,
    );

    function EchoingDraftHost() {
      const [draft, setDraft] = useState("");
      return (
        <ContentEditor
          defaultValue={draft}
          onUpdate={setDraft}
          debounceMs={100}
        />
      );
    }

    render(<EchoingDraftHost />);
    editorState.markdown = "dev.de";

    act(() => {
      latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
      vi.advanceTimersByTime(100);
    });

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("recognizes a synchronized value echo before preprocessing it", () => {
    vi.useFakeTimers();
    preprocessMarkdownMock.mockImplementation((value: string) =>
      value === "dev.de" ? "[dev.de](http://dev.de)" : value,
    );

    function SynchronizedDraftHost() {
      const [draft, setDraft] = useState("");
      return (
        <ContentEditor value={draft} onUpdate={setDraft} debounceMs={100} />
      );
    }

    render(<SynchronizedDraftHost />);
    editorState.markdown = "dev.de";

    act(() => {
      latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
      vi.advanceTimersByTime(100);
    });

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("does not sync while a file upload is in flight (in-flight upload node must survive external value changes)", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor value="old content" />);

    // A file is uploading: the doc holds a node with attrs.uploading. An
    // external value change (e.g. chat lazy-creating a session mid-upload
    // flips the draft key → value) must NOT setContent over it, or the
    // uploading node is wiped and the upload's finalize can't find it.
    editorState.uploadingNodes = [{ attrs: { uploading: true } }];
    rerender(<ContentEditor value="" />);

    expect(mockSetContent).not.toHaveBeenCalled();

    // Once the upload settles (no uploading node), a later external change syncs.
    editorState.uploadingNodes = [];
    rerender(<ContentEditor value="new content from server" />);
    expect(mockSetContent).toHaveBeenCalledTimes(1);
  });

  it("does not sync when editor is focused and has unsaved local edits", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor value="old content" />);

    // User is typing — focused AND dirty (markdown diverges from
    // lastEmittedRef, which was seeded with "old content" by onCreate).
    editorState.isFocused = true;
    editorState.markdown = "user-typed-content";

    rerender(<ContentEditor value="incoming external change" />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("syncs even when editor is focused, as long as it is clean (focused-but-clean must not be permanently dropped)", () => {
    // This case is the regression test for the focused-but-clean hole:
    // user clicks into the editor (focused = true) but types nothing
    // (markdown still equals lastEmittedRef). An external update arrives.
    // With an unconditional `if (isFocused) return`, this sync would be lost
    // forever because onBlur has no replay path.
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor value="old content" />);

    editorState.isFocused = true;
    editorState.markdown = "old content"; // clean — no typing happened

    rerender(<ContentEditor value="new content from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      "new content from server",
      expect.objectContaining({ emitUpdate: false, contentType: "markdown" }),
    );
  });

  it("does not sync when editor is unfocused but has unsaved local edits (blur-before-debounce window)", () => {
    editorState.markdown = "old content";
    const { rerender } = render(
      <ContentEditor value="old content" onUpdate={() => {}} />,
    );

    // User typed locally, then blurred. Debounce hasn't flushed yet so
    // lastEmittedRef inside the component still reflects "old content".
    editorState.isFocused = false;
    editorState.markdown = "user typed but unsaved";

    rerender(
      <ContentEditor
        value="external update from another agent"
        onUpdate={() => {}}
      />,
    );

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  // flushPendingUpdate exists for hosts that re-point ONE editor instance at a
  // different destination mid-debounce (chat swapping draftKey between
  // sessions). Without it the armed debounce fires after the switch and, since
  // onUpdate always resolves to the latest render's closure, files the old
  // document under the new destination (MUL-4864).
  describe("flushPendingUpdate", () => {
    it("hands back the pending markdown and cancels the debounce so it cannot fire later", () => {
      vi.useFakeTimers();
      const onUpdate = vi.fn();
      const ref = createRef<ContentEditorRef>();
      editorState.markdown = "old content";
      render(
        <ContentEditor ref={ref} defaultValue="old content" onUpdate={onUpdate} debounceMs={100} />,
      );

      editorState.markdown = "typed but not yet flushed";
      act(() => {
        latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
      });

      // Taken back, not emitted — the host routes it to the source draft.
      expect(ref.current?.flushPendingUpdate()).toBe("typed but not yet flushed");
      expect(onUpdate).not.toHaveBeenCalled();

      // The armed timer must be dead: firing now would write these bytes into
      // whatever destination the host has since switched to.
      act(() => {
        vi.advanceTimersByTime(500);
      });
      expect(onUpdate).not.toHaveBeenCalled();
    });

    it("returns null when nothing is pending", () => {
      const ref = createRef<ContentEditorRef>();
      editorState.markdown = "settled";
      render(<ContentEditor ref={ref} defaultValue="settled" onUpdate={vi.fn()} />);

      expect(ref.current?.flushPendingUpdate()).toBeNull();
    });

    it("leaves the editor clean so the next value sync is no longer blocked by the dirty guard", () => {
      vi.useFakeTimers();
      const ref = createRef<ContentEditorRef>();
      editorState.markdown = "draft A text";
      const { rerender } = render(
        <ContentEditor ref={ref} value="draft A text" onUpdate={vi.fn()} debounceMs={100} />,
      );

      // Unflushed local edits — this is what makes the editor dirty.
      editorState.markdown = "draft A text, still typing";
      act(() => {
        latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
      });
      act(() => {
        ref.current?.flushPendingUpdate();
      });

      // Host has taken the bytes, so switching destination must now load the
      // incoming draft rather than leaving the old document on screen.
      rerender(
        <ContentEditor ref={ref} value="draft B text" onUpdate={vi.fn()} debounceMs={100} />,
      );
      expect(mockSetContent).toHaveBeenCalled();
    });

    it("still blocks the sync when the pending update was NOT flushed (guard intact)", () => {
      vi.useFakeTimers();
      editorState.markdown = "draft A text";
      const { rerender } = render(
        <ContentEditor value="draft A text" onUpdate={vi.fn()} debounceMs={100} />,
      );

      editorState.markdown = "draft A text, still typing";
      act(() => {
        latestEditorOptions.current?.onUpdate?.({ editor: editorRef.current });
      });

      // No flush → dirty → the guard must still protect the unsaved bytes.
      rerender(<ContentEditor value="draft B text" onUpdate={vi.fn()} debounceMs={100} />);
      expect(mockSetContent).not.toHaveBeenCalled();
    });
  });

  it("does not sync when value normalizes to the current editor markdown", () => {
    editorState.markdown = "same content";
    const { rerender } = render(<ContentEditor value="same content" />);

    // Different `value` string forces the effect to re-run (the dep
    // array sees a new value), but the trailing whitespace normalises away
    // via `trimEnd()`, so `setContent` must still short-circuit.
    rerender(<ContentEditor value={"same content\n"} />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("refactor safety net: imperative getMarkdown() stays untrimmed, keeping its exact current return value", () => {
    // The imperative `getMarkdown()` is deliberately NOT routed through
    // `normalizeMarkdown` (which would `trimEnd()`). This pins down that the
    // F2a/F3 dedupe refactor preserved the method's exact return value —
    // trailing blank lines included — instead of folding it into the trimming
    // helper. `stripBlobUrls` (unmocked here) only strips blob image markdown,
    // so the trailing newlines survive untouched.
    editorState.markdown = "kept body\n\n";

    const ref = createRef<ContentEditorRef>();
    render(<ContentEditor ref={ref} />);

    expect(ref.current).not.toBeNull();
    expect(ref.current?.getMarkdown()).toBe("kept body\n\n");
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

  it("refreshes the live placeholder getter and repaints when the placeholder prop changes", () => {
    // Repro for MUL-4276: Tiptap's Placeholder snapshots a *string* option at
    // mount, so switching between an archived and an active chat session under
    // the SAME agent (no editor remount) left the input frozen on the archived
    // copy. The fix wires a *getter* over a live ref into the extension and, on
    // change, dispatches an empty transaction to force a decoration recompute.
    const { rerender } = render(
      <ContentEditor placeholder="This session is archived" />,
    );
    // What reaches the Placeholder extension is a getter, not a static string.
    const getter = capturedExtOptions.current?.placeholder;
    expect(typeof getter).toBe("function");
    expect((getter as () => string)()).toBe("This session is archived");

    mockDispatch.mockClear();

    rerender(<ContentEditor placeholder="Message agent…" />);

    // Same getter identity now returns the new text (reads the live ref)…
    expect((getter as () => string)()).toBe("Message agent…");
    // …and a repaint was nudged so the mounted decoration re-reads it.
    expect(mockDispatch).toHaveBeenCalledTimes(1);
    expect(mockDispatch).toHaveBeenCalledWith(emptyTr);
  });

  it("does not repaint when the placeholder prop is unchanged", () => {
    const { rerender } = render(<ContentEditor placeholder="Message agent…" />);

    mockDispatch.mockClear();

    rerender(<ContentEditor placeholder="Message agent…" />);

    expect(mockDispatch).not.toHaveBeenCalled();
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

// MUL-4808 — the document IS the upload queue, so hosts gate submit off it
// instead of each keeping a counter. These pin the publisher's contract.
describe("ContentEditor — onUploadingChange (MUL-4808)", () => {
  const uploadingNode = { attrs: { uploading: true } };
  const settledNode = { attrs: { uploading: false } };

  it("publishes true when a node starts uploading and false once it settles", () => {
    const onUploadingChange = vi.fn();
    render(<ContentEditor onUploadingChange={onUploadingChange} />);
    // Mount publishes the current answer so the host can't be left holding a
    // stale one from a previous editor instance (see the remount case below).
    expect(onUploadingChange).toHaveBeenLastCalledWith(false);

    editorState.uploadingNodes = [uploadingNode];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(true);

    editorState.uploadingNodes = [settledNode];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(false);
  });

  it("keeps publishing true until the LAST concurrent upload settles", () => {
    const onUploadingChange = vi.fn();
    render(<ContentEditor onUploadingChange={onUploadingChange} />);

    editorState.uploadingNodes = [{ attrs: { uploading: true } }, { attrs: { uploading: true } }];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(true);

    // First of two finishes — submit must STAY gated.
    editorState.uploadingNodes = [settledNode, { attrs: { uploading: true } }];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(true);

    editorState.uploadingNodes = [settledNode, settledNode];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(false);
  });

  // The regression that rules out driving this off `onUpdate`: a failed upload
  // removes its placeholder, and since the blob URL was stripped from
  // serialized markdown all along, the markdown is byte-identical before and
  // after. onUpdate is debounced AND skips no-change emissions, so the host
  // would stay gated forever with no pending upload left to un-gate it.
  it("publishes false when a failed upload's placeholder is removed, even though the markdown never changed", () => {
    const onUploadingChange = vi.fn();
    const onUpdate = vi.fn();
    editorState.markdown = "same body";
    render(<ContentEditor onUploadingChange={onUploadingChange} onUpdate={onUpdate} />);

    editorState.uploadingNodes = [uploadingNode];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(true);

    // Upload fails → node removed. Markdown is unchanged throughout.
    editorState.uploadingNodes = [];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(false);
    expect(editorState.markdown).toBe("same body");
  });

  it("publishes only on flips, not on every transaction", () => {
    const onUploadingChange = vi.fn();
    render(<ContentEditor onUploadingChange={onUploadingChange} />);
    onUploadingChange.mockClear(); // drop the mount publish

    editorState.uploadingNodes = [uploadingNode];
    act(() => emitTransaction());
    // Typing while the upload is still in flight: same answer, no re-publish.
    act(() => emitTransaction());
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenCalledTimes(1);
  });

  // An editor torn down mid-upload takes its pending node with it, but the
  // host's gate state survives. Hosts that remount the editor under a living
  // parent — comment edit (cancel → re-enter) and chat (agent switch rebuilds
  // it by `key`) — would otherwise be stuck disabled forever, with no pending
  // upload left in the document to ever flip them back.
  it("republishes on remount so a host can't stay gated by a dead editor's upload", () => {
    const onUploadingChange = vi.fn();
    const { unmount } = render(<ContentEditor onUploadingChange={onUploadingChange} />);

    editorState.uploadingNodes = [uploadingNode];
    act(() => emitTransaction());
    expect(onUploadingChange).toHaveBeenLastCalledWith(true);

    // Editor dies while the upload is still pending; the host stays mounted
    // holding `uploading: true`.
    unmount();
    editorRef.current = null;
    onCreateFired.value = false;
    editorState.uploadingNodes = [];

    onUploadingChange.mockClear();
    render(<ContentEditor onUploadingChange={onUploadingChange} />);
    expect(onUploadingChange).toHaveBeenCalledWith(false);
  });

  it("does not subscribe at all when the host omits onUploadingChange", () => {
    render(<ContentEditor />);
    // Non-gating hosts (the autosaved description editor) must not pay for a
    // doc scan on every keystroke.
    expect(transactionListeners.current).toHaveLength(0);
  });

  it("unsubscribes on unmount", () => {
    const { unmount } = render(<ContentEditor onUploadingChange={vi.fn()} />);
    expect(transactionListeners.current).toHaveLength(1);
    unmount();
    expect(transactionListeners.current).toHaveLength(0);
  });
});

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
