// @vitest-environment node
import { describe, expect, it } from "vitest";
import { markdownSearchText } from "./markdown-search-text";

// Text nodes of the rendered comment, as find walks them.
const nodes = (markdown: string) => markdownSearchText(markdown).split("\n").map((part) => part.trim()).filter(Boolean);

describe("markdownSearchText", () => {
  it("keeps prose and drops block markers", () => {
    expect(nodes("## Summary\n\n> quoted line\n\n- first item\n2. second item\n- [x] done item")).toEqual([
      "Summary", "quoted line", "first item", "second item", "done item",
    ]);
  });

  it("splits text where inline formatting starts a new text node", () => {
    expect(nodes("foo **bar** baz `code()` and _em_ ~~old~~")).toEqual([
      "foo", "bar", "baz", "code()", "and", "em", "old",
    ]);
  });

  it("keeps snake_case and spaced operators literal", () => {
    expect(nodes("call snake_case_name with a * b")).toEqual(["call snake_case_name with a * b"]);
  });

  it("renders links and mentions as their label, and images as nothing", () => {
    expect(nodes("See [the docs](https://example.com/x) and [@Link](mention://agent/1) ![shot](https://cdn/x.png)")).toEqual([
      "See", "the docs", "and", "@Link",
    ]);
  });

  it("keeps fenced code lines and drops the fences", () => {
    expect(nodes("before\n```ts\nconst a = 1;\n**not bold**\n```\nafter")).toEqual([
      "before", "const a = 1;", "**not bold**", "after",
    ]);
  });

  it("reads table cells and skips the delimiter row", () => {
    expect(nodes("| Step | Time |\n| --- | ---: |\n| parse | 12 ms |")).toEqual(["Step", "Time", "parse", "12 ms"]);
  });

  it("unescapes characters and entities", () => {
    expect(nodes("a \\*literal\\* &amp; more")).toEqual(["a *literal* & more"]);
  });
});
