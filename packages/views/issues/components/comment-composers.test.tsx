import { forwardRef, useEffect, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useCommentComposerStore, useCommentDraftStore } from "@multica/core/issues/stores";
import { renderWithI18n } from "../../test/i18n";
import { CommentInput } from "./comment-input";
import { ReplyInput } from "./reply-input";

const uploadWithToast = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {},
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
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
      onReady,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File) => Promise<UploadResult | null>;
      onReady?: () => void;
    },
    ref: Ref<unknown>,
  ) {
    const valueRef = useRef(defaultValue ?? "");

    useEffect(() => {
      onReady?.();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
      },
      focus: () => {},
      focusAtCoords: () => {},
      blur: () => {},
      uploadFile: async (file: File) => {
        const result = await onUploadFile?.(file);
        if (!result) return;
        valueRef.current = `${valueRef.current}\n${result.url}`.trim();
        onUpdate?.(valueRef.current);
      },
      hasActiveUploads: () => false,
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
}: {
  onSubmit?: (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => Promise<boolean>;
  size?: "sm" | "default";
} = {}) {
  const view = renderWithProviders(
    <ReplyInput
      issueId="issue-1"
      parentId="comment-1"
      avatarType="member"
      avatarId="user-1"
      onSubmit={onSubmit}
      size={size}
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
  localStorage.clear();
  useCommentComposerStore.setState({ sticky: true });
  // The draft store is a module singleton — a draft left by a previous test
  // (e.g. the failed-send case) would trip the composers' draft-direct-mount
  // path and hide the shell the next test expects.
  useCommentDraftStore.setState({ drafts: {} });
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
