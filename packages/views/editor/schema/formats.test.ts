import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import { createEditorExtensions } from "../extensions";
import { preprocessMarkdown } from "../utils/preprocess";
import {
  MARKDOWN_CHUNK_THRESHOLD,
  parseMarkdownChunked,
  type MarkdownManagerLike,
} from "../utils/parse-markdown-chunked";
import { createDocumentCodec } from "./codec";

const CDN = "cdn.multica.test";

// Every format the docs editor can hold, written the way users and agents
// write it, including the legacy forms still stored in older documents.
const ALL_FORMATS = [
  "# Heading 1",
  "## Heading 2",
  "### Heading 3",
  "#### Heading 4",
  "##### Heading 5",
  "###### Heading 6",
  "",
  "Plain text with **bold**, *italic*, ***both***, ~~strike~~, `inline code`, ==highlight==, **==bold highlight==** and an escaped \\*star\\*.",
  "",
  "A [link](https://example.com \"title\"), a bare URL https://multica.ai/docs and a mailto <hello@multica.ai>.",
  "",
  "Mentions: [@Ada](mention://member/11111111-1111-4111-8111-111111111111), [@Bot\\[TF\\]](mention://agent/22222222-2222-4222-8222-222222222222), [MUL-42](mention://issue/33333333-3333-4333-8333-333333333333), and a legacy [@ id=\"44444444-4444-4444-8444-444444444444\" label=\"Grace\"].",
  "",
  "A skill [/deploy](slash://skill/55555555-5555-4555-8555-555555555555) and a price of $100~$120 that is not math.",
  "",
  "中文段落，混排 English 与 emoji 🚀，以及全角标点：「引号」。",
  "",
  "Line one with a hard break  ",
  "line two.",
  "",
  "- bullet",
  "   - nested bullet",
  "      - deeper",
  "- second **bold** bullet",
  "",
  "1. first",
  "2. second",
  "   1. nested ordered",
  "   2. another",
  "3. third",
  "",
  "- [ ] open task",
  "- [x] done task",
  "   - [ ] nested task",
  "- [ ] [MUL-42](mention://issue/33333333-3333-4333-8333-333333333333) task with a mention",
  "",
  "> A quote with **bold**",
  ">",
  "> - and a list",
  "> > nested quote",
  "",
  "---",
  "",
  "```ts",
  "const a: number = 1; // **not bold**",
  "if (a == 1) console.log(\"==no highlight==\");",
  "```",
  "",
  "```",
  "plain fence with <tag> and $$ not math $$",
  "```",
  "",
  "```mermaid",
  "graph TD; A-->B;",
  "```",
  "",
  "$$",
  "\\int_0^1 x^2 \\, dx = \\frac{1}{3}",
  "$$",
  "",
  "| Left | Center | Right |",
  "| :--- | :---: | ---: |",
  "| **bold** | `code` | [link](https://example.com) |",
  "| [@Ada](mention://member/11111111-1111-4111-8111-111111111111) | ==hi== | 中文 |",
  "",
  "![A screenshot](https://cdn.multica.test/images/shot.png \"Shot title\")",
  "",
  "![attachment](/api/attachments/66666666-6666-4666-8666-666666666666/download)",
  "",
  "!file[report \\[v2\\].pdf](https://cdn.multica.test/files/report.pdf)",
  "",
  "[legacy.zip](https://cdn.multica.test/files/legacy.zip)",
  "",
  "Last paragraph.",
].join("\n");

const editors: Editor[] = [];
afterEach(() => {
  editors.splice(0).forEach((editor) => editor.destroy());
  document.body.innerHTML = "";
});

function mountEditor(collaboration?: { document: Y.Doc }) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: createEditorExtensions({
      placeholder: "",
      enableSlashCommands: false,
      onUploadFileRef: { current: undefined },
      ...(collaboration
        ? {
            collaboration: {
              document: collaboration.document,
              provider: { awareness: new Awareness(collaboration.document) },
              user: { name: "Ada", color: "#3b82f6" },
            },
          }
        : {}),
    }),
  });
  editors.push(editor);
  return editor;
}

