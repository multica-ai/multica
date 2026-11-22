import type { Link, PhrasingContent, Root, Text } from "mdast";
import { CONTINUE, SKIP, visit } from "unist-util-visit";

const sha = "[0-9a-f]{7,40}";
const range = "\\.\\.\\.?";
const prefix = "(?:commit|head)[ \\t]*:?[ \\t]+";
const plainReferences = new RegExp(`\\b(${prefix})?(${sha}(?:${range}${sha})?)\\b`, "gi");
const inlineReference = new RegExp(`^${sha}(?:${range}${sha})?$`, "i");
const inlineContext = new RegExp(`\\b${prefix}$`, "i");

function link(repository: string, reference: string, content: Link["children"]): Link {
  const route = reference.includes("..") ? "compare" : "commit";
  return { type: "link", url: `${repository}/${route}/${reference}`, children: content };
}

function text(value: string): Text {
  return { type: "text", value };
}

function linkText(node: Text, repository: string): PhrasingContent[] {
  const children: PhrasingContent[] = [];
  let cursor = 0;
  for (const { index, 1: context, 2: reference } of node.value.matchAll(plainReferences)) {
    if (!reference || (!context && !reference.includes(".."))) continue;
    const start = index + (context?.length ?? 0);
    if (cursor < start) children.push(text(node.value.slice(cursor, start)));
    children.push(link(repository, reference, [text(reference)]));
    cursor = start + reference.length;
  }
  if (!cursor) return [node];
  if (cursor < node.value.length) children.push(text(node.value.slice(cursor)));
  return children;
}

export function remarkCommitReferences(repository: string) {
  return (tree: Root) => {
    visit(tree, (node) => {
      if (node.type === "link" || node.type === "linkReference") return SKIP;
      if (node.type !== "paragraph" && node.type !== "heading" && node.type !== "tableCell" &&
          node.type !== "emphasis" && node.type !== "strong" && node.type !== "delete") return CONTINUE;
      node.children = node.children.flatMap((child, index, siblings): PhrasingContent[] => {
        if (child.type === "text") return linkText(child, repository);
        if (child.type === "inlineCode" && inlineReference.test(child.value)) {
          const previous = siblings[index - 1];
          if (child.value.includes("..") || (previous?.type === "text" && inlineContext.test(previous.value))) {
            return [link(repository, child.value, [child])];
          }
        }
        return [child];
      });
      return CONTINUE;
    });
  };
}
