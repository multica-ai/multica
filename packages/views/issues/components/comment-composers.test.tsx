import { forwardRef, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
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

const authState = vi.hoisted(() => ({
  user: { message_enter_key_behavior: "newline" as "send" | "newline" },
}));

vi.mock("../../editor", () => ({
  useFileDropZone: () => ({
    isDragOver: false,
    dropZoneProps: { "data-testid": "drop-zone" },
  }),
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      onSubmit,
      placeholder,
      onUploadFile,
      submitOnEnter,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      onSubmit?: () => void;
      placeholder?: string;
      onUploadFile?: (file: File) => Promise<UploadResult | null>;
      submitOnEnter?: boolean;
    },
    ref: Ref<unknown>,
  ) {
    const valueRef = useRef(defaultValue ?? "");

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
      },
      focus: () => {},
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
        onKeyDown={(event) => {
          if (event.key !== "Enter") return;
          if (event.metaKey || event.ctrlKey) {
            event.preventDefault();
            onSubmit?.();
            return;
          }
          if (!event.shiftKey && submitOnEnter) {
            event.preventDefault();
            onSubmit?.();
            return;
          }
          valueRef.current += "\n";
          onUpdate?.(valueRef.current);
        }}
      />
    );
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (s: typeof authState) => unknown) =>
    selector ? selector(authState) : authState,
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

function renderCommentInput(onSubmit = vi.fn().mockResolvedValue(undefined)) {
  const view = renderWithProviders(<CommentInput issueId="issue-1" onSubmit={onSubmit} />);
  return { ...view, onSubmit };
}

function renderReplyInput({
  onSubmit = vi.fn().mockResolvedValue(undefined),
  size = "sm",
}: {
  onSubmit?: (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => Promise<void>;
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

function getSubmitButton(container: HTMLElement): HTMLButtonElement {
  const button = container.querySelectorAll("button")[1];
  if (!button) throw new Error("Expected submit button to render");
  return button;
}

beforeEach(() => {
  uploadWithToast.mockReset();
  localStorage.clear();
  authState.user.message_enter_key_behavior = "newline";
  useCommentDraftStore.setState({ drafts: {} });
});

describe("comment composers", () => {
  it("renders the main comment composer without a manual expand control", () => {
    const { container } = renderCommentInput();

    expect(screen.getByPlaceholderText("Leave a comment...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Attach file" })).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(2);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain("h-[70vh]");
  });

  it("renders reply composer without a manual expand control", () => {
    const { container } = renderReplyInput();

    expect(screen.getByPlaceholderText("Leave a reply...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Attach file" })).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(2);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain("h-[60vh]");
  });

  it("lets default-size replies grow without a height cap", () => {
    const { container } = renderReplyInput({ size: "default" });

    expect(screen.getByPlaceholderText("Leave a reply...")).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(2);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
  });

  it("keeps main comment submission wired after removing expand", async () => {
    const { container, onSubmit } = renderCommentInput();

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

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "thread reply" },
    });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("thread reply", undefined, undefined);
    });
  });

  it("shows the default send shortcut hint for top-level comments after content is entered", () => {
    renderCommentInput();

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });

    expect(
      screen.getByText(`${formatShortcut(modKey, enterKey)} to send`),
    ).toBeTruthy();
  });

  it("shows Enter as the send shortcut hint for top-level comments when configured", () => {
    authState.user.message_enter_key_behavior = "send";
    renderCommentInput();

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });

    expect(
      screen.getByText(`${formatShortcut(enterKey)} to send`),
    ).toBeTruthy();
  });

  it("shows the default send shortcut hint for replies after content is entered", () => {
    renderReplyInput();

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "reply comment" } });

    expect(
      screen.getByText(`${formatShortcut(modKey, enterKey)} to send`),
    ).toBeTruthy();
  });

  it("submits a top-level comment when Enter-to-send is configured", async () => {
    authState.user.message_enter_key_behavior = "send";
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderCommentInput(onSubmit);

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });
    fireEvent.keyDown(editor, { key: "Enter" });

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("new comment", undefined, undefined);
    });
  });

  it("submits a reply when Enter-to-send is configured", async () => {
    authState.user.message_enter_key_behavior = "send";
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderReplyInput({ onSubmit });

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "reply comment" } });
    fireEvent.keyDown(editor, { key: "Enter" });

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("reply comment", undefined, undefined);
    });
  });

  it("treats unknown Enter behavior values as newline mode for top-level comments", () => {
    (
      authState.user as { message_enter_key_behavior: string }
    ).message_enter_key_behavior = "unknown";
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderCommentInput(onSubmit);

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });

    expect(
      screen.getByText(`${formatShortcut(modKey, enterKey)} to send`),
    ).toBeTruthy();

    fireEvent.keyDown(editor, { key: "Enter" });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("treats unknown Enter behavior values as newline mode for replies", () => {
    (
      authState.user as { message_enter_key_behavior: string }
    ).message_enter_key_behavior = "unknown";
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderReplyInput({ onSubmit });

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "reply comment" } });

    expect(
      screen.getByText(`${formatShortcut(modKey, enterKey)} to send`),
    ).toBeTruthy();

    fireEvent.keyDown(editor, { key: "Enter" });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps Shift+Enter available for a newline without submitting", () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderCommentInput(onSubmit);

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });
    fireEvent.keyDown(editor, { key: "Enter", shiftKey: true });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps Enter as a newline for top-level comments by default", () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderCommentInput(onSubmit);

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });
    fireEvent.keyDown(editor, { key: "Enter" });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits top-level comments with Ctrl+Enter when Enter inserts newlines", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderCommentInput(onSubmit);

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "new comment" } });
    fireEvent.keyDown(editor, { key: "Enter", ctrlKey: true });

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("new comment", undefined, undefined);
    });
  });

  it("keeps Enter as a newline for replies by default", () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderReplyInput({ onSubmit });

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "reply comment" } });
    fireEvent.keyDown(editor, { key: "Enter" });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits replies with Ctrl+Enter when Enter inserts newlines", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderReplyInput({ onSubmit });

    const editor = screen.getByTestId("editor");
    fireEvent.change(editor, { target: { value: "reply comment" } });
    fireEvent.keyDown(editor, { key: "Enter", ctrlKey: true });

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("reply comment", undefined, undefined);
    });
  });
});
