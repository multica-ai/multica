import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
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
vi.mock("../../editor/extensions", () => ({
  createEditorExtensions: () => [],
}));
vi.mock("../../editor/extensions/file-upload", () => ({
  uploadAndInsertFile: vi.fn(),
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

function store() {
  return useChatStore.getState() as unknown as {
    activeSessionId: string | null;
    selectedAgentId: string;
    inputDrafts: Record<string, string>;
    inputDraftAttachments: Record<string, unknown[]>;
  };
}

function element() {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={vi.fn()} agentName="Multica" />
    </I18nProvider>
  );
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
