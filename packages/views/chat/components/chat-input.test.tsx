import { forwardRef, useEffect, useRef, useImperativeHandle } from "react";
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
const editorState = vi.hoisted(() => ({ cleared: 0, blurred: 0, focused: 0 }));

vi.mock("../../editor", async () => ({
  // Real submit gate (pure React) driven by the mock editor's
  // `hasActiveUploads` / `onUploadingChange`.
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>(
    "../../editor/use-upload-gate",
  )),
  useFileDropZone: ({ onDrop }: { onDrop: (files: File[]) => void }) => {
    dropHandlers.onDrop = onDrop;
    return { isDragOver: false, dropZoneProps: { "data-testid": "drop-zone" } };
  },
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    props: {
      defaultValue?: string;
      value?: string;
      onUpdate?: (md: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      mentionMode?: string;
      mentionContextItems?: unknown[];
    },
    ref: React.Ref<unknown>,
  ) {
    const {
      defaultValue,
      value,
      onUpdate,
      placeholder,
      onUploadFile,
      onUploadingChange,
    } = props;
    editorProps.last = props as unknown as Record<string, unknown>;
    const valueRef = useRef<string>(value ?? defaultValue ?? "");
    const uploadingRef = useRef(0);
    useEffect(() => {
      if (value !== undefined) valueRef.current = value;
    }, [value]);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        editorState.cleared += 1;
        valueRef.current = "";
      },
      blur: () => {
        editorState.blurred += 1;
      },
      focus: () => {
        editorState.focused += 1;
      },
      uploadFile: async (file: File) => {
        uploadingRef.current += 1;
        // Mirror the real editor: the pending node lands before the await, and
        // the host learns about it through onUploadingChange, not by polling.
        if (uploadingRef.current === 1) onUploadingChange?.(true);
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
          if (uploadingRef.current === 0) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => uploadingRef.current > 0,
      // This mock emits onUpdate synchronously, so a pending debounced update
      // never exists and there is nothing to hand back. The real debounce (and
      // the draft-switch flush that depends on it) is covered against the real
      // ContentEditor in chat-input-draft-isolation.test.tsx.
      flushPendingUpdate: () => null,
      // Same file: the upload-pinned adopt path needs the real Guard 0, which
      // this mock has no concept of. Kept so the ref honours the full contract.
      adoptContent: (markdown: string) => {
        valueRef.current = markdown;
      },
    }));
    return (
      <textarea
        data-testid="editor"
        defaultValue={value ?? defaultValue}
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
  editorState.focused = 0;
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

function element(props: Partial<React.ComponentProps<typeof ChatInput>>) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={vi.fn()} onUploadFile={vi.fn()} agentName="Multica" {...props} />
    </I18nProvider>
  );
}

// MUL-4864: an uncreated chat has ONE draft per workspace. `selectedAgentId`
// picks where the first send goes; it does not own the draft. Switching agent
// mid-compose must therefore change nothing the user can see.
describe("ChatInput new-chat draft identity", () => {
  function switchAgentTo(agentId: string, rerender: (ui: React.ReactElement) => void) {
    const state = useChatStore.getState() as unknown as { selectedAgentId: string };
    state.selectedAgentId = agentId;
    // The mock store is not reactive; a real store switch re-renders the tree.
    rerender(element({ agentName: agentId }));
  }

  it("writes to the single new-chat slot regardless of the selected agent", () => {
    const { rerender } = render(element({ agentName: "agent-1" }));

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "half a thought" } });
    switchAgentTo("agent-2", rerender);
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "half a thought, finished" } });

    const state = useChatStore.getState() as unknown as { inputDrafts: Record<string, string> };
    // One slot, not one per agent — the hidden multi-draft state is gone.
    expect(Object.keys(state.inputDrafts)).toEqual(["__draft_new__"]);
    expect(state.inputDrafts["__draft_new__"]).toBe("half a thought, finished");
  });

  it("keeps the live editor instance across an agent switch", () => {
    const { rerender } = render(element({ agentName: "agent-1" }));
    const before = screen.getByTestId("editor");

    switchAgentTo("agent-2", rerender);

    // Identity, not just content: a remount would silently drop whatever the
    // 100ms draft debounce had not yet persisted — the last thing typed.
    expect(screen.getByTestId("editor")).toBe(before);
  });

  it("keeps text the draft debounce has not persisted yet across an agent switch", () => {
    const { rerender } = render(element({ agentName: "agent-1" }));
    // The uncontrolled textarea models the live editor document: text lives in
    // the instance, and only a remount can lose it.
    const editor = screen.getByTestId("editor") as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: "unsaved words" } });

    switchAgentTo("agent-2", rerender);

    expect((screen.getByTestId("editor") as HTMLTextAreaElement).value).toBe("unsaved words");
  });

  it("keeps staged attachments across an agent switch", async () => {
    const onUploadFile = vi.fn(async (_file: File) =>
      makeUpload({ id: "att-kept", link: "/api/attachments/att-kept/download", filename: "a.png" }),
    );
    const { rerender } = render(element({ agentName: "agent-1", onUploadFile }));

    await act(async () => {
      dropHandlers.onDrop?.([new File(["x"], "a.png", { type: "image/png" })]);
      await Promise.resolve();
    });
    switchAgentTo("agent-2", rerender);

    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, UploadResult[]>;
    };
    // Body and attachments share one attribution rule, so the files follow the
    // text across the switch instead of stranding in the old agent's slot.
    expect(state.inputDraftAttachments["__draft_new__"]?.map((a) => a.id)).toEqual(["att-kept"]);
    expect(Object.keys(state.inputDraftAttachments)).toEqual(["__draft_new__"]);
  });

  it("still gives each created session its own draft slot", () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = "session-a";
    const { rerender } = render(element({ agentName: "agent-1" }));
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "for A" } });

    state.activeSessionId = "session-b";
    rerender(element({ agentName: "agent-1" }));
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "for B" } });

    // Real sessions stay isolated — unifying the NEW-chat draft must not bleed
    // one conversation's context into another.
    expect(state.inputDrafts).toEqual({ "session-a": "for A", "session-b": "for B" });
  });
});

