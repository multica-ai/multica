import { describe, expect, it } from "vitest";
import type { NoteLineAttr } from "../core/types";
import {
  attrsForBlockTexts,
  candidatesFromMarkdown,
  stripMarkdownLine,
} from "./line-attr-map";

function attr(createdBy: string): NoteLineAttr {
  return {
    created_by: createdBy,
    created_at: "2026-07-07T12:00:00Z",
    updated_by: createdBy,
    updated_at: "2026-07-07T12:00:00Z",
  };
}

describe("stripMarkdownLine", () => {
  it("strips headings, list markers, and emphasis", () => {
    expect(stripMarkdownLine("## Month view")).toBe("Month view");
    expect(stripMarkdownLine("- **JEH** Month view")).toBe("JEH Month view");
    expect(stripMarkdownLine("1. do the thing")).toBe("do the thing");
    expect(stripMarkdownLine("> quoted words")).toBe("quoted words");
    expect(stripMarkdownLine("see [docs](https://x.dk) now")).toBe(
      "see docs now",
    );
  });
});

describe("candidatesFromMarkdown", () => {
  it("skips blank lines and keeps attrs aligned", () => {
    const md = "# Title\n\n- point one\n\n- point two";
    const attrs = [attr("a"), attr(""), attr("b"), attr(""), attr("c")];
    const cands = candidatesFromMarkdown(md, attrs);
    expect(cands.map((c) => c.key)).toEqual(["title", "point one", "point two"]);
    expect(cands.map((c) => c.attr.created_by)).toEqual(["a", "b", "c"]);
  });

  it("folds a fenced code block into one entry attributed to its opener", () => {
    const md = "intro\n```\ncode line 1\ncode line 2\n```\noutro";
    const attrs = [attr("a"), attr("b"), attr("c"), attr("d"), attr("e"), attr("f")];
    const cands = candidatesFromMarkdown(md, attrs);
    expect(cands).toHaveLength(3);
    expect(cands[1]?.key).toBe("code line 1 code line 2");
    expect(cands[1]?.attr.created_by).toBe("b");
  });
});

describe("attrsForBlockTexts", () => {
  it("maps rendered blocks to their markdown lines in order", () => {
    const md = "# IDS\n\n- **JEH** Month view\n- **JEH** KPI i month view";
    const attrs = [attr("jesper"), attr(""), attr("jesper"), attr("sabine")];
    const mapped = attrsForBlockTexts(md, attrs, [
      "IDS",
      "JEH Month view",
      "JEH KPI i month view",
    ]);
    expect(mapped.map((a) => a?.created_by)).toEqual([
      "jesper",
      "jesper",
      "sabine",
    ]);
  });

  it("returns null for blocks it cannot match without derailing the rest", () => {
    const md = "alpha line here\nbravo line here";
    const attrs = [attr("a"), attr("b")];
    const mapped = attrsForBlockTexts(md, attrs, [
      "alpha line here",
      "totally different unmatched",
      "bravo line here",
    ]);
    expect(mapped[0]?.created_by).toBe("a");
    expect(mapped[1]).toBeNull();
    expect(mapped[2]?.created_by).toBe("b");
  });

  it("matches a line that grew in the browser since the last save", () => {
    const md = "shopping list for sunday";
    const attrs = [attr("a")];
    const mapped = attrsForBlockTexts(md, attrs, [
      "shopping list for sunday and monday",
    ]);
    expect(mapped[0]?.created_by).toBe("a");
  });

  it("handles duplicate line texts by document order", () => {
    const md = "- todo\n- todo";
    const attrs = [attr("first"), attr("second")];
    const mapped = attrsForBlockTexts(md, attrs, ["todo", "todo"]);
    expect(mapped.map((a) => a?.created_by)).toEqual(["first", "second"]);
  });
});
