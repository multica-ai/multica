import { describe, expect, it } from "vitest";
import {
  findInlineCodeRanges,
  hasMatchingFencePair,
  isClosingFenceLine,
  parseOpeningFenceLine,
} from "./markdown-codemirror-parser";

describe("markdown CodeMirror parser helpers", () => {
  it("matches closing fences with the same marker and enough length", () => {
    const fence = parseOpeningFenceLine("````ts");

    expect(fence).toEqual({ marker: "`", length: 4 });
    expect(fence && isClosingFenceLine("````", fence)).toBe(true);
    expect(fence && isClosingFenceLine("```", fence)).toBe(false);
    expect(fence && isClosingFenceLine("~~~~", fence)).toBe(false);
  });

  it("only unwraps selections with a valid matching fence pair", () => {
    expect(hasMatchingFencePair("```ts", "```")).toBe(true);
    expect(hasMatchingFencePair("````ts", "```")).toBe(false);
    expect(hasMatchingFencePair("~~~", "```")).toBe(false);
  });

  it("finds simple inline code ranges", () => {
    const text = "Use `code` now";
    const ranges = findInlineCodeRanges(text);

    expect(ranges.map((range) => text.slice(range.from, range.to))).toEqual(["`code`"]);
  });

  it("supports multi-backtick inline code delimiters", () => {
    const text = "Use ``code with ` tick`` now";
    const ranges = findInlineCodeRanges(text);

    expect(ranges.map((range) => text.slice(range.from, range.to))).toEqual([
      "``code with ` tick``",
    ]);
  });

  it("ignores escaped inline code delimiters", () => {
    const text = String.raw`Use \`not code\` and ` + "`real`";
    const ranges = findInlineCodeRanges(text);

    expect(ranges.map((range) => text.slice(range.from, range.to))).toEqual(["`real`"]);
  });
});