describe("ChatInput focusRequest", () => {
  it("focuses the editor when focusRequest becomes a non-zero value (new chat)", () => {
    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} agentName="Multica" focusRequest={0} />
      </I18nProvider>,
    );
    // The inert initial value must not steal focus (e.g. a plain deep-link open).
    expect(editorState.focused).toBe(0);

    // Starting a new chat bumps the nonce — the compose box grabs focus.
    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} agentName="Multica" focusRequest={1} />
      </I18nProvider>,
    );
    expect(editorState.focused).toBe(1);

    // Each subsequent new chat re-focuses.
    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} agentName="Multica" focusRequest={2} />
      </I18nProvider>,
    );
    expect(editorState.focused).toBe(2);
  });

  it("does not focus on mount when focusRequest is undefined or 0", () => {
    renderInput();
    expect(editorState.focused).toBe(0);
  });
});

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
    // the last action button on the bar (ChatAddMenu "+" is on the left,
    // SubmitButton is last).
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
      "__draft_new__",
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
    // The ChatAddMenu "+" (which hosts file upload) only mounts when upload
    // wiring is present — without it the chat input falls back to "submit +
    // extras" only. Probe by counting buttons: with no upload, only the
    // submit button is in the action row.
    const buttons = screen.getAllByRole("button");
    // The agent picker may render zero buttons
    // in this test (no leftAdornment passed). So a single button = submit.
    expect(buttons.length).toBe(1);
  });
});

