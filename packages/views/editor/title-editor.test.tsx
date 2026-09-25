import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

const mockFocus = vi.hoisted(() => vi.fn());
const mockSetContent = vi.hoisted(() => vi.fn());
const mockBlur = vi.hoisted(() => vi.fn());
const editorState = vi.hoisted(() => ({
  isFocused: false,
  isDestroyed: false,
  text: "",
}));

vi.mock("../i18n", () => ({
  useT: () => ({ t: (fn: unknown) => (typeof fn === "function" ? "" : "") }),
}));

const editorRef = vi.hoisted<{ current: unknown }>(() => ({ current: null }));

vi.mock("@tiptap/react", () => ({
  useEditor: () => {
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
          blur: mockBlur,
          setContent: mockSetContent,
        },
        getText: () => editorState.text,
      };
    }
    return editorRef.current;
  },
  EditorContent: () => <div data-testid="editor-content" />,
}));

import { createShortcutChord } from "@multica/core/shortcuts";
import { TitleEditor, titleShortcutSubmitAllowed } from "./title-editor";

describe("TitleEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    editorState.isFocused = false;
    editorState.isDestroyed = false;
    editorState.text = "";
    editorRef.current = null;
  });

  it("syncs editor content when defaultValue changes externally and editor is unfocused", () => {
    editorState.text = "old title";
    const { rerender } = render(<TitleEditor defaultValue="old title" />);

    expect(mockSetContent).not.toHaveBeenCalled();

    rerender(<TitleEditor defaultValue="new title from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      {
        type: "doc",
        content: [
          {
            type: "paragraph",
            content: [{ type: "text", text: "new title from server" }],
          },
        ],
      },
      { emitUpdate: false },
    );
  });

  it("does not overwrite the user's in-flight edits when the editor is focused and dirty", () => {
    editorState.text = "old title";
    const { rerender } = render(<TitleEditor defaultValue="old title" />);

    editorState.isFocused = true;
    editorState.text = "user typed but not yet blurred";

    rerender(<TitleEditor defaultValue="external update" />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  // Regression: a focused but clean editor (user clicked in but never typed)
  // must still accept external updates, otherwise the subsequent blur would
  // compare stale editor text to the new server value and silently roll the
  // external update back.
  it("syncs to new defaultValue when editor is focused but clean", () => {
    editorState.text = "old title";
    const { rerender } = render(<TitleEditor defaultValue="old title" />);

    // User clicked into the title field but has not typed anything yet:
    // editor text still equals the previous defaultValue.
    editorState.isFocused = true;
    editorState.text = "old title";

    rerender(<TitleEditor defaultValue="new title from server" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith(
      {
        type: "doc",
        content: [
          {
            type: "paragraph",
            content: [{ type: "text", text: "new title from server" }],
          },
        ],
      },
      { emitUpdate: false },
    );
  });

  it("short-circuits when editor text already equals incoming defaultValue", () => {
    editorState.text = "same title";
    const { rerender } = render(<TitleEditor defaultValue="same title" />);

    // Force the effect to re-run by rendering with a different prop, then
    // back to the same value. Even an identity-equal prop should be skipped.
    rerender(<TitleEditor defaultValue="same title" />);

    expect(mockSetContent).not.toHaveBeenCalled();
  });

  it("clears the editor when defaultValue transitions to empty", () => {
    editorState.text = "old title";
    const { rerender } = render(<TitleEditor defaultValue="old title" />);

    rerender(<TitleEditor defaultValue="" />);

    expect(mockSetContent).toHaveBeenCalledTimes(1);
    expect(mockSetContent).toHaveBeenCalledWith("", { emitUpdate: false });
  });
});

// MUL-4931 — which `send` bindings the title's shortcut-submit path honors.
describe("titleShortcutSubmitAllowed", () => {
  it("allows the default Mod+Enter chord", () => {
    expect(
      titleShortcutSubmitAllowed(createShortcutChord("Enter", { primary: true })),
    ).toBe(true);
  });

  it("refuses plain Enter, which the single-line keymap already owns", () => {
    // A user may bind `send` to plain Enter for chat. The title must not
    // inherit it — Enter there means "done", and creating from a half-typed
    // title is the misfire #5532 removed.
    expect(titleShortcutSubmitAllowed(createShortcutChord("Enter"))).toBe(false);
  });

  it("refuses an unbound send action", () => {
    expect(titleShortcutSubmitAllowed(null)).toBe(false);
  });
});
