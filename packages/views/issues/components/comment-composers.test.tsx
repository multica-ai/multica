import { forwardRef, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
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
      placeholder,
      onUploadFile,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File) => Promise<UploadResult | null>;
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
});
