import { describe, expect, it, vi } from "vitest";
import {
  MARKDOWN_CHUNK_THRESHOLD,
  parseMarkdownChunked,
} from "./parse-markdown-chunked";

describe("parseMarkdownChunked", () => {
  it("routes a typical 22k issue through the chunked parse path", () => {
    expect(MARKDOWN_CHUNK_THRESHOLD).toBeLessThan(22_000);
  });

  it("keeps ordinary paragraph chunks small enough to avoid quadratic scans", () => {
    const markdown = Array.from(
      { length: 240 },
      (_, index) => `${index}: ${"x".repeat(92)}`,
    ).join("\n\n");
    const parsedLengths: number[] = [];
    const manager = {
      parse: vi.fn((chunk: string) => {
        parsedLengths.push(chunk.length);
        return {
          type: "doc",
          content: [{ type: "paragraph", attrs: { sourceLength: chunk.length } }],
        };
      }),
    };

    const result = parseMarkdownChunked(manager, markdown);

    expect(markdown.length).toBeGreaterThan(22_000);
    expect(manager.parse).toHaveBeenCalled();
    expect(parsedLengths.length).toBeGreaterThan(1);
    expect(Math.max(...parsedLengths)).toBeLessThan(6_000);
    expect(result.content).toHaveLength(parsedLengths.length);
  });
});
