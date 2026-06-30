/**
 * Regression tests for the inline-code Markdown shortcut.
 *
 * Bug #4728: typing `` `code` `` while the cursor sits anywhere after a non-
 * backtick character would swallow the character immediately preceding the
 * opening backtick. Repro: type `abcd\`123\`` — the final string is
 * `abc\`123\``, with the `d` eaten.
 *
 * Root cause: the upstream `markInputRule` helper in `@tiptap/core@3.22.1`
 * computes the start of the deletion range using `fullMatch.search(/\S/)`,
 * which assumes the regex's leading capture group is whitespace. The
 * `@tiptap/extension-code` regex `/(^|[^\`])\`([^\`]+)\`(?!\`)$/` uses a
 * NON-whitespace capture (`[^\`]`) as a guard against nested backticks, so
 * the search returns 0 and `markInputRule` deletes from the start of the
 * full match — eating the guard character along with the opening backtick.
 *
 * These tests pin both the bug shape and the contract the fix must satisfy.
 *
 * Test driver: ProseMirror's input rules fire from `handleTextInput`, which
 * is invoked by EditorView during real DOM input — not by `insertContent` /
 * `setContent` commands. We seed the document with the pre-trigger text via
 * commands and then drive the final backtick through `view.someProp(
 * "handleTextInput", ...)` to exercise the exact code path a real keystroke
 * would take. This is the lowest-level public API that the inputRules
 * plugin listens on.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { Editor } from "@tiptap/core";
import Document from "@tiptap/extension-document";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { Code } from "@tiptap/extension-code";
import { BulletList, ListItem } from "@tiptap/extension-list";
import { Markdown } from "@tiptap/markdown";
import { InlineCodeFixedExtension } from "./inline-code-input";

import type { AnyExtension } from "@tiptap/core";

function makeEditor(extensions: AnyExtension[]) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: [Document, Paragraph, Text, ...extensions],
    content: "",
  });
  return { editor, element };
}

/**
 * Seed the editor with `prefix`, then drive each character of `suffix`
 * through `handleTextInput` so input rules fire. The split mirrors what a
 * real user does: they type the lead-in, then the final trigger character.
 */
function typeWithRules(editor: Editor, prefix: string, suffix: string) {
  if (prefix) {
    editor.commands.insertContent(prefix);
  }
  // Move the selection to the end of the document so handleTextInput inserts
  // at the right position.
  editor.commands.focus("end");
  const view = editor.view;
  type TextInputHandler = (
    view: typeof editor.view,
    from: number,
    to: number,
    text: string,
  ) => boolean;
  for (const ch of suffix) {
    const { from, to } = view.state.selection;
    // handleTextInput is what prosemirror-inputrules subscribes to. Returns
    // true if a rule handled it; otherwise we insert the text ourselves so
    // the document stays in sync. The someProp generic is too narrow for
    // jsdom's prosemirror types — we know the runtime contract.
    const handled = (
      view.someProp as (
        prop: "handleTextInput",
        f: (handler: TextInputHandler) => boolean,
      ) => boolean | undefined
    )("handleTextInput", (f) => f(view, from, to, ch));
    if (!handled) {
      view.dispatch(view.state.tr.insertText(ch, from, to));
    }
  }
}

