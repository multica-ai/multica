import { cloneElement, forwardRef, useEffect, useRef, useImperativeHandle } from "react";
import { beforeEach, describe, it, expect, vi } from "vitest";
import { act, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import type { DraftUpload } from "@multica/core/drafts";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import enEditor from "../../locales/en/editor.json";

// Uploads flow through the module-level coordinator, which calls
// `api.uploadFile(file, ctx, signal)` (MUL-5181 L2). Tests drive uploads by
// mocking that call; it resolves a server Attachment row (makeUpload's extra
// link/markdownLink fields are ignored by the engine, which re-derives them).
const mockApiUploadFile = vi.hoisted(() => vi.fn());
// Observability for the write-back insert path: a settle whose mount died
// delivers into the live editor through this method.
const insertMarkdownSpy = vi.hoisted(() => vi.fn());

// The real handle mints an id when it inserts the placeholder and hands it to
// the uploader, which adopts it as the draft `clientUploadId`. Mocks must do
// the same or the two records drift apart only in tests.
let mockUploadIdSeq = 0;

vi.mock("@multica/core/api", () => ({
  api: { uploadFile: mockApiUploadFile },
}));

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

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat, editor: enEditor } };

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
  // Real await-then-render submit contract (pure React) — it only imports
  // types from ContentEditor / the upload gate, so it pulls in no Tiptap tree.
  ...(await vi.importActual<typeof import("../../editor/use-composer-submit")>(
    "../../editor/use-composer-submit",
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
      onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
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
          const result = await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
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
      // Placeholder rebuild contract: the real handle draws a card for an
      // upload the document is not showing and reports whether it landed.
      // Mocks track ids only — no document to draw into.
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
      insertMarkdownAtEnd: (md: string) => {
        insertMarkdownSpy(md);
        valueRef.current = `${valueRef.current}\n\n${md}`.trim();
        onUpdate?.(valueRef.current);
        return true;
      },
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

vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: ({
    projectId,
    onUpdate,
    triggerRender,
    disabled,
  }: {
    projectId: string;
    onUpdate: (updates: { project_id: string | null }) => void;
    triggerRender: React.ReactElement<{
      onClick?: () => void;
      children?: React.ReactNode;
      "data-project-picker-disabled"?: string;
    }>;
    disabled?: boolean;
  }) =>
    // Surface the `disabled` prop ChatInput passes so a test can assert the
    // shared picker (and thus its keyboard clear control) is locked mid-send.
    // The real keyboard-inertness of that control is covered against the real
    // ProjectPicker in project-picker.test.tsx.
    cloneElement(triggerRender, {
      onClick: () => onUpdate({ project_id: null }),
      children: projectId,
      "data-project-picker-disabled": disabled ? "true" : "false",
    }),
}));

