// @vitest-environment node

import { describe, expect, it } from "vitest";
import { buildCommentQuoteMarkdown } from "./comment-quote";

describe("buildCommentQuoteMarkdown", () => {
  it("quotes the original Markdown without rewriting mentions", () => {
    expect(
      buildCommentQuoteMarkdown(
        "[@Reviewer](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please check\nsecond line",
      ),
    ).toBe(
      "> [@Reviewer](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please check\n" +
        "> second line",
    );
  });

  it("keeps blank lines inside a multi-paragraph quote", () => {
    expect(buildCommentQuoteMarkdown("First paragraph\n\nSecond paragraph")).toBe(
      "> First paragraph\n> \n> Second paragraph",
    );
  });
});