/** How the doc page loads a document when it is edited alone (ContentEditor). */
function openSolo(markdown: string) {
  const editor = mountEditor();
  const source = preprocessMarkdown(markdown, { cdnDomain: CDN });
  if (source.length > MARKDOWN_CHUNK_THRESHOLD) {
    const manager = (editor.storage as { markdown: { manager: MarkdownManagerLike } }).markdown.manager;
    editor.commands.setContent(parseMarkdownChunked(manager, source), { emitUpdate: false });
  } else {
    editor.commands.setContent(source, { emitUpdate: false, contentType: "markdown" });
  }
  return editor;
}

/** How it loads in a live session: the collaboration server builds the Yjs
 *  document from the stored Markdown and the browser binds to a copy of it. */
function openLive(markdown: string) {
  const server = createDocumentCodec({ cdnDomain: CDN }).createYDoc(markdown);
  const client = new Y.Doc();
  Y.applyUpdate(client, Y.encodeStateAsUpdate(server));
  return { editor: mountEditor({ document: client }), server };
}

describe.each([
  ["every format", ALL_FORMATS],
  ["every format in a document long enough to parse in chunks", Array(8).fill(ALL_FORMATS).join("\n\n")],
])("a live document with %s", (_name, markdown) => {
  it("is the document the solo editor builds", () => {
    const solo = openSolo(markdown);
    const { editor: live } = openLive(markdown);
    expect(live.getJSON()).toEqual(solo.getJSON());
    expect(live.getHTML()).toBe(solo.getHTML());
  });

  it("saves the Markdown the solo editor saves", () => {
    const solo = openSolo(markdown);
    const { editor: live, server } = openLive(markdown);
    const codec = createDocumentCodec({ cdnDomain: CDN });
    expect(codec.ydocToMarkdown(server)).toBe(solo.getMarkdown().trim());
    expect(live.getMarkdown().trim()).toBe(solo.getMarkdown().trim());
  });

  it("is stable when the saved Markdown is loaded again", () => {
    const codec = createDocumentCodec({ cdnDomain: CDN });
    const saved = codec.ydocToMarkdown(codec.createYDoc(markdown));
    expect(codec.ydocToMarkdown(codec.createYDoc(saved))).toBe(saved);
  });
});

describe("formats made in the browser", () => {
  it("reach the other editor and the saved Markdown unchanged", () => {
    const codec = createDocumentCodec({ cdnDomain: CDN });
    const server = codec.createYDoc("Start");
    const ada = new Y.Doc();
    const bob = new Y.Doc();
    for (const doc of [ada, bob]) Y.applyUpdate(doc, Y.encodeStateAsUpdate(server));
    // Relay every update the way the collaboration server does.
    const peers = [server, ada, bob];
    for (const doc of peers) {
      doc.on("update", (update: Uint8Array, origin: unknown) => {
        if (origin === "relay") return;
        for (const peer of peers) if (peer !== doc) Y.applyUpdate(peer, update, "relay");
      });
    }

    const adaEditor = mountEditor({ document: ada });
    const bobEditor = mountEditor({ document: bob });
    adaEditor.commands.setContent(preprocessMarkdown(ALL_FORMATS, { cdnDomain: CDN }), {
      contentType: "markdown",
    });

    expect(bobEditor.getJSON()).toEqual(adaEditor.getJSON());
    expect(bobEditor.getHTML()).toBe(adaEditor.getHTML());
    expect(codec.ydocToMarkdown(server)).toBe(adaEditor.getMarkdown().trim());
    expect(codec.ydocToMarkdown(server)).toBe(openSolo(ALL_FORMATS).getMarkdown().trim());
  });
});
