import { forwardRef, useRef, useImperativeHandle } from "react";
import { beforeEach, describe, it, expect, vi } from "vitest";
import { act, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";

function makeUpload(overrides: Partial<UploadResult> & { id: string; link: string; filename: string }): UploadResult {
  return {
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    url: overrides.link,
    download_url: overrides.link,
    markdown_url: overrides.link,
    content_type: "image/png",
    size_bytes: 1,
    created_at: new Date(0).toISOString(),
    // markdownLink defaults to the same value as `link` so legacy
    // tests assert the previous URL shape unless they pass an
    // explicit override. Real callers always set it to the stable
    // /api/attachments/<id>/download path via useFileUpload.
    markdownLink: overrides.link,
    ...overrides,
  };
}

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

// Track drop-zone callbacks so the test can simulate a real drop.
const dropHandlers = vi.hoisted(() => ({
  onDrop: null as null | ((files: File[]) => void),
}));
const editorProps = vi.hoisted(() => ({
  last: null as null | Record<string, unknown>,
}));
// Records imperative editor calls so tests can assert whether a commit
// scrubbed the editor (clearEditor) or left it intact (fire-and-forget).
const editorState = vi.hoisted(() => ({ cleared: 0, blurred: 0 }));

vi.mock("../../editor", () => ({
  useFileDropZone: ({ onDrop }: { onDrop: (files: File[]) => void }) => {
    dropHandlers.onDrop = onDrop;
    return { isDragOver: false, dropZoneProps: { "data-testid": "drop-zone" } };
  },
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    props: {
      defaultValue?: string;
      onUpdate?: (md: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File) => Promise<UploadResult | null>;
      mentionMode?: string;
      mentionContextItems?: unknown[];
    },
    ref: React.Ref<unknown>,
  ) {
    const {
      defaultValue,
      onUpdate,
      placeholder,
      onUploadFile,
    } = props;
    editorProps.last = props as unknown as Record<string, unknown>;
    const valueRef = useRef<string>(defaultValue ?? "");
    const uploadingRef = useRef(0);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        editorState.cleared += 1;
        valueRef.current = "";
      },
      blur: () => {
        editorState.blurred += 1;
      },
      focus: () => {},
      uploadFile: async (file: File) => {
        uploadingRef.current += 1;
        try {
          const result = await onUploadFile?.(file);
          if (result) {
            // Mirror the real editor (uploadAndInsertFile in
            // packages/views/editor/extensions/file-upload.ts): the
            // markdown body captures `markdownLink` (the stable
            // /api/attachments/<id>/download URL) when the upload
            // returned one, falling back to `link` for the
            // no-workspace avatar branch. The chat input's
            // uploadMapRef must use the same value as its key —
            // pinning that contract is the regression below.
            const persistedURL = result.markdownLink || result.link;
            valueRef.current = `${valueRef.current}![](${persistedURL})`.trim();
            onUpdate?.(valueRef.current);
          }
        } finally {
          uploadingRef.current = Math.max(0, uploadingRef.current - 1);
        }
      },
      hasActiveUploads: () => uploadingRef.current > 0,
    }));
    return (
      <textarea
        data-testid="editor"
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
      />
    );
  }),
}));

// Mock chat store with an in-memory implementation that supports both
// (selector) calls and getState().
vi.mock("@multica/core/chat", () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1",
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, UploadResult[]>,
    setInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    addInputDraftAttachment: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  return {
    DRAFT_NEW_SESSION: "__draft_new__",
    newSessionDraftKey: (agentId: string | null) => `__draft_new__:${agentId ?? ""}`,
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) =>
        selector ? selector(state) : state,
      { getState: () => state },
    ),
  };
});

import { ChatInput } from "./chat-input";
import { useChatStore } from "@multica/core/chat";

type ChatInputOnSend = React.ComponentProps<typeof ChatInput>["onSend"];
type ChatInputCommit = Parameters<ChatInputOnSend>[2];

