import { describe, it, expect } from "vitest";
import { stripMentionMarkdown } from "./strip-mention-markdown";

describe("stripMentionMarkdown", () => {
  it("strips simple agent mention", () => {
    expect(
      stripMentionMarkdown("[@魏和尚](mention://agent/de8efbcc-eaa1-4605-a6ac-d50cfa88e447)"),
    ).toBe("@魏和尚");
  });

  it("strips simple member mention", () => {
    expect(
      stripMentionMarkdown("[@Alice](mention://member/abc-123)"),
    ).toBe("@Alice");
  });

  it("strips issue mention (no @ prefix)", () => {
    expect(
      stripMentionMarkdown("[MUL-123](mention://issue/some-uuid)"),
    ).toBe("MUL-123");
  });

  it("handles escaped brackets in names", () => {
    expect(
      stripMentionMarkdown("[@David\\[TF\\]](mention://agent/id-123)"),
    ).toBe("@David[TF]");
  });

  it("handles multiple mentions in one string", () => {
    expect(
      stripMentionMarkdown(
        "Triggered by [@Alice](mention://member/a1) and [@Bob](mention://agent/b2)",
      ),
    ).toBe("Triggered by @Alice and @Bob");
  });

  it("does NOT strip regular markdown links", () => {
    expect(
      stripMentionMarkdown("[docs](https://example.com)"),
    ).toBe("[docs](https://example.com)");
  });

  it("does NOT strip non-mention parenthetical links", () => {
    expect(
      stripMentionMarkdown("[click here](http://foo.bar/baz)"),
    ).toBe("[click here](http://foo.bar/baz)");
  });

  it("handles backslash-escaped content that is NOT a mention", () => {
    expect(
      stripMentionMarkdown("\\[@Literal](mention://agent/id)"),
    ).toBe("\\[@Literal](mention://agent/id)");
  });

  it("returns plain text unchanged", () => {
    expect(stripMentionMarkdown("hello world")).toBe("hello world");
  });

  it("handles empty string", () => {
    expect(stripMentionMarkdown("")).toBe("");
  });
});
