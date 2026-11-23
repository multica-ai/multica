import { replaceSource } from "fumadocs-core/mdx-plugins/stringifier";
import type { Root } from "mdast";

type IncludedNode = { type: string; depth?: number; children?: IncludedNode[] };
type SourceFile = Parameters<typeof replaceSource>[0];
type SourceNode = Parameters<typeof replaceSource>[1];

/**
 * A Markdown file embedded with `<include>` (such as the repository's
 * `SELF_HOSTING_AI.md`) keeps its own H1 for readers on GitHub. On a docs
 * page the frontmatter title is the H1, so the included one is dropped from
 * both the HTML and the exported Markdown.
 */
export function remarkDropIncludedTitle() {
  return (tree: Root, file: SourceFile) => {
    for (const node of tree.children as IncludedNode[]) {
      // remark-include turns the `<include>` element into the included root.
      if (node.type !== "root" || !node.children) continue;
      const index = node.children.findIndex((child) => child.type === "heading" && child.depth === 1);
      if (index === -1) continue;
      const [title] = node.children.splice(index, 1);
      replaceSource(file, title as SourceNode, "");
    }
  };
}
