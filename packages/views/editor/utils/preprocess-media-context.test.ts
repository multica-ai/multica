import { describe, expect, it } from "vitest";
import { MarkdownManager } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";
import { fromMarkdown } from "mdast-util-from-markdown";
import { preprocessMarkdown } from "./preprocess";

// This is the engine's attributed source shape: fenced code inside a blockquote keeps
// file-card preprocessing from treating historical text as reporter uploads.
describe("historical media source projection", () => {
  for (const source of [
    "![external](https://example.invalid/source.png)",
    "!file[source](/api/attachments/22222222-2222-4222-8222-222222222222/download)",
    "[source](https://bucket.s3.amazonaws.com/source.pdf)",
  ]) {
    it(`keeps ${source} literal through the actual editor preprocessing`, () => {
      const markdown = `Referenced channel context:\n\n> \`\`\`\n> <quoted_message sender="Alice">\n> [Image 1: 22222222-2222-4222-8222-222222222222]\n> ${source}\n> </quoted_message>\n> \`\`\``;
      const processed = preprocessMarkdown(markdown, { cdnDomain: "" });
      const tree = fromMarkdown(processed);
      expect(processed).not.toContain('data-type="fileCard"');
      expect(tree.children.map((node) => node.type)).toEqual(["paragraph", "blockquote"]);
      const quote = tree.children[1];
      expect(quote?.type).toBe("blockquote");
      if (quote?.type !== "blockquote") throw new Error("missing literal quote");
      expect(quote.children).toHaveLength(1);
      expect(quote.children[0]?.type).toBe("code");
      expect(quote.children[0]).toMatchObject({ value: expect.stringContaining(source) });
      const manager = new MarkdownManager({ extensions: [StarterKit] });
      expect(manager.serialize(manager.parse(processed))).toBe(markdown);
    });
  }
});
