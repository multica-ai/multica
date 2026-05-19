import { describe, it, expect, afterEach, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { createMarkdownPasteExtension } from "./markdown-paste";

interface FakeClipboard {
  files: never[];
  items?: Array<{ kind: string; getAsFile: () => File | null }>;
  getData: (type: string) => string;
}

function fakePasteEvent(
  text: string,
  html?: string,
  options?: {
    items?: Array<{ kind: string; getAsFile: () => File | null }>;
  },
) {
  const data: FakeClipboard = {
    files: [],
    items: options?.items,
    getData: (type) =>
      type === "text/plain" ? text : type === "text/html" ? (html ?? "") : "",
  };
  return {
    clipboardData: data,
    preventDefault: () => {},
  } as unknown as ClipboardEvent;
}

function makeEditor(content: object) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [StarterKit, Markdown, createMarkdownPasteExtension()],
    content,
  });
}

function paste(editor: Editor, text: string, html?: string): boolean {
  const event = fakePasteEvent(text, html);
  return (
    editor.view.someProp("handlePaste", (handler) =>
      handler(editor.view, event, editor.view.state.selection.content()),
    ) === true
  );
}

interface JsonNode {
  type: string;
  text?: string;
  content?: JsonNode[];
}

function findFirst(json: JsonNode, type: string): JsonNode | undefined {
  if (json.type === type) return json;
  for (const child of json.content ?? []) {
    const hit = findFirst(child, type);
    if (hit) return hit;
  }
  return undefined;
}

function nodeText(node: JsonNode): string {
  if (node.text !== undefined) return node.text;
  return (node.content ?? []).map(nodeText).join("");
}

function expectLiteralPaste(editor: Editor, text: string) {
  editor.commands.setTextSelection(1);
  const parseSpy = vi.spyOn(editor.markdown!, "parse");

  const handled = paste(editor, text);

  expect(handled).toBe(true);
  expect(parseSpy).not.toHaveBeenCalled();
  expect(editor.getText()).toBe(text);
  expect(editor.getMarkdown()).toBe(text);
}

describe("markdownPaste — code block context", () => {
  let editor: Editor | null = null;

  afterEach(() => {
    vi.restoreAllMocks();
    editor?.destroy();
    editor = null;
    document.body.innerHTML = "";
  });

  it("preserves blank lines when pasting into a code block (#1982)", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "codeBlock", content: [{ type: "text", text: "x" }] }],
    });

    // Place caret after "x" inside the code block.
    editor.commands.setTextSelection(2);
    expect(editor.state.selection.$from.parent.type.name).toBe("codeBlock");

    const handled = paste(editor, "line1\n\nline2");
    expect(handled).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const codeBlock = findFirst(json, "codeBlock");
    expect(codeBlock).toBeDefined();
    // Code block content is preserved verbatim — blank line stays inside.
    expect(nodeText(codeBlock!)).toBe("xline1\n\nline2");
    // No paragraph leaked out carrying any of the pasted text.
    const leakedParagraph = (json.content ?? []).find(
      (n) => n.type === "paragraph" && nodeText(n).length > 0,
    );
    expect(leakedParagraph).toBeUndefined();
  });

  it("preserves fence characters pasted into a code block", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "codeBlock", content: [] }],
    });

    editor.commands.setTextSelection(1);
    expect(editor.state.selection.$from.parent.type.name).toBe("codeBlock");

    paste(editor, "```\nhello\n```");

    const json = editor.getJSON() as JsonNode;
    const codeBlock = findFirst(json, "codeBlock");
    expect(codeBlock).toBeDefined();
    expect(nodeText(codeBlock!)).toBe("```\nhello\n```");
  });

  it("still parses Markdown when pasting into a regular paragraph", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    editor.commands.setTextSelection(1);
    expect(editor.state.selection.$from.parent.type.name).toBe("paragraph");

    paste(editor, "# Heading\n\nbody");

    const json = editor.getJSON() as JsonNode;
    const types = (json.content ?? []).map((n) => n.type);
    // Markdown parsing produced a heading at the top.
    expect(types).toContain("heading");
  });

  it("inserts JSON clipboard text without running the Markdown parser", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    const json = JSON.stringify(
      {
        type: "issue.comment",
        payload: {
          title: "Paste JSON into a reply",
          nested: { ok: true, count: 3 },
          items: ["alpha", "beta", "gamma"],
        },
      },
      null,
      2,
    );

    expectLiteralPaste(editor, json);
  });

  it("inserts very large plain text without running the Markdown parser", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    const text = Array.from(
      { length: 1600 },
      (_, index) => `log ${index}: ${"payload".repeat(6)}`,
    ).join("\n");
    expect(text.length).toBeGreaterThan(50_000);

    expectLiteralPaste(editor, text);
  });

  it("inserts medium single-line SQL-like text without running the Markdown parser", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    const columns = Array.from(
      { length: 120 },
      (_, index) => `\`column_${index}\``,
    ).join(", ");
    const values = Array.from(
      { length: 120 },
      (_, index) => `'value_${index}_${"x".repeat(12)}'`,
    ).join(", ");
    const sql = `INSERT INTO \`huge_table\` (${columns}) VALUES (${values});`;

    expect(sql.length).toBeGreaterThan(2_000);
    expect(sql.split("\n")).toHaveLength(1);

    expectLiteralPaste(editor, sql);
  });

  it("still parses medium-length Markdown with explicit block syntax", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    const markdown = [
      "# Incident report",
      "",
      ...Array.from(
        { length: 40 },
        (_, index) => `- item ${index}: keep **Markdown** parsing enabled for structured notes`,
      ),
      "",
      ...Array.from(
        { length: 12 },
        () => "Regular wrapped prose keeps line lengths modest while total content stays large.",
      ),
    ].join("\n");

    expect(markdown.length).toBeGreaterThan(2_000);

    const handled = paste(editor, markdown);
    expect(handled).toBe(true);

    const json = editor.getJSON() as JsonNode;
    const types = (json.content ?? []).map((n) => n.type);
    expect(types).toContain("heading");
    expect(types).toContain("bulletList");
  });

  it("does not parse oversized bracketed plain text as JSON", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    const parseJsonSpy = vi.spyOn(JSON, "parse");
    const text = `{${"not-json".repeat(7_000)}}`;
    expect(text.length).toBeGreaterThan(50_000);

    expectLiteralPaste(editor, text);
    expect(parseJsonSpy).not.toHaveBeenCalled();
  });

  it("defers to file upload when clipboard items contain an image file", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    const screenshot = new File(["x"], "paste.png", { type: "image/png" });
    const event = fakePasteEvent("", "", {
      items: [{ kind: "file", getAsFile: () => screenshot }],
    });
    const currentEditor = editor;

    const handled =
      currentEditor.view.someProp("handlePaste", (handler) =>
        handler(currentEditor.view, event, currentEditor.view.state.selection.content()),
      ) === true;

    // markdownPaste should return false (defers to fileUpload which is not loaded in this test)
    expect(handled).toBe(false);
  });
});
