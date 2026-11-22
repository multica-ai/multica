import type { Link, PhrasingContent, Root, Text } from "mdast";
import { CONTINUE, SKIP, visit } from "unist-util-visit";

const sha = "[0-9a-f]{7,40}";
const range = "\\.\\.\\.?";
const prefix = "(?:commit|head)[ \\t]*:?[ \\t]+";
const plainReferences = new RegExp(`\\b(${prefix})?(${sha}(?:${range}${sha})?)\\b`, "gi");
const inlineReference = new RegExp(`^${sha}(?:${range}${sha})?$`, "i");
const inlineContext = new RegExp(`\\b${prefix}$`, "i");

export function commitReferenceUrl(repository: string, reference: string): string {
  const route = reference.includes("..") ? "compare" : "commit";
  return `${repository}/${route}/${reference}`;
}

function link(repository: string, reference: string, content: Link["children"]): Link {
  return { type: "link", url: commitReferenceUrl(repository, reference), children: content };
}

function text(value: string): Text {
  return { type: "text", value };
}

export function findCommitReferences(value: string) {
  const matches: { start: number; reference: string }[] = [];
  for (const { index, 1: context, 2: reference } of value.matchAll(plainReferences)) {
    if (!reference || (!context && !reference.includes(".."))) continue;
    matches.push({ start: index + (context?.length ?? 0), reference });
  }
  return matches;
}

export function isInlineCommitReference(value: string, preceding: string): boolean {
  return inlineReference.test(value) && (value.includes("..") || inlineContext.test(preceding));
}

function linkText(node: Text, repository: string): PhrasingContent[] {
  const children: PhrasingContent[] = [];
  let cursor = 0;
  for (const { start, reference } of findCommitReferences(node.value)) {
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
        const previous = siblings[index - 1];
        if (child.type === "inlineCode" && isInlineCommitReference(child.value, previous?.type === "text" ? previous.value : "")) {
          return [link(repository, child.value, [child])];
        }
        return [child];
      });
      return CONTINUE;
    });
  };
}
