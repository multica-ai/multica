import { forwardRef, useEffect, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import type { Attachment } from "@multica/core/types";
import { useCommentComposerStore, useCommentDraftStore } from "@multica/core/issues/stores";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { renderWithI18n } from "../../test/i18n";
import { CommentInput } from "./comment-input";
import { ReplyInput } from "./reply-input";

/** Shape of ContentEditor's `quickActionMenu` prop, as the composers pass it. */
type QuickActionMenuProp = {
  getQuickActions?: () => { id: string; name: string; description?: string }[];
  renderQuickAction?: (quickActionId: string) => Promise<string>;
  onRenderError?: (error: unknown) => void;
};

// Uploads now flow through the module-level coordinator, which calls
// `api.uploadFile(file, ctx, signal)` (MUL-5181). Tests drive uploads by
// mocking that call directly rather than the old `uploadWithToast` hook.
const apiUploadFile = vi.hoisted(() => vi.fn());
const apiListWorkspaces = vi.hoisted(() => vi.fn());
const apiListQuickActions = vi.hoisted(() => vi.fn());
const apiRenderQuickAction = vi.hoisted(() => vi.fn());
const uploadWithToast = vi.hoisted(() => vi.fn());
const editorDefaultValues = vi.hoisted(() => ({
  values: [] as Array<string | undefined>,
}));
// The `/` quick-action menu is wired through a ContentEditor prop, so the mock
// editor records it — that prop being absent is exactly the MUL-5588 bug.
const editorQuickActionMenu = vi.hoisted(() => ({
  last: undefined as QuickActionMenuProp | undefined,
}));
// Observability + failure control for the write-back insert path (MUL-5181):
// `insertMarkdownAtEnd` returns false while the (simulated) Tiptap instance
// doesn't exist yet, exactly like the real handle.
// Post-send caret policy: the top-level composer blurs (the page scrolls to the
// posted comment instead), a thread reply keeps the caret for the next reply.
const focusCalls = vi.hoisted(() => ({ focused: 0, blurred: 0 }));
const insertMarkdownSpy = vi.hoisted(() => vi.fn());
const insertPlaceholderSpy = vi.hoisted(() => vi.fn());
const insertMarkdownBehavior = vi.hoisted(() => ({ succeed: true }));
// Lets a test drop the editor's uploading placeholder out of the document
// (Cmd+Z after a paste) without settling the upload behind it.
const editorUploadSignal = vi.hoisted(
  () => ({ notify: undefined as ((uploading: boolean) => void) | undefined }),
);

// The real handle mints an id when it inserts the placeholder and hands it to
// the uploader, which adopts it as the draft `clientUploadId`. Mocks must do
// the same or the two records drift apart only in tests.
let mockUploadIdSeq = 0;

vi.mock("@multica/core/api", () => ({
  api: {
    uploadFile: apiUploadFile,
    listWorkspaces: apiListWorkspaces,
    listQuickActions: apiListQuickActions,
    renderQuickAction: apiRenderQuickAction,
  },
}));

vi.mock("@multica/core/hooks/use-file-upload", async () => ({
  ...(await vi.importActual<typeof import("@multica/core/hooks/use-file-upload")>(
    "@multica/core/hooks/use-file-upload",
  )),
  useFileUpload: () => ({ uploadWithToast }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="actor-avatar">
      {actorType}:{actorId}
    </span>
  ),
}));

vi.mock("../../editor", async () => ({
  // The lazy-mount controller is pure React (no Tiptap) — use the real one so
  // shell → activate → ready flows behave exactly as in production.
  ...(await vi.importActual<typeof import("../../editor/use-lazy-editor")>(
    "../../editor/use-lazy-editor",
  )),
  // Real submit gate (pure React) driven by the mock editor's
  // `hasActiveUploads` / `onUploadingChange` below.
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>(
    "../../editor/use-upload-gate",
  )),
  // Real await-then-render submit contract (pure React) — the composers now
  // delegate their send handler to it.
  ...(await vi.importActual<typeof import("../../editor/use-composer-submit")>(
    "../../editor/use-composer-submit",
  )),
  useEditorUpload: () => ({ uploadWithToast, upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({
    isDragOver: false,
    dropZoneProps: { "data-testid": "drop-zone" },
  }),
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      placeholder,
      onUploadFile,
      onUploadingChange,
      onSubmit,
      onReady,
      quickActionMenu,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      onSubmit?: () => void;
      onReady?: () => void;
      quickActionMenu?: QuickActionMenuProp;
    },
    ref: Ref<unknown>,
  ) {
    editorDefaultValues.values.push(defaultValue);
    editorQuickActionMenu.last = quickActionMenu;
    editorUploadSignal.notify = onUploadingChange;
    const valueRef = useRef(defaultValue ?? "");
    // Mirrors the real editor's `uploading` node attrs: the placeholder exists
    // from before the await until the upload settles, `hasActiveUploads` reads
    // it synchronously, and the host is told through onUploadingChange.
    const inFlightRef = useRef(0);
    // Mirrors `editor.isDestroyed`: a settle continuation after unmount must
    // not write the document or emit onUpdate (uploadAndInsertFile guards on
    // isDestroyed in production).
    const destroyedRef = useRef(false);

    useEffect(() => {
      onReady?.();
      return () => {
        destroyedRef.current = true;
      };
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
      },
      focus: () => { focusCalls.focused += 1; },
      focusAtCoords: () => {},
      blur: () => { focusCalls.blurred += 1; },
      uploadFile: async (file: File) => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          const result = await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
          if (!result || destroyedRef.current) return;
          valueRef.current = `${valueRef.current}\n${result.url}`.trim();
          onUpdate?.(valueRef.current);
        } finally {
          inFlightRef.current -= 1;
          if (inFlightRef.current === 0 && !destroyedRef.current) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => inFlightRef.current > 0,
      // Placeholder rebuild contract: the real handle draws a card for an
      // upload the document is not showing and reports whether it landed.
      // Mocks track ids only — no document to draw into.
      insertUploadPlaceholder: (upload: unknown) => {
        insertPlaceholderSpy(upload);
        return true;
      },
      settleUploadPlaceholder: () => false,
      insertMarkdownAtEnd: (md: string) => {
        insertMarkdownSpy(md);
        if (destroyedRef.current || !insertMarkdownBehavior.succeed) return false;
        valueRef.current = `${valueRef.current}\n\n${md}`.trim();
        onUpdate?.(valueRef.current);
        return true;
      },
    }));

    return (
      <textarea
        data-testid="editor"
        defaultValue={defaultValue}
        placeholder={placeholder}
        onChange={(event) => {
          valueRef.current = event.target.value;
          onUpdate?.(event.target.value);
        }}
        onKeyDown={(event) => {
          // The editor's Mod+Enter submit shortcut — the path that skips the
          // send button entirely.
          if ((event.metaKey || event.ctrlKey) && event.key === "Enter") onSubmit?.();
        }}
      />
    );
  }),
}));

