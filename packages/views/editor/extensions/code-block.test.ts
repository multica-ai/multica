import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from ".";
import { preprocessMarkdown } from "../utils/preprocess";

const editors: Editor[] = [];

afterEach(() => {
  editors.forEach((editor) => editor.destroy());
  editors.length = 0;
  document.body.innerHTML = "";
});

function load(markdown: string): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: createEditorExtensions({ disableMentions: true }),
    content: preprocessMarkdown(markdown, { cdnDomain: "" }),
    contentType: "markdown",
  });
  editors.push(editor);
  return editor;
}

function roundTrip(source: string, content: string, language: string | null) {
  const editor = load(source);
  expect(editor.state.doc.child(1).type.name).toBe("codeBlock");
  expect(editor.state.doc.child(1).textContent).toBe(content);
  expect(editor.state.doc.child(1).attrs.language).toBe(language);

  // Editing an unrelated paragraph saves every block in ContentEditor.
  editor.commands.setTextSelection(1);
  editor.commands.insertContent("Edited ");
  const beforeSave = editor.getJSON();
  const saved = editor.getMarkdown();
  const reloaded = load(saved);
  expect(reloaded.getJSON()).toEqual(beforeSave);
  expect(reloaded.getMarkdown()).toBe(saved);
  return saved;
}

describe("code block Markdown save and reload", () => {
  for (const length of [3, 4, 6]) {
    for (const language of ["", "markdown", "typescript", "unknown-language"]) {
      it(`preserves a ${length}-backtick line with language '${language}'`, () => {
        const inner = "`".repeat(length);
        const outer = "`".repeat(length + 1);
        const content = `before\n${inner}\n# still code\n\n<tag> & *literal*\nafter`;
        const source = `Intro\n\n${outer}${language}\n${content}\n${outer}\n\nOutro`;
        const saved = roundTrip(source, content, language || null);
        expect(saved).toContain(`${outer}${language}\n${content}\n${outer}`);
      });
    }
  }

  it.each([
    ["", ""],
    ["javascript", "console.log('hello');"],
    ["", "one `tick` and ``two``"],
    ["text", "first\n\nlast\n"],
  ])("keeps ordinary '%s' blocks unchanged (%j)", (language, content) => {
    const source = `Intro\n\n\`\`\`${language}\n${content}\n\`\`\`\n\nOutro`;
    expect(roundTrip(source, content, language || null)).toBe(`Edited ${source}`);
  });

  it("safely serializes tilde-fenced input containing backtick fences", () => {
    const content = "```js\nconst value = 1;\n```\ntail";
    roundTrip(`Intro\n\n~~~markdown\n${content}\n~~~\n\nOutro`, content, "markdown");
  });

  it("preserves nested fences in a blockquote", () => {
    const source = "Intro\n\n> `````markdown\n> before\n> ````\n> after\n> `````\n\nOutro";
    const editor = load(source);
    const saved = editor.getMarkdown();
    expect(load(saved).getJSON()).toEqual(editor.getJSON());
  });
});
