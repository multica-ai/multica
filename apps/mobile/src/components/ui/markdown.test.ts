import { describe, expect, it } from "vitest";
import { tokenizeInline } from "./markdown-tokenize";

describe("mobile markdown tokenizer", () => {
  it("parses all mention target types", () => {
    expect(tokenizeInline("[@All members](mention://all/all)")).toEqual([
      {
        type: "mention",
        content: "@All members",
        mentionType: "all",
        mentionId: "all",
      },
    ]);
    expect(tokenizeInline("[@Alice](mention://member/user-1)")).toEqual([
      {
        type: "mention",
        content: "@Alice",
        mentionType: "member",
        mentionId: "user-1",
      },
    ]);
    expect(tokenizeInline("[@Builder](mention://agent/agent-1)")).toEqual([
      {
        type: "mention",
        content: "@Builder",
        mentionType: "agent",
        mentionId: "agent-1",
      },
    ]);
    expect(tokenizeInline("[@Frontend](mention://squad/squad-1)")).toEqual([
      {
        type: "mention",
        content: "@Frontend",
        mentionType: "squad",
        mentionId: "squad-1",
      },
    ]);
    expect(tokenizeInline("[MUL-123](mention://issue/issue-1)")).toEqual([
      {
        type: "mention",
        content: "MUL-123",
        mentionType: "issue",
        mentionId: "issue-1",
      },
    ]);
  });

  it("keeps surrounding text around issue mentions", () => {
    expect(tokenizeInline("See [MUL-123](mention://issue/issue-1) now")).toEqual([
      { type: "text", content: "See " },
      {
        type: "mention",
        content: "MUL-123",
        mentionType: "issue",
        mentionId: "issue-1",
      },
      { type: "text", content: " now" },
    ]);
  });

  it("parses markdown http links", () => {
    expect(tokenizeInline("Open [docs](https://example.com/path?a=1)")).toEqual([
      { type: "text", content: "Open " },
      {
        type: "link",
        content: "docs",
        href: "https://example.com/path?a=1",
      },
    ]);
  });

  it("linkifies bare http and https URLs", () => {
    expect(tokenizeInline("See https://example.com/a?b=1 and http://foo.test.")).toEqual([
      { type: "text", content: "See " },
      {
        type: "link",
        content: "https://example.com/a?b=1",
        href: "https://example.com/a?b=1",
      },
      { type: "text", content: " and " },
      {
        type: "link",
        content: "http://foo.test",
        href: "http://foo.test",
      },
      { type: "text", content: "." },
    ]);
  });

  it("handles long pasted URLs without recursive regex matching", () => {
    const url = `https://example.com/${"a".repeat(5000)}?q=${"b".repeat(5000)}`;

    expect(tokenizeInline(url)).toEqual([{ type: "link", content: url, href: url }]);
  });
});
