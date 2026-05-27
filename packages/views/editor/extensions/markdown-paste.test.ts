import { describe, it, expect, afterEach, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import {
  createMarkdownPasteExtension,
  escapeNonStandardHtmlTags,
} from "./markdown-paste";

interface FakeClipboard {
  files: never[];
  getData: (type: string) => string;
}

function fakePasteEvent(text: string, html?: string) {
  const data: FakeClipboard = {
    files: [],
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

  it("preserves single unknown HTML-like tag (e.g. <T>)", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    editor.commands.setTextSelection(1);

    const handled = paste(editor, "<T>");
    expect(handled).toBe(true);
    expect(editor.getText()).toBe("<T>");
  });

  it("preserves any unknown HTML-like tag (e.g. <MyComponent>)", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    editor.commands.setTextSelection(1);

    const handled = paste(editor, "<MyComponent>");
    expect(handled).toBe(true);
    expect(editor.getText()).toBe("<MyComponent>");
  });

  it("preserves unknown tags in mixed multi-line content", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph" }],
    });

    editor.commands.setTextSelection(1);

    const text = "<t>\n\n裸 `<tag>` 做转\n\n<tag>\n\n<t>";
    const handled = paste(editor, text);
    expect(handled).toBe(true);

    const editorText = editor.getText();
    expect(editorText).toContain("<t>");
    expect(editorText).toContain("<tag>");
    expect(editorText).toContain("裸");
    expect(editorText).toContain("做转");
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
});

describe("escapeNonStandardHtmlTags", () => {
  it("escapes unknown tags", () => {
    expect(escapeNonStandardHtmlTags("<T>")).toBe("&lt;T&gt;");
    expect(escapeNonStandardHtmlTags("<tag>")).toBe("&lt;tag&gt;");
    expect(escapeNonStandardHtmlTags("<MyComponent>")).toBe(
      "&lt;MyComponent&gt;",
    );
    expect(escapeNonStandardHtmlTags("</tag>")).toBe("&lt;/tag&gt;");
  });

  it("preserves standard HTML tags", () => {
    expect(escapeNonStandardHtmlTags("<div>")).toBe("<div>");
    expect(escapeNonStandardHtmlTags("<br>")).toBe("<br>");
    expect(escapeNonStandardHtmlTags("</div>")).toBe("</div>");
    expect(escapeNonStandardHtmlTags('<img src="x">')).toBe('<img src="x">');
  });

  it("does not escape inside inline code spans", () => {
    expect(escapeNonStandardHtmlTags("`<tag>`")).toBe("`<tag>`");
    expect(escapeNonStandardHtmlTags("text `<T>` more")).toBe(
      "text `<T>` more",
    );
    expect(escapeNonStandardHtmlTags("``<tag>``")).toBe("``<tag>``");
  });

  it("does not escape inside fenced code blocks", () => {
    expect(escapeNonStandardHtmlTags("```\n<T>\n```")).toBe("```\n<T>\n```");
    expect(escapeNonStandardHtmlTags("~~~\n<tag>\n~~~")).toBe(
      "~~~\n<tag>\n~~~",
    );
  });

  it("escapes unknown tags while preserving standard tags in mixed content", () => {
    expect(escapeNonStandardHtmlTags("<T> and <div>")).toBe(
      "&lt;T&gt; and <div>",
    );
  });

  it("handles multi-line mixed content", () => {
    const input = "<t>\n\n裸 `<tag>` 做转\n\n<tag>\n\n<t>";
    const result = escapeNonStandardHtmlTags(input);
    expect(result).toBe(
      "&lt;t&gt;\n\n裸 `<tag>` 做转\n\n&lt;tag&gt;\n\n&lt;t&gt;",
    );
  });

  it("does not touch math expressions", () => {
    expect(escapeNonStandardHtmlTags("1 < 2 > 0")).toBe("1 < 2 > 0");
    expect(escapeNonStandardHtmlTags("x<y")).toBe("x<y");
  });
});
