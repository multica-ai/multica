import { describe, expect, it } from "vitest";
import { utf8ByteLength } from "./note-queries";

// The server computes ProjectNoteSummary.BodySize as len(n.BodyMd) — a UTF-8
// byte count. When a write patches the list cache locally we must derive the
// same number, or the row's size would visibly change the moment a note is
// saved and then snap back on the next refetch.
describe("utf8ByteLength", () => {
  it("matches Go's len() for ASCII", () => {
    expect(utf8ByteLength("hello")).toBe(5);
    expect(utf8ByteLength("")).toBe(0);
  });

  it("counts CJK as 3 bytes per character, not 1 UTF-16 unit", () => {
    // "笔记内容".length === 4 in JS; Go reports 12.
    expect(utf8ByteLength("笔记内容")).toBe(12);
  });

  it("counts astral-plane characters as 4 bytes", () => {
    // "🎉".length === 2 in JS (surrogate pair); Go reports 4.
    expect(utf8ByteLength("🎉")).toBe(4);
    expect(utf8ByteLength("emoji 🎉🎉")).toBe(14);
  });

  it("counts 2-byte range characters", () => {
    // U+00E9 é encodes to 2 bytes.
    expect(utf8ByteLength("é")).toBe(2);
  });

  it("counts newlines and the append separator verbatim", () => {
    expect(utf8ByteLength("a\n\nb")).toBe(4);
  });
});
