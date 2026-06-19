import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { BlockMathExtension, InlineMathExtension } from "./math";

const FINANCE_TEXT =
  "Revenue increased from $100~$120 in Q1 to $140~$160 in Q2.";

interface JsonNode {
  type?: string;
  attrs?: Record<string, unknown>;
  content?: JsonNode[];
}

function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit,
      BlockMathExtension,
      InlineMathExtension,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

function findAll(node: JsonNode, type: string, acc: JsonNode[] = []): JsonNode[] {
  if (node.type === type) acc.push(node);
  for (const child of node.content ?? []) findAll(child, type, acc);
  return acc;
}

function typeText(editor: Editor, text: string) {
  for (const ch of text) {
    const { from, to } = editor.state.selection;
    const handled = editor.view.someProp("handleTextInput", (handler) =>
      handler(editor.view, from, to, ch, () => editor.state.tr),
    );
    if (!handled) editor.view.dispatch(editor.state.tr.insertText(ch, from, to));
  }
}

let editor: Editor | null = null;

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

describe("math editor extension", () => {
  it("keeps typed single-dollar amounts as literal text", () => {
    editor = makeEditor();

    typeText(editor, FINANCE_TEXT);

    expect(findAll(editor.getJSON() as JsonNode, "inlineMath")).toHaveLength(0);
    expect(editor.getText()).toBe(FINANCE_TEXT);
    expect(editor.getMarkdown().trim()).toBe(FINANCE_TEXT);
  });

  it("parses single-dollar markdown as literal text", () => {
    editor = makeEditor();

    editor.commands.setContent(FINANCE_TEXT, { contentType: "markdown" });

    expect(findAll(editor.getJSON() as JsonNode, "inlineMath")).toHaveLength(0);
    expect(editor.getText()).toBe(FINANCE_TEXT);
    expect(editor.getMarkdown().trim()).toBe(FINANCE_TEXT);
  });

  it("still parses explicit display math blocks", () => {
    editor = makeEditor();
    const markdown = "$$\nx^2 + y^2 = z^2\n$$";

    editor.commands.setContent(markdown, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "blockMath");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.attrs?.expression).toBe("x^2 + y^2 = z^2");
    expect(editor.getMarkdown().trim()).toBe(markdown);
  });
});
