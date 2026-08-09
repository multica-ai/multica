import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { NodeSelection } from "@tiptap/pm/state";
import { ImageExtension } from "./index";

const IMAGE_URL = "https://cdn.example.com/screen.png";

let editors: Editor[] = [];

function makeEditorWithSelectedImage() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: [StarterKit, ImageExtension, Markdown],
  });
  editors.push(editor);
  editor.commands.setContent({
    type: "doc",
    content: [{ type: "image", attrs: { src: IMAGE_URL, alt: "screen" } }],
  });
  // Select the image node so the commands act on it.
  const { state, view } = editor;
  view.dispatch(state.tr.setSelection(NodeSelection.create(state.doc, 0)));
  return editor;
}

afterEach(() => {
  for (const editor of editors) editor.destroy();
  editors = [];
  document.body.innerHTML = "";
});

describe("image placement commands", () => {
  it("setImageWidthPct sizes the selected image and serialises it", () => {
    const editor = makeEditorWithSelectedImage();
    expect(editor.commands.setImageWidthPct(50)).toBe(true);
    expect(editor.getMarkdown().trimEnd()).toContain('data-width-pct="50"');
  });

  it("setImageAlign aligns the selected image and serialises it", () => {
    const editor = makeEditorWithSelectedImage();
    expect(editor.commands.setImageAlign("center")).toBe(true);
    expect(editor.getMarkdown().trimEnd()).toContain('data-align="center"');
  });

  it("clearing width and align returns to plain ![alt](url)", () => {
    const editor = makeEditorWithSelectedImage();
    editor.commands.setImageWidthPct(50);
    editor.commands.setImageAlign("center");
    editor.commands.setImageWidthPct(null);
    editor.commands.setImageAlign(null);
    expect(editor.getMarkdown().trimEnd()).toBe(`![screen](${IMAGE_URL})`);
  });
});
