import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from ".";

/**
 * Underline has no Markdown representation. Tiptap's Underline extension
 * serializes the mark as `++text++`, which neither CommonMark nor GFM defines,
 * so ReadonlyContent (react-markdown + remark-gfm) renders the delimiters
 * literally and the user sees `++text++` in a saved comment.
 *
 * StarterKit registers Underline by default, and it maps BOTH `<u>` and
 * `text-decoration: underline` to the mark, so rich-text paste was enough to
 * produce unrenderable Markdown. These tests pin the mark out of the schema.
 */

let editor: Editor | null = null;

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

function makeProductionEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);

  return new Editor({
    element,
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      enableSlashCommands: false,
      onUploadFileRef: { current: undefined },
    }),
  });
}

/**
 * Dispatches a real `paste` event at the editor DOM, which is where
 * ProseMirror binds its own paste handler. That runs the genuine production
 * chain — clipboard parse against the editor schema, then the handlePaste
 * props — rather than a reimplementation of it.
 *
 * The path exercised here is the native one: markdownPaste classifies semantic
 * rich HTML (`<u>`, styled spans) as "native" and declines it, so ProseMirror
 * parses the HTML against the schema, which is exactly where a mark that no
 * registered extension claims gets dropped.
 *
 * `bubbles: false` keeps ProseMirror's eventBelongsToView check trivially
 * satisfied; it does not affect its own listener on view.dom.
 */
function paste(ed: Editor, text: string, html: string): void {
  const event = new Event("paste", { bubbles: false, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      files: [],
      getData: (type: string) =>
        type === "text/plain" ? text : type === "text/html" ? html : "",
    },
  });
  ed.view.dom.dispatchEvent(event);
}

describe("underline is not in the editor schema", () => {
  it("registers no underline mark, so Cmd+U cannot produce one", () => {
    editor = makeProductionEditor();

    // The mark type backs both the Mod-u shortcut and HTML parsing. Absent
    // from the schema, neither entry point can create `++text++`.
    expect(editor.schema.marks.underline).toBeUndefined();
  });
});

describe("pasting underlined rich text", () => {
  it("keeps the text of a pasted <u> tag and writes no ++ delimiters", () => {
    editor = makeProductionEditor();

    paste(
      editor,
      "before underlined after",
      '<meta charset="utf-8"><p>before <u>underlined</u> after</p>',
    );

    expect(editor.getText()).toContain("underlined");
    expect(editor.getMarkdown()).toContain("underlined");
    expect(editor.getMarkdown()).not.toContain("++");
  });

  it("keeps the text of a pasted text-decoration: underline span and writes no ++ delimiters", () => {
    editor = makeProductionEditor();

    paste(
      editor,
      "before underlined after",
      '<meta charset="utf-8"><p>before <span style="text-decoration: underline">underlined</span> after</p>',
    );

    expect(editor.getText()).toContain("underlined");
    expect(editor.getMarkdown()).toContain("underlined");
    expect(editor.getMarkdown()).not.toContain("++");
  });

  it("preserves sibling formatting when dropping the underline mark", () => {
    editor = makeProductionEditor();

    paste(
      editor,
      "bold underlined",
      '<meta charset="utf-8"><p><strong>bold</strong> <u>underlined</u></p>',
    );

    const markdown = editor.getMarkdown();
    expect(markdown).toContain("**bold**");
    expect(markdown).toContain("underlined");
    expect(markdown).not.toContain("++");
  });
});

describe("existing content containing ++", () => {
  it("round-trips literal ++ text unchanged", () => {
    // Historical comments already contain `++` (both from this bug and from
    // legitimate text like `g++`). Disabling Underline also removes its
    // markdown tokenizer, so `++x++` is now plain text — editing and re-saving
    // such a comment must not rewrite or re-escape it.
    editor = makeProductionEditor();
    editor.commands.setContent(editor.markdown!.parse("a ++b++ c"));

    expect(editor.getText()).toBe("a ++b++ c");
    expect(editor.getMarkdown().trim()).toBe("a ++b++ c");
  });
});
