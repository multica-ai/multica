import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import TableCell from "@tiptap/extension-table-cell";
import TableHeader from "@tiptap/extension-table-header";
import TableRow from "@tiptap/extension-table-row";
import { TableWithLineBreaks } from "./table";

let editor: Editor;

afterEach(() => editor?.destroy());

describe("table line breaks", () => {
  it.each(["paragraph", "hardBreak"])(
    "preserves %s breaks across repeated saves",
    (kind) => {
      editor = new Editor({
        extensions: [
          StarterKit,
          TableWithLineBreaks,
          TableRow,
          TableHeader,
          TableCell,
          Markdown,
        ],
      });
      const first = { type: "text", text: "First" };
      const second = { type: "text", text: "Second" };
      const content =
        kind === "paragraph"
          ? [
              { type: "paragraph", content: [first] },
              { type: "paragraph", content: [second] },
            ]
          : [
              {
                type: "paragraph",
                content: [first, { type: "hardBreak" }, second],
              },
            ];

      editor.commands.setContent({
        type: "doc",
        content: [
          {
            type: "table",
            content: [
              {
                type: "tableRow",
                content: [
                  {
                    type: "tableHeader",
                    content: [
                      {
                        type: "paragraph",
                        content: [{ type: "text", text: "Header" }],
                      },
                    ],
                  },
                ],
              },
              {
                type: "tableRow",
                content: [{ type: "tableCell", content }],
              },
            ],
          },
        ],
      });

      for (let i = 0; i < 3; i++) {
        const markdown = editor.getMarkdown();
        expect(markdown).toContain("<br>");
        expect(markdown).not.toContain("\u001f");
        editor.commands.setContent(markdown, { contentType: "markdown" });
        expect(editor.getHTML()).toContain("<br>");
        expect(editor.getText()).toContain("First");
        expect(editor.getText()).toContain("Second");
      }
    },
  );
});
