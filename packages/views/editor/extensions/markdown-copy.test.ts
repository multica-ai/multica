import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { NodeSelection } from "@tiptap/pm/state";
import { serializeMarkdownCopyText } from "./markdown-copy";

function makeEditor(content: object) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ codeBlock: {} }),
      Markdown,
    ],
    content,
  });
}

function serializeSelection(editor: Editor): string {
  return serializeMarkdownCopyText(editor.state.selection.content(), {
    markdown: editor.markdown,
    schema: editor.schema,
  });
}

describe("markdownCopy", () => {
  let editor: Editor | null = null;

  afterEach(() => {
    editor?.destroy();
    editor = null;
    document.body.innerHTML = "";
  });

  it("copies a manual partial selection inside a code block as plain visible text", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "codeBlock",
          attrs: { language: "bash" },
          content: [
            {
              type: "text",
              text: [
                "line one: echo alpha",
                "line two: printf '中文 beta --symbols []'",
                'line three: curl -H "X-Test: yes" https://example.invalid/api',
              ].join("\n"),
            },
          ],
        },
      ],
    });

    const fullText = editor.state.doc.textBetween(0, editor.state.doc.content.size, "\n");
    const selectedText = [
      "line two: printf '中文 beta --symbols []'",
      'line three: curl -H "X-Test: yes" https://example.invalid/api',
    ].join("\n");
    const from = fullText.indexOf(selectedText) + 1;
    const to = from + selectedText.length;
    editor.commands.setTextSelection({ from, to });

    const slice = editor.state.selection.content();
    expect(slice.openStart).toBeGreaterThan(0);
    expect(slice.content.firstChild?.type.name).toBe("codeBlock");
    expect(serializeSelection(editor)).toBe(selectedText);
  });

  it("still serializes a whole selected code block as fenced Markdown", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "codeBlock",
          attrs: { language: "bash" },
          content: [{ type: "text", text: "pnpm test\npnpm build" }],
        },
      ],
    });

    editor.view.dispatch(
      editor.state.tr.setSelection(NodeSelection.create(editor.state.doc, 0)),
    );

    const slice = editor.state.selection.content();
    expect(slice.openStart).toBe(0);
    expect(slice.content.firstChild?.type.name).toBe("codeBlock");
    expect(serializeSelection(editor)).toBe("```bash\npnpm test\npnpm build\n```");
  });

  it("continues to serialize rich non-code selections as Markdown", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        {
          type: "heading",
          attrs: { level: 2 },
          content: [{ type: "text", text: "Heading" }],
        },
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                {
                  type: "paragraph",
                  content: [{ type: "text", text: "item one" }],
                },
              ],
            },
          ],
        },
      ],
    });

    editor.commands.setTextSelection({
      from: 0,
      to: editor.state.doc.content.size,
    });

    expect(serializeSelection(editor)).toBe("## Heading\n\n- item one");
  });
});
