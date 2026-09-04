import type { ChatMessage } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";

/**
 * Split an assistant timeline into three regions for the conductor-style fold:
 *   preface — text items before the first thinking/tool/error item
 *   middle  — everything from the first to the last non-text item (inclusive),
 *             including any text items sandwiched between them
 *   final   — text items after the last non-text item
 *
 * UI renders preface above the outer fold, middle inside the fold (with each
 * row keeping its existing inner Collapsible), and final below the fold.
 * Copy concatenates preface + final — the fold's contents are intentionally
 * omitted, mirroring what's visible when the fold is closed.
 */
export function splitTimeline(items: ChatTimelineItem[]): {
  preface: ChatTimelineItem[];
  middle: ChatTimelineItem[];
  final: ChatTimelineItem[];
} {
  const firstNonTextIdx = items.findIndex((i) => i.type !== "text");
  if (firstNonTextIdx === -1) {
    return { preface: [], middle: [], final: items };
  }
  let lastNonTextIdx = items.length - 1;
  while (lastNonTextIdx >= 0 && items[lastNonTextIdx]!.type === "text") {
    lastNonTextIdx--;
  }
  return {
    preface: items.slice(0, firstNonTextIdx),
    middle: items.slice(firstNonTextIdx, lastNonTextIdx + 1),
    final: items.slice(lastNonTextIdx + 1),
  };
}

/**
 * Markdown source the Copy action puts on the clipboard. By design this is
 * the user-visible answer only — anything inside the outer fold (thinking,
 * tool calls, sandwiched intermediate text) is dropped. Falls back to
 * `message.content` for legacy messages without a timeline and for the
 * pathological all-non-text shape so Copy never produces an empty string.
 */
export function extractCopyText(
  message: ChatMessage,
  timeline: ChatTimelineItem[],
): string {
  if (timeline.length === 0) return message.content ?? "";
  const { preface, final } = splitTimeline(timeline);
  const pieces = [...preface, ...final]
    .map((i) => i.content ?? "")
    .filter((s) => s.length > 0);
  if (pieces.length === 0) return message.content ?? "";
  return pieces.join("\n\n");
}

/**
 * Whether the timeline's trailing text (`final`) is the same answer the
 * persisted row carries in `message.content`, compared after normalization.
 *
 * The two sources describe one turn, but they are produced by different write
 * paths, so incidental formatting differences (trailing newlines, CRLF vs LF,
 * blank-line separators between segments, whitespace runs) must not read as a
 * divergence. Any real text difference does: a truncated tail (final is a
 * strict prefix of content — a daemon tail-flush lost the last chunks) or a
 * stale surviving frame (the final answer never persisted; a mid-run
 * narration text sits after the last non-text item instead). This is the
 * Fallback trigger: when the two disagree and content is non-empty, the
 * render path uses content as the authoritative answer.
 */
export function timelineFinalMatchesContent(
  finalItems: ChatTimelineItem[],
  content: string | null | undefined,
): boolean {
  const normalize = (s: string) => s.replace(/\s+/g, " ").trim();
  return (
    normalize(finalItems.map((i) => i.content ?? "").join("")) ===
    normalize(content ?? "")
  );
}
