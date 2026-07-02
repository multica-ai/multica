import type { JSONContent } from "@tiptap/core";
import { LARGE_TEXT_THRESHOLD } from "@multica/ui/markdown";

/**
 * Shared cutoff for avoiding unsafe whole-document markdown parsing. Current
 * ContentEditor behavior bypasses parsing above this size; this chunker remains
 * as the bounded parser fallback if the bypass threshold is ever raised.
 */
export const MARKDOWN_CHUNK_THRESHOLD = LARGE_TEXT_THRESHOLD;

export interface MarkdownManagerLike {
  parse(markdown: string): JSONContent;
}

/**
 * Parse markdown into a ProseMirror JSON doc in chunks to dodge marked's O(n²).
 *
 * Splitting into k chunks and parsing each independently drops the cost to
 * O(n²/k) — marked only ever scans within one small chunk. Cuts happen only at
 * blank lines OUTSIDE fenced code blocks, so every chunk is a complete sequence
 * of block nodes; concatenating the per-chunk docs reproduces the same document
 * a single parse would have produced.
 *
 * Known limitation: a "loose" list (items separated by blank lines) straddling a
 * chunk boundary may render as two adjacent lists. Acceptable trade-off vs. a
 * minute-long freeze, and only reachable on documents past the threshold.
 */
export function parseMarkdownChunked(
  manager: MarkdownManagerLike,
  markdown: string,
  chunkSize = 16_000,
): JSONContent {
  const lines = markdown.split("\n");
  const chunks: string[] = [];
  let current: string[] = [];
  let currentLen = 0;
  let inFence = false;

  for (const line of lines) {
    // Track fenced code blocks so a cut never lands inside one.
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
    current.push(line);
    currentLen += line.length + 1;

    // Cut only at a paragraph boundary (blank line) outside a fence, once the
    // accumulated chunk is large enough.
    if (currentLen >= chunkSize && !inFence && line.trim() === "") {
      chunks.push(current.join("\n"));
      current = [];
      currentLen = 0;
    }
  }
  if (current.length) chunks.push(current.join("\n"));

  const merged: JSONContent = { type: "doc", content: [] };
  for (const chunk of chunks) {
    const doc = manager.parse(chunk);
    if (doc.content) merged.content!.push(...doc.content);
  }
  return merged;
}
