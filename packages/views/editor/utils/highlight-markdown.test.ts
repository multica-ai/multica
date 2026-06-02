import { describe, it, expect } from "vitest";
import { highlightToHtml } from "./highlight-markdown";

describe("highlightToHtml", () => {
  it("lowers a basic highlight to <mark>", () => {
    expect(highlightToHtml("a ==hi== b")).toBe("a <mark>hi</mark> b");
  });

  it("keeps inner markdown intact for nested formatting", () => {
    expect(highlightToHtml("==**bold**==")).toBe("<mark>**bold**</mark>");
  });

  it("handles multiple highlights on one line", () => {
    expect(highlightToHtml("==a== and ==b==")).toBe(
      "<mark>a</mark> and <mark>b</mark>",
    );
  });

  it("requires non-space directly inside the fences", () => {
    expect(highlightToHtml("== spaced ==")).toBe("== spaced ==");
  });

  it("does not match empty fences", () => {
    expect(highlightToHtml("====")).toBe("====");
  });

  it("is a no-op when there is no ==", () => {
    const md = "plain **bold** _italic_ text";
    expect(highlightToHtml(md)).toBe(md);
  });

  it("ignores == inside inline code", () => {
    expect(highlightToHtml("`a ==b== c`")).toBe("`a ==b== c`");
  });

  it("ignores == inside fenced code blocks", () => {
    const md = "```\nx ==y== z\n```";
    expect(highlightToHtml(md)).toBe(md);
  });

  it("ignores == inside inline math", () => {
    expect(highlightToHtml("$a ==b== c$")).toBe("$a ==b== c$");
  });

  it("highlights outside code while leaving code untouched", () => {
    expect(highlightToHtml("==hi== `x ==y==`")).toBe(
      "<mark>hi</mark> `x ==y==`",
    );
  });

  it("does not treat a == b (comparison) as a highlight", () => {
    expect(highlightToHtml("if a == b then")).toBe("if a == b then");
  });
});