describe("inline-code input rule", () => {
  describe("upstream @tiptap/extension-code (broken in 3.22.1)", () => {
    let editor: Editor;
    let element: HTMLElement;

    beforeEach(() => {
      const made = makeEditor([Code]);
      editor = made.editor;
      element = made.element;
    });

    afterEach(() => {
      editor.destroy();
      element.remove();
    });

    // This test pins the BUG. When the fix is applied via overlay, we still
    // expect the unpatched extension to misbehave — it documents WHY the
    // overlay exists. If TipTap upstream ever fixes markInputRule and we
    // bump versions, this test will start failing and tell us we can drop
    // the overlay.
    it("[REGRESSION DOC] swallows the character preceding the opening backtick", () => {
      // Type `abcd` first, then the rule trigger sequence `\`123\``.
      typeWithRules(editor, "abcd", "`123`");
      const html = editor.getHTML();
      expect(html).toContain("<code>123</code>");
      // The bug: `d` was eaten. If this assertion ever fails (i.e. `d` shows
      // up adjacent to the <code>), the upstream bug has been fixed and the
      // overlay can be removed.
      expect(html).not.toMatch(/d<code>/);
    });
  });

  describe("overlay fix InlineCodeFixedExtension", () => {
    let editor: Editor;
    let element: HTMLElement;

    beforeEach(() => {
      const made = makeEditor([InlineCodeFixedExtension]);
      editor = made.editor;
      element = made.element;
    });

    afterEach(() => {
      editor.destroy();
      element.remove();
    });

    it("preserves the character before the opening backtick (the reported bug)", () => {
      typeWithRules(editor, "abcd", "`123`");
      const html = editor.getHTML();
      // Expected: the `d` survives, only the surrounding backticks are
      // consumed, and `123` is wrapped in a <code> mark.
      expect(html).toContain("d<code>123</code>");
      expect(html).not.toContain("`");
    });

    it("still applies the code mark when the opening backtick is at the start of the line", () => {
      typeWithRules(editor, "", "`hello`");
      expect(editor.getHTML()).toContain("<code>hello</code>");
      expect(editor.getHTML()).not.toContain("`");
    });

    it("still applies the code mark when the opening backtick is preceded by a space", () => {
      typeWithRules(editor, "see ", "`here`");
      const html = editor.getHTML();
      expect(html).toContain("see <code>here</code>");
      expect(html).not.toContain("`");
    });

    it("preserves a Chinese character before the opening backtick", () => {
      // Multi-byte characters are the original repro target's domain — issue
      // author's screenshots include CJK text. This pin guards against a
      // naive fix that uses byte offsets instead of code-unit offsets.
      typeWithRules(editor, "代码", "`abc`");
      const html = editor.getHTML();
      expect(html).toContain("代码<code>abc</code>");
      expect(html).not.toContain("`");
    });

    it("does NOT fire on triple backticks (preserves fenced-code-block syntax)", () => {
      // The upstream regex uses `(?!\`)` to skip if a third backtick follows.
      // The overlay must preserve that guard so it doesn't grab content that
      // a fenced-code-block input rule should handle.
      typeWithRules(editor, "a", "```code```");
      const html = editor.getHTML();
      // No <code> mark should appear; the literal backticks stay as text.
      expect(html).not.toContain("<code>");
    });

    it("does NOT fire when the user is mid-content and there is no closing backtick yet", () => {
      typeWithRules(editor, "abcd", "`123");
      const html = editor.getHTML();
      expect(html).not.toContain("<code>");
      // All four leading chars must survive.
      expect(html).toContain("abcd");
    });

    it("handles empty content between backticks gracefully (no crash, no empty code mark)", () => {
      typeWithRules(editor, "a", "``");
      const html = editor.getHTML();
      // The upstream regex requires `[^\`]+` inside, so an empty pair should
      // NOT trigger the rule. Verify nothing crashed and no <code>.
      expect(html).not.toContain("<code>");
    });

    it("handles consecutive code spans on the same line", () => {
      typeWithRules(editor, "a", "`x` b`y`");
      const html = editor.getHTML();
      expect(html).toContain("<code>x</code>");
      expect(html).toContain("<code>y</code>");
      // No raw backticks left.
      expect(html).not.toContain("`");
    });

    it("preserves the boundary inside a non-empty paragraph (mid-sentence)", () => {
      // Seed a longer pre-existing paragraph so the input rule fires while
      // the cursor is well past the start of a textblock — the rule's
      // `range.from` is no longer the start of the document. This is the
      // shape closest to a real user typing into an existing comment.
      typeWithRules(editor, "I prefer abcd", "`123`");
      const html = editor.getHTML();
      expect(html).toContain("I prefer abcd<code>123</code>");
      expect(html).not.toContain("`");
    });
  });

  describe("overlay fix InlineCodeFixedExtension — inside non-paragraph blocks", () => {
    let editor: Editor;
    let element: HTMLElement;

    beforeEach(() => {
      const made = makeEditor([
        BulletList,
        ListItem,
        InlineCodeFixedExtension,
      ]);
      editor = made.editor;
      element = made.element;
    });

    afterEach(() => {
      editor.destroy();
      element.remove();
    });

    it("preserves the boundary character inside a list item", () => {
      // Start a bullet list, then type into the item.
      editor.commands.toggleBulletList();
      editor.commands.focus("end");
      typeWithRules(editor, "abcd", "`123`");
      const html = editor.getHTML();
      // The list-item content must show `abcd<code>123</code>` — boundary
      // preserved even inside a non-paragraph textblock.
      expect(html).toContain("abcd<code>123</code>");
      expect(html).not.toContain("`");
    });
  });

  describe("overlay fix InlineCodeFixedExtension — markdown round-trip", () => {
    let editor: Editor;
    let element: HTMLElement;

    beforeEach(() => {
      // Markdown extension provides `renderMarkdown` + `parseMarkdown`
      // wired to the Code mark's serializer. Lets us check the output
      // format the rest of the product reads/writes.
      const made = makeEditor([InlineCodeFixedExtension, Markdown]);
      editor = made.editor;
      element = made.element;
    });

    afterEach(() => {
      editor.destroy();
      element.remove();
    });

    it("emits standard Markdown backticks for an inline code span", () => {
      typeWithRules(editor, "abcd", "`123`");
      // The Markdown extension installs `getMarkdown` directly on the
      // Editor instance; cast through unknown so we don't leak the
      // extension's augmentation typing into this file.
      const md = (
        editor as unknown as { getMarkdown: () => string }
      ).getMarkdown();
      expect(md).toContain("abcd`123`");
    });
  });

  describe("overlay fix InlineCodeFixedExtension — paste path (#4728 paste twin)", () => {
    let editor: Editor;
    let element: HTMLElement;
    let removeClipboardPolyfill: (() => void) | null = null;

    beforeEach(() => {
      // jsdom does not implement ClipboardEvent or DataTransfer at all.
      // TipTap's PasteRule plugin constructs `new ClipboardEvent("paste")`
      // internally even when paste is *simulated* via `applyPasteRules`.
      // Polyfill just enough surface to let the plugin run; the handlers
      // under test never read the event payload (only the meta flag).
      if (typeof globalThis.ClipboardEvent === "undefined") {
        const g = globalThis as unknown as {
          ClipboardEvent?: unknown;
          DataTransfer?: unknown;
        };
        g.ClipboardEvent = class ClipboardEvent extends Event {
          clipboardData: unknown;
          constructor(type: string, init?: { clipboardData?: unknown }) {
            super(type, init as EventInit);
            this.clipboardData = init?.clipboardData ?? null;
          }
        };
        g.DataTransfer = class DataTransfer {
          private store = new Map<string, string>();
          setData(format: string, data: string) {
            this.store.set(format, data);
          }
          getData(format: string) {
            return this.store.get(format) ?? "";
          }
        };
        removeClipboardPolyfill = () => {
          delete g.ClipboardEvent;
          delete g.DataTransfer;
        };
      }

      const made = makeEditor([InlineCodeFixedExtension]);
      editor = made.editor;
      element = made.element;
    });

    afterEach(() => {
      editor.destroy();
      element.remove();
      if (removeClipboardPolyfill) {
        removeClipboardPolyfill();
        removeClipboardPolyfill = null;
      }
    });

    it("preserves the boundary character when the markdown text is pasted, not typed", () => {
      // TipTap's PasteRule plugin treats `insertContent(..., {
      // applyPasteRules: true })` exactly like a real DOM paste event,
      // including the offset arithmetic — see
      // `transaction.getMeta("applyPasteRules")` in PasteRule.ts. With
      // the ClipboardEvent / DataTransfer polyfill above, the plugin can
      // run in jsdom without touching the real DOM clipboard API.
      editor.commands.insertContent("abcd");
      editor.commands.focus("end");
      editor
        .chain()
        .insertContent("`123`", { applyPasteRules: true })
        .run();

      const html = editor.getHTML();
      // The paste-twin of #4728: pasting `\`123\`` must NOT eat `d`.
      expect(html).toContain("abcd<code>123</code>");
      expect(html).not.toContain("`");
    });
  });

  describe("overlay fix InlineCodeFixedExtension — full product extension stack", () => {
    // End-to-end sanity: load the SAME extension array the product uses
    // (StarterKit + PatchedListItem + InlineCodeFixedExtension + everything
    // else) and verify the bug is fixed in that exact configuration. This
    // catches any conflict introduced by a sibling extension's input or
    // paste rule.
    let editor: Editor;
    let element: HTMLElement;

    beforeEach(async () => {
      const { createEditorExtensions } = await import("./index");
      const extensions = createEditorExtensions({});
      element = document.createElement("div");
      document.body.appendChild(element);
      editor = new Editor({
        element,
        extensions,
        content: "",
      });
    });

    afterEach(() => {
      editor.destroy();
      element.remove();
    });

    it("preserves the boundary character with the full product extension stack", () => {
      typeWithRules(editor, "abcd", "`123`");
      const html = editor.getHTML();
      expect(html).toContain("abcd<code>123</code>");
      expect(html).not.toContain("`");
    });

    it("preserves a Chinese boundary character with the full product extension stack", () => {
      typeWithRules(editor, "代码", "`abc`");
      const html = editor.getHTML();
      expect(html).toContain("代码<code>abc</code>");
      expect(html).not.toContain("`");
    });
  });
});
