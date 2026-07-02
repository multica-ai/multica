import { describe, expect, it } from "vitest";
import { offsetToSourcePoint, sourceRangeFromOffsets } from "./markdown-source-position";

describe("markdown source position", () => {
  it("maps single-line offsets to 1-based line and character positions", () => {
    const source = "hello world";
    expect(offsetToSourcePoint(source, 0)).toEqual({ line: 1, character: 1, offset: 0 });
    expect(offsetToSourcePoint(source, 6)).toEqual({ line: 1, character: 7, offset: 6 });
  });

  it("maps multi-line offsets to the correct line", () => {
    const source = "line one\nline two\nline three";
    expect(offsetToSourcePoint(source, 9)).toEqual({ line: 2, character: 1, offset: 9 });
    expect(offsetToSourcePoint(source, 14)).toEqual({ line: 2, character: 6, offset: 14 });
  });

  it("counts Chinese characters by Unicode code point", () => {
    const source = "你好世界";
    expect(offsetToSourcePoint(source, "你好".length)).toEqual({ line: 1, character: 3, offset: 2 });
  });

  it("counts emoji as one displayed character", () => {
    const source = "a🙂b";
    expect(offsetToSourcePoint(source, "a🙂".length)).toEqual({ line: 1, character: 3, offset: 3 });
  });

  it("formats ranges as inclusive end positions", () => {
    const source = "hello world";
    expect(sourceRangeFromOffsets(source, 6, 11)).toEqual({
      start: { line: 1, character: 7, offset: 6 },
      end: { line: 1, character: 11, offset: 10 },
    });
  });
});
