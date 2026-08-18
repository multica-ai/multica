import { describe, it, expect, beforeAll } from "vitest";
import { Editor, Extension } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { Suggestion } from "@tiptap/suggestion";
import { PluginKey, TextSelection } from "@tiptap/pm/state";
import { EditorView } from "@tiptap/pm/view";
import { createMarkdownPasteExtension } from "./markdown-paste";
import {
  SuggestionTriggerArmingExtension,
  isTriggerArmedAt,
} from "./suggestion-trigger-arming";

// ---------------------------------------------------------------------------
// MUL-5429. These drive the REAL pipeline end to end — a `paste` event on the
// editor DOM, and typing routed through `handleTextInput` exactly the way
// prosemirror-view's readDOMChange does it. That matters: the previous attempt
// at this fix passed its tests by hand-building transactions carrying a
// `uiEvent: "paste"` meta, which the real paste path never produces, because
// markdownPaste's catch-all `handlePaste` returns true and so short-circuits
// `doPaste` before ProseMirror stamps that meta. Anything that stubs the
// transaction instead of the event can be green while the product is broken.
// ---------------------------------------------------------------------------

const key = new PluginKey("test-mention-suggestion");
const slashKey = new PluginKey("test-slash-suggestion");

function suggestionProbe(name: string, char: "@" | "/", pluginKey: PluginKey) {
  return Extension.create({
    name,
    addProseMirrorPlugins() {
      return [
        Suggestion({
          editor: this.editor,
          char,
          pluginKey,
          // The mention picker's real config; `allowSpaces` is what lets a
          // single stray `@` swallow the rest of the line.
          allowSpaces: char === "@",
          shouldShow: ({ editor, range }) => isTriggerArmedAt(editor, range.from),
          items: () => [{ id: "1", label: "Alice" }],
          render: () => ({}),
        }),
      ];
    },
  });
}

function makeEditor(content?: object) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit,
      Markdown,
      createMarkdownPasteExtension(),
      SuggestionTriggerArmingExtension,
      suggestionProbe("mentionProbe", "@", key),
      suggestionProbe("slashProbe", "/", slashKey),
    ],
    content: content ?? { type: "doc", content: [{ type: "paragraph" }] },
  });
}

/** Fires a real `paste` event, so the whole handlePaste chain runs. */
function paste(editor: Editor, text: string, html = ""): void {
  const event = new Event("paste", { bubbles: false, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      files: [],
      getData: (type: string) =>
        type === "text/plain" ? text : type === "text/html" ? html : "",
    },
  });
  editor.view.dom.dispatchEvent(event);
}

/**
 * Types text the way ProseMirror does: `readDOMChange` consults
 * `handleTextInput` and, when no plugin claims the keystroke, dispatches the
 * insertion itself. Going through the prop is the point — that is the only
 * signal that separates typing from pasting.
 */
function type(editor: Editor, text: string): void {
  for (const ch of text) {
    const { from, to } = editor.state.selection;
    const handled = editor.view.someProp("handleTextInput", (fn) =>
      fn(editor.view, from, to, ch, () => editor.state.tr.insertText(ch, from, to)),
    );
    if (!handled) editor.view.dispatch(editor.state.tr.insertText(ch, from, to));
  }
}

function picker(editor: Editor, pluginKey: PluginKey = key) {
  const state = pluginKey.getState(editor.state);
  return { active: state?.active, query: state?.query };
}

beforeAll(() => {
  // jsdom has no layout, so ProseMirror's post-dispatch scroll walks
  // coordsAtPos into getClientRects() on nodes jsdom does not implement.
  // Scrolling is irrelevant here; these tests read plugin state.
  (EditorView.prototype as unknown as { scrollToSelection: () => void }).scrollToSelection =
    () => {};
  Element.prototype.scrollIntoView = () => {};
});

describe("suggestion trigger arming", () => {
  describe("text the user did not type never opens a picker", () => {
    it("pasted text containing @", () => {
      const editor = makeEditor();
      editor.commands.focus("end");

      paste(editor, "npx @aiforui/install --token=abc");

      expect(editor.getText()).toContain("@aiforui/install");
      expect(picker(editor).active).toBe(false);
    });

    it("pasted text containing a / path", () => {
      const editor = makeEditor();
      editor.commands.focus("end");

      paste(editor, "binary lives in /usr/local/bin");

      expect(picker(editor, slashKey).active).toBe(false);
    });

    it("stays shut across the transactions that follow the paste", () => {
      const editor = makeEditor();
      editor.commands.focus("end");
      paste(editor, "npx @aiforui/install");

      // A bare selection change. Under the previous fix this single extra
      // transaction was enough to re-open the picker: the paste meta was gone,
      // and `shouldShow` cannot see that the picker was closed a moment ago.
      editor.view.dispatch(
        editor.state.tr.setSelection(
          TextSelection.create(editor.state.doc, editor.state.doc.content.size - 1),
        ),
      );

      expect(picker(editor).active).toBe(false);
    });

    it("stays shut when the user keeps typing after the pasted @", () => {
      const editor = makeEditor();
      editor.commands.focus("end");
      paste(editor, "npx @aiforui");

      type(editor, "x");

      expect(picker(editor).active).toBe(false);
    });

    it("text already in the document when the editor mounts", () => {
      // No paste at all — a loaded draft, an undo, a collaborative edit. Tiptap
      // matches on document content alone, so this opened the picker too.
      const editor = makeEditor({
        type: "doc",
        content: [
          { type: "paragraph", content: [{ type: "text", text: "npx @aiforui/install" }] },
        ],
      });

      editor.commands.focus("end");

      expect(picker(editor).active).toBe(false);
    });
  });

  describe("typing still works exactly as before", () => {
    it("typing @ opens the mention picker", () => {
      const editor = makeEditor();
      editor.commands.focus("end");

      type(editor, "hi @al");

      expect(picker(editor)).toEqual({ active: true, query: "al" });
    });

    it("typing / opens the slash picker", () => {
      const editor = makeEditor();
      editor.commands.focus("end");

      type(editor, "/shi");

      expect(picker(editor, slashKey)).toEqual({ active: true, query: "shi" });
    });

    it("keeps multi-word queries alive, so allowSpaces issue search survives", () => {
      const editor = makeEditor();
      editor.commands.focus("end");

      type(editor, "@login redirect bug");

      expect(picker(editor)).toEqual({ active: true, query: "login redirect bug" });
    });

    it("pasting into a query the user opened by typing keeps it open", () => {
      // Arming is about the trigger character, not about every later edit —
      // pasting a name into a mention you deliberately started is still a
      // mention.
      const editor = makeEditor();
      editor.commands.focus("end");
      type(editor, "@");

      paste(editor, "alice");

      expect(picker(editor)).toEqual({ active: true, query: "alice" });
    });
  });

  describe("disarming", () => {
    it("deleting the typed @ disarms it", () => {
      const editor = makeEditor();
      editor.commands.focus("end");
      type(editor, "@al");

      editor.commands.deleteRange({ from: 1, to: 4 });
      type(editor, "x");

      expect(picker(editor).active).toBe(false);
    });

    it("moving the caret away and back does not re-open the picker", () => {
      // ueberdosis/tiptap#7371: after leaving a half-typed mention, arrowing
      // back onto it re-opened the menu. A deliberate caret move disarms.
      const editor = makeEditor();
      editor.commands.focus("end");
      type(editor, "@al");
      expect(picker(editor).active).toBe(true);

      editor.commands.setTextSelection(1);
      editor.commands.setTextSelection(editor.state.doc.content.size - 1);

      expect(picker(editor).active).toBe(false);
    });
  });
});
