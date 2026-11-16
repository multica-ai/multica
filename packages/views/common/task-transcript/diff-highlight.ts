// Syntax highlighting for transcript diffs. Highlighting runs over a whole
// side at once — never line by line — so a multi-line string, comment or
// template literal is coloured as the one token it is. The highlighted tree is
// then split at newlines, re-opening the enclosing spans on each line, which is
// what lets a per-line diff gutter coexist with block-accurate grammar.
//
// Same engine (`lowlight`) and same `.hljs-*` class contract as the rich
// content code block, so a Rust file looks the same in a transcript as it does
// in a comment.

import { toHtml } from "hast-util-to-html";
import type { Element, ElementContent, Properties, Root, RootContent } from "hast";
import { highlightCode } from "../../editor/syntax-highlight";

/**
 * File extension to a lowlight grammar name. Unlisted extensions resolve to
 * plaintext inside `highlightCode`, so this map only needs the languages worth
 * naming — a miss degrades to unhighlighted text, never to an error.
 */
const LANGUAGE_BY_EXTENSION: Record<string, string> = {
  bash: "bash",
  c: "c",
  cc: "cpp",
  cjs: "javascript",
  cpp: "cpp",
  cs: "csharp",
  css: "css",
  go: "go",
  h: "c",
  hpp: "cpp",
  htm: "xml",
  html: "xml",
  java: "java",
  js: "javascript",
  json: "json",
  jsx: "javascript",
  kt: "kotlin",
  lua: "lua",
  md: "markdown",
  mjs: "javascript",
  php: "php",
  pl: "perl",
  py: "python",
  r: "r",
  rb: "ruby",
  rs: "rust",
  scala: "scala",
  scss: "scss",
  sh: "bash",
  sql: "sql",
  swift: "swift",
  toml: "ini",
  ts: "typescript",
  tsx: "typescript",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  zsh: "bash",
};

/** Grammar for a path, by extension. Undefined means "highlight as plaintext". */
export function languageForPath(path: string): string | undefined {
  const base = path.split("/").pop() ?? path;
  const dot = base.lastIndexOf(".");
  if (dot <= 0) return undefined;
  return LANGUAGE_BY_EXTENSION[base.slice(dot + 1).toLowerCase()];
}

/** The open-element chain a piece of text sits inside, innermost last. */
type Ancestors = ReadonlyArray<Pick<Element, "tagName" | "properties">>;

function wrap(text: string, ancestors: Ancestors): ElementContent {
  let node: ElementContent = { type: "text", value: text };
  for (let i = ancestors.length - 1; i >= 0; i--) {
    const ancestor = ancestors[i];
    if (!ancestor) continue;
    node = {
      type: "element",
      tagName: ancestor.tagName,
      properties: (ancestor.properties ?? {}) as Properties,
      children: [node],
    };
  }
  return node;
}

function collect(
  nodes: ReadonlyArray<RootContent>,
  ancestors: Ancestors,
  lines: ElementContent[][],
): void {
  for (const node of nodes) {
    if (node.type === "text") {
      const parts = node.value.split("\n");
      for (let i = 0; i < parts.length; i++) {
        if (i > 0) lines.push([]);
        const part = parts[i];
        if (part === undefined || part.length === 0) continue;
        lines[lines.length - 1]?.push(wrap(part, ancestors));
      }
      continue;
    }
    if (node.type === "element") {
      collect(node.children, [...ancestors, { tagName: node.tagName, properties: node.properties }], lines);
    }
    // Comments/doctypes cannot appear in a lowlight result; ignoring them keeps
    // the walker total without inventing output for impossible input.
  }
}

/**
 * Highlight `text` as one block and return one HTML string per line. Falls back
 * to `null` so callers can render the original text unhighlighted rather than
 * showing nothing when a grammar throws.
 */
/** Highlight `text` as one block. `null` when the grammar throws. */
export function highlightBlock(text: string, language: string | undefined): string | null {
  try {
    return toHtml(highlightCode(text, language) as Root);
  } catch {
    return null;
  }
}

export function highlightToLines(text: string, language: string | undefined): string[] | null {
  try {
    const tree = highlightCode(text, language) as Root;
    const lines: ElementContent[][] = [[]];
    collect(tree.children, [], lines);
    return lines.map((children) => toHtml({ type: "root", children }));
  } catch {
    return null;
  }
}
