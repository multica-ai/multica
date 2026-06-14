import { describe, it, expect } from "vitest";
import { parseOutline, countDocument } from "./document-outline";

describe("parseOutline", () => {
  it("extracts ATX headings in order with levels and running index", () => {
    const md = "# Title\n\nintro\n\n## Section A\ntext\n\n### Sub\n\n## Section B";
    expect(parseOutline(md)).toEqual([
      { level: 1, text: "Title", index: 0 },
      { level: 2, text: "Section A", index: 1 },
      { level: 3, text: "Sub", index: 2 },
      { level: 2, text: "Section B", index: 3 },
    ]);
  });

  it("ignores '#' lines inside fenced code blocks", () => {
    const md = "# Real\n\n```bash\n# not a heading\n```\n\n## Also real";
    expect(parseOutline(md).map((h) => h.text)).toEqual(["Real", "Also real"]);
  });

  it("returns an empty list when there are no headings", () => {
    expect(parseOutline("just a paragraph\nand another")).toEqual([]);
  });
});

describe("countDocument", () => {
  it("counts readable words and characters, ignoring markdown markup", () => {
    const { words } = countDocument("# Title\n\nHello **world** here");
    expect(words).toBe(4); // Title, Hello, world, here
  });

  it("excludes whitespace from the character count", () => {
    expect(countDocument("a b  c").characters).toBe(3);
  });

  it("returns zero for an empty document", () => {
    expect(countDocument("   \n  ")).toEqual({ words: 0, characters: 0 });
  });

  it("drops fenced code blocks from the count", () => {
    const counts = countDocument("word\n\n```\nlots of code here\n```\n");
    expect(counts.words).toBe(1);
  });
});