// Mock chat store with an in-memory implementation that supports both
// (selector) calls and getState(). Draft attachments hold coordinator-owned
// DraftUpload entries (MUL-5181 L2).
vi.mock("@multica/core/chat", () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1",
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn(),
    appendToInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    addInputDraftAttachment: vi.fn(),
    addInputDraftUpload: vi.fn(),
    settleInputDraftUpload: vi.fn(),
    failInputDraftUpload: vi.fn(),
    removeInputDraftUpload: vi.fn(),
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
    appendToInputDraft: ReturnType<typeof vi.fn>;
    clearInputDraft: ReturnType<typeof vi.fn>;
    inputDraftAttachments: Record<string, DraftUpload[]>;
    setInputDraftAttachments: ReturnType<typeof vi.fn>;
    addInputDraftAttachment: ReturnType<typeof vi.fn>;
    addInputDraftUpload: ReturnType<typeof vi.fn>;
    settleInputDraftUpload: ReturnType<typeof vi.fn>;
    failInputDraftUpload: ReturnType<typeof vi.fn>;
    removeInputDraftUpload: ReturnType<typeof vi.fn>;
  };
  state.activeSessionId = null;
  state.selectedAgentId = "agent-1";
  state.inputDrafts = {};
  state.inputDraftAttachments = {};
  state.setInputDraft.mockClear();
  state.setInputDraft.mockImplementation((key: string, value: string) => {
    state.inputDrafts[key] = value;
  });
  state.appendToInputDraft.mockClear();
  // Mirrors the real store: trailing whitespace trimmed before the separator.
  state.appendToInputDraft.mockImplementation((key: string, markdown: string) => {
    const existing = state.inputDrafts[key] ?? "";
    state.inputDrafts[key] = existing.trim()
      ? `${existing.replace(/\s+$/, "")}\n\n${markdown}`
      : markdown;
  });
  state.setInputDraftAttachments.mockClear();
  state.setInputDraftAttachments.mockImplementation((key: string, uploads: DraftUpload[]) => {
    if (uploads.length > 0) state.inputDraftAttachments[key] = uploads;
    else delete state.inputDraftAttachments[key];
  });
  state.addInputDraftAttachment.mockClear();
  state.addInputDraftAttachment.mockImplementation((key: string, attachment: UploadResult) => {
    const existing = state.inputDraftAttachments[key] ?? [];
    state.inputDraftAttachments[key] = [
      ...existing,
      {
        clientUploadId: attachment.id,
        status: "uploaded",
        filename: attachment.filename,
        size: attachment.size_bytes,
        attachment,
      } as DraftUpload,
    ];
  });
  state.addInputDraftUpload.mockClear();
  state.addInputDraftUpload.mockImplementation((key: string, upload: DraftUpload) => {
    const existing = state.inputDraftAttachments[key] ?? [];
    if (existing.some((u) => u.clientUploadId === upload.clientUploadId)) return;
    state.inputDraftAttachments[key] = [...existing, upload];
  });
  state.settleInputDraftUpload.mockClear();
  state.settleInputDraftUpload.mockImplementation(
    (key: string, clientUploadId: string, attachment: UploadResult) => {
      const existing = state.inputDraftAttachments[key] ?? [];
      state.inputDraftAttachments[key] = existing.map((u) =>
        u.clientUploadId === clientUploadId
          ? ({
              clientUploadId,
              status: "uploaded",
              filename: attachment.filename,
              size: attachment.size_bytes,
              attachment,
            } as DraftUpload)
          : u,
      );
    },
  );
  state.failInputDraftUpload.mockClear();
  state.failInputDraftUpload.mockImplementation(
    (key: string, clientUploadId: string, error?: string) => {
      const existing = state.inputDraftAttachments[key] ?? [];
      state.inputDraftAttachments[key] = existing.map((u) =>
        u.clientUploadId === clientUploadId
          ? ({ ...u, status: "failed", error } as DraftUpload)
          : u,
      );
    },
  );
  state.removeInputDraftUpload.mockClear();
  state.removeInputDraftUpload.mockImplementation((key: string, clientUploadId: string) => {
    const remaining = (state.inputDraftAttachments[key] ?? []).filter(
      (u) => u.clientUploadId !== clientUploadId,
    );
    if (remaining.length > 0) state.inputDraftAttachments[key] = remaining;
    else delete state.inputDraftAttachments[key];
  });
  state.clearInputDraft.mockClear();
  state.clearInputDraft.mockImplementation((key: string) => {
    delete state.inputDrafts[key];
    delete state.inputDraftAttachments[key];
  });
  mockApiUploadFile.mockReset();
  mockApiUploadFile.mockImplementation(async () =>
    makeUpload({ id: "att-1", link: "https://cdn.example/att-1.png", filename: "img.png" }),
  );
  insertMarkdownSpy.mockReset();
});

function renderInput(props: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  const onSend = props.onSend ?? vi.fn();
  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={onSend} uploadEnabled agentName="Multica" {...props} />
    </I18nProvider>,
  );
  return { onSend, ...view };
}

function element(props: Partial<React.ComponentProps<typeof ChatInput>>) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={vi.fn()} uploadEnabled agentName="Multica" {...props} />
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
    mockApiUploadFile.mockImplementation(async () =>
      makeUpload({ id: "att-kept", link: "/api/attachments/att-kept/download", filename: "a.png" }),
    );
    const { rerender } = render(element({ agentName: "agent-1" }));

    await act(async () => {
      dropHandlers.onDrop?.([new File(["x"], "a.png", { type: "image/png" })]);
      await Promise.resolve();
    });
    switchAgentTo("agent-2", rerender);

    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };
    // Body and attachments share one attribution rule, so the files follow the
    // text across the switch instead of stranding in the old agent's slot.
    expect(
      state.inputDraftAttachments["__draft_new__"]?.map((u) =>
        u.status === "uploaded" ? u.attachment.id : u.status,
      ),
    ).toEqual(["att-kept"]);
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

