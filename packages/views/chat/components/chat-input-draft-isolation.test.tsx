import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";

/**
 * Draft isolation across a composer switch, driven through the REAL
 * ContentEditor — its real 100ms `onUpdate` debounce, its real dirty guard,
 * and its real `onUpdateRef`-resolves-to-latest-render behavior.
 *
 * chat-input.test.tsx mocks ContentEditor with an instant `onUpdate`, which
 * cannot see this class of bug at all: the hazard only exists in the window
 * between a keystroke and its debounce firing. So only Tiptap's primitive is
 * mocked here (as in editor/content-editor.test.tsx); everything from
 * ContentEditor upward is the real component.
 *
 * The bug being pinned (MUL-4864 review): one editor instance serves every
 * chat draft. Typing in session A arms a debounce; switching to session B
 * before it fires means the timer runs with B's `draftKey` in scope, filing
 * A's document into B's draft — breaking "existing sessions keep independent
 * drafts" and risking A's context being sent to B's agent.
 */

const editorState = vi.hoisted(() => ({
  isFocused: false,
  isDestroyed: false,
  markdown: "",
  uploadingNodes: [] as Array<{ attrs: { uploading?: boolean } }>,
}));
const editorInstance = vi.hoisted<{ current: unknown }>(() => ({ current: null }));
const onCreateFired = vi.hoisted(() => ({ value: false }));
const transactionListeners = vi.hoisted(() => ({ current: [] as Array<() => void> }));
const latestEditorOptions = vi.hoisted<{
  current?: { onUpdate?: (args: { editor: unknown }) => void };
}>(() => ({}));
const mockSetContent = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));
// Captures what ContentEditor wires into its extensions. `onSubmitRef` is the
// Mod+Enter path — it bypasses the SubmitButton entirely, which is why the
// send guards live inside the handler and not only in the button's disabled
// state.
const capturedExtOptions = vi.hoisted<{
  current?: { onSubmitRef?: { current?: () => void } };
}>(() => ({}));
vi.mock("../../editor/extensions", () => ({
  createEditorExtensions: (options: { onSubmitRef?: { current?: () => void } }) => {
    capturedExtOptions.current = options;
    return [];
  },
}));
// Mirrors the real contract that matters here: `handler` is captured at CALL
// time (content-editor passes `onUploadFileRef.current` when the upload
// starts), and on completion the real implementation dispatches into the SAME
// editor instance — which is what fires onUpdate. Tests drive that dispatch
// explicitly via `finishUpload()`.
vi.mock("../../editor/extensions/file-upload", () => ({
  uploadAndInsertFile: async (
    _editor: unknown,
    file: File,
    handler: (f: File) => Promise<unknown>,
  ) => {
    await handler(file);
  },
}));
vi.mock("../../editor/utils/preprocess", () => ({
  preprocessMarkdown: (value: string) => value,
}));
vi.mock("../../editor/utils/repair-list-items", () => ({
  repairEmptyListItems: vi.fn(() => false),
}));
vi.mock("../../editor/bubble-menu", () => ({
  EditorBubbleMenu: () => null,
}));
vi.mock("../../editor/attachment-download-context", () => ({
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@tiptap/react", () => ({
  useEditor: (options: {
    onCreate?: (args: { editor: unknown }) => void;
    onUpdate?: (args: { editor: unknown }) => void;
  }) => {
    latestEditorOptions.current = options;
    if (!editorInstance.current) {
      editorInstance.current = {
        get isFocused() {
          return editorState.isFocused;
        },
        get isDestroyed() {
          return editorState.isDestroyed;
        },
        commands: {
          focus: vi.fn(),
          blur: vi.fn(),
          clearContent: vi.fn(() => {
            editorState.markdown = "";
          }),
          setContent: mockSetContent,
          setTextSelection: vi.fn(),
        },
        getMarkdown: () => editorState.markdown,
        on: (event: string, cb: () => void) => {
          if (event === "transaction") transactionListeners.current.push(cb);
        },
        off: (event: string, cb: () => void) => {
          if (event !== "transaction") return;
          transactionListeners.current = transactionListeners.current.filter((l) => l !== cb);
        },
        view: { dispatch: vi.fn() },
        state: {
          get tr() {
            return { __emptyTransaction: true };
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
      options?.onCreate?.({ editor: editorInstance.current });
    }
    return editorInstance.current;
  },
  EditorContent: ({ className }: { className?: string }) => (
    <div className={className} data-testid="editor-content" />
  ),
}));

vi.mock("@multica/core/chat", () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1",
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn((key: string, value: string) => {
      state.inputDrafts[key] = value;
    }),
    setInputDraftAttachments: vi.fn(),
    addInputDraftAttachment: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  return {
    DRAFT_NEW_SESSION: "__new__",
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});

import { ChatInput } from "./chat-input";
import { useChatStore } from "@multica/core/chat";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

function makeUpload(id: string, filename: string): UploadResult {
  const link = `/api/attachments/${id}/download`;
  return {
    id,
    filename,
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    url: link,
    download_url: link,
    markdown_url: link,
    content_type: "image/png",
    size_bytes: 1,
    created_at: new Date(0).toISOString(),
    markdownLink: link,
    link,
  };
}

function store() {
  return useChatStore.getState() as unknown as {
    activeSessionId: string | null;
    selectedAgentId: string;
    inputDrafts: Record<string, string>;
    inputDraftAttachments: Record<string, unknown[]>;
  };
}

function element(props: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={vi.fn()} agentName="Multica" {...props} />
    </I18nProvider>
  );
}

/** ContentEditor publishes upload-queue flips off `editor.on("transaction")`,
 *  not off onUpdate — every insert/dispatch below is a transaction in the real
 *  editor, so the tests emit one too. */
function emitTransaction() {
  act(() => {
    for (const listener of [...transactionListeners.current]) listener();
  });
}

/** Put the editor in the state a live upload leaves it in: file-upload.ts
 *  inserts a blob placeholder node (attrs.uploading = true) via a transaction,
 *  so `hasUploadingNode` — and therefore Guard 0, `hasActiveUploads`, and the
 *  host's upload gate — all report an upload in flight. */
function beginUpload(markdown: string) {
  editorState.uploadingNodes = [{ attrs: { uploading: true } }];
  type(markdown);
  emitTransaction();
}

/** What file-upload.ts does on completion: swap the blob for the CDN URL and
 *  clear the uploading flag, then dispatch into the SAME editor instance. That
 *  dispatch both fires onUpdate and, as a transaction, is how ContentEditor
 *  publishes the flip that un-gates the host. */
function finishUpload(markdown: string) {
  editorState.uploadingNodes = [];
  type(markdown);
  emitTransaction();
}

/** Simulate real typing: move the document, then fire the editor's own
 *  (debounced) onUpdate exactly as Tiptap would. */
function type(markdown: string) {
  editorState.markdown = markdown;
  act(() => {
    latestEditorOptions.current?.onUpdate?.({ editor: editorInstance.current });
  });
}

describe("ChatInput draft isolation across a composer switch (real debounce)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    editorState.isFocused = true;
    editorState.isDestroyed = false;
    editorState.markdown = "";
    editorState.uploadingNodes = [];
    editorInstance.current = null;
    onCreateFired.value = false;
    latestEditorOptions.current = undefined;
    mockSetContent.mockClear();
    const s = store();
    s.activeSessionId = null;
    s.selectedAgentId = "agent-1";
    s.inputDrafts = {};
    s.inputDraftAttachments = {};
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("files unflushed keystrokes under the session they were typed in, never the one switched to", () => {
    store().activeSessionId = "session-a";
    const { rerender } = render(element());

    // Typed in A. The debounce is armed but has NOT fired — this is the whole
    // hazard window.
    type("secret plan for agent A");
    expect(store().inputDrafts).toEqual({});

    // User clicks session B inside that window.
    store().activeSessionId = "session-b";
    rerender(element());

    // A's words belong to A.
    expect(store().inputDrafts["session-a"]).toBe("secret plan for agent A");
    // …and must never have reached B.
    expect(store().inputDrafts["session-b"]).toBeUndefined();

    // The armed timer must not resurrect the cross-write after the fact.
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(store().inputDrafts["session-b"]).toBeUndefined();
    expect(store().inputDrafts["session-a"]).toBe("secret plan for agent A");
  });

  it("loads the incoming session's own draft instead of leaving the old document on screen", () => {
    store().activeSessionId = "session-a";
    store().inputDrafts["session-b"] = "B's own words";
    const { rerender } = render(element());

    type("A's unflushed words");
    store().activeSessionId = "session-b";
    rerender(element());

    // The dirty guard would have suppressed this sync; flushing clears the
    // dirt, so B's draft actually loads.
    expect(mockSetContent).toHaveBeenCalled();
  });

  it("keeps a New Chat draft intact when only the agent changes", () => {
    // The MUL-4864 headline behavior, verified through the real debounce:
    // switching agent does not move draftKey, so there is nothing to flush and
    // nothing to lose.
    const { rerender } = render(element());
    type("half a thought");

    store().selectedAgentId = "agent-2";
    rerender(element());

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    // One slot, holding the text, still the New Chat slot.
    expect(store().inputDrafts).toEqual({ __new__: "half a thought" });
  });

  // An in-flight upload PINS the editor to the source document: Guard 0 in
  // ContentEditor refuses to setContent over an `uploading` node (doing so
  // strands the upload's finalize and the file vanishes). So while the upload
  // runs, the instance still holds A's document even though the user is on B —
  // and the upload's own completion dispatch fires onUpdate on that instance.
  // Every write it produces belongs to A, not to wherever the user navigated.
  //
  // `useUploadGate` does NOT protect this: it gates submit, not session/agent
  // navigation (use-chat-controller.ts handleSelectSession has no upload check).
  describe("with an upload in flight", () => {
    it("keeps the completing upload's markdown in the source draft, never the one switched to", () => {
      store().activeSessionId = "session-a";
      store().inputDrafts["session-b"] = "B's own words";
      const { rerender } = render(element());

      beginUpload("look at this ![](blob:local-preview)");

      // User clicks session B while the upload is still running.
      store().activeSessionId = "session-b";
      rerender(element());

      // Upload lands: blob → CDN URL, uploading flag cleared, dispatched into
      // the same instance.
      finishUpload("look at this ![](/api/attachments/att-1/download)");
      act(() => {
        vi.advanceTimersByTime(1000);
      });

      // B must never receive A's body or its attachment URL.
      expect(store().inputDrafts["session-b"]).toBe("B's own words");
      // The upload result belongs to the draft it was started in.
      expect(store().inputDrafts["session-a"]).toBe(
        "look at this ![](/api/attachments/att-1/download)",
      );
    });

    it("binds an attachment dropped while the editor is still pinned to the source draft", async () => {
      store().activeSessionId = "session-a";
      const onUploadFile = vi.fn(async (_file: File) => makeUpload("att-2", "second.png"));
      const { rerender } = render(element({ onUploadFile }));

      // An upload is already in flight, so Guard 0 pins the editor to A's
      // document.
      beginUpload("first ![](blob:one)");
      store().activeSessionId = "session-b";
      rerender(element({ onUploadFile }));

      // The composer still shows A's document, so a file dropped now lands in
      // A's body — its attachment row has to bind to A, or the body and the
      // staged attachment end up in different drafts.
      await act(async () => {
        fireEvent.drop(screen.getByTestId("editor-content"), {
          dataTransfer: { files: [new File(["x"], "second.png", { type: "image/png" })] },
        });
        await Promise.resolve();
      });

      const addAttachment = (
        useChatStore.getState() as unknown as {
          addInputDraftAttachment: ReturnType<typeof vi.fn>;
        }
      ).addInputDraftAttachment;
      expect(addAttachment).toHaveBeenCalledWith(
        "session-a",
        expect.objectContaining({ id: "att-2" }),
      );
      expect(addAttachment).not.toHaveBeenCalledWith("session-b", expect.anything());
    });

    it("refuses to send while the composer still holds the source draft's document", () => {
      // The upload gate covers most of the pinned window, but not the sliver
      // between the upload's final dispatch (uploading node gone, so
      // `hasActiveUploads()` is already false) and the re-render that adopts —
      // React flushes that update on a scheduler task, so a click can land
      // first. Sending then would post A's text into B's session.
      store().activeSessionId = "session-a";
      const onSend = vi.fn();
      const { rerender } = render(element({ onSend }));

      beginUpload("A's words ![](blob:one)");
      store().activeSessionId = "session-b";
      rerender(element({ onSend }));

      // Upload's node is gone, but no transaction has re-rendered us yet, so
      // the adopt has not run: the editor still holds A.
      editorState.uploadingNodes = [];
      act(() => {
        latestEditorOptions.current?.onUpdate?.({ editor: editorInstance.current });
      });

      // Mod+Enter, which skips the SubmitButton (and its disabled state)
      // entirely — the only way to actually reach handleSend in this window.
      act(() => {
        capturedExtOptions.current?.onSubmitRef?.current?.();
      });

      expect(onSend).not.toHaveBeenCalled();
    });

    it("loads the target draft once the upload guard clears", () => {
      store().activeSessionId = "session-a";
      store().inputDrafts["session-b"] = "B's own words";
      const { rerender } = render(element());

      beginUpload("uploading ![](blob:local-preview)");
      store().activeSessionId = "session-b";
      rerender(element());

      // Guard 0 blocks the sync while the upload runs — and it CONSUMES the
      // defaultValue change (lastDefaultValueRef advances before the guard), so
      // nothing re-applies it later on its own.
      mockSetContent.mockClear();

      finishUpload("uploaded ![](/api/attachments/att-1/download)");
      act(() => {
        vi.advanceTimersByTime(1000);
      });

      // The composer must end up showing B's draft, not A's document.
      expect(mockSetContent).toHaveBeenCalled();
    });
  });

  it("does not strand the last keystrokes when a lazy session create re-keys the draft mid-compose", () => {
    // First send / first upload in a New Chat flips activeSessionId null → uuid
    // under a live editor. Those bytes were typed in the New Chat slot, so they
    // must land there, not be dropped or filed under the new session.
    const { rerender } = render(element());
    type("creating a session with this");

    store().activeSessionId = "session-new";
    rerender(element());

    expect(store().inputDrafts["__new__"]).toBe("creating a session with this");
    expect(store().inputDrafts["session-new"]).toBeUndefined();
  });
});
