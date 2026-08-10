import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { ImageCaption } from "@multica/cerebro-editor-image";
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
      ImageCaption,
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

describe("ImageExtension inline resize round-trip (Phase 5)", () => {
  const SIZED = `<img src="${IMAGE_URL}" alt="screen" data-width-pct="55" data-align="center">`;

  it("round-trips a resized + aligned image through the HTML form", () => {
    const [out] = roundTripMany(SIZED, 1);
    expect(out).toContain('data-width-pct="55"');
    expect(out).toContain('data-align="center"');
    expect(out).not.toContain("![");
  });

  it("round-trips width alone and alignment alone", () => {
    const [wOnly] = roundTripMany(
      `<img src="${IMAGE_URL}" alt="screen" data-width-pct="50">`,
      1,
    );
    expect(wOnly).toContain('data-width-pct="50"');
    expect(wOnly).not.toContain("data-align");

    const [aOnly] = roundTripMany(
      `<img src="${IMAGE_URL}" alt="screen" data-align="right">`,
      1,
    );
    expect(aOnly).toContain('data-align="right"');
    expect(aOnly).not.toContain("data-width-pct");
  });

  it("keeps a sized image stable across repeated round-trips", () => {
    const outputs = roundTripMany(SIZED, 4);
    expect(new Set(outputs).size).toBe(1);
  });

  it("leaves an untouched image byte-identical to ![alt](url)", () => {
    const out = roundTripMany(IMAGE_MD, 1)[0] ?? "";
    expect(out.trim()).toBe(IMAGE_MD);
    expect(out).not.toContain("<img");
    expect(out).not.toContain("data-width-pct");
  });

  it("keeps explicit inline placement without forcing a size or alignment", () => {
    const placed = `<img src="${IMAGE_URL}" alt="screen" data-placement="inline">`;
    const [out] = roundTripMany(placed, 2);
    expect(out).toContain('data-placement="inline"');
    expect(out).not.toContain("data-width-pct");
    expect(out).not.toContain("data-align");
  });
});

describe("ImageCaption markdown round-trip (Phase 5)", () => {
  const CAPTION_MD = `<figcaption>A caption</figcaption>`;

  it("round-trips a caption as a <figcaption> block", () => {
    const [out] = roundTripMany(CAPTION_MD, 1);
    expect(out?.trim()).toBe(CAPTION_MD);
  });

  it("keeps a captioned image stable across repeated round-trips", () => {
    const captioned = [
      `<img src="${IMAGE_URL}" alt="screen" data-width-pct="55" data-align="center">`,
      "",
      CAPTION_MD,
    ].join("\n");
    const outputs = roundTripMany(captioned, 4);
    expect(new Set(outputs).size).toBe(1);
    expect(outputs[0]).toContain('data-width-pct="55"');
    expect(outputs[0]).toContain(CAPTION_MD);
  });

  it("leaves an image with no caption free of any <figcaption>", () => {
    const out = roundTripMany(IMAGE_MD, 1)[0] ?? "";
    expect(out).not.toContain("figcaption");
  });
});
