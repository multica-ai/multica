import { describe, expect, it } from "vitest";
import { preprocessLinks } from "@multica/ui/markdown/linkify";

describe("preprocessLinks — code blocks with URLs/@/#", () => {
  it("should NOT linkify URLs inside fenced code blocks", () => {
    const input = "```\nvisit https://www.wujieai.com for details\n```";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("should NOT linkify URLs inside fenced code blocks with language tag", () => {
    const input = "```bash\nvisit https://www.wujieai.com for details\n```";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("should NOT linkify URLs in code block with @ symbols", () => {
    const input = "```text\n@someone said #123 at https://example.com\n```";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("should NOT linkify @-prefixed text inside code blocks", () => {
    const input = "```\n@agent hello world\n```";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("should linkify URLs OUTSIDE code blocks", () => {
    const input = "Visit https://www.wujieai.com for details";
    expect(preprocessLinks(input)).toBe("Visit [https://www.wujieai.com](https://www.wujieai.com) for details");
  });

  it("should handle code block followed by URL", () => {
    const input = "```\ncode\n```\nVisit https://example.com after";
    expect(preprocessLinks(input)).toBe("```\ncode\n```\nVisit [https://example.com](https://example.com) after");
  });

  it("should NOT linkify URLs inside inline code", () => {
    const input = "Use `curl https://example.com/api` to test";
    expect(preprocessLinks(input)).toBe(input);
  });

  it("should NOT linkify file paths inside code blocks", () => {
    const input = "```\ncat ./src/index.ts\n```";
    expect(preprocessLinks(input)).toBe(input);
  });
});
