import { afterEach, describe, expect, it } from "vitest";
import { Editor, type JSONContent } from "@tiptap/core";
import { createEditorExtensions } from ".";
import { findDetailsEnd } from "./details";
import { preprocessMarkdown } from "../utils/preprocess";
import { parseMarkdownChunked } from "../utils/parse-markdown-chunked";

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

/** Load → save the way ContentEditor does, returning the saved markdown. */
function roundTrip(markdown: string): string {
  editor = makeProductionEditor();
  editor.commands.setContent(preprocessMarkdown(markdown, { cdnDomain: "" }), {
    contentType: "markdown",
  });
  return editor.getMarkdown();
}

const LOG_BLOCK = [
  "Before.",
  "",
  "<details>",
  "<summary>Full log</summary>",
  "",
  "- first",
  "- **second**",
  "",
  "```",
  "exit 1",
  "```",
  "",
  "</details>",
  "",
  "After.",
].join("\n");

describe("details round-trip", () => {
  it("keeps a <details> block and its markdown body", () => {
    expect(roundTrip(LOG_BLOCK).trim()).toBe(LOG_BLOCK);
  });

  it("loads the block as a details node, not loose paragraphs", () => {
    roundTrip(LOG_BLOCK);
    const json: JSONContent = editor!.getJSON();
    const details = json.content?.find((node) => node.type === "details");
    expect(details?.content?.[0]).toMatchObject({
      type: "detailsSummary",
      content: [{ type: "text", text: "Full log" }],
    });
    expect(details?.content?.[1]?.content?.map((child: JSONContent) => child.type)).toEqual([
      "bulletList",
      "codeBlock",
    ]);
  });

  it("is stable across a second save", () => {
    const once = roundTrip(LOG_BLOCK);
    editor?.destroy();
    expect(roundTrip(once)).toBe(once);
  });

  it("keeps the open attribute as authored", () => {
    const source = "<details open>\n<summary>Shown</summary>\n\nBody\n\n</details>";
    expect(roundTrip(source).trim()).toBe(source);
  });

  it("normalises a summary on the same line as <details>", () => {
    const saved = roundTrip("<details><summary>Inline</summary>\n\nBody\n\n</details>");
    expect(saved.trim()).toBe("<details>\n<summary>Inline</summary>\n\nBody\n\n</details>");
  });

  it("escapes HTML in the summary text", () => {
    const source = "<details>\n<summary>a &lt;b&gt; &amp; c</summary>\n\nBody\n\n</details>";
    expect(roundTrip(source).trim()).toBe(source);
    expect(editor!.getText()).toContain("a <b> & c");
  });

  it("keeps nested details blocks", () => {
    const source = [
      "<details>",
      "<summary>Outer</summary>",
      "",
      "<details>",
      "<summary>Inner</summary>",
      "",
      "Deep",
      "",
      "</details>",
      "",
      "</details>",
    ].join("\n");
    expect(roundTrip(source).trim()).toBe(source);
  });

  it("does not end the block at a </details> inside fenced code", () => {
    const source = [
      "<details>",
      "<summary>Sample</summary>",
      "",
      "```html",
      "</details>",
      "```",
      "",
      "</details>",
    ].join("\n");
    expect(roundTrip(source).trim()).toBe(source);
  });

  it("keeps an empty body", () => {
    const source = "<details>\n<summary>Empty</summary>\n\n</details>";
    expect(roundTrip(source).trim()).toBe(source);
  });

  it("loses no text from an unclosed <details>", () => {
    const saved = roundTrip("<details>\n<summary>Never closed</summary>\n\nBody");
    expect(saved).toContain("<summary>Never closed</summary>");
    expect(saved).toContain("Body");
  });

  it("parses pasted HTML details into the same nodes", () => {
    editor = makeProductionEditor();
    editor.commands.setContent(
      "<details open><summary>Pasted</summary><p>Body</p></details>",
    );
    expect(editor.getMarkdown().trim()).toBe(
      "<details open>\n<summary>Pasted</summary>\n\nBody\n\n</details>",
    );
  });
});

describe("findDetailsEnd", () => {
  it("returns -1 when the block never closes", () => {
    expect(findDetailsEnd("<details>\nbody")).toBe(-1);
  });

  it("matches the closing tag of the outer block", () => {
    const source = "<details>\n<details>\n</details>\n</details>\nafter";
    expect(source.slice(0, findDetailsEnd(source))).toBe(
      "<details>\n<details>\n</details>\n</details>",
    );
  });
});

describe("chunked parsing", () => {
  it("never cuts inside a details block", () => {
    editor = makeProductionEditor();
    const manager = (editor.storage as unknown as {
      markdown: { manager: { parse(markdown: string): object } };
    }).markdown.manager;
    const filler = Array.from({ length: 40 }, (_, i) => `Line ${i} of the log.`).join("\n\n");
    const source = `<details>\n<summary>Long</summary>\n\n${filler}\n\n</details>\n\nAfter.`;

    const doc = parseMarkdownChunked(manager as never, source, 200);

    expect(doc.content?.map((node) => node.type)).toEqual(["details", "paragraph"]);
  });
});

describe("GitHub alert markers", () => {
  it("keeps > [!WARNING] unescaped on save", () => {
    const source = "> [!WARNING]\n> Rotate the key first.";
    expect(roundTrip(source).trim()).toBe(source);
  });

  it("still escapes brackets in ordinary quotes", () => {
    const saved = roundTrip("> see [note] here");
    expect(saved.trim()).toBe("> see \\[note\\] here");
  });
});
