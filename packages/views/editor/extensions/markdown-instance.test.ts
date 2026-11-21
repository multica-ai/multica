import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { marked } from "marked";
import { createEditorExtensions } from ".";

/**
 * Without a marked instance of its own, @tiptap/markdown registers each
 * editor's tokenizers on the global `marked` and never removes them. Every
 * editor that ever mounted then stayed reachable from that global, with its
 * React refs and DOM, and every parse ran all of their tokenizers: switching
 * issues in the Inbox grew memory by ~200 MB a minute (MUL-9634).
 */

/** Two syntaxes parsed by tokenizers the editor registers: highlight and display math. */
const SOURCE = "Text with ==highlight==\n\n$$\nx^2\n$$";
const editors: Editor[] = [];

afterEach(() => {
  for (const editor of editors.splice(0)) editor.destroy();
  document.body.innerHTML = "";
});

function mountEditor(markdown: string): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    content: markdown,
    contentType: "markdown",
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      enableSlashCommands: false,
      onUploadFileRef: { current: undefined },
    }),
  });
  editors.push(editor);
  return editor;
}

function globalTokenizerCount(): number {
  const extensions = marked.defaults.extensions;
  return (extensions?.inline?.length ?? 0) + (extensions?.block?.length ?? 0);
}

describe("editor markdown parser", () => {
  it("leaves the global marked untouched, however many editors mount", () => {
    const before = globalTokenizerCount();
    for (let i = 0; i < 5; i++) mountEditor(SOURCE);
    expect(globalTokenizerCount()).toBe(before);
  });

  it("keeps each editor's own tokenizers", () => {
    const editor = mountEditor(SOURCE);
    const json = JSON.stringify(editor.getJSON());
    expect(json).toContain('"type":"highlight"');
    expect(json).toContain('"type":"blockMath"');
    expect(editor.getMarkdown().trim()).toBe(SOURCE);
  });
});
