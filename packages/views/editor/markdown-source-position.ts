import type { MarkdownSourceSelection, SourcePoint, SourceRange } from "./markdown-annotation-types";

interface LineStart {
  offset: number;
  line: number;
}

function codePointLength(value: string): number {
  return Array.from(value).length;
}

export function buildLineStarts(source: string): LineStart[] {
  const starts: LineStart[] = [{ offset: 0, line: 1 }];
  for (let i = 0; i < source.length; i++) {
    if (source[i] === "\n") {
      starts.push({ offset: i + 1, line: starts.length + 1 });
    }
  }
  return starts;
}

export function offsetToSourcePoint(source: string, offset: number): SourcePoint {
  const safeOffset = Math.max(0, Math.min(offset, source.length));
  const lineStarts = buildLineStarts(source);
  let lo = 0;
  let hi = lineStarts.length - 1;
  while (lo <= hi) {
    const mid = Math.floor((lo + hi) / 2);
    if (lineStarts[mid]!.offset <= safeOffset) {
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }
  const line = lineStarts[Math.max(0, hi)]!;
  const linePrefix = source.slice(line.offset, safeOffset);
  return {
    line: line.line,
    character: codePointLength(linePrefix) + 1,
    offset: safeOffset,
  };
}

export function sourceRangeFromOffsets(source: string, startOffset: number, endExclusiveOffset: number): SourceRange {
  const start = Math.max(0, Math.min(startOffset, source.length));
  const endExclusive = Math.max(start, Math.min(endExclusiveOffset, source.length));
  const endInclusive = Math.max(start, endExclusive - 1);
  return {
    start: offsetToSourcePoint(source, start),
    end: offsetToSourcePoint(source, endInclusive),
  };
}

function sourceElementFromNode(node: Node | null): HTMLElement | null {
  if (!node) return null;
  const element = node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement;
  return element?.closest<HTMLElement>("[data-md-start][data-md-end]") ?? null;
}

function textBeforePoint(container: HTMLElement, node: Node, offset: number): string {
  const range = document.createRange();
  range.selectNodeContents(container);
  range.setEnd(node, offset);
  return range.toString();
}

function pointOffset(container: HTMLElement, node: Node, offset: number): number | null {
  const rawStart = container.dataset.mdStart;
  if (!rawStart) return null;
  const start = Number(rawStart);
  if (!Number.isFinite(start)) return null;
  return start + textBeforePoint(container, node, offset).length;
}

export function selectionToMarkdownSourceSelection(
  root: HTMLElement,
  source: string,
  selection: Selection | null,
): MarkdownSourceSelection | null {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;

  const startElement = sourceElementFromNode(range.startContainer);
  const endElement = sourceElementFromNode(range.endContainer);
  if (!startElement || !endElement) return null;
  if (!root.contains(startElement) || !root.contains(endElement)) return null;

  const startOffset = pointOffset(startElement, range.startContainer, range.startOffset);
  const endOffset = pointOffset(endElement, range.endContainer, range.endOffset);
  if (startOffset == null || endOffset == null || endOffset <= startOffset) return null;

  const quote = selection.toString();
  if (!quote.trim()) return null;

  return {
    quote,
    range: sourceRangeFromOffsets(source, startOffset, endOffset),
  };
}
