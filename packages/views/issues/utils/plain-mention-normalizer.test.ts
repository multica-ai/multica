import { describe, expect, it } from "vitest";
import { normalizePlainMentions, type PlainMentionTarget } from "./plain-mention-normalizer";

const targets: PlainMentionTarget[] = [
  { id: "agent-1", name: "한소희(designer)", type: "agent" },
  { id: "agent-2", name: "하니(fe)", type: "agent" },
  { id: "member-1", name: "Alice", type: "member" },
  { id: "member-2", name: "David[TF]", type: "member" },
];

describe("normalizePlainMentions", () => {
  it("converts plain agent and member mentions to triggerable markdown", () => {
    expect(normalizePlainMentions("@한소희(designer) 확인 부탁 @Alice", targets)).toBe(
      "[@한소희(designer)](mention://agent/agent-1) 확인 부탁 [@Alice](mention://member/member-1)",
    );
  });

  it("does not rewrite existing mention markdown or regular links", () => {
    const text = "이미 [@하니(fe)](mention://agent/agent-2) 와 [@Alice](https://example.com)";
    expect(normalizePlainMentions(text, targets)).toBe(text);
  });

  it("does not rewrite code, escaped mentions, or email-like text", () => {
    const text = [
      "`@Alice`",
      "\\@Alice",
      "ops@Alice.com",
      "```",
      "@한소희(designer)",
      "```",
    ].join("\n");
    expect(normalizePlainMentions(text, targets)).toBe(text);
  });

  it("escapes markdown-sensitive label characters", () => {
    expect(normalizePlainMentions("@David[TF]", targets)).toBe(
      "[@David\\[TF\\]](mention://member/member-2)",
    );
  });

  it("prefers the longest matching name", () => {
    const overlapping: PlainMentionTarget[] = [
      { id: "member-short", name: "Alice", type: "member" },
      { id: "member-long", name: "Alice Kim", type: "member" },
    ];
    expect(normalizePlainMentions("@Alice Kim", overlapping)).toBe(
      "[@Alice Kim](mention://member/member-long)",
    );
  });
});