beforeEach(() => {
  dropHandlers.onDrop = null;
  editorProps.last = null;
  editorState.cleared = 0;
  editorState.blurred = 0;
  const state = useChatStore.getState() as unknown as {
    activeSessionId: string | null;
    selectedAgentId: string;
    inputDrafts: Record<string, string>;
    setInputDraft: ReturnType<typeof vi.fn>;
    clearInputDraft: ReturnType<typeof vi.fn>;
    inputDraftAttachments: Record<string, UploadResult[]>;
    setInputDraftAttachments: ReturnType<typeof vi.fn>;
    addInputDraftAttachment: ReturnType<typeof vi.fn>;
  };
  state.activeSessionId = null;
  state.selectedAgentId = "agent-1";
  state.inputDrafts = {};
  state.inputDraftAttachments = {};
  state.setInputDraft.mockClear();
  state.setInputDraft.mockImplementation((key: string, value: string) => {
    state.inputDrafts[key] = value;
  });
  state.setInputDraftAttachments.mockClear();
  state.setInputDraftAttachments.mockImplementation((key: string, attachments: UploadResult[]) => {
    if (attachments.length > 0) state.inputDraftAttachments[key] = attachments;
    else delete state.inputDraftAttachments[key];
  });
  state.addInputDraftAttachment.mockClear();
  state.addInputDraftAttachment.mockImplementation((key: string, attachment: UploadResult) => {
    const existing = state.inputDraftAttachments[key] ?? [];
    state.inputDraftAttachments[key] = existing.some((a) => a.id === attachment.id)
      ? existing.map((a) => (a.id === attachment.id ? attachment : a))
      : [...existing, attachment];
  });
  state.clearInputDraft.mockClear();
  state.clearInputDraft.mockImplementation((key: string) => {
    delete state.inputDrafts[key];
    delete state.inputDraftAttachments[key];
  });
});

function renderInput(props: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  const onSend = props.onSend ?? vi.fn();
  const onUploadFile =
    props.onUploadFile ??
    vi.fn(async (_file: File) =>
      makeUpload({ id: "att-1", link: "https://cdn.example/att-1.png", filename: "img.png" }),
    );
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={onSend} onUploadFile={onUploadFile} agentName="Multica" {...props} />
    </I18nProvider>,
  );
  return { onSend, onUploadFile };
}

describe("ChatInput @ context wiring", () => {
  it("configures chat @ with current/recent issue/project context", () => {
    const contextItems = [
      { id: "issue-1", label: "MUL-1", type: "issue" as const, group: "current" as const },
    ];

    renderInput({ contextItems });

    expect(editorProps.last?.mentionMode).toBe("context");
    expect(editorProps.last?.mentionContextItems).toBe(contextItems);
  });
});