describe("ChatInput project context", () => {
  type ChatProject = NonNullable<
    React.ComponentProps<typeof ChatInput>["projects"]
  >[number];
  const sampleProject: ChatProject = {
    id: "project-alpha",
    workspace_id: "ws-1",
    title: "Project Alpha",
    description: null,
    icon: "📘",
    status: "planned",
    priority: "none",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
  };

  it("warns next to the chip when the agent's daemon cannot apply the project description", () => {
    renderInput({
      projects: [sampleProject],
      projectId: "project-alpha",
      onProjectChange: vi.fn(),
      projectContextUnsupported: true,
    });

    expect(
      screen.getByText(
        "Project description won't apply — this agent's daemon needs an upgrade",
      ),
    ).toBeInTheDocument();
    // Soft gate: the warning must not lock the control.
    expect(
      screen.getByRole("button", { name: "Change project context" }),
    ).not.toBeDisabled();
  });

  it("shows no daemon warning when support is current or unknown", () => {
    renderInput({
      projects: [sampleProject],
      projectId: "project-alpha",
      onProjectChange: vi.fn(),
    });

    expect(
      screen.queryByText(
        "Project description won't apply — this agent's daemon needs an upgrade",
      ),
    ).not.toBeInTheDocument();
  });

  it("renders the selected project chip and forwards context changes", () => {
    const onProjectChange = vi.fn();
    renderInput({
      projects: [sampleProject],
      projectId: "project-alpha",
      onProjectChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Change project context" }));

    expect(onProjectChange).toHaveBeenCalledWith(null);
  });

  it("allows removing project context while the agent is running", () => {
    const onProjectChange = vi.fn();
    renderInput({
      projects: [sampleProject],
      projectId: "project-alpha",
      onProjectChange,
      isRunning: true,
    });

    const projectControl = screen.getByRole("button", {
      name: "Change project context",
    });
    expect(projectControl).not.toBeDisabled();
    fireEvent.click(projectControl);
    expect(onProjectChange).toHaveBeenCalledWith(null);
  });

  it("swaps Stop for Queue Send when the running composer has content", async () => {
    const onSend = vi.fn<ChatInputOnSend>(async () => true);
    const onStop = vi.fn();
    renderInput({ isRunning: true, allowSubmitWhileRunning: true, onSend, onStop });

    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Queue message" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "follow-up" },
    });

    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();
    const queueButton = screen.getByRole("button", { name: "Queue message" });
    expect(queueButton).not.toBeDisabled();
    fireEvent.click(queueButton);

    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
    });
    expect(screen.queryByRole("button", { name: "Queue message" })).not.toBeInTheDocument();
  });

  it("keeps Stop available while queued content is uploading", async () => {
    let resolveUpload!: (value: UploadResult) => void;
    mockApiUploadFile.mockImplementation(
      () => new Promise<UploadResult>((resolve) => (resolveUpload = resolve)),
    );
    const onStop = vi.fn();
    renderInput({ isRunning: true, allowSubmitWhileRunning: true, onStop });

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "follow-up with attachment" },
    });
    expect(screen.getByRole("button", { name: "Queue message" })).toBeInTheDocument();

    await act(async () => {
      dropHandlers.onDrop?.([
        new File(["x"], "slow.png", { type: "image/png" }),
      ]);
      await Promise.resolve();
    });

    const stopButton = await screen.findByRole("button", { name: "Stop" });
    expect(screen.queryByRole("button", { name: "Queue message" })).not.toBeInTheDocument();
    fireEvent.click(stopButton);
    expect(onStop).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveUpload(
        makeUpload({
          id: "att-slow-stop",
          link: "/api/attachments/att-slow-stop/download",
          filename: "slow.png",
        }),
      );
      await Promise.resolve();
    });

    expect(await screen.findByRole("button", { name: "Queue message" })).not.toBeDisabled();
  });

  it("shows only Stop while an older server is running", () => {
    renderInput({ isRunning: true, onStop: vi.fn() });

    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Queue message" })).not.toBeInTheDocument();
  });

  it("keeps the composer chrome independent of the queue", () => {
    // The follow-up queue tucks its bottom edge under the composer, which
    // therefore ALWAYS paints on top (static z-10) — but the queue's presence
    // must never restyle the input surface itself.
    const { container } = renderInput();
    const surface = container.querySelector('[data-slot="chat-input-surface"]');

    expect(container.firstElementChild).toHaveClass("relative", "z-10");
    expect(surface).toHaveClass("rounded-lg");
    expect(surface).not.toHaveClass(
      "rounded-4xl",
      "shadow-[var(--menu-shadow)]",
    );
  });

  it("locks the project control while a send is in flight so a mid-send switch cannot retarget the session", async () => {
    // A brand-new chat creates its session row lazily during send, bound to
    // the project selected at click time. If the user could switch project
    // while that create is in flight, the session would be created against the
    // old project while the UI already shows the new one — the agent would
    // then receive a project/repo context the user no longer intends
    // (MUL-5150). The control must stay locked for the whole send, not only
    // once the agent is running.
    let resolveSend: (accepted: boolean) => void;
    const sendPromise = new Promise<boolean>((res) => {
      resolveSend = res;
    });
    const onSend = vi.fn<ChatInputOnSend>(() => sendPromise);
    const onProjectChange = vi.fn();
    renderInput({
      projects: [sampleProject],
      projectId: "project-alpha",
      onProjectChange,
      onSend,
    });

    // Interactive before send starts.
    expect(
      screen.getByRole("button", { name: "Change project context" }),
    ).not.toBeDisabled();

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "slow network" },
    });
    let sendBtn: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendBtn = buttons[buttons.length - 1]!;
      expect(sendBtn).not.toBeDisabled();
    });
    fireEvent.click(sendBtn!);

    // Send pending → project control locked; a click cannot fire a change.
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Change project context" }),
      ).toBeDisabled(),
    );
    // ChatInput also propagates the lock to the shared ProjectPicker so its
    // internal clear button (a keyboard-reachable path the wrapper's
    // pointer-events-none does not cover) is disabled too. The clear control's
    // keyboard-inertness itself is covered against the real ProjectPicker in
    // project-picker.test.tsx.
    expect(
      screen.getByRole("button", { name: "Change project context" }),
    ).toHaveAttribute("data-project-picker-disabled", "true");
    fireEvent.click(screen.getByRole("button", { name: "Change project context" }));
    expect(onProjectChange).not.toHaveBeenCalled();

    // Resolve the pending send so the promise doesn't dangle past the test.
    await act(async () => {
      resolveSend!(true);
      await sendPromise;
    });
  });
});

