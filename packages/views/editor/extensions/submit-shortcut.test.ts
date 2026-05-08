import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { createSubmitExtension } from "./submit-shortcut";

function makeEditor(opts: { submitOnEnter: boolean; onSubmit: () => boolean }) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit,
      createSubmitExtension(opts.onSubmit, { submitOnEnter: opts.submitOnEnter }),
    ],
  });
}

// Simulate the keypress through ProseMirror's keymap. ProseMirror's Mod
// detection uses navigator.platform: metaKey on mac, ctrlKey elsewhere.
// jsdom reports a non-mac platform by default, so the canonical Mod-Enter
// in tests sets ctrlKey only — setting both flags trips a different
// branch in prosemirror-keymap that doesn't match "Mod-Enter".
function pressKey(editor: Editor, opts: { key: string; mod?: boolean }) {
  const event = new KeyboardEvent("keydown", {
    key: opts.key,
    ctrlKey: opts.mod ?? false,
    bubbles: true,
    cancelable: true,
  });
  return (
    editor.view.someProp("handleKeyDown", (handler) =>
      handler(editor.view, event),
    ) ?? false
  );
}

describe("submit-shortcut extension", () => {
  let editor: Editor;
  let submitCount: number;

  beforeEach(() => {
    submitCount = 0;
  });

  afterEach(() => {
    editor?.destroy();
  });

  describe("submitOnEnter: false (default)", () => {
    beforeEach(() => {
      editor = makeEditor({
        submitOnEnter: false,
        onSubmit: () => {
          submitCount += 1;
          return true;
        },
      });
    });

    it("Mod-Enter submits", () => {
      pressKey(editor, { key: "Enter", mod: true });
      expect(submitCount).toBe(1);
    });

    it("bare Enter does NOT submit (falls through to default newline)", () => {
      pressKey(editor, { key: "Enter" });
      expect(submitCount).toBe(0);
    });
  });

  describe("submitOnEnter: true (chat / comment / quick-create style)", () => {
    beforeEach(() => {
      editor = makeEditor({
        submitOnEnter: true,
        onSubmit: () => {
          submitCount += 1;
          return true;
        },
      });
    });

    it("bare Enter submits", () => {
      pressKey(editor, { key: "Enter" });
      expect(submitCount).toBe(1);
    });

    it("Mod-Enter still submits", () => {
      pressKey(editor, { key: "Enter", mod: true });
      expect(submitCount).toBe(1);
    });

    // IME guard — Chinese pinyin / Japanese kana / Korean hangul IMEs commit
    // a multi-key composition with Enter. Submitting on that keypress fires
    // before the user has finished typing, so we explicitly fall through
    // (return false) when ProseMirror reports `view.composing`. Mirrors the
    // condition in @multica/core/utils.isImeComposing.
    it("does NOT submit on Enter while ProseMirror is composing", () => {
      // Mutate the composing flag directly. ProseMirror sets this from
      // compositionstart/compositionend on the editable; the implementation
      // detail we care about is that the extension respects it.
      Object.defineProperty(editor.view, "composing", {
        value: true,
        configurable: true,
      });

      pressKey(editor, { key: "Enter" });
      expect(submitCount).toBe(0);
    });

    // Code blocks need bare Enter to insert a newline (otherwise users can't
    // write multi-line code in a comment without remembering Shift-Enter).
    it("does NOT submit on Enter inside a code block", () => {
      editor.chain().focus().setCodeBlock().run();
      pressKey(editor, { key: "Enter" });
      expect(submitCount).toBe(0);
    });
  });
});
