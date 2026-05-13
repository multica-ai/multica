import { describe, expect, it } from "vitest";
import { tokenizeInline } from "./markdown-tokenize";

describe("mobile markdown mention tokenizer", () => {
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
});
