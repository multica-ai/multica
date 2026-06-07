export interface CodeFence {
  marker: "`" | "~";
  length: number;
}

export function parseOpeningFenceLine(text: string): CodeFence | null {
  const match = text.match(/^ {0,3}(`{3,}|~{3,})/);
  if (!match) return null;
  const sequence = match[1];
  if (!sequence) return null;
  return {
    marker: sequence[0] as "`" | "~",
    length: sequence.length,
  };
}

export function isClosingFenceLine(text: string, fence: CodeFence): boolean {
  const match = text.match(/^ {0,3}(`{3,}|~{3,})\s*$/);
  if (!match) return false;
  const sequence = match[1];
  if (!sequence) return false;
  return sequence[0] === fence.marker && sequence.length >= fence.length;
}

export function hasMatchingFencePair(firstLine: string, lastLine: string): boolean {
  const fence = parseOpeningFenceLine(firstLine);
  return fence !== null && isClosingFenceLine(lastLine, fence);
}

export function findInlineCodeRanges(text: string): Array<{ from: number; to: number }> {
  const ranges: Array<{ from: number; to: number }> = [];
  let index = 0;

  while (index < text.length) {
    const start = text.indexOf("`", index);
    if (start === -1) break;

    const escapedBackslashes = text.slice(0, start).match(/\\+$/)?.[0].length ?? 0;
    if (escapedBackslashes % 2 === 1) {
      index = start + 1;
      continue;
    }

    let delimiterLength = 1;
    while (text[start + delimiterLength] === "`") delimiterLength++;

    const delimiter = "`".repeat(delimiterLength);
    let searchFrom = start + delimiterLength;
    let end = -1;

    while (searchFrom < text.length) {
      const candidate = text.indexOf(delimiter, searchFrom);
      if (candidate === -1) break;
      const candidateEscapes = text.slice(0, candidate).match(/\\+$/)?.[0].length ?? 0;
      if (candidateEscapes % 2 === 0) {
        end = candidate;
        break;
      }
      searchFrom = candidate + delimiterLength;
    }

    if (end === -1) {
      index = start + delimiterLength;
      continue;
    }

    ranges.push({ from: start, to: end + delimiterLength });
    index = end + delimiterLength;
  }

  return ranges;
}
