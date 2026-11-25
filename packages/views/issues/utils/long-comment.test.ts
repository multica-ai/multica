// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  LONG_COMMENT_CHARS,
  PLAIN_TEXT_COMMENT_CHARS,
  isLongComment,
  longCommentPreview,
  visibleCommentMarkdown,
} from "./long-comment";

describe("isLongComment", () => {
  it("collapses only past the threshold", () => {
    expect(isLongComment("a".repeat(LONG_COMMENT_CHARS))).toBe(false);
    expect(isLongComment("a".repeat(LONG_COMMENT_CHARS + 1))).toBe(true);
    expect(isLongComment(null)).toBe(false);
    expect(isLongComment(undefined)).toBe(false);
  });

  it("formats expanded comments up to the plain-text limit", () => {
    expect(PLAIN_TEXT_COMMENT_CHARS).toBeGreaterThan(LONG_COMMENT_CHARS);
  });
});

describe("longCommentPreview", () => {
  it("cuts a single huge line before it reaches the renderer", () => {
    const json = `{"rows":[${'{"id":1,"name":"x"},'.repeat(100_000)}]}`;
    const preview = longCommentPreview(json);

    expect(preview.length).toBeLessThanOrEqual(2_000);
    expect(json.startsWith(preview)).toBe(true);
  });

  it("ends on a line break when one is close to the cut", () => {
    const line = "word ".repeat(15).trim();
    const preview = longCommentPreview(`${line}\n`.repeat(400));

    expect(preview.endsWith(line)).toBe(true);
    expect(preview.length).toBeLessThanOrEqual(2_000);
  });

  it("keeps at most 30 lines", () => {
    const preview = longCommentPreview("line\n".repeat(5_000));

    expect(preview.split("\n")).toHaveLength(30);
  });

  it("starts at the text when the body opens with blank lines", () => {
    expect(longCommentPreview(`${"\n".repeat(40)}${"a".repeat(20_000)}`)).toBe("a".repeat(2_000));
    expect(longCommentPreview(`${" ".repeat(2_000)}${"b".repeat(20_000)}`)).toBe("b".repeat(2_000));
  });

  it("never splits a surrogate pair", () => {
    const preview = longCommentPreview(`${"a".repeat(1_999)}${"😀".repeat(10_000)}`);

    expect(preview).toBe("a".repeat(1_999));
  });
});

describe("visibleCommentMarkdown", () => {
  it("shows the preview of a collapsed long comment and the full text once expanded", () => {
    const long = `${"intro ".repeat(500)}\n${"tail ".repeat(5_000)}`;

    expect(visibleCommentMarkdown("short", false)).toBe("short");
    expect(visibleCommentMarkdown(long, false)).toBe(longCommentPreview(long));
    expect(visibleCommentMarkdown(long, true)).toBe(long);
  });
});
