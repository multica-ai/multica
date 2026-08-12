import type { Content, Parent, Root, Text } from "mdast";
import type { Plugin } from "unified";
import { fromMarkdown } from "mdast-util-from-markdown";

export type LatexFormula = {
  kind: "inline" | "display";
  value: string;
};

export type ExtractedLatex = {
  markdown: string;
  formulas: LatexFormula[];
  tokenPrefix?: string;
};

type Range = { start: number; end: number };

const TOKEN_END = "\uE001";

/** Source ranges that CommonMark parsed as actual text or code. */
function markdownRanges(source: string): { text: Range[]; code: Range[] } {
  const text: Range[] = [];
  const code: Range[] = [];
  const collect = (node: Root | Content) => {
    if (node.type === "code" || node.type === "inlineCode") {
      const start = node.position?.start.offset;
      const end = node.position?.end.offset;
      if (start != null && end != null) code.push({ start, end });
      return;
    }
    if (node.type === "text") {
      const start = node.position?.start.offset;
      const end = node.position?.end.offset;
      if (start != null && end != null) text.push({ start, end });
      return;
    }
    if ("children" in node) {
      for (const child of node.children) collect(child);
    }
  };
  collect(fromMarkdown(source));
  return {
    text: text.sort((a, b) => a.start - b.start),
    code: code.sort((a, b) => a.start - b.start),
  };
}

function rangeAt(ranges: readonly Range[], offset: number): Range | undefined {
  let low = 0;
  let high = ranges.length - 1;
  while (low <= high) {
    const middle = (low + high) >> 1;
    const range = ranges[middle]!;
    if (offset < range.start) {
      high = middle - 1;
    } else if (offset >= range.end) {
      low = middle + 1;
    } else {
      return range;
    }
  }
  return undefined;
}

/** Existing dollar math is already supported and must remain opaque here. */
function dollarMathRanges(source: string, code: readonly Range[]): Range[] {
  const ranges: Range[] = [];
  let offset = 0;

  while (offset < source.length - 1) {
    const codeRange = rangeAt(code, offset);
    if (codeRange) {
      offset = codeRange.end;
      continue;
    }
    if (source[offset] !== "$" || source[offset + 1] !== "$") {
      offset++;
      continue;
    }

    let cursor = offset + 2;
    let closing = -1;
    while (cursor < source.length - 1) {
      const protectedRange = rangeAt(code, cursor);
      if (protectedRange) {
        cursor = protectedRange.end;
        continue;
      }
      if (source[cursor] === "$" && source[cursor + 1] === "$") {
        closing = cursor + 2;
        break;
      }
      cursor++;
    }

    if (closing !== -1) {
      ranges.push({ start: offset, end: closing });
      offset = closing;
    } else {
      offset += 2;
    }
  }

  return ranges;
}

function isSingleSlash(source: string, offset: number): boolean {
  return (
    source[offset] === "\\" &&
    source[offset - 1] !== "\\" &&
    source[offset + 1] !== "\\"
  );
}

function findClosingDelimiter(
  source: string,
  start: number,
  end: number,
  closing: ")" | "]",
  protectedRanges: readonly Range[],
): number {
  let offset = start;
  while (offset < end - 1) {
    const protectedRange = rangeAt(protectedRanges, offset);
    if (protectedRange) {
      offset = protectedRange.end;
      continue;
    }
    if (
      isSingleSlash(source, offset) &&
      source[offset + 1] === closing
    ) {
      return offset;
    }
    offset++;
  }
  return -1;
}

function uniqueTokenPrefix(source: string): string {
  let namespace = 0;
  let prefix: string;
  do {
    prefix = `\uE000multica-latex-${namespace}-`;
    namespace++;
  } while (source.includes(prefix));
  return prefix;
}

function tokenPattern(prefix: string): RegExp {
  const escaped = prefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`${escaped}(\\d+)${TOKEN_END}`, "g");
}

/**
 * Replace supported LaTeX pairs with Markdown-opaque tokens before parsing.
 *
 * A token is emitted only after its closing delimiter exists. This makes the
 * streaming path monotonic: partial `\(` / `\[` input stays source text, then
 * upgrades once the matching `\)` / `\]` arrives. Only source ranges parsed
 * as mdast text are eligible, so HTML attributes, link destinations, code and
 * other non-text fields stay untouched. Existing dollar math is protected and
 * doubled backslashes remain deliberately literal.
 */