function renderWithProviders(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

function renderCommentInput(onSubmit = vi.fn().mockResolvedValue(true)) {
  const view = renderWithProviders(<CommentInput issueId="issue-1" onSubmit={onSubmit} />);
  return { ...view, onSubmit };
}

function renderReplyInput({
  onSubmit = vi.fn().mockResolvedValue(true),
  size = "sm",
  draftKey,
}: {
  onSubmit?: (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => Promise<boolean>;
  size?: "sm" | "default";
  draftKey?: `reply:${string}:${string}`;
} = {}) {
  const view = renderWithProviders(
    <ReplyInput
      issueId="issue-1"
      parentId="comment-1"
      avatarType="member"
      avatarId="user-1"
      onSubmit={onSubmit}
      size={size}
      draftKey={draftKey}
    />,
  );
  return { ...view, onSubmit };
}

// Composers render readonly-first: a static shell until clicked (unless an
// unsent draft exists). Tests that interact with the editor activate it the
// same way a user does.
function activateComposer(shellTestId: "comment-composer-shell" | "reply-composer-shell") {
  fireEvent.click(screen.getByTestId(shellTestId));
}

function getSubmitButton(container: HTMLElement): HTMLButtonElement {
  // Submit is always the last button in a composer's action cluster.
  const buttons = container.querySelectorAll("button");
  const button = buttons[buttons.length - 1];
  if (!button) throw new Error("Expected submit button to render");
  return button;
}

beforeEach(() => {
  uploadWithToast.mockReset();
  apiUploadFile.mockReset();
  apiListWorkspaces.mockReset();
  apiListQuickActions.mockReset();
  apiRenderQuickAction.mockReset();
  insertMarkdownSpy.mockReset();
  insertPlaceholderSpy.mockReset();
  insertMarkdownBehavior.succeed = true;
  localStorage.clear();
  useCommentComposerStore.setState({ sticky: true });
  // The draft store is a module singleton — a draft left by a previous test
  // (e.g. the failed-send case) would trip the composers' draft-direct-mount
  // path and hide the shell the next test expects.
  useCommentDraftStore.setState({ drafts: {} });
  editorDefaultValues.values = [];
  editorQuickActionMenu.last = undefined;
  focusCalls.focused = 0;
  focusCalls.blurred = 0;
});

// ---------------------------------------------------------------------------
// Quick action `/` menu (MUL-5588)
// ---------------------------------------------------------------------------

describe("quick action `/` menu", () => {
  const workspace = { id: "ws-1", slug: "acme", name: "Acme" };

  function renderInWorkspace(ui: ReactNode) {
    apiListWorkspaces.mockResolvedValue([workspace]);
    apiListQuickActions.mockResolvedValue({
      quick_actions: [
        { id: "qa-1", name: "review", description: "Ask for a review", status: "active" },
        { id: "qa-2", name: "retired", description: "", status: "archived" },
      ],
    });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return renderWithI18n(
      <QueryClientProvider client={queryClient}>
        {/* useQuickActionMenu resolves the workspace from the slug context; without
            it the catalog query stays disabled and every menu reads as empty. */}
        <WorkspaceSlugProvider slug="acme">{ui}</WorkspaceSlugProvider>
      </QueryClientProvider>,
    );
  }

  // Both composers post to the same issue, so `/` must offer the same catalog in
  // both. The reply box shipped without the prop entirely (MUL-5588): its menu
  // listed only the built-in `/note` while the top-level composer listed the
  // workspace's quick actions.
  const composers = [
    {
      name: "top-level comment composer",
      shell: "comment-composer-shell" as const,
      element: <CommentInput issueId="issue-1" onSubmit={vi.fn().mockResolvedValue(true)} />,
    },
    {
      name: "thread reply composer",
      shell: "reply-composer-shell" as const,
      element: (
        <ReplyInput
          issueId="issue-1"
          parentId="comment-1"
          avatarType="member"
          avatarId="user-1"
          onSubmit={vi.fn().mockResolvedValue(true)}
        />
      ),
    },
  ];

  for (const composer of composers) {
    it(`offers the workspace's active quick actions in the ${composer.name}`, async () => {
      renderInWorkspace(composer.element);
      activateComposer(composer.shell);

      await waitFor(() => {
        expect(editorQuickActionMenu.last?.getQuickActions?.()).toEqual([
          { id: "qa-1", name: "review", description: "Ask for a review" },
        ]);
      });
    });

    it(`binds quick action rendering to this issue in the ${composer.name}`, async () => {
      apiRenderQuickAction.mockResolvedValue("rendered body");
      renderInWorkspace(composer.element);
      activateComposer(composer.shell);

      await waitFor(() => {
        expect(editorQuickActionMenu.last?.renderQuickAction).toBeTypeOf("function");
      });
      await editorQuickActionMenu.last?.renderQuickAction?.("qa-1");

      expect(apiRenderQuickAction).toHaveBeenCalledWith("issue-1", "qa-1");
    });
  }
});

describe("comment composers", () => {
  it("renders the main comment composer without a manual expand control", () => {
    const { container } = renderCommentInput();

    // Readonly-first: shell shows the placeholder text; clicking mounts the
    // real editor in place.
    expect(screen.getByTestId("comment-composer-shell")).toHaveTextContent("Leave a comment...");
    activateComposer("comment-composer-shell");
    expect(screen.getByPlaceholderText("Leave a comment...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Attach file" })).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(2);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain("h-[70vh]");
  });

  it("renders reply composer without a manual expand control", () => {
    const { container } = renderReplyInput();

    expect(screen.getByTestId("reply-composer-shell")).toHaveTextContent("Leave a reply...");
    activateComposer("reply-composer-shell");
    expect(screen.getByPlaceholderText("Leave a reply...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Attach file" })).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(2);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain("h-[60vh]");
  });

  it("lets default-size replies grow without a height cap", () => {
    const { container } = renderReplyInput({ size: "default" });

    activateComposer("reply-composer-shell");
    expect(screen.getByPlaceholderText("Leave a reply...")).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(2);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
  });

  it("keeps main comment submission wired after removing expand", async () => {
    const { container, onSubmit } = renderCommentInput();

    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "hello from composer" },
    });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("hello from composer", undefined, undefined);
    });
  });

  it("keeps reply submission wired after removing expand", async () => {
    const { container, onSubmit } = renderReplyInput();

    activateComposer("reply-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "thread reply" },
    });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("thread reply", undefined, undefined);
    });
  });

  it("keeps the main comment editor's initial draft snapshot after persistence rerenders", () => {
    renderCommentInput();
    activateComposer("comment-composer-shell");

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "test.de" },
    });

    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBe("test.de");
    expect(editorDefaultValues.values.at(-1)).toBeUndefined();
  });

  it("keeps the reply editor's initial draft snapshot after persistence rerenders", () => {
    renderReplyInput({ draftKey: "reply:issue-1:comment-1" });
    activateComposer("reply-composer-shell");

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "test.de" },
    });

    expect(
      useCommentDraftStore
        .getState()
        .getDraft("reply:issue-1:comment-1"),
    ).toBe("test.de");
    expect(editorDefaultValues.values.at(-1)).toBeUndefined();
  });

  it("locks the editor while the send is in flight, then clears on success", async () => {
    let resolveSubmit: (ok: boolean) => void = () => {};
    const onSubmit = vi.fn(
      () => new Promise<boolean>((resolve) => { resolveSubmit = resolve; }),
    );
    const { container } = renderCommentInput(onSubmit);

    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "sending" } });
    fireEvent.click(getSubmitButton(container));

    // In flight: text kept, editor wrapper locked (aria-busy), not cleared yet.
    await waitFor(() =>
      expect(screen.getByTestId("editor").closest("[aria-busy]")).toHaveAttribute(
        "aria-busy",
        "true",
      ),
    );
    expect(onSubmit).toHaveBeenCalledWith("sending", undefined, undefined);

    resolveSubmit(true);

    // Success: the composer clears (now empty → submit disabled, lock released).
    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(screen.getByTestId("editor").closest("[aria-busy]")).toBeNull();
  });

  // Post-send caret policy. The two composers deliberately disagree: posting a
  // top-level comment ends the turn and drops the caret, while a thread reply
  // leaves the user mid-conversation with the box ready for the next one.
  it("blurs the top-level composer after a posted comment", async () => {
    const { container } = renderCommentInput();

    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "posted" } });
    // Discard the focus the lazy shell→editor swap performed; what follows is
    // entirely the post-send policy.
    focusCalls.focused = 0;
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(focusCalls.blurred).toBeGreaterThan(0));
    expect(focusCalls.focused).toBe(0);
  });

  it("keeps the caret in the reply box after a posted reply", async () => {
    const { container } = renderReplyInput();

    activateComposer("reply-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "replied" } });
    // Discard the shell→editor activation focus; what follows is the refocus.
    focusCalls.focused = 0;
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(focusCalls.focused).toBeGreaterThan(0));
    expect(focusCalls.blurred).toBe(0);
  });

  it("does not refocus the reply box when the send fails", async () => {
    const onSubmit = vi.fn().mockResolvedValue(false);
    const { container } = renderReplyInput({ onSubmit });

    activateComposer("reply-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "nope" } });
    focusCalls.focused = 0;
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(focusCalls.focused).toBe(0);
  });

  // Regression: the tab-switch flush re-writes IDENTICAL content mid-flight;
  // that must not read as "edited during the request" — the posted comment's
  // draft still clears (it previously resurrected with Send re-enabled).
  it("a tab switch during the send does not resurrect the posted draft", async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(() => new Promise<boolean>((r) => { resolveSubmit = r; }));
    renderCommentInput(onSubmit);
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "draft A" } });
    fireEvent.keyDown(screen.getByTestId("editor"), { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    // Backgrounding the tab fires the visibility flush with unchanged content.
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "hidden",
    });
    fireEvent(document, new Event("visibilitychange"));

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBeUndefined();

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });
  });

  // MUL-5181 P0: the editor stays interactive during a send — text typed
  // while draft A is in flight must survive A's success, in store AND editor.
  it("text typed while a comment send is in flight survives the success", async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(() => new Promise<boolean>((r) => { resolveSubmit = r; }));
    renderCommentInput(onSubmit);
    activateComposer("comment-composer-shell");
    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "draft A" } });
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    // Still mounted, still typing while the request is in flight.
    fireEvent.change(editor, { target: { value: "draft A plus more" } });

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBe("draft A plus more");
    // …and the post-send caret policy must stand down with it: the guard left
    // the editor holding "draft A plus more", so blurring would yank the caret
    // out of the sentence the user is still writing.
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(focusCalls.blurred).toBe(0);
  });

  // Same coupling on the reply side: refocus is bound to having actually wiped
  // the editor, not merely to acceptance.
  it("does not refocus a reply box whose mid-flight draft was kept", async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(() => new Promise<boolean>((r) => { resolveSubmit = r; }));
    renderReplyInput({ onSubmit, draftKey: "reply:issue-1:comment-1" });
    activateComposer("reply-composer-shell");
    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "reply A" } });
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    fireEvent.change(editor, { target: { value: "reply A plus more" } });
    focusCalls.focused = 0;

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft("reply:issue-1:comment-1")).toBe(
      "reply A plus more",
    );
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(focusCalls.focused).toBe(0);
  });

  // MUL-5181 P0: a submit that outlives its composer may only clear the draft
  // it submitted — never one typed after the composer unmounted and reopened.
  it("a late comment success does NOT clear a draft typed after unmount", async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(() => new Promise<boolean>((r) => { resolveSubmit = r; }));
    const view = renderCommentInput(onSubmit);
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "draft A" } });
    fireEvent.keyDown(screen.getByTestId("editor"), { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    view.unmount();
    // A reopened composer typed draft B under the same key.
    useCommentDraftStore.getState().setDraft("new:issue-1", "draft B");

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBe("draft B");
  });

  it("a late comment success still clears an untouched draft", async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(() => new Promise<boolean>((r) => { resolveSubmit = r; }));
    const view = renderCommentInput(onSubmit);
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "draft A" } });
    fireEvent.keyDown(screen.getByTestId("editor"), { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    view.unmount();
    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBeUndefined();
  });

  it("keeps the draft when the send fails (no optimistic clear)", async () => {
    const onSubmit = vi.fn().mockResolvedValue(false);
    const { container } = renderCommentInput(onSubmit);

    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "will fail" } });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    // Failed send must NOT clear — the box still has content, submit stays live.
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
  });
});