describe("ChatInput attachment wiring", () => {
  it("routes dropped files through the editor's upload handler", async () => {
    const { onUploadFile } = renderInput();
    expect(dropHandlers.onDrop).not.toBeNull();
    const file = new File(["x"], "drop.png", { type: "image/png" });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      // Microtask: the mock editor awaits onUploadFile before mutating its value.
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(onUploadFile).toHaveBeenCalledWith(file);
  });

  it("passes attachment_ids to onSend for uploads still referenced in the content", async () => {
    const onSend = vi.fn();
    const onUploadFile = vi.fn(async (_file: File) =>
      makeUpload({ id: "att-42", link: "https://cdn.example/att-42.png", filename: "x.png" }),
    );
    renderInput({ onSend, onUploadFile });

    // Simulate the drop → editor.uploadFile → onUploadFile happy path. The
    // mock editor appends the markdown link into its value and calls
    // onUpdate so the input flips out of the empty state.
    const file = new File(["x"], "drop.png", { type: "image/png" });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
      await Promise.resolve();
    });

    // Wait for the submit button to become enabled (onUpdate has fired and
    // React has re-rendered). SubmitButton has no aria-label, so we pick
    // the last action button on the bar (FileUploadButton, SubmitButton).
    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledTimes(1);
    const [, ids] = onSend.mock.calls[0]!;
    expect(ids).toEqual(["att-42"]);
    expect(useChatStore.getState().addInputDraftAttachment).toHaveBeenCalledWith(
      "__draft_new__:agent-1",
      expect.objectContaining({ id: "att-42" }),
    );
  });

  it("binds attachment_ids when the upload's markdownLink differs from its link (MUL-3130 regression)", async () => {
    // Pin: real LocalStorage uploads return `link` =
    // /uploads/<key>?exp&sig (short-lived) and `markdownLink` =
    // /api/attachments/<id>/download (stable). The editor persists
    // `markdownLink` into the markdown body, so chat-input's upload
    // map MUST key on `markdownLink` too — keying on `link` would
    // leave content.includes(url) false at send time and silently
    // drop the attachment binding. This is exactly the blocker
    // GPT-Boy raised in PR #3937 review.
    const onSend = vi.fn();
    const SHORT_LIVED_LINK = "/uploads/workspaces/ws-1/foo.png?exp=42&sig=stale";
    const STABLE_MARKDOWN_LINK = "/api/attachments/att-99/download";
    const onUploadFile = vi.fn(async (_file: File) =>
      makeUpload({
        id: "att-99",
        link: SHORT_LIVED_LINK,
        markdownLink: STABLE_MARKDOWN_LINK,
        filename: "foo.png",
      }),
    );
    renderInput({ onSend, onUploadFile });

    const file = new File(["x"], "foo.png", { type: "image/png" });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
      await Promise.resolve();
    });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledTimes(1);
    const [content, ids] = onSend.mock.calls[0]!;
    // The markdown body carries the stable URL — the short-lived
    // signed `?exp&sig` link must never make it into the message body.
    expect(content).toContain(STABLE_MARKDOWN_LINK);
    expect(content).not.toContain("?exp=");
    expect(content).not.toContain("?sig=");
    // And the attachment id is bound, even though `result.link` no
    // longer matches the URL the editor actually persisted.
    expect(ids).toEqual(["att-99"]);
  });

  it("disables send while an upload is in flight, re-enables after it resolves", async () => {
    let resolveUpload: (v: UploadResult) => void;
    const uploadPromise = new Promise<UploadResult>((res) => {
      resolveUpload = res;
    });
    const onSend = vi.fn();
    const onUploadFile = vi.fn(() => uploadPromise);
    renderInput({ onSend, onUploadFile });

    // Give the editor some text so isEmpty=false — this isolates the
    // disabled state to the pending-upload condition (otherwise both
    // checks would fire and the test couldn't tell them apart).
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "preview text" } });

    const file = new File(["x"], "slow.png", { type: "image/png" });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
    });

    // While the upload is pending the SubmitButton must be disabled.
    // Bypassing this would send the message with the attachment id
    // missing from the body.
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      const sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).toBeDisabled();
    });

    await act(async () => {
      resolveUpload!(makeUpload({ id: "att-slow", link: "https://cdn.example/att-slow.png", filename: "slow.png" }));
      await Promise.resolve();
    });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);
    expect(onSend).toHaveBeenCalledTimes(1);
    const [, ids] = onSend.mock.calls[0]!;
    expect(ids).toEqual(["att-slow"]);
  });

  it("does not render the file upload button when onUploadFile is omitted", () => {
    renderInput({ onUploadFile: undefined });
    // FileUploadButton renders an icon button labelled by its tooltip — when
    // upload wiring is absent the chat input falls back to "submit + extras"
    // only. Probe by counting buttons: with no upload, only the submit
    // button is in the action row.
    const buttons = screen.getAllByRole("button");
    // The agent picker may render zero buttons
    // in this test (no leftAdornment passed). So a single button = submit.
    expect(buttons.length).toBe(1);
  });
});

describe("ChatInput async send", () => {
  it("restores a cancelled empty run draft into the editor", async () => {
    const onRestoreDraftConsumed = vi.fn();
    renderInput({
      restoreDraftRequest: {
        id: "msg-restored",
        content: "bring this back",
      },
      onRestoreDraftConsumed,
    });

    await waitFor(() => {
      expect(useChatStore.getState().setInputDraft).toHaveBeenCalledWith(
        "__draft_new__:agent-1",
        "bring this back",
      );
      expect(editorProps.last?.defaultValue).toBe("bring this back");
      expect(onRestoreDraftConsumed).toHaveBeenCalledTimes(1);
    });
  });

  it("consumes a restore request even when an existing draft blocks restore", async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.inputDrafts["__draft_new__:agent-1"] = "already typing";
    const onRestoreDraftConsumed = vi.fn();

    renderInput({
      restoreDraftRequest: {
        id: "msg-restored",
        content: "bring this back",
      },
      onRestoreDraftConsumed,
    });

    await waitFor(() => {
      expect(onRestoreDraftConsumed).toHaveBeenCalledTimes(1);
    });
    expect(state.setInputDraft).not.toHaveBeenCalledWith(
      "__draft_new__:agent-1",
      "bring this back",
    );
  });

  it("keeps the draft while send is pending until the owner commits the handoff", async () => {
    let resolveSend: (accepted: boolean) => void;
    const sendPromise = new Promise<boolean>((res) => {
      resolveSend = res;
    });
    const onSend = vi.fn<ChatInputOnSend>(() => sendPromise);
    renderInput({ onSend });

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "slow network" } });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });

    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledWith(
      "slow network",
      undefined,
      expect.any(Function),
      [],
    );
    expect(useChatStore.getState().clearInputDraft).not.toHaveBeenCalled();
    await waitFor(() => expect(sendButton!).toBeDisabled());

    const commitInput = onSend.mock.calls[0]![2] as ChatInputCommit;
    act(() => {
      commitInput({ extraDraftKeys: ["session-1"] });
    });

    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__:agent-1");
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("session-1");

    await act(async () => {
      resolveSend!(true);
      await sendPromise;
    });

    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledTimes(2);
  });

  it("keeps the draft when send is rejected by the owner", async () => {
    const onSend = vi.fn(async () => false);
    renderInput({ onSend });

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "retry me" } });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(sendButton!);
      await Promise.resolve();
    });

    expect(onSend).toHaveBeenCalledWith("retry me", undefined, expect.any(Function), []);
    expect(useChatStore.getState().clearInputDraft).not.toHaveBeenCalled();
  });

  it("sends attachment ids restored from persisted draft attachments", async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
      inputDraftAttachments: Record<string, UploadResult[]>;
    };
    const attachment = makeUpload({
      id: "att-persisted",
      link: "/api/attachments/att-persisted/download",
      filename: "persisted.png",
    });
    state.inputDrafts["__draft_new__:agent-1"] = "see ![](/api/attachments/att-persisted/download)";
    state.inputDraftAttachments["__draft_new__:agent-1"] = [attachment];

    const onSend = vi.fn<ChatInputOnSend>((_content, _ids, commitInput) => {
      commitInput();
      return true;
    });
    renderInput({ onSend });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });

    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledWith(
      "see ![](/api/attachments/att-persisted/download)",
      ["att-persisted"],
      expect.any(Function),
      [attachment],
    );
  });
});

