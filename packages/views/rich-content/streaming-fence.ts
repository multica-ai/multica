import { fromMarkdown } from "mdast-util-from-markdown";
import type { Root, RootContent, Code } from "mdast";

/**
 * Fenced-code closedness gate for streaming RichContent (MUL-4922).
 *
 * A Mermaid diagram or sandboxed HTML iframe must never be instantiated from a
 * fence the author is still typing: mid-stream the source is syntactically
 * incomplete, so Mermaid would throw on every keystroke and an iframe would be
 * created and destroyed dozens of times per second. Only a *closed* fence may
 * upgrade to a rich block.
 *
 * This module answers exactly one question — "which fenced code blocks in this
 * source are closed?" — and returns offsets. It deliberately does NOT render.
 * Rendering stays in the single ReactMarkdown pipeline; a gate that rendered
 * would become the second renderer this sweep exists to delete.
 *
 * Closedness is derived from a real CommonMark parse rather than a
 * `startsWith("```")` scan, so indented fences, tilde fences, longer-than-three
 * markers, fences inside list items, and nested fences are all judged the way
 * the actual Markdown parser judges them.
 */

// Opening fence: up to 3 leading spaces, then >= 3 backticks or tildes.
const FENCE_OPEN_RE = /^ {0,3}(`{3,}|~{3,})/;

function isCodeNode(node: RootContent | Root): node is Code {
  return node.type === "code";
}

function collectCodeNodes(node: Root | RootContent, out: Code[]): void {
  if (isCodeNode(node)) {
    out.push(node);
    return;
  }
  const children = (node as { children?: RootContent[] }).children;
  if (children) {
    for (const child of children) collectCodeNodes(child, out);
  }
}

/**
 * True when `raw` (the exact source span of one mdast `code` node) ends with a
 * closing fence valid for its own opener.
 *
 * CommonMark requires the closer to use the same character as the opener and be
 * at least as long, so an opener of ```` closed by ``` is still OPEN — the
 * shorter run is content, not a terminator. Length is checked explicitly
 * because a regex that only matched "a line of >=3 markers" reports that case
 * closed and would let a half-written diagram reach Mermaid.
 */
function endsWithClosingFence(raw: string): boolean {
  const lines = raw.split("\n");
  const openMatch = FENCE_OPEN_RE.exec(lines[0] ?? "");
  // No opener means an indented (4-space) code block. It has no fence to leave
  // dangling, and carries no info string, so it can never dispatch to a rich
  // block — treat it as settled.
  if (!openMatch?.[1]) return true;

  const openFence = openMatch[1];
  const marker = openFence[0] as "`" | "~";
  // A single line cannot be both opener and closer.
  if (lines.length < 2) return false;

  // mdast may or may not include the trailing newline in the node span.
  const lastLine = (lines[lines.length - 1] === "" ? lines[lines.length - 2] : lines[lines.length - 1]) ?? "";
  const closeMatch = /^ {0,3}([`~]+)[ \t]*$/.exec(lastLine);
  if (!closeMatch?.[1]) return false;

  const closeFence = closeMatch[1];
  return closeFence[0] === marker && closeFence.length >= openFence.length;
}

/**
 * Start offsets of every fenced code block that is closed in `source`.
 *
 * `source` MUST be the final processed Markdown handed to ReactMarkdown — the
 * same string preprocess/highlight already rewrote. Offsets computed against
 * the raw pre-preprocess text would drift and mis-match the wrong node.
 */
export function computeClosedFenceOffsets(source: string): Set<number> {
  const closed = new Set<number>();
  if (!source) return closed;

  let tree: Root;
  try {
    tree = fromMarkdown(source);
  } catch {
    // A parse failure must not upgrade anything: fall back to "nothing is
    // closed", which shows source instead of running Mermaid on bad input.
    return closed;
  }

  const codes: Code[] = [];
  collectCodeNodes(tree, codes);

  for (const node of codes) {
    const start = node.position?.start.offset;
    const end = node.position?.end.offset;
    if (start == null || end == null) continue;
    if (endsWithClosingFence(source.slice(start, end))) closed.add(start);
  }

  return closed;
}
