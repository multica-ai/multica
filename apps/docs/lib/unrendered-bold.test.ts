// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import type { Nodes } from "mdast";
import remarkGfm from "remark-gfm";
import remarkMdx from "remark-mdx";
import remarkParse from "remark-parse";
import { unified } from "unified";
import { describe, expect, it } from "vitest";

// CommonMark only closes `**` when the delimiter is "right-flanking": a closing
// `**` that follows punctuation and is immediately followed by a letter does
// not count. CJK prose often runs straight from bold into a particle
// (`**넘기기…**를`, `**変更しますか？**と`), so the asterisks render literally.
// Parsing with the same remark-mdx + GFM pipeline fumadocs-mdx uses, any `**`
// left inside a plain text node is one the reader will see. Code and inline
// code are separate node types, so globs like `**/*.ts` are not flagged.

const docsDir = path.resolve(__dirname, "../content/docs");
const processor = unified().use(remarkParse).use(remarkMdx).use(remarkGfm);

function listMdxFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return listMdxFiles(full);
    return entry.name.endsWith(".mdx") ? [full] : [];
  });
}

function stripFrontmatter(source: string): string {
  const match = /^---\r?\n[\s\S]*?\r?\n---\r?\n/.exec(source);
  // Keep the line count so reported line numbers match the file.
  return match ? source.replace(match[0], "\n".repeat(match[0].split("\n").length - 1)) : source;
}

function findUnrenderedBold(source: string): { line: number; text: string }[] {
  const found: { line: number; text: string }[] = [];
  const walk = (node: Nodes) => {
    if (node.type === "text" && node.value.includes("**")) {
      found.push({ line: node.position?.start.line ?? 0, text: node.value.trim() });
    }
    if ("children" in node) node.children.forEach((child) => walk(child as Nodes));
  };
  walk(processor.parse(stripFrontmatter(source)));
  return found;
}

describe("findUnrenderedBold", () => {
  it("flags a closing ** that follows punctuation and runs into text", () => {
    expect(findUnrenderedBold("**+**를 클릭하고")).toHaveLength(1);
    expect(findUnrenderedBold("**この変更をどこに適用しますか？**と")).toHaveLength(1);
  });

  it("accepts bold that closes cleanly", () => {
    expect(findUnrenderedBold("**+** 를 클릭하고")).toEqual([]);
    expect(findUnrenderedBold("**この変更をどこに適用しますか**？と")).toEqual([]);
    expect(findUnrenderedBold("Click **Save**.")).toEqual([]);
  });

  it("ignores ** inside code", () => {
    expect(findUnrenderedBold("Match `src/**/*.ts`.\n\n```\nglob: **/*.md\n```")).toEqual([]);
  });
});

describe("docs content", () => {
  const files = listMdxFiles(docsDir);

  it("finds the docs pages", () => {
    expect(files.length).toBeGreaterThan(0);
  });

  it.each(files.map((file) => [path.relative(docsDir, file), file]))(
    "%s has no unrendered **",
    (_name, file) => {
      expect(findUnrenderedBold(readFileSync(file, "utf8"))).toEqual([]);
    },
  );
});
