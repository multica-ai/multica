import { describe, expect, it } from "vitest";
import { preprocessLinks } from "@multica/ui/markdown/linkify";

describe("findCodeRanges — all code block types", () => {
  it("does NOT linkify URLs inside backtick-fenced code blocks", () => {
    const input = "```bash\nvisit https://www.wujieai.com\n```";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("does NOT linkify URLs inside tilde-fenced code blocks", () => {
    const input = "~~~bash\nvisit https://www.wujieai.com\n~~~";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("does NOT linkify URLs inside indented code blocks (4-space indent)", () => {
    const input = "    visit https://www.wujieai.com\n    more code";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("does NOT linkify URLs inside indented code blocks (tab indent)", () => {
    const input = "\tvisit https://www.wujieai.com\n\tmore code";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("does NOT linkify URLs inside tilde fence with language tag", () => {
    const input = "~~~python\nurl = 'https://www.wujieai.com'\n~~~";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("correctly linkifies URLs OUTSIDE code blocks", () => {
    const input = "Visit https://www.wujieai.com now";
    expect(preprocessLinks(input)).toBe("Visit [https://www.wujieai.com](https://www.wujieai.com) now");
  });

  it("handles backtick code block followed by URL in text", () => {
    const input = "```\ncode\n```\nSee https://example.com";
    const result = preprocessLinks(input);
    expect(result).toContain("```\ncode\n```");
    expect(result).toContain("[https://example.com](https://example.com)");
  });

  it("handles tilde code block followed by URL in text", () => {
    const input = "~~~\ncode\n~~~\nSee https://example.com";
    const result = preprocessLinks(input);
    expect(result).toContain("~~~\ncode\n~~~");
    expect(result).toContain("[https://example.com](https://example.com)");
  });

  it("handles @ and # inside backtick-fenced code blocks", () => {
    const input = "```text\n@someone did #123 at https://example.com\n```";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("handles inline code with URL", () => {
    const input = "Use `curl https://example.com/api` to test";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("handles multiline indented code block with blank line gap", () => {
    const input = "    line1 https://example.com/a\n\n    line2 https://example.com/b";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("handles mixed backtick and tilde fences", () => {
    const input = "```bash\nurl1 https://a.com\n```\n~~~\nurl2 https://b.com\n~~~";
    expect(preprocessLinks(input)).toBe(input);
  });
});
