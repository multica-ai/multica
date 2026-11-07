import { describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from "../extensions";
import { preprocessMarkdown } from "./preprocess";
import { highlightToHtml } from "./highlight-markdown";
import { preprocessLinks } from "@multica/ui/markdown/linkify";
import { preprocessIssueIdentifiers } from "@multica/ui/markdown";

describe("code boundaries in editor and read-only preprocessing", () => {
  it("keeps tab-indented list code literal through editor preprocessing", () => {
    const source =
      "Intro\n\n- ```\n\thttps://example.com ==keep==\n\t```\n\nOutro";
    const editor = new Editor({
      extensions: createEditorExtensions({ disableMentions: true }),
      content: source,
      contentType: "markdown",
    });
    try {
      const raw = editor.getJSON();
      expect(JSON.stringify(raw)).toContain('"type":"codeBlock"');
      expect(editor.getText()).toContain("https://example.com ==keep==");
      const processed = preprocessMarkdown(source, { cdnDomain: "" });
      editor.commands.setContent(processed, { contentType: "markdown" });
      expect.soft(editor.getJSON()).toEqual(raw);
      expect
        .soft(editor.getMarkdown())
        .not.toContain("[https://example.com](https://example.com)");
      expect.soft(highlightToHtml(source)).toBe(source);
      expect.soft(preprocessLinks(source)).toBe(source);
    } finally {
      editor.destroy();
    }
  });

  it("ends an unclosed fence opened on a list continuation line", () => {
    const source = "- item\n\n  ```\n  code\n\n==outside== MUL-123 ./foo.ts";
    expect.soft(preprocessLinks(source)).toContain("[./foo.ts](./foo.ts)");
    expect.soft(highlightToHtml(source)).toContain("<mark>outside</mark>");
    expect
      .soft(preprocessIssueIdentifiers(source))
      .toContain("[MUL-123](mention://issue/MUL-123)");
  });
});