// A failed fire-and-forget send must restore into the session it was sent
// FROM, never into whatever session the user navigated to in the meantime.
describe("ChatInput session-aware restore", () => {
  function element(props: Partial<React.ComponentProps<typeof ChatInput>>) {
    return (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} onUploadFile={vi.fn()} agentName="Multica" {...props} />
      </I18nProvider>
    );
  }

  it("holds a session-scoped restore until the user returns to the source session", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    // User is viewing session-b; the failed send belongs to session-a.
    state.activeSessionId = "session-b";
    const onRestoreDraftConsumed = vi.fn();
    const props = {
      restoreDraftRequest: { id: "r1", content: "from A", sessionId: "session-a" },
      onRestoreDraftConsumed,
    };
    const { rerender } = render(element(props));

    // Pending — must NOT dump A's content into session-b.
    expect(onRestoreDraftConsumed).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith("session-b", "from A");

    // User navigates back to the source session → the pending restore fires.
    state.activeSessionId = "session-a";
    rerender(element(props));

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith("session-a", "from A");
      expect(onRestoreDraftConsumed).toHaveBeenCalledTimes(1);
    });
  });

  it("consumes a session-scoped restore when already on that session", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.activeSessionId = "session-a";
    const onRestoreDraftConsumed = vi.fn();
    render(
      element({
        restoreDraftRequest: { id: "r2", content: "hi A", sessionId: "session-a" },
        onRestoreDraftConsumed,
      }),
    );

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith("session-a", "hi A");
      expect(onRestoreDraftConsumed).toHaveBeenCalledTimes(1);
    });
  });
});

// commitInput is the handoff: the owner (ChatWindow) decides WHEN and HOW to
// clear the input. clearEditor:false is the fire-and-forget case — the user
// navigated away, so the shared editor now shows another session's draft and
// must not be scrubbed, but the SENT draft's data is still cleared.
describe("ChatInput commit handoff", () => {
  async function typeAndSend(onSend: ChatInputOnSend) {
    renderInput({ onSend });
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "msg" } });
    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);
    await waitFor(() => expect(onSend).toHaveBeenCalled());
  }

  it("scrubs the editor and clears the draft on a normal commit", async () => {
    const onSend = vi.fn<ChatInputOnSend>((_content, _ids, commitInput) => {
      commitInput();
      return true;
    });
    await typeAndSend(onSend);

    expect(editorState.cleared).toBeGreaterThan(0);
    expect(editorState.blurred).toBeGreaterThan(0);
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__:agent-1");
  });

  it("leaves the editor intact on a fire-and-forget commit but still clears the sent draft", async () => {
    const onSend = vi.fn<ChatInputOnSend>((_content, _ids, commitInput) => {
      commitInput({ clearEditor: false });
      return true;
    });
    await typeAndSend(onSend);

    // Editor untouched — it now shows the session the user navigated to.
    expect(editorState.cleared).toBe(0);
    expect(editorState.blurred).toBe(0);
    // …but the sent session's persisted draft is cleared regardless.
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__:agent-1");
  });
});
