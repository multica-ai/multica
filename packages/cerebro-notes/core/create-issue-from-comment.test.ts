import { describe, expect, it } from "vitest";
import {
  commentIssueTitle,
  safeParseCreateIssueResult,
  safeParseNoteComments,
} from "./comments";

// FIR-3102 — Create issue from a note comment: default-title derivation and
// defensive parsing of the create response + the comment's new issue_id. The
// API Response Compatibility rule requires malformed responses to fail soft.

describe("commentIssueTitle", () => {
  it("uses the first non-empty line of the body", () => {
    expect(commentIssueTitle("Fix the redirect\n\nmore detail")).toBe(
      "Fix the redirect",
    );
  });
  it("skips leading blank lines and strips markdown heading marks", () => {
    expect(commentIssueTitle("\n\n## Broken filter\nbody")).toBe(
      "Broken filter",
    );
  });
  it("returns empty for a blank body (so Create stays disabled)", () => {
    expect(commentIssueTitle("")).toBe("");
    expect(commentIssueTitle("   \n  \n")).toBe("");
  });
  it("caps a very long first line at 120 chars", () => {
    const long = "a".repeat(200);
    expect(commentIssueTitle(long)).toHaveLength(120);
  });
});

describe("issue_id on a note comment", () => {
  it("defaults a missing issue_id to null", () => {
    const out = safeParseNoteComments([{ id: "c1", body: "hi" }]);
    expect(out[0]!.issue_id).toBeNull();
  });
  it("keeps a present issue_id", () => {
    const id = "00000000-0000-0000-0000-0000000000ee";
    const out = safeParseNoteComments([{ id: "c1", body: "hi", issue_id: id }]);
    expect(out[0]!.issue_id).toBe(id);
  });
});

describe("safeParseCreateIssueResult", () => {
  it("parses a full response, including the linked comment's issue_id", () => {
    const issueId = "00000000-0000-0000-0000-0000000000ff";
    const out = safeParseCreateIssueResult({
      issue_id: issueId,
      number: 123,
      comment: { id: "c1", body: "hi", issue_id: issueId },
    });
    expect(out.issue_id).toBe(issueId);
    expect(out.number).toBe(123);
    expect(out.comment.issue_id).toBe(issueId);
  });
  it("fails soft on a malformed response", () => {
    const out = safeParseCreateIssueResult("garbage");
    expect(out.issue_id).toBe("");
    expect(out.number).toBe(0);
    expect(out.comment.issue_id).toBeNull();
  });
  it("tolerates missing fields", () => {
    const out = safeParseCreateIssueResult({ number: 7 });
    expect(out.number).toBe(7);
    expect(out.issue_id).toBe("");
    expect(out.comment.id).toBe("");
  });
});
