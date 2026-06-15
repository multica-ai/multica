import { forwardRef, useImperativeHandle, useRef, type ReactElement, type ReactNode, type Ref } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { renderWithI18n } from "../../test/i18n";
import { CommentInput } from "./comment-input";
import { ReplyInput } from "./reply-input";

const uploadWithToast = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {},
}));

// CEREBRO-PATCH: mock auth store — the cerebro TriggerTargetBar
// (reply-target-agent-indicator, FIR-2392) reads useAuthStore on render.
const mockAuthUser = { id: "user-1", email: "test@test.com", name: "Test User" };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

// CEREBRO-PATCH: mock cerebro-preferences to avoid auth-store dependency in tests
vi.mock("@multica/cerebro-preferences/views", () => ({
  useSubmitOnEnter: () => true,
  useTheme: () => "system",
}));

// CEREBRO-PATCH(comment-drafts): composer unit tests don't mount the workspace
// route/query providers that the real draft hook uses for its feature flag.
vi.mock("@multica/cerebro-comment-drafts", () => ({
  DraftSavedHint: () => null,
  useCommentDraft: () => ({
    defaultValue: "",
    save: vi.fn(),
    clear: vi.fn(),
    saved: false,
  }),
}));

// CEREBRO-PATCH: mock cerebro-access send-confirm hook (FIR-32) — it reads
// feature flags + workspace context the composer unit tests don't wire up.
vi.mock("@multica/cerebro-access/views", () => ({
  usePrivateAgentSendConfirm: () => ({
    confirmBeforeSend: () => Promise.resolve(true),
    confirmDialog: null,
  }),
  resolveCommentTriggerAgentIds: () => [],
}));

// CEREBRO-PATCH: mock cerebro-pin-input — useInputPin/useFloatPosition call
// useQuery (need a QueryClient). Pin disabled keeps the composer to its two
// base buttons (attach + submit), matching the upstream composer assertions.
vi.mock("@multica/cerebro-pin-input", () => ({
  PinButton: () => null,
  useInputPin: () => ({
    enabled: false,
    isPinned: false,
    togglePin: () => {},
    unpin: () => {},
  }),
  useFloatPosition: () => null,
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

vi.mock("../../editor/use-file-drop-zone", () => ({
  useFileDropZone: () => ({
    isDragOver: false,
    dropZoneProps: { "data-testid": "drop-zone" },
  }),
}));

vi.mock("../../editor/file-drop-overlay", () => ({
  FileDropOverlay: () => null,
}));

vi.mock("../../editor/content-editor", () => ({
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

// Composers read the cerebro feature-flags query (via the trigger-target bar
// and pin hooks), so renders need a QueryClient in context.
function withQueryClient(children: ReactNode): ReactElement {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderCommentInput(onSubmit = vi.fn().mockResolvedValue(undefined)) {
  const view = renderWithI18n(withQueryClient(<CommentInput issueId="issue-1" onSubmit={onSubmit} />));
  return { ...view, onSubmit };
}

function renderReplyInput({
  onSubmit = vi.fn().mockResolvedValue(undefined),
  size = "sm",
}: {
  onSubmit?: (content: string, attachmentIds?: string[]) => Promise<void>;
  size?: "sm" | "default";
} = {}) {
  const view = renderWithI18n(
    withQueryClient(
      <ReplyInput
        issueId="issue-1"
        avatarType="member"
        avatarId="user-1"
        onSubmit={onSubmit}
        size={size}
      />,
    ),
  );
  return { ...view, onSubmit };
}

beforeEach(() => {
  uploadWithToast.mockReset();
  localStorage.clear();
});

describe("comment composers", () => {
  // TECH-3536: every composer (issue comment, channel, DM, reply, agent chat)
  // now carries the same unified expand pill — an extra button labelled
  // "Expand"/"Collapse" alongside the attach + submit buttons. The height cap
  // is an inline style on the inner scroll container, not a className on the
  // shell, so the shell-className assertions below still hold.
  it("renders the main comment composer with the unified expand control", () => {
    const { container } = renderCommentInput();

    expect(screen.getByPlaceholderText("Leave a comment...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add file" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand" })).toBeInTheDocument();
    // Expand pill + attach + submit.
    expect(container.querySelectorAll("button")).toHaveLength(3);

    // The cerebro comment composer sets its own data-testid="comment-input"
    // on the shell, which shadows the drop-zone test id from dropZoneProps.
    const shell = screen.getByTestId("comment-input");
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain("h-[70vh]");
  });

  it("renders reply composer with the unified expand control", () => {
    const { container } = renderReplyInput();

    expect(screen.getByPlaceholderText("Leave a reply...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add file" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand" })).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(3);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain("h-[60vh]");
  });

  it("renders the unified expand control on default-size replies too", () => {
    const { container } = renderReplyInput({ size: "default" });

    expect(screen.getByPlaceholderText("Leave a reply...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand" })).toBeInTheDocument();
    expect(container.querySelectorAll("button")).toHaveLength(3);

    const shell = screen.getByTestId("drop-zone");
    expect(shell.className).not.toMatch(/max-h-/);
  });

  it("keeps main comment submission wired alongside the expand control", async () => {
    const { onSubmit } = renderCommentInput();

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "hello from composer" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Submit comment" }));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("hello from composer", undefined);
    });
  });

  it("keeps reply submission wired alongside the expand control", async () => {
    const { onSubmit } = renderReplyInput();

    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "thread reply" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("thread reply", undefined);
    });
  });
});
