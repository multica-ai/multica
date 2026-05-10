/**
 * Detects bare JSON object/array literals in markdown content that are not
 * already inside a fenced code block or inline code span, and wraps them in
 * ```json … ``` code blocks for proper syntax-highlighted display.
 *
 * This covers the common case where an agent outputs a raw error payload such
 * as:
 *   {"error":{"message":"openai_error","type":"api_error"},"status":400}
 *
 * which would otherwise render as an overflowing plain-text paragraph.
 *
 * Only single-line JSON is handled here (the dominant case in agent output).
 * Multi-line JSON already benefits from the code block treatment if the agent
 * wraps it in ``` fences.
 */

interface SkipRange {
  start: number;
  end: number;
}

function findSkipRanges(markdown: string): SkipRange[] {
  const ranges: SkipRange[] = [];
  // Fenced code blocks: ```...```
  const fencedRe = /```[\s\S]*?```/g;
  let m: RegExpExecArray | null;
  while ((m = fencedRe.exec(markdown)) !== null) {
    ranges.push({ start: m.index, end: m.index + m[0].length });
  }
  // Inline code: `...`
  const inlineRe = /`[^`\n]+`/g;
  while ((m = inlineRe.exec(markdown)) !== null) {
    // Skip if already inside a fenced block
    const pos = m.index;
    if (!ranges.some((r) => pos >= r.start && pos < r.end)) {
      ranges.push({ start: pos, end: pos + m[0].length });
    }
  }
  return ranges;
}

function isInCode(pos: number, ranges: SkipRange[]): boolean {
  return ranges.some((r) => pos >= r.start && pos < r.end);
}

/**
 * Wraps bare JSON lines with ```json … ``` fences.
 *
 * A line qualifies when ALL of the following hold:
 * 1. It is not inside an existing code block or inline code span.
 * 2. Its trimmed content starts with `{` and ends with `}`, or `[` and `]`.
 * 3. The trimmed content is valid JSON with a non-null object/array root.
 * 4. The content is long enough to be worth formatting (≥ 10 chars), which
 *    avoids false-positives on short expressions like `{}` that are unlikely
 *    to be the whole comment.
 */
export function preprocessJsonLiterals(markdown: string): string {
  if (!markdown) return markdown;

  // Quick bail-out: no bare JSON candidates on their own line.
  if (!/(?:^|\n)\s*[{[]/.test(markdown)) return markdown;

  const skipRanges = findSkipRanges(markdown);
  const lines = markdown.split("\n");
  const out: string[] = [];
  let offset = 0;

  for (const line of lines) {
    const lineStart = offset;
    offset += line.length + 1; // +1 for the \n that split() consumed

    const trimmed = line.trim();

    if (
      trimmed.length >= 10 &&
      !isInCode(lineStart, skipRanges) &&
      ((trimmed.startsWith("{") && trimmed.endsWith("}")) ||
        (trimmed.startsWith("[") && trimmed.endsWith("]")))
    ) {
      try {
        const parsed: unknown = JSON.parse(trimmed);
        if (parsed !== null && typeof parsed === "object") {
          const pretty = JSON.stringify(parsed, null, 2);
          out.push("```json");
          out.push(pretty);
          out.push("```");
          continue;
        }
      } catch {
        // Not valid JSON — leave the line unchanged.
      }
    }

    out.push(line);
  }

  return out.join("\n");
}
