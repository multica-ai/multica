import { describe, it, expect } from "vitest";
import { matchHighlightAt, findLiteralRanges } from "./highlight-match";

/** Convenience: match at the first `==` and return the inner text, or null. */
function inner(text: string): string | null {
  const i = text.indexOf("==");
  if (i === -1) return null;
  const m = matchHighlightAt(text, i);
  return m ? m.inner : null;
}

describe("matchHighlightAt", () => {
  it("matches a basic highlight at the opening fence", () => {
    const m = matchHighlightAt("==hi==", 0);
    expect(m).toEqual({ end: 6, inner: "hi" });
  });

  it("returns null when the opening fence is not at i", () => {
    expect(matchHighlightAt("x==hi==", 0)).toBeNull();
  });

  it("rejects whitespace directly inside the fences", () => {
    expect(inner("== x ==")).toBeNull();
  });

  it("rejects empty content", () => {
    expect(inner("====")).toBeNull();
  });

  it("skips a closing == that sits inside inline code", () => {
    // ==a `b==c` d== → inner is the whole `a `b==c` d`, not `a `b`
    expect(inner("==a `b==c` d==")).toBe("a `b==c` d");
  });

  it("skips a closing == that sits inside inline math", () => {
    expect(inner("==a $b==c$ d==")).toBe("a $b==c$ d");
  });

  it("does not open a highlight whose opening == is inside inline code", () => {
    expect(inner("`x==y==z`")).toBeNull();
  });

  it("does not cross a blank line", () => {
    expect(inner("==a\n\nb==")).toBeNull();
  });

  it("allows a soft line break inside a block", () => {
    expect(inner("==a\nb==")).toBe("a\nb");
  });

  it("does not cross a CRLF blank line", () => {
    expect(inner("==a\r\n\r\nb==")).toBeNull();
  });

  it("allows a CRLF soft line break inside a block", () => {
    expect(inner("==a\r\nb==")).toBe("a\r\nb");
  });

  it("matches the nearest valid closing fence", () => {
    // first valid close wins; trailing == is left over
    expect(matchHighlightAt("==a==b==", 0)).toEqual({ end: 5, inner: "a" });
  });
});

describe("findLiteralRanges", () => {
  it("treats == inside a fenced block as literal", () => {
    const text = "```\n==x==\n```";
    const ranges = findLiteralRanges(text);
    const fencePos = text.indexOf("==x==");
    expect(ranges.some((r) => fencePos >= r.start && fencePos < r.end)).toBe(true);
  });
});
