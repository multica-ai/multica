import { describe, expect, it } from "vitest";
import type { MarkdownAnnotationDraft } from "./markdown-annotation-types";
import { formatMarkdownAnnotationsComment, truncateQuote } from "./markdown-annotation-comment";

function annotation(overrides: Partial<MarkdownAnnotationDraft>): MarkdownAnnotationDraft {
  return {
    id: "a1",
    attachmentId: "att-1",
    filename: "README.md",
    quote: "selected text",
    note: "Needs more detail",
    createdAt: 1,
    range: {
      start: { line: 1, character: 1, offset: 0 },
      end: { line: 1, character: 13, offset: 12 },
    },
    ...overrides,
  };
}

describe("markdown annotation comment formatting", () => {
  it("formats a single annotation with filename, range, quote, and note", () => {
    const body = formatMarkdownAnnotationsComment("README.md", [annotation({})]);
    expect(body).toContain("Markdown 批注：README.md");
    expect(body).toContain("`README.md:L1:C1-L1:C13`");
    expect(body).toContain("> selected text");
    expect(body).toContain("备注：Needs more detail");
  });

  it("sorts annotations by source range before formatting", () => {
    const body = formatMarkdownAnnotationsComment("README.md", [
      annotation({
        id: "late",
        createdAt: 1,
        quote: "late",
        range: {
          start: { line: 4, character: 1, offset: 40 },
          end: { line: 4, character: 4, offset: 43 },
        },
      }),
      annotation({
        id: "early",
        createdAt: 2,
        quote: "early",
        range: {
          start: { line: 2, character: 1, offset: 10 },
          end: { line: 2, character: 5, offset: 14 },
        },
      }),
    ]);

    expect(body.indexOf("early")).toBeLessThan(body.indexOf("late"));
  });

  it("renders multi-line quotes as markdown blockquotes", () => {
    const body = formatMarkdownAnnotationsComment("README.md", [
      annotation({ quote: "first line\nsecond line" }),
    ]);
    expect(body).toContain("> first line\n   > second line");
  });

  it("truncates long quotes without splitting by code unit count", () => {
    expect(truncateQuote("🙂".repeat(8), 5)).toBe("🙂🙂…🙂🙂");
  });
});
