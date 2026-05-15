import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { CommentInput } from "./comment-input";

const TEST_RESOURCES = {
  en: { common: enCommon, issues: enIssues },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const draftKey = "new:issue-1";
const draftState = vi.hoisted(() => ({
  drafts: {} as Record<string, string>,
}));

const mockUser = vi.hoisted(() => ({
  id: "user-1",
  email: "user@example.com",
  name: "Test User",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: any) => {
    const state = { user: mockUser };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/issues/stores", () => {
  const store = {
    getDraft: (key: string) => draftState.drafts[key],
    setDraft: (key: string, content: string) => {
      draftState.drafts[key] = content;
    },
    clearDraft: (key: string) => {
      delete draftState.drafts[key];
    },
  };
  const useCommentDraftStore = Object.assign(
    (selector?: (state: typeof store) => unknown) => (
      selector ? selector(store) : store
    ),
    {
      getState: () => store,
    },
  );
  return { useCommentDraftStore };
});

vi.mock("@multica/core/api", () => ({
  api: {},
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({
    uploadWithToast: vi.fn(),
  }),
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => <button type="button">Upload</button>,
}));

vi.mock("@multica/ui/components/common/submit-button", () => ({
  SubmitButton: ({ disabled, loading, onClick }: {
    disabled?: boolean;
    loading?: boolean;
    onClick: () => void;
  }) => (
    <button
      aria-label="Submit comment"
      disabled={disabled || loading}
      type="button"
      onClick={onClick}
    >
      Submit
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({
    open,
    children,
  }: {
    open: boolean;
    children: React.ReactNode;
  }) => (open ? <div role="alertdialog">{children}</div> : null),
  AlertDialogAction: (props: React.ComponentProps<"button">) => (
    <button type="button" {...props} />
  ),
  AlertDialogCancel: (props: React.ComponentProps<"button">) => (
    <button type="button" {...props} />
  ),
  AlertDialogContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  AlertDialogDescription: ({ children }: { children: React.ReactNode }) => (
    <p>{children}</p>
  ),
  AlertDialogFooter: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  AlertDialogHeader: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  AlertDialogTitle: ({ children }: { children: React.ReactNode }) => (
    <h2>{children}</h2>
  ),
}));

vi.mock("../../editor", () => ({
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    { defaultValue, onUpdate, placeholder }: any,
    ref: any,
  ) {
    const valueRef = useRef(defaultValue ?? "");
    const [value, setValue] = useState(defaultValue ?? "");

    const setEditorValue = (next: string) => {
      valueRef.current = next;
      setValue(next);
      onUpdate?.(next);
    };

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      setMarkdown: setEditorValue,
      appendMarkdown: (markdown: string) => setEditorValue(
        valueRef.current.trimEnd()
          ? `${valueRef.current.trimEnd()}\n\n${markdown.trimEnd()}`
          : markdown.trimEnd(),
      ),
      clearContent: () => setEditorValue(""),
      focus: () => {},
      uploadFile: () => {},
      hasActiveUploads: () => false,
    }));

    return (
      <textarea
        placeholder={placeholder}
        value={value}
        onChange={(event) => setEditorValue(event.target.value)}
      />
    );
  }),
}));

describe("CommentInput drafts", () => {
  beforeEach(() => {
    vi.useRealTimers();
    draftState.drafts = {};
  });

  it("saves issue scoped drafts when content changes", async () => {
    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={vi.fn()} /></I18nWrapper>);

    fireEvent.change(screen.getByPlaceholderText("Leave a comment..."), {
      target: { value: "draft text" },
    });

    expect(draftState.drafts[draftKey]).toBe("draft text");
  });

  it("hydrates an existing draft and clears it after submit", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    draftState.drafts[draftKey] = "restored draft";

    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={onSubmit} /></I18nWrapper>);

    expect(screen.getByPlaceholderText("Leave a comment...")).toHaveValue(
      "restored draft",
    );

    fireEvent.click(screen.getByLabelText("Submit comment"));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("restored draft", undefined, "comment");
    });
    expect(draftState.drafts[draftKey]).toBeUndefined();
  });

  it("keeps the saved draft when submit fails", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("network failed"));
    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={onSubmit} /></I18nWrapper>);

    fireEvent.change(screen.getByPlaceholderText("Leave a comment..."), {
      target: { value: "do not lose me" },
    });

    await act(async () => {
      fireEvent.click(screen.getByLabelText("Submit comment"));
    });

    expect(onSubmit).toHaveBeenCalledWith("do not lose me", undefined, "comment");
    expect(draftState.drafts[draftKey]).toBe("do not lose me");
    expect(screen.getByPlaceholderText("Leave a comment...")).toHaveValue(
      "do not lose me",
    );
  });

  it("clears the saved draft when content becomes empty", async () => {
    draftState.drafts[draftKey] = "old draft";
    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={vi.fn()} /></I18nWrapper>);

    fireEvent.change(screen.getByPlaceholderText("Leave a comment..."), {
      target: { value: "" },
    });

    expect(draftState.drafts[draftKey]).toBeUndefined();
  });
});
