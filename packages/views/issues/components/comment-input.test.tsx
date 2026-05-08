import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { forwardRef, useImperativeHandle, type Ref } from "react";

const editorFocus = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/cerebro-preferences/views", () => ({
  useSubmitOnEnter: () => false,
}));

vi.mock("../../editor", () => {
  const ContentEditor = forwardRef(
    (
      _props: Record<string, unknown>,
      ref: Ref<{
        getMarkdown: () => string;
        clearContent: () => void;
        uploadFile: () => void;
        focus: () => void;
      }>,
    ) => {
      useImperativeHandle(ref, () => ({
        getMarkdown: () => "",
        clearContent: () => {},
        uploadFile: () => {},
        focus: () => editorFocus(),
      }));
      return <div data-testid="content-editor" />;
    },
  );
  ContentEditor.displayName = "ContentEditor";
  return {
    ContentEditor,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

import { CommentInput } from "./comment-input";

const flushRaf = () =>
  new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));

beforeEach(() => {
  editorFocus.mockClear();
});

describe("CommentInput — JEH-756 autofocus", () => {
  it("focuses the editor on mount when autoFocus is set", async () => {
    render(
      <CommentInput issueId="c1" onSubmit={vi.fn()} autoFocus />,
    );
    await flushRaf();
    expect(editorFocus).toHaveBeenCalled();
  });

  it("does not focus on mount when autoFocus is omitted", async () => {
    render(<CommentInput issueId="c1" onSubmit={vi.fn()} />);
    await flushRaf();
    expect(editorFocus).not.toHaveBeenCalled();
  });

  it("re-focuses when issueId changes (channel switch)", async () => {
    const { rerender } = render(
      <CommentInput issueId="c1" onSubmit={vi.fn()} autoFocus />,
    );
    await flushRaf();
    expect(editorFocus).toHaveBeenCalledTimes(1);

    rerender(<CommentInput issueId="c2" onSubmit={vi.fn()} autoFocus />);
    await flushRaf();
    expect(editorFocus).toHaveBeenCalledTimes(2);
  });

  it("yields focus when an open dialog is trapping focus", async () => {
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    const dialogInput = document.createElement("input");
    dialog.appendChild(dialogInput);
    document.body.appendChild(dialog);
    dialogInput.focus();

    render(<CommentInput issueId="c1" onSubmit={vi.fn()} autoFocus />);
    await flushRaf();
    expect(editorFocus).not.toHaveBeenCalled();

    document.body.removeChild(dialog);
  });
});
