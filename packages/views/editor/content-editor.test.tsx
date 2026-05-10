import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mockFocus = vi.hoisted(() => vi.fn());
const lastUseEditorOptions = vi.hoisted(
  () => ({ value: undefined as Record<string, unknown> | undefined }),
);

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("./extensions", () => ({
  createEditorExtensions: () => [],
}));

vi.mock("./extensions/file-upload", () => ({
  uploadAndInsertFile: vi.fn(),
}));

vi.mock("./utils/preprocess", () => ({
  preprocessMarkdown: (value: string) => value,
}));

vi.mock("./bubble-menu", () => ({
  EditorBubbleMenu: () => null,
}));

vi.mock("@tiptap/react", () => ({
  useEditor: (options: Record<string, unknown>) => {
    lastUseEditorOptions.value = options;
    return {
      commands: {
        focus: mockFocus,
        clearContent: vi.fn(),
      },
      getMarkdown: () => "",
      state: {
        doc: {
          content: {
            size: 0,
          },
        },
        selection: {
          empty: true,
        },
      },
    };
  },
  EditorContent: ({ className }: { className?: string }) => (
    <div className={className} data-testid="editor-content">
      <div className="ProseMirror rich-text-editor" data-testid="prosemirror" />
    </div>
  ),
}));

import { ContentEditor } from "./content-editor";

describe("ContentEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    lastUseEditorOptions.value = undefined;
  });

  it("focuses the editor when clicking the empty container area", () => {
    render(<ContentEditor placeholder="Add description..." />);

    const shell = screen.getByTestId("editor-content").parentElement;
    expect(shell).not.toBeNull();

    fireEvent.mouseDown(shell!);

    expect(mockFocus).toHaveBeenCalledWith("end");
  });

  it("does not hijack clicks that land inside the ProseMirror node", () => {
    render(<ContentEditor placeholder="Add description..." />);

    fireEvent.mouseDown(screen.getByTestId("prosemirror"));

    expect(mockFocus).not.toHaveBeenCalled();
  });
});

describe("ContentEditor — JEH-756 autofocus", () => {
  // The previous fix used a post-mount RAF effect that called
  // `editor.commands.focus()`. With `immediatelyRender: false` the editor
  // is null on first render, so the effect ran before the editor existed
  // and silently no-op'd. The fix is to delegate timing to Tiptap's own
  // `autofocus` option — these tests pin the option's value through the
  // useEditor boundary.

  beforeEach(() => {
    vi.clearAllMocks();
    lastUseEditorOptions.value = undefined;
  });

  it("passes `autofocus: \"end\"` to useEditor when autoFocus is true", () => {
    render(<ContentEditor autoFocus placeholder="…" />);
    expect(lastUseEditorOptions.value?.autofocus).toBe("end");
  });

  it("passes `autofocus: false` when autoFocus is omitted", () => {
    render(<ContentEditor placeholder="…" />);
    expect(lastUseEditorOptions.value?.autofocus).toBe(false);
  });

  it("yields focus to a focus-trapping dialog at mount", () => {
    // A dialog with focus inside it: the editor must NOT ask Tiptap to
    // autofocus, otherwise the dialog's focus trap fights the editor.
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    const dialogInput = document.createElement("input");
    dialog.appendChild(dialogInput);
    document.body.appendChild(dialog);
    dialogInput.focus();

    render(<ContentEditor autoFocus placeholder="…" />);
    expect(lastUseEditorOptions.value?.autofocus).toBe(false);

    document.body.removeChild(dialog);
  });

  it("autofocuses normally when the dialog has been dismissed", () => {
    // Dialog removed before mount → activeElement is body → autofocus on.
    render(<ContentEditor autoFocus placeholder="…" />);
    expect(lastUseEditorOptions.value?.autofocus).toBe("end");
  });
});
