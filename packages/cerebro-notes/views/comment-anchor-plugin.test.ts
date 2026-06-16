import { describe, it, expect } from "vitest";
import { findRange } from "./comment-anchor-plugin";

// Build a {text, map} pair the way collectText would, but by hand so the test
// stays a pure-logic check of findRange. PM positions conventionally start at 1
// inside the top block, so map[i] = i + 1 is a faithful stand-in.
function collected(text: string) {
  return { text, map: Array.from({ length: text.length }, (_, i) => i + 1) };
}

describe("findRange", () => {
  it("locates an exact substring and maps it to PM positions", () => {
    const r = findRange(collected("hello world"), "world");
    // "world" starts at index 6 → pos 7, ends at index 10 → pos 11, +1 = 12.
    expect(r).toEqual({ from: 7, to: 12 });
  });

  it("anchors a quote even when whitespace was collapsed across a wrap", () => {
    // Stored quote has single spaces; the body has a newline between the words.
    const r = findRange(collected("keep bar\nbaz here"), "bar baz");
    // "bar\nbaz" spans index 5..11 → pos 6..12, +1 = 13.
    expect(r).toEqual({ from: 6, to: 13 });
  });

  it("trims the quote before matching", () => {
    const r = findRange(collected("alpha beta"), "  beta  ");
    expect(r).toEqual({ from: 7, to: 11 });
  });

  it("returns null when the quote is absent", () => {
    expect(findRange(collected("nothing to see"), "missing")).toBeNull();
  });

  it("returns null for an empty quote", () => {
    expect(findRange(collected("anything"), "   ")).toBeNull();
  });

  it("does not treat regex metacharacters in the quote as a pattern", () => {
    const r = findRange(collected("price is $5 (cheap)"), "$5 (cheap)");
    expect(r).toEqual({ from: 10, to: 20 });
  });
});
