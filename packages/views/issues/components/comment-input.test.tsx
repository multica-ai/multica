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

const draftKey = "multica:issue-comment-draft:user-1:issue-1";

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
    { onUpdate, placeholder }: any,
    ref: any,
  ) {
    const valueRef = useRef("");
    const [value, setValue] = useState("");

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

function makeDraft(content: string, overrides: Partial<Record<string, unknown>> = {}) {
  return JSON.stringify({
    version: 1,
    issueId: "issue-1",
    userId: "user-1",
    content,
    uploads: [],
    updatedAt: 123,
    tabId: "other-tab",
    ...overrides,
  });
}

describe("CommentInput drafts", () => {
  beforeEach(() => {
    vi.useRealTimers();
    window.localStorage.clear();
  });

  it("saves issue and user scoped drafts after a debounce", async () => {
    vi.useFakeTimers();
    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={vi.fn()} /></I18nWrapper>);

    fireEvent.change(screen.getByPlaceholderText("Leave a comment..."), {
      target: { value: "draft text" },
    });

    act(() => vi.advanceTimersByTime(499));
    expect(window.localStorage.getItem(draftKey)).toBeNull();

    act(() => vi.advanceTimersByTime(1));

    const draft = JSON.parse(window.localStorage.getItem(draftKey) ?? "{}");
    expect(draft).toMatchObject({
      version: 1,
      issueId: "issue-1",
      userId: "user-1",
      content: "draft text",
      uploads: [],
    });
    expect(typeof draft.updatedAt).toBe("number");
    expect(typeof draft.tabId).toBe("string");
  });

  it("prompts to restore an existing draft and clears it after submit", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    window.localStorage.setItem(draftKey, makeDraft("restored draft"));

    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={onSubmit} /></I18nWrapper>);

    expect(await screen.findByText("Restore draft?")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Restore"));

    expect(screen.getByPlaceholderText("Leave a comment...")).toHaveValue(
      "restored draft",
    );

    fireEvent.click(screen.getByLabelText("Submit comment"));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("restored draft", undefined, "comment");
    });
    expect(window.localStorage.getItem(draftKey)).toBeNull();
  });

  it("keeps the saved draft when submit fails", async () => {
    vi.useFakeTimers();
    const onSubmit = vi.fn().mockRejectedValue(new Error("network failed"));
    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={onSubmit} /></I18nWrapper>);

    fireEvent.change(screen.getByPlaceholderText("Leave a comment..."), {
      target: { value: "do not lose me" },
    });
    act(() => vi.advanceTimersByTime(500));

    await act(async () => {
      fireEvent.click(screen.getByLabelText("Submit comment"));
    });

    expect(onSubmit).toHaveBeenCalledWith("do not lose me", undefined, "comment");
    expect(JSON.parse(window.localStorage.getItem(draftKey) ?? "{}")).toMatchObject({
      content: "do not lose me",
    });
    expect(screen.getByPlaceholderText("Leave a comment...")).toHaveValue(
      "do not lose me",
    );
  });

  it("prompts before applying a draft written from another tab", async () => {
    render(<I18nWrapper><CommentInput issueId="issue-1" onSubmit={vi.fn()} /></I18nWrapper>);

    act(() => {
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: draftKey,
          newValue: makeDraft("from another tab", { updatedAt: 456 }),
          oldValue: null,
        }),
      );
    });

    expect(await screen.findByText("Restore draft?")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Restore"));

    expect(screen.getByPlaceholderText("Leave a comment...")).toHaveValue(
      "from another tab",
    );
  });
});
