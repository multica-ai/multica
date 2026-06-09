import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mockFocus = vi.hoisted(() => vi.fn());
const mockSetContent = vi.hoisted(() => vi.fn());
const mockSetTextSelection = vi.hoisted(() => vi.fn());
const editorState = vi.hoisted(() => ({
  isFocused: false,
  isDestroyed: false,
  markdown: "",
}));

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

const editorRef = vi.hoisted<{ current: unknown }>(() => ({ current: null }));
const onCreateFired = vi.hoisted(() => ({ value: false }));

vi.mock("@tiptap/react", () => ({
  useEditor: (options: { onCreate?: (args: { editor: unknown }) => void }) => {
    if (!editorRef.current) {
      editorRef.current = {
        get isFocused() {
          return editorState.isFocused;
        },
        get isDestroyed() {
          return editorState.isDestroyed;
        },
        commands: {
          focus: mockFocus,
          clearContent: vi.fn(),
          setContent: mockSetContent,
          setTextSelection: mockSetTextSelection,
        },
        getMarkdown: () => editorState.markdown,
        state: {
          doc: { content: { size: 0 } },
          selection: { empty: true, from: 0, to: 0 },
        },
      };
    }
    if (!onCreateFired.value) {
      onCreateFired.value = true;
      options?.onCreate?.({ editor: editorRef.current });
    }
    return editorRef.current;
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
    editorState.isFocused = false;
    editorState.isDestroyed = false;
    editorState.markdown = "";
    editorRef.current = null;
    onCreateFired.value = false;
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

  it("syncs editor content when defaultValue changes externally and editor is unfocused", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor defaultValue="old content" />);

    expect(mockSetContent).not.toHaveBeenCalled();

    // Editor still holds the old, in-sync content; external value changes.
    editorState.markdown = "old content";
    rerender(<ContentEditor defaultValue="new content from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      "new content from server",
      expect.objectContaining({ emitUpdate: false, contentType: "markdown" }),
    );
  });

  it("does not sync when editor is focused and has unsaved local edits", () => {
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor defaultValue="old content" />);

    // User is typing — focused AND dirty (markdown diverges from
    // lastEmittedRef, which was seeded with "old content" by onCreate).
    editorState.isFocused = true;
    editorState.markdown = "user-typed-content";

    rerender(<ContentEditor defaultValue="incoming external change" />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("syncs even when editor is focused, as long as it is clean (focused-but-clean must not be permanently dropped)", () => {
    // This case is the regression test for the focused-but-clean hole:
    // user clicks into the editor (focused = true) but types nothing
    // (markdown still equals lastEmittedRef). An external update arrives.
    // With an unconditional `if (isFocused) return`, this sync would be lost
    // forever because onBlur has no replay path.
    editorState.markdown = "old content";
    const { rerender } = render(<ContentEditor defaultValue="old content" />);

    editorState.isFocused = true;
    editorState.markdown = "old content"; // clean — no typing happened

    rerender(<ContentEditor defaultValue="new content from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      "new content from server",
      expect.objectContaining({ emitUpdate: false, contentType: "markdown" }),
    );
  });

  it("does not sync when editor is unfocused but has unsaved local edits (blur-before-debounce window)", () => {
    editorState.markdown = "old content";
    const { rerender } = render(
      <ContentEditor defaultValue="old content" onUpdate={() => {}} />,
    );

    // User typed locally, then blurred. Debounce hasn't flushed yet so
    // lastEmittedRef inside the component still reflects "old content".
    editorState.isFocused = false;
    editorState.markdown = "user typed but unsaved";

    rerender(
      <ContentEditor
        defaultValue="external update from another agent"
        onUpdate={() => {}}
      />,
    );

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("does not sync when defaultValue normalizes to the current editor markdown", () => {
    editorState.markdown = "same content";
    const { rerender } = render(<ContentEditor defaultValue="same content" />);

    // Different `defaultValue` string forces the effect to re-run (the dep
    // array sees a new value), but the trailing whitespace normalises away
    // via `trimEnd()`, so `setContent` must still short-circuit.
    rerender(<ContentEditor defaultValue={"same content\n"} />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });
});
