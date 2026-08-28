// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  COMMENT_SUMMARY_MAX_CODE_POINTS,
  commentSummary,
} from "./comment-summary";

/** “a”。length 1 → 1 code point。 */
const CP = (n: number, ch = "a") => ch.repeat(n);

describe("commentSummary", () => {
  it("passes short single-line content through unchanged", () => {
    expect(commentSummary("hello world")).toBe("hello world");
  });

  it("collapses markdown syntax (mirrors stripMarkdown)", () => {
    expect(commentSummary("[@Alice](mention://member/uuid) fixed it")).toBe(
      "@Alice fixed it",
    );
  });

  it("normalizes whitespace: runs of blank lines collapse to a single newline", () => {
    expect(commentSummary("para one\n\n\n\npara two")).toBe(
      "para one\npara two",
    );
  });

  it("truncates at 120 Unicode code points and appends an ellipsis", () => {
    const out = commentSummary(CP(300));
    expect(out).toHaveLength(COMMENT_SUMMARY_MAX_CODE_POINTS + 1);
    expect(out.endsWith("…")).toBe(true);
  });

  it("counts Unicode code points, not UTF-16 code units (emoji counted once)", () => {
    // 119 emoji + 1 ascii = 120 code points, but 119*2+1 UTF-16 units.
    const emoji = "👍".repeat(119) + "x";
    expect(commentSummary(emoji)).toBe(emoji);
  });

  it("keeps at most 2 lines: third and later lines are dropped", () => {
    const out = commentSummary("one\ntwo\nthree\nfour");
    expect(out).toBe("one\ntwo");
  });

  it("applies the code-point cap before the 2-line cut", () => {
    // Two lines that jointly exceed the cap: cap wins, then line cut is a
    // no-op (the string already has ≤2 lines after capping).
    const out = commentSummary(`${CP(80)}\n${CP(80)}`);
    expect([...out].length).toBeLessThanOrEqual(
      COMMENT_SUMMARY_MAX_CODE_POINTS + 1,
    );
  });

  it("line cutting happens on the capped string, not the raw one", () => {
    // First line alone fills the cap; the second line must still be cut
    // away entirely rather than surviving as a fragment.
    const out = commentSummary(`${CP(130)}\nsecond line`);
    expect(out).not.toContain("second");
  });

  it("returns an empty string for empty / whitespace-only content", () => {
    expect(commentSummary("")).toBe("");
    expect(commentSummary("   \n  ")).toBe("");
  });

  it("does not count the appended ellipsis toward the cap", () => {
    // Exactly at the cap: no ellipsis is added.
    expect(commentSummary(CP(COMMENT_SUMMARY_MAX_CODE_POINTS))).toBe(
      CP(COMMENT_SUMMARY_MAX_CODE_POINTS),
    );
  });
});
