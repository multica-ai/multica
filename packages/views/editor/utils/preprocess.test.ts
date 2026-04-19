import { describe, expect, it, vi } from "vitest";

vi.mock("@multica/core/config", () => ({
  configStore: { getState: () => ({ cdnDomain: "" }) },
}));

import { preprocessMarkdown } from "./preprocess";

describe("preprocessMarkdown — blockquote rendering (issue #1303)", () => {
  it("decodes line-leading &gt; back to > so blockquotes render", () => {
    // @tiptap/markdown encodes > as &gt; when serializing paragraph text.
    // remark/marked then sees the entity instead of the blockquote marker,
    // which previously rendered as a literal '>'.
    const input = "Some context:\n\n&gt; quoted line\n\nAfter quote.";
    const out = preprocessMarkdown(input);
    expect(out).toContain("\n> quoted line\n");
    expect(out).not.toContain("&gt; quoted line");
  });

  it("preserves inline &gt; in prose (only line-leading is decoded)", () => {
    const input = "Compare 2 &gt; 1 in the middle of a line.";
    const out = preprocessMarkdown(input);
    expect(out).toContain("2 &gt; 1");
  });

  it("decodes leading &gt; even with leading whitespace", () => {
    const input = "  &gt; indented quote";
    const out = preprocessMarkdown(input);
    expect(out.startsWith("  > ")).toBe(true);
  });

  it("handles multiple consecutive blockquote lines", () => {
    const input = "&gt; line one\n&gt; line two\n&gt; line three";
    const out = preprocessMarkdown(input);
    expect(out).toBe("> line one\n> line two\n> line three");
  });

  it("leaves already-literal > blockquotes untouched", () => {
    const input = "> normal blockquote\n\ntext";
    const out = preprocessMarkdown(input);
    expect(out).toContain("> normal blockquote");
  });
});
