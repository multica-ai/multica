import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from ".";
import { preprocessMarkdown } from "../utils/preprocess";
import {
  MARKDOWN_CHUNK_THRESHOLD,
  parseMarkdownChunked,
  type MarkdownManagerLike,
} from "../utils/parse-markdown-chunked";

const editors: Editor[] = [];

afterEach(() => {
  editors.forEach((editor) => editor.destroy());
  editors.length = 0;
  document.body.innerHTML = "";
});

function load(markdown: string): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const processed = preprocessMarkdown(markdown, { cdnDomain: "" });
  const chunked = processed.length > MARKDOWN_CHUNK_THRESHOLD;
  const editor = new Editor({
    element,
    extensions: createEditorExtensions({ disableMentions: true }),
    content: chunked ? "" : processed,
    contentType: chunked ? undefined : "markdown",
  });
  if (chunked) {
    const manager = (editor.storage as { markdown: { manager: MarkdownManagerLike } })
      .markdown.manager;
    editor.commands.setContent(parseMarkdownChunked(manager, processed), {
      emitUpdate: false,
    });
  }
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

describe("fence info string beyond the language", () => {
  // The language is the info string's first word; the rest (a title, line
  // marks) is metadata the editor keeps and writes back unchanged (#10255).
  it.each([
    ['mermaid title="Request flow"', "mermaid", 'title="Request flow"'],
    ["ts title='client.ts' {1,3}", "ts", "title='client.ts' {1,3}"],
    ["html title=Report", "html", "title=Report"],
  ])("splits %j into language and metadata, and saves it verbatim", (info, language, meta) => {
    const content = "graph LR\n  A --> B";
    const source = `Intro\n\n\`\`\`${info}\n${content}\n\`\`\`\n\nOutro`;
    const saved = roundTrip(source, content, language);
    expect(load(source).state.doc.child(1).attrs.meta).toBe(meta);
    expect(saved).toBe(`Edited ${source}`);
  });

  it("has no metadata for a bare language", () => {
    const editor = load("Intro\n\n```ts\nconst a = 1;\n```\n\nOutro");
    expect(editor.state.doc.child(1).attrs.meta).toBeNull();
  });

  it("keeps the metadata through the HTML clipboard path", () => {
    const editor = load('Intro\n\n```mermaid title="Request flow"\ngraph LR\n```\n\nOutro');
    const html = editor.getHTML();
    const pasted = load("");
    pasted.commands.setContent(html);
    const block = pasted.state.doc.child(1);
    expect(block.attrs.language).toBe("mermaid");
    expect(block.attrs.meta).toBe('title="Request flow"');
  });
});

describe("production preprocessing and large-document loading", () => {
  it("keeps a URL literal after serialization grows the outer fence", () => {
    const content = "https://example.com\nconst ticks = '`````';";
    const source = `Intro\n\n\`\`\`javascript\n${content}\n\`\`\`\n\nOutro`;
    roundTrip(source, content, "javascript");
  });

  it("keeps URLs literal between nested fences on initial load", () => {
    const content = "```\nhttps://example.com\n```\nafter";
    const source = `Intro\n\n\`\`\`\`markdown\n${content}\n\`\`\`\`\n\nOutro`;
    roundTrip(source, content, "markdown");
  });

  it.each(["```", "~~~", "````javascript"])(
    "does not split a large code block at an inner %s line",
    (inner) => {
      const content = `${"x".repeat(4_100)}\n${inner}\n\n# still literal\n${"y".repeat(4_100)}`;
      const source = `Intro\n\n\`\`\`\`markdown\n${content}\n\`\`\`\`\n\nOutro`;
      expect(source.length).toBeGreaterThan(MARKDOWN_CHUNK_THRESHOLD);
      roundTrip(source, content, "markdown");
    },
  );
});
