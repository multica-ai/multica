import { describe, expect, it } from "vitest";
import { activeMention, editableMentions, insertMention, matchMentions, withMentionLinks, type MentionTarget } from "./mentions";

const lambda: MentionTarget = { type: "agent", id: "a1", label: "Lambda" };
const issue: MentionTarget = { type: "issue", id: "i1", label: "MUL-12" };

describe("activeMention", () => {
  it("finds the query after an @ that starts a word", () => {
    expect(activeMention("hi @lam", 7)).toEqual({ start: 3, query: "lam" });
    expect(activeMention("@", 1)).toEqual({ start: 0, query: "" });
  });

  it("ignores emails and finished words", () => {
    expect(activeMention("dev@localhost", 13)).toBeNull();
    expect(activeMention("@lam done", 9)).toBeNull();
  });
});

describe("insertMention", () => {
  it("replaces the query with the token and moves the cursor past it", () => {
    expect(insertMention("hi @la there", 3, 6, lambda)).toEqual({ text: "hi @Lambda  there", cursor: 11 });
  });
});

describe("withMentionLinks", () => {
  it("links each token still in the text", () => {
    expect(withMentionLinks("@Lambda see MUL-12, thanks", [lambda, issue])).toBe("[@Lambda](mention://agent/a1) see [MUL-12](mention://issue/i1), thanks");
  });

  it("leaves longer words and removed mentions alone", () => {
    expect(withMentionLinks("@Lambdas MUL-123", [lambda, issue])).toBe("@Lambdas MUL-123");
  });

  it("round-trips with editableMentions", () => {
    const stored = "ping [@Lambda](mention://agent/a1) about [MUL-12](mention://issue/i1)";
    const { text, targets } = editableMentions(stored);
    expect(text).toBe("ping @Lambda about MUL-12");
    expect(withMentionLinks(text, targets)).toBe(stored);
  });
});

describe("matchMentions", () => {
  it("ranks prefix matches first", () => {
    const candidates = [{ type: "member" as const, id: "1", label: "Ola" }, { type: "member" as const, id: "2", label: "Lara" }];
    expect(matchMentions(candidates, "la").map((c) => c.label)).toEqual(["Lara", "Ola"]);
  });
});