// MUL-4808 — posting mid-upload strips the pending image's blob URL out of the
// body and binds no attachment id, so the comment lands without the file.
describe("comment composers — upload submit gate", () => {
  const uploadAttachment = (id: string, url: string) =>
    ({
      id,
      url,
      download_url: url,
      markdown_url: url,
      filename: `${id}.png`,
      content_type: "image/png",
      size_bytes: 1,
    }) as unknown as Attachment;

  /**
   * Start an upload that stays in flight until the returned resolver runs. The
   * coordinator calls `api.uploadFile`, so this controls THAT promise: resolve
   * it with an attachment (success) or reject it (failure).
   */
  function startPendingUpload(container: HTMLElement, filename = "slow.png") {
    let resolveUpload!: (att: Attachment) => void;
    let rejectUpload!: (err: Error) => void;
    apiUploadFile.mockImplementationOnce(
      () =>
        new Promise<Attachment>((resolve, reject) => {
          resolveUpload = resolve;
          rejectUpload = reject;
        }),
    );
    // FileUploadButton's visible control is a button that clicks a hidden
    // input; the input is what carries the selection.
    const input = container.querySelector('input[type="file"]');
    if (!input) throw new Error("Expected a file input to render");
    fireEvent.change(input, {
      target: { files: [new File(["x"], filename, { type: "image/png" })] },
    });
    return {
      resolve: (att: Attachment) => resolveUpload(att),
      fail: () => rejectUpload(new Error("upload failed")),
    };
  }

  it("disables send while an upload is in flight and re-enables once it settles", async () => {
    const { container } = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "look at this" } });

    const pending = startPendingUpload(container);

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    // Screen readers must hear "busy", not just a dead control.
    expect(getSubmitButton(container)).toHaveAttribute("aria-busy", "true");

    await act(async () => {
      pending.resolve(uploadAttachment("att-1", "https://cdn.example/att-1.png"));
    });

    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
    expect(getSubmitButton(container)).not.toHaveAttribute("aria-busy");
  });



  it("never asks the rebuild to draw an upload this mount started", async () => {
    // The editor drew that node itself, synchronously, before the draft record
    // existed. Registering the id at handleUpload rather than waiting for the
    // effect to discover the node closes the window in which a delete could be
    // undone by a redraw.
    const { container } = renderCommentInput();
    activateComposer("comment-composer-shell");
    const pending = startPendingUpload(container, "mine.png");

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(insertPlaceholderSpy).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve(uploadAttachment("att-mine", "https://cdn.example/att-mine.png"));
    });
    expect(insertPlaceholderSpy).not.toHaveBeenCalled();
  });

  it("does not redraw a placeholder the user deleted mid-upload", async () => {
    // MUL-5181's rule: a placeholder the user removed stays removed. The
    // rebuild runs once per id per mount, so the next store write cannot undo
    // that decision — the send button is what still says the upload is live.
    useCommentDraftStore.getState().addUpload("new:issue-1", {
      clientUploadId: "u-deleted",
      status: "uploading",
      filename: "gone.png",
      size: 1,
    });
    renderCommentInput();
    await screen.findByTestId("editor");
    await waitFor(() => expect(insertPlaceholderSpy).toHaveBeenCalledTimes(1));

    // A second upload changes `uploads`, which re-runs the rebuild effect —
    // the moment a naive implementation would redraw the first one.
    act(() => {
      useCommentDraftStore.getState().addUpload("new:issue-1", {
        clientUploadId: "u-second",
        status: "uploading",
        filename: "other.png",
        size: 1,
      });
    });

    await waitFor(() =>
      expect(
        insertPlaceholderSpy.mock.calls.some(([u]) => u.uploadId === "u-second"),
      ).toBe(true),
    );
    expect(
      insertPlaceholderSpy.mock.calls.filter(([u]) => u.uploadId === "u-deleted"),
    ).toHaveLength(1);
  });

  it("leaves nothing behind when an upload fails", async () => {
    // The toast already said it, at the moment it happened, and the file is
    // still on disk. A chip cannot retry (the bytes were never persisted) and
    // `isMeaningful` counts it, so keeping one would hold an otherwise-empty
    // draft alive across reloads until dismissed by hand.
    const { container } = renderCommentInput();
    activateComposer("comment-composer-shell");

    const pending = startPendingUpload(container, "doomed.png");
    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());

    await act(async () => {
      pending.fail();
    });

    await waitFor(() =>
      expect(useCommentDraftStore.getState().getUploads("new:issue-1")).toHaveLength(0),
    );
    expect(screen.queryByText(/doomed\.png/)).toBeNull();
    // The empty draft is gone with it, not kept alive by a dead placeholder.
    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBeFalsy();
  });

  it("rebuilds a placeholder for an upload inherited from the persisted draft", async () => {
    // Started by a mount that is gone: its placeholder node died with that
    // editor, and placeholders are never serialised, so the reopened composer
    // has the record but no node. It draws one again — otherwise the composer
    // looks idle while `gate` quietly blocks the send.
    useCommentDraftStore.getState().addUpload("new:issue-1", {
      clientUploadId: "from-a-previous-mount",
      status: "uploading",
      filename: "orphan.png",
      size: 1,
    });

    renderCommentInput();
    await screen.findByTestId("editor");

    await waitFor(() =>
      expect(insertPlaceholderSpy).toHaveBeenCalledWith(
        expect.objectContaining({ uploadId: "from-a-previous-mount", filename: "orphan.png" }),
      ),
    );
  });

  it("blocks the Cmd+Enter path while an upload is in flight", async () => {
    const { container, onSubmit } = renderCommentInput();
    activateComposer("comment-composer-shell");
    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "look at this" } });

    const pending = startPendingUpload(container);

    // The shortcut never touches the disabled button — the handler's own
    // re-read of the queue is the only thing standing between this keystroke
    // and a comment posted without its attachment.
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await Promise.resolve();
    expect(onSubmit).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve(uploadAttachment("att-1", "https://cdn.example/att-1.png"));
    });

    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  });

  it("blocks send while a coordinator upload from a PREVIOUS mount is still in flight", async () => {
    // Reopened-composer scenario: the upload placeholder lives in the draft
    // store (coordinator-owned), but this mount's editor is clean — the editor
    // gate alone sees no active uploads. Sending here would clear the draft
    // out from under the settling upload and silently drop the file.
    useCommentDraftStore.getState().setDraft("new:issue-1", "recovered draft");
    useCommentDraftStore.getState().addUpload("new:issue-1", {
      clientUploadId: "prev-mount-upload",
      status: "uploading",
      filename: "shot.png",
      size: 10,
    });

    const { container, onSubmit } = renderCommentInput();
    // Draft + pending upload mount the editor directly (no shell).
    const editor = await screen.findByTestId("editor");

    expect(getSubmitButton(container)).toBeDisabled();
    expect(getSubmitButton(container)).toHaveAttribute("aria-busy", "true");

    // The shortcut path bypasses the button — the submit-time re-read of the
    // DRAFT's uploads is the only guard.
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await Promise.resolve();
    expect(onSubmit).not.toHaveBeenCalled();

    // The old upload settles (coordinator onSettled → store.settleUpload) —
    // the gate opens and the send goes through.
    await act(async () => {
      useCommentDraftStore
        .getState()
        .settleUpload(
          "new:issue-1",
          "prev-mount-upload",
          uploadAttachment("att-prev", "https://cdn.example/att-prev.png"),
        );
    });
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  });

  it("stays gated until the LAST of two concurrent uploads settles", async () => {
    const { container } = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "two files" } });

    const first = startPendingUpload(container, "a.png");
    const second = startPendingUpload(container, "b.png");

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());

    // First one lands — the second is still in flight, so send must stay shut.
    await act(async () => {
      first.resolve(uploadAttachment("att-a", "https://cdn.example/att-a.png"));
    });
    expect(getSubmitButton(container)).toBeDisabled();

    await act(async () => {
      second.resolve(uploadAttachment("att-b", "https://cdn.example/att-b.png"));
    });
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
  });

  it("re-enables send after a FAILED upload so the draft isn't stuck", async () => {
    const { container } = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "will fail" } });

    const pending = startPendingUpload(container);
    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());

    // uploadWithToast reports the failure and resolves null — the placeholder
    // is dropped and the body never referenced it, so submit must come back.
    await act(async () => {
      pending.fail();
    });
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
  });

  it("sends the attachment id once the upload completes", async () => {
    const { container, onSubmit } = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "shipping it" } });

    const pending = startPendingUpload(container);
    await act(async () => {
      pending.resolve(uploadAttachment("att-9", "https://cdn.example/att-9.png"));
    });

    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
    fireEvent.click(getSubmitButton(container));

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(
        expect.stringContaining("https://cdn.example/att-9.png"),
        ["att-9"],
        undefined,
      ),
    );
  });

  it("does NOT bind an upload the user deleted from the body", async () => {
    const { container, onSubmit } = renderCommentInput();
    activateComposer("comment-composer-shell");
    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "keep this" } });

    const pending = startPendingUpload(container);
    await act(async () => {
      pending.resolve(uploadAttachment("att-del", "https://cdn.example/att-del.png"));
    });

    // The completed upload's link is in the body; the user deletes it. What
    // is not referenced must not ship — deleting really unbinds (MUL-5181).
    fireEvent.change(editor, { target: { value: "keep this, dropped the image" } });
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(onSubmit).toHaveBeenCalledWith("keep this, dropped the image", undefined, undefined);
  });

  it("writes the finished upload's link into the persisted draft after the composer unmounts", async () => {
    const { container, unmount } = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "wip text" } });

    const pending = startPendingUpload(container);
    unmount();

    await act(async () => {
      pending.resolve(uploadAttachment("att-wb", "https://cdn.example/att-wb.png"));
    });

    // The upload outlived the composer; its link must land in the draft BODY —
    // that's what a future submit binds, and what a reopen renders.
    const draft = useCommentDraftStore.getState().getDraft("new:issue-1");
    expect(draft).toContain("wip text");
    expect(draft).toContain("https://cdn.example/att-wb.png");
    const uploads = useCommentDraftStore.getState().getUploads("new:issue-1");
    expect(uploads).toHaveLength(1);
    expect(uploads[0]?.status).toBe("uploaded");
  });

  it("hands the finished upload's link to a REOPENED composer's live editor", async () => {
    const first = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "draft body" } });

    const pending = startPendingUpload(first.container);
    first.unmount();

    // Reopen: the draft + pending upload mount the editor directly (no shell).
    renderCommentInput();
    await screen.findByTestId("editor");

    await act(async () => {
      pending.resolve(uploadAttachment("att-live", "https://cdn.example/att-live.png"));
    });

    // The LIVE EDITOR received the insert (not just the store — an append the
    // mounted editor never saw would be erased by its next emit)…
    expect(insertMarkdownSpy).toHaveBeenCalledWith(
      expect.stringContaining("https://cdn.example/att-live.png"),
    );
    // …and the draft body converged through the normal onUpdate pipeline.
    await waitFor(() =>
      expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toContain(
        "https://cdn.example/att-live.png",
      ),
    );
  });

  it("retries the insert while the reopened editor's instance is still warming up", async () => {
    const first = renderCommentInput();
    activateComposer("comment-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "draft body" } });

    const pending = startPendingUpload(first.container);
    first.unmount();

    renderCommentInput();
    await screen.findByTestId("editor");

    // Simulate the real handle's window where the component committed but the
    // Tiptap instance hasn't been created yet: insert reports failure.
    insertMarkdownBehavior.succeed = false;
    await act(async () => {
      pending.resolve(uploadAttachment("att-retry", "https://cdn.example/att-retry.png"));
    });

    // Nothing may land in the store while a mounted editor can't show it —
    // that append would be silently erased by the editor's first emit.
    expect(useCommentDraftStore.getState().getDraft("new:issue-1") ?? "").not.toContain(
      "att-retry.png",
    );

    // Instance comes up → the retry delivers into the editor.
    insertMarkdownBehavior.succeed = true;
    await waitFor(
      () =>
        expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toContain(
          "https://cdn.example/att-retry.png",
        ),
      { timeout: 3000 },
    );
  });

  it("gates the reply composer's send button too", async () => {
    const { container } = renderReplyInput();
    activateComposer("reply-composer-shell");
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "replying" } });

    startPendingUpload(container);

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(getSubmitButton(container)).toHaveAttribute("aria-busy", "true");
  });

  it("blocks the reply composer's Cmd+Enter path", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    const { container } = renderReplyInput({ onSubmit });
    activateComposer("reply-composer-shell");
    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "replying" } });

    startPendingUpload(container);

    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
    await Promise.resolve();
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe("sticky composer preference", () => {
  it("caps the editor height while the sticky preference is on (default)", () => {
    renderCommentInput();

    activateComposer("comment-composer-shell");
    // The height cap lives on the editor wrapper, not the card shell.
    expect(screen.getByTestId("editor").parentElement?.className).toContain("max-h-[40vh]");
  });

  it("lets the editor grow when the preference is off", () => {
    useCommentComposerStore.setState({ sticky: false });
    renderCommentInput();

    activateComposer("comment-composer-shell");
    expect(screen.getByTestId("editor").parentElement?.className).not.toContain("max-h-[40vh]");
  });
});