export function extractLatexDelimiters(source: string): ExtractedLatex {
  if (!source || (!source.includes("\\(") && !source.includes("\\["))) {
    return { markdown: source, formulas: [] };
  }

  const ranges = markdownRanges(source);
  const protectedRanges = [
    ...ranges.code,
    ...dollarMathRanges(source, ranges.code),
  ].sort((a, b) => a.start - b.start);
  const prefix = uniqueTokenPrefix(source);
  const formulas: LatexFormula[] = [];
  let markdown = "";
  let copiedThrough = 0;

  for (const textRange of ranges.text) {
    let offset = Math.max(textRange.start, copiedThrough);
    while (offset < textRange.end - 1) {
      const protectedRange = rangeAt(protectedRanges, offset);
      if (protectedRange) {
        offset = protectedRange.end;
        continue;
      }
      if (!isSingleSlash(source, offset)) {
        offset++;
        continue;
      }

      const opener = source[offset + 1];
      if (opener !== "(" && opener !== "[") {
        offset++;
        continue;
      }
      const closing = findClosingDelimiter(
        source,
        offset + 2,
        textRange.end,
        opener === "(" ? ")" : "]",
        protectedRanges,
      );
      if (closing === -1) {
        offset += 2;
        continue;
      }

      const value = source.slice(offset + 2, closing);
      markdown += source.slice(copiedThrough, offset);
      markdown += `${prefix}${formulas.length}${TOKEN_END}`;
      formulas.push({ kind: opener === "(" ? "inline" : "display", value });
      copiedThrough = closing + 2;
      offset = copiedThrough;
    }
  }

  if (formulas.length === 0) return { markdown: source, formulas };
  markdown += source.slice(copiedThrough);
  return { markdown, formulas, tokenPrefix: prefix };
}

type MathNode = {
  type: "math" | "inlineMath";
  value: string;
  data:
    | {
        hName: "code";
        hProperties: { className: ["language-math", "math-inline"] };
        hChildren: [{ type: "text"; value: string }];
      }
    | {
        hName: "pre";
        hChildren: [
          {
            type: "element";
            tagName: "code";
            properties: { className: ["language-math", "math-display"] };
            children: [{ type: "text"; value: string }];
          },
        ];
      };
};

function mathNode(formula: LatexFormula): MathNode {
  if (formula.kind === "inline") {
    return {
      type: "inlineMath",
      value: formula.value,
      data: {
        hName: "code",
        hProperties: { className: ["language-math", "math-inline"] },
        hChildren: [{ type: "text", value: formula.value }],
      },
    };
  }
  return {
    type: "math",
    value: formula.value,
    data: {
      hName: "pre",
      hChildren: [
        {
          type: "element",
          tagName: "code",
          properties: { className: ["language-math", "math-display"] },
          children: [{ type: "text", value: formula.value }],
        },
      ],
    },
  };
}

function splitTokens(
  text: Text,
  formulas: readonly LatexFormula[],
  pattern: RegExp,
): Content[] {
  pattern.lastIndex = 0;
  const nodes: Content[] = [];
  let copiedThrough = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text.value)) !== null) {
    const index = Number(match[1]);
    const formula = formulas[index];
    if (!formula) continue;
    if (match.index > copiedThrough) {
      nodes.push({ type: "text", value: text.value.slice(copiedThrough, match.index) });
    }
    nodes.push(mathNode(formula) as Content);
    copiedThrough = match.index + match[0].length;
  }

  if (copiedThrough === 0) return [text];
  if (copiedThrough < text.value.length) {
    nodes.push({ type: "text", value: text.value.slice(copiedThrough) });
  }
  return nodes;
}

function lowerTokens(
  parent: Parent | Root,
  formulas: readonly LatexFormula[],
  pattern: RegExp,
) {
  for (const child of parent.children) {
    if (child.type === "text") continue;
    if ("children" in child) lowerTokens(child as Parent, formulas, pattern);
  }

  parent.children = parent.children.flatMap((child) =>
    child.type === "text" ? splitTokens(child, formulas, pattern) : [child],
  );

  // Display math is a block. When its token appeared beside prose in one
  // Markdown paragraph, lift it into sibling blocks while preserving the prose
  // on either side as paragraphs.
  parent.children = parent.children.flatMap((child) => {
    if (child.type !== "paragraph") return [child];
    const paragraphChildren = child.children as unknown as Content[];
    if (!paragraphChildren.some((node) => node.type === "math")) return [child];

    const blocks: Content[] = [];
    let inline: Content[] = [];
    const flushInline = () => {
      if (inline.length > 0) {
        blocks.push({ type: "paragraph", children: inline } as Content);
        inline = [];
      }
    };

    for (const node of paragraphChildren) {
      if (node.type === "math") {
        flushInline();
        blocks.push(node);
      } else {
        inline.push(node);
      }
    }
    flushInline();
    return blocks;
  });
}

/** Lower opaque extraction tokens into the mdast math nodes remark-math owns. */
export const remarkLatexDelimiters: Plugin<
  [{ formulas: readonly LatexFormula[]; tokenPrefix?: string }],
  Root
> = ({ formulas, tokenPrefix }) => {
  if (!tokenPrefix) return;
  const pattern = tokenPattern(tokenPrefix);
  return (tree) => lowerTokens(tree, formulas, pattern);
};
