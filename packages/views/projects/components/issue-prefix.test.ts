import { describe, expect, it } from "vitest";
import {
  ISSUE_PREFIX_MAX_LENGTH,
  isValidIssuePrefix,
  normalizeIssuePrefix,
} from "./issue-prefix";

describe("normalizeIssuePrefix", () => {
  it("upper-cases and drops everything that is not A–Z or 0–9", () => {
    expect(normalizeIssuePrefix("pro-a 1!")).toBe("PROA1");
    expect(normalizeIssuePrefix("  ab_12  ")).toBe("AB12");
  });

  it("caps the value at the server's 10-character limit", () => {
    expect(normalizeIssuePrefix("abcdefghijkl")).toHaveLength(ISSUE_PREFIX_MAX_LENGTH);
    expect(normalizeIssuePrefix("abcdefghijkl")).toBe("ABCDEFGHIJ");
  });

  it("normalizes an empty or symbol-only value to empty", () => {
    expect(normalizeIssuePrefix("")).toBe("");
    expect(normalizeIssuePrefix("- !")).toBe("");
  });
});

describe("isValidIssuePrefix", () => {
  it("accepts what the server accepts", () => {
    expect(isValidIssuePrefix("A")).toBe(true);
    expect(isValidIssuePrefix("PROA1")).toBe(true);
    expect(isValidIssuePrefix("A1B2C3D4E5")).toBe(true);
  });

  it("rejects empty, lowercase, over-long, and punctuated values", () => {
    expect(isValidIssuePrefix("")).toBe(false);
    expect(isValidIssuePrefix("proa")).toBe(false);
    expect(isValidIssuePrefix("A1B2C3D4E5F")).toBe(false);
    expect(isValidIssuePrefix("PRO-A")).toBe(false);
  });
});
