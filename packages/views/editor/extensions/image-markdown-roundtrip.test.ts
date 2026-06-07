import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { ImageExtension } from "./index";

const IMAGE_URL = "https://cdn.example.com/screen.png";
const IMAGE_MD = `![screen](${IMAGE_URL})`;

let editors: Editor[] = [];

function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: [
      StarterKit,
      ImageExtension,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
  editors.push(editor);
  return editor;
}

function roundTripMany(input: string, rounds: number) {
  const editor = makeEditor();
  const outputs: string[] = [];
  let markdown = input;

  for (let i = 0; i < rounds; i++) {
    editor.commands.setContent(markdown, { contentType: "markdown" });
    markdown = editor.getMarkdown().trimEnd();
    outputs.push(markdown);
  }

  return outputs;
}

function findParagraphTexts(editor: Editor) {
  return Array.from(editor.view.dom.querySelectorAll("p")).map(
    (p) => p.textContent ?? "",
  );
}

afterEach(() => {
  for (const editor of editors) editor.destroy();
  editors = [];
  document.body.innerHTML = "";
});

describe("ImageExtension markdown round-trip", () => {
  it("does not accumulate blank paragraphs around an internal image", () => {
    const input = ["before", "", IMAGE_MD, "", "after"].join("\n");
    const outputs = roundTripMany(input, 5);

    expect(outputs).toEqual([input, input, input, input, input]);
  });

  it("does not reparse a live image followed by text into an empty paragraph", () => {
    const editor = makeEditor();
    editor.commands.setContent({
      type: "doc",
      content: [
        {
          type: "image",
          attrs: { src: IMAGE_URL, alt: "screen" },
        },
        {
          type: "paragraph",
          content: [{ type: "text", text: "after" }],
        },
      ],
    });

    const emitted = editor.getMarkdown().trimEnd();
    expect(emitted).toBe([IMAGE_MD, "", "after"].join("\n"));

    const reparsed = makeEditor();
    reparsed.commands.setContent(emitted, { contentType: "markdown" });

    expect(reparsed.getHTML()).toBe(
      `<img src="${IMAGE_URL}" alt="screen"><p>after</p>`,
    );
    expect(findParagraphTexts(reparsed)).toEqual(["after"]);
  });
});
