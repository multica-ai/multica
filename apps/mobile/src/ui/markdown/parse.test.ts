import { describe, expect, it } from "vitest";
import { markdownPreview, parseInline, parseMarkdown } from "./parse";

describe("parseInline", () => {
  it("reads emphasis, code and links", () => {
    expect(parseInline("a **b** `c` [d](https://x.dev) *e*")).toEqual([
      { kind: "text", text: "a " },
      { kind: "strong", children: [{ kind: "text", text: "b" }] },
      { kind: "text", text: " " },
      { kind: "code", text: "c" },
      { kind: "text", text: " " },
      { kind: "link", url: "https://x.dev", children: [{ kind: "text", text: "d" }] },
      { kind: "text", text: " " },
      { kind: "em", children: [{ kind: "text", text: "e" }] },
    ]);
  });

  it("turns Multica mention links into mentions", () => {
    expect(parseInline("ping [@Lambda](mention://agent/a1) on [MUL-1](mention://issue/i9)")).toEqual([
      { kind: "text", text: "ping " },
      { kind: "mention", type: "agent", id: "a1", label: "Lambda" },
      { kind: "text", text: " on " },
      { kind: "mention", type: "issue", id: "i9", label: "MUL-1" },
    ]);
  });

  it("leaves snake_case words and lone markers alone", () => {
    expect(parseInline("use snake_case_name * 2")).toEqual([{ kind: "text", text: "use snake_case_name * 2" }]);
  });

  it("links bare URLs", () => {
    expect(parseInline("see https://multica.ai/docs.")).toEqual([
      { kind: "text", text: "see " },
      { kind: "link", url: "https://multica.ai/docs", children: [{ kind: "text", text: "https://multica.ai/docs" }] },
      { kind: "text", text: "." },
    ]);
  });
});

describe("parseMarkdown", () => {
  it("splits headings, paragraphs, lists, code and quotes", () => {
    const blocks = parseMarkdown(["## Plan", "", "First line", "second line", "", "- one", "- two", "", "1. a", "2. b", "", "```ts", "const x = 1;", "```", "", "> quoted"].join("\n"));
    expect(blocks.map((block) => block.kind)).toEqual(["heading", "paragraph", "list", "list", "code", "quote"]);
    expect(blocks[1]).toEqual({ kind: "paragraph", content: [{ kind: "text", text: "First line\nsecond line" }] });
    expect(blocks[2]).toMatchObject({ ordered: false, items: [[{ text: "one" }], [{ text: "two" }]] });
    expect(blocks[3]).toMatchObject({ ordered: true, start: 1 });
    expect(blocks[4]).toEqual({ kind: "code", lang: "ts", text: "const x = 1;" });
  });

  it("keeps an unterminated fence as code", () => {
    expect(parseMarkdown("```\nrunning")).toEqual([{ kind: "code", lang: "", text: "running" }]);
  });

  it("drops task-list checkboxes", () => {
    expect(parseMarkdown("- [x] done")).toEqual([{ kind: "list", ordered: false, start: 1, items: [[{ kind: "text", text: "done" }]] }]);
  });
});

describe("markdownPreview", () => {
  it("flattens to one line", () => {
    expect(markdownPreview("# Title\n\n**Bold** and [@Ann](mention://member/u1)\n\n> note")).toBe("Title Bold and @Ann note");
  });
});
