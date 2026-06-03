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

  // Boundary regressions (Emacs review, PR #3661).

  it("does not let a == inside inline code close the highlight", () => {
    // The inner `==` lives in inline code; the highlight must wrap the whole
    // span, not stop at the code's `==`.
    expect(highlightToHtml("==a `b==c` d==")).toBe(
      "<mark>a `b==c` d</mark>",
    );
  });

  it("does not let a == inside inline math close the highlight", () => {
    expect(highlightToHtml("==a $b==c$ d==")).toBe(
      "<mark>a $b==c$ d</mark>",
    );
  });

  it("does not highlight across a blank line / block boundary", () => {
    expect(highlightToHtml("==a\n\nb==")).toBe("==a\n\nb==");
  });

  it("still highlights across a soft line break within a block", () => {
    expect(highlightToHtml("==a\nb==")).toBe("<mark>a\nb</mark>");
  });

  it("does not highlight across a CRLF blank line", () => {
    expect(highlightToHtml("==a\r\n\r\nb==")).toBe("==a\r\n\r\nb==");
  });

  it("still highlights across a CRLF soft line break", () => {
    expect(highlightToHtml("==a\r\nb==")).toBe("<mark>a\r\nb</mark>");
  });
});