describe("ChatInput async send", () => {
  it("restores a cancelled empty run draft into the editor", async () => {
    const onRestoreDraftApplied = vi.fn();
    renderInput({
      restoreDraftRequest: {
        id: "msg-restored",
        content: "bring this back",
      },
      onRestoreDraftApplied,
    });

    await waitFor(() => {
      expect(useChatStore.getState().setInputDraft).toHaveBeenCalledWith(
        "__draft_new__",
        "bring this back",
      );
      expect(editorProps.last?.value).toBe("bring this back");
      // The single terminal transition — the owner may now delete the server row.
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });

  // A restore the composer cannot apply yet must NOT be reported as done: the
  // owner keeps it pending (and the durable row un-consumed) and this composer
  // stays willing to take it. Marking it terminal here was the bug — the restore
  // was never re-offered for the rest of the component\'s life.
  it("waits — does not report — while an existing draft blocks the restore", async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.inputDrafts["__draft_new__"] = "already typing";
    const onRestoreDraftApplied = vi.fn();

    const { rerender } = render(
      element({
        restoreDraftRequest: { id: "msg-restored", content: "bring this back" },
        onRestoreDraftApplied,
      }),
    );

    await waitFor(() => {
      expect(editorProps.last?.value).toBe("already typing");
    });
    expect(onRestoreDraftApplied).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith(
      "__draft_new__",
      "bring this back",
    );

    // The user sends/clears what they were typing: the same restore, still
    // pending, now lands.
    state.inputDrafts["__draft_new__"] = "";
    rerender(
      element({
        restoreDraftRequest: { id: "msg-restored", content: "bring this back" },
        onRestoreDraftApplied,
      }),
    );

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith(
        "__draft_new__",
        "bring this back",
      );
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });

  it("holds the restore when the draft has staged attachments but no text", async () => {
    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, { id: string }[]>;
      setInputDraft: ReturnType<typeof vi.fn>;
      setInputDraftAttachments: ReturnType<typeof vi.fn>;
    };
    state.inputDraftAttachments["__draft_new__"] = [{ id: "att-staged" }];
    const onRestoreDraftApplied = vi.fn();

    renderInput({
      restoreDraftRequest: {
        id: "msg-restored",
        content: "bring this back",
      },
      onRestoreDraftApplied,
    });

    await waitFor(() => {
      expect(editorProps.last).toBeTruthy();
    });
    expect(onRestoreDraftApplied).not.toHaveBeenCalled();
    // The staged attachment list must never be replaced by the restore.
    expect(state.setInputDraftAttachments).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith(
      "__draft_new__",
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

    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__");
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
    state.inputDrafts["__draft_new__"] = "see ![](/api/attachments/att-persisted/download)";
    state.inputDraftAttachments["__draft_new__"] = [attachment];

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
// The send affordance must not hang off `isEmpty` alone. ChatInput does not
// remount on a session switch and ContentEditor's synchronized value uses
// emitUpdate:false, so a draft that arrives from the store — a restore parked
// by useChatDraftRestore, or any draft typed in another session — never moves
// `isEmpty`. Pinning the button to it left the user staring at their own text
// with Send greyed out.
describe("ChatInput send affordance", () => {
  function sendButton() {
    const buttons = screen.getAllByRole("button");
    return buttons[buttons.length - 1]!;
  }

  it("enables Send for a draft that arrived from the store, not from typing", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    // Mount on an EMPTY session: isEmpty initializes to true. The bug needs this
    // instance to survive the session switch — ChatInput is never remounted for
    // one, so isEmpty keeps the value it took here.
    state.activeSessionId = "session-a";
    state.inputDrafts = { "session-b": "the text that failed to send" };

    const { rerender } = render(element({}));
    expect(sendButton()).toBeDisabled();

    // Switch to the session holding the parked draft. The editor adopts it via
    // the value sync (emitUpdate:false → no onUpdate → isEmpty untouched).
    state.activeSessionId = "session-b";
    rerender(element({}));

    await waitFor(() => expect(sendButton()).not.toBeDisabled());
  });

  it("stays disabled when neither the editor nor the draft slot has content", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = "session-b";
    state.inputDrafts = {};

    renderInput();

    await waitFor(() => expect(sendButton()).toBeDisabled());
  });

  // The neighbouring path a naive "reset isEmpty on draftKey change" fix would
  // have broken: the first upload in a brand-new chat lazily creates the session,
  // flipping draftKey from the per-agent slot to the session id mid-compose. The
  // editor keeps the typed text; the new draft slot is empty. Send must stay live.
  it("keeps Send enabled when a lazy session create flips the draft key under a typed editor", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = null;
    state.inputDrafts = {};

    const { rerender } = render(element({}));
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "typed before the session existed" } });
    await waitFor(() => expect(sendButton()).not.toBeDisabled());

    // ensureSession lands: draftKey flips to a slot the draft was never written to.
    state.activeSessionId = "session-new";
    rerender(element({}));

    expect(sendButton()).not.toBeDisabled();
  });
});

describe("ChatInput session-aware restore", () => {
  it("holds a session-scoped restore until the user returns to the source session", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    // User is viewing session-b; the failed send belongs to session-a.
    state.activeSessionId = "session-b";
    const onRestoreDraftApplied = vi.fn();
    const props = {
      restoreDraftRequest: { id: "r1", content: "from A", sessionId: "session-a" },
      onRestoreDraftApplied,
    };
    const { rerender } = render(element(props));

    // Pending — must NOT dump A's content into session-b.
    expect(onRestoreDraftApplied).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith("session-b", "from A");

    // User navigates back to the source session → the pending restore fires.
    state.activeSessionId = "session-a";
    rerender(element(props));

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith("session-a", "from A");
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });

  it("consumes a session-scoped restore when already on that session", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.activeSessionId = "session-a";
    const onRestoreDraftApplied = vi.fn();
    render(
      element({
        restoreDraftRequest: { id: "r2", content: "hi A", sessionId: "session-a" },
        onRestoreDraftApplied,
      }),
    );

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith("session-a", "hi A");
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
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
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__");
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
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__");
  });
});