describe("ChatInput attachment wiring", () => {
  it("routes dropped files through the coordinator upload", async () => {
    renderInput();
    expect(dropHandlers.onDrop).not.toBeNull();
    const file = new File(["x"], "drop.png", { type: "image/png" });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      // Microtask: the mock editor awaits onUploadFile before mutating its value.
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockApiUploadFile).toHaveBeenCalledTimes(1);
    expect(mockApiUploadFile.mock.calls[0]?.[0]).toBe(file);
  });

  it("passes attachment_ids to onSend for uploads still referenced in the content", async () => {
    const onSend = vi.fn();
    mockApiUploadFile.mockImplementation(async () =>
      makeUpload({ id: "att-42", link: "https://cdn.example/att-42.png", filename: "x.png" }),
    );
    renderInput({ onSend });

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
    // The placeholder was staged in the LOADED draft slot at pick time and
    // settled into the uploaded entry the send just bound.
    expect(useChatStore.getState().addInputDraftUpload).toHaveBeenCalledWith(
      "__draft_new__",
      expect.objectContaining({ status: "uploading", filename: "drop.png" }),
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
    mockApiUploadFile.mockImplementation(async () =>
      makeUpload({
        id: "att-99",
        link: SHORT_LIVED_LINK,
        // The engine derives markdownLink from the row's markdown_url.
        markdown_url: STABLE_MARKDOWN_LINK,
        markdownLink: STABLE_MARKDOWN_LINK,
        filename: "foo.png",
      }),
    );
    renderInput({ onSend });

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
    mockApiUploadFile.mockImplementation(() => uploadPromise);
    renderInput({ onSend });

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

  it("delivers a dead mount's settle into the editor still HOLDING that draft, not the selected one", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };

    // Mount #1 composes to session-a and starts an upload that outlives it.
    state.activeSessionId = "session-a";
    let resolveA!: (v: UploadResult) => void;
    mockApiUploadFile.mockImplementationOnce(
      () => new Promise<UploadResult>((r) => (resolveA = r)),
    );
    const first = render(element({}));
    await act(async () => {
      dropHandlers.onDrop?.([new File(["x"], "a.png", { type: "image/png" })]);
      await Promise.resolve();
    });
    first.unmount();

    // Mount #2 also loads session-a, then gets PINNED to it by its own upload
    // while the user switches selection to session-b: loaded=A, selected=B.
    let resolveB!: (v: UploadResult) => void;
    mockApiUploadFile.mockImplementationOnce(
      () => new Promise<UploadResult>((r) => (resolveB = r)),
    );
    const second = render(element({}));
    await act(async () => {
      dropHandlers.onDrop?.([new File(["y"], "b.png", { type: "image/png" })]);
      await Promise.resolve();
    });
    state.activeSessionId = "session-b";
    second.rerender(element({}));

    // Mount #1's upload settles. Its target draft is session-a — which mount
    // #2's editor is HOLDING (not showing as selected). The registry must be
    // keyed by the LOADED draft, so the insert reaches that document; keying
    // by selection would misroute or drop it.
    await act(async () => {
      resolveA(
        makeUpload({
          id: "att-pinned",
          link: "/api/attachments/att-pinned/download",
          filename: "a.png",
        }),
      );
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(insertMarkdownSpy).toHaveBeenCalledWith(
      expect.stringContaining("/api/attachments/att-pinned/download"),
    );
    expect(state.inputDrafts["session-a"] ?? "").toContain(
      "/api/attachments/att-pinned/download",
    );
    // The selected-but-not-loaded draft must not receive the link.
    expect(state.inputDrafts["session-b"] ?? "").not.toContain("att-pinned");

    await act(async () => {
      resolveB(makeUpload({ id: "att-b", link: "/api/attachments/att-b/download", filename: "b.png" }));
      await Promise.resolve();
    });
  });

  it("text typed while a chat send is in flight survives the success", async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
    };
    let resolveSend!: (v: boolean) => void;
    const onSend = vi.fn(
      (
        _content: string,
        _ids: string[] | undefined,
        _commit: (o?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void,
      ) => new Promise<boolean>((r) => { resolveSend = r; }),
    );
    renderInput({ onSend: onSend as never });

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "draft A" } });
    const buttons = screen.getAllByRole("button");
    fireEvent.click(buttons[buttons.length - 1]!);
    await waitFor(() => expect(onSend).toHaveBeenCalled());

    // The editor stays interactive during the send; the debounced commit files
    // the newer text under the same slot mid-flight.
    fireEvent.change(editor, { target: { value: "draft B typed during send" } });

    await act(async () => {
      resolveSend(true);
      await Promise.resolve();
    });

    // Success may only clear what it sent — the newer draft survives, and the
    // editor was not scrubbed over it.
    expect(state.inputDrafts["__draft_new__"]).toBe("draft B typed during send");
    expect(editorState.cleared).toBe(0);
  });

  it("keeps an in-flight placeholder while the user keeps typing", () => {
    // commitDraft prunes uploaded rows the body no longer references; a
    // placeholder has no body reference yet, so a keystroke must never prune
    // it — that would drop the chip and strand the settle on the guard.
    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };
    state.inputDraftAttachments["__draft_new__"] = [
      { clientUploadId: "c-flight", status: "uploading", filename: "up.png", size: 1 },
    ];
    renderInput();

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "still typing" } });

    expect(state.inputDraftAttachments["__draft_new__"]).toHaveLength(1);
    expect(state.inputDraftAttachments["__draft_new__"]?.[0]).toMatchObject({
      status: "uploading",
      clientUploadId: "c-flight",
    });
  });

  it("does not render the file upload button when uploads are disabled", () => {
    renderInput({ uploadEnabled: false });
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
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };
    const attachment = makeUpload({
      id: "att-persisted",
      link: "/api/attachments/att-persisted/download",
      filename: "persisted.png",
    });
    state.inputDrafts["__draft_new__"] = "see ![](/api/attachments/att-persisted/download)";
    // Persisted drafts hold DraftUpload entries (the real store normalizes
    // legacy bare rows into this shape on load).
    state.inputDraftAttachments["__draft_new__"] = [
      {
        clientUploadId: "att-persisted",
        status: "uploaded",
        filename: attachment.filename,
        size: attachment.size_bytes,
        attachment,
      },
    ];

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
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith("__draft_new__");
    // Chat is a conversation: the caret returns to the box for the next turn
    // rather than being dropped (MUL-5181 follow-up).
    await waitFor(() => expect(editorState.focused).toBeGreaterThan(0));
    expect(editorState.blurred).toBe(0);
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
    // And the caret must NOT be pulled into an editor that is showing someone
    // else's draft: refocus is bound to having actually scrubbed the document.
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(editorState.focused).toBe(0);
  });
});
