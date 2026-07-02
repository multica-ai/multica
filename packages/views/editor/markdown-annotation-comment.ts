import type { MarkdownAnnotationDraft, SourceRange } from "./markdown-annotation-types";

const MAX_QUOTE_LENGTH = 500;

function rangeLabel(filename: string, range: SourceRange): string {
  return `${filename}:L${range.start.line}:C${range.start.character}-L${range.end.line}:C${range.end.character}`;
}

export function truncateQuote(quote: string, maxLength = MAX_QUOTE_LENGTH): string {
  const chars = Array.from(quote.trim());
  if (chars.length <= maxLength) return chars.join("");
  const head = chars.slice(0, Math.max(0, Math.floor((maxLength - 1) / 2))).join("");
  const tail = chars.slice(chars.length - Math.max(0, Math.ceil((maxLength - 1) / 2))).join("");
  return `${head}…${tail}`;
}

function quoteBlock(quote: string): string {
  return truncateQuote(quote)
    .split(/\r?\n/)
    .map((line) => `   > ${line}`)
    .join("\n");
}

export function formatMarkdownAnnotationsComment(filename: string, annotations: MarkdownAnnotationDraft[]): string {
  const sorted = [...annotations].sort((a, b) => {
    const byOffset = a.range.start.offset - b.range.start.offset;
    if (byOffset !== 0) return byOffset;
    return a.createdAt - b.createdAt;
  });

  const lines = [`Markdown 批注：${filename}`, ""];
  sorted.forEach((annotation, index) => {
    lines.push(`${index + 1}. \`${rangeLabel(annotation.filename, annotation.range)}\``);
    lines.push("");
    lines.push(quoteBlock(annotation.quote));
    lines.push("");
    lines.push(`   备注：${annotation.note.trim()}`);
    if (index < sorted.length - 1) lines.push("");
  });
  return lines.join("\n");
}
