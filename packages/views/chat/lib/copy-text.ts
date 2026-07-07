import type { ChatMessage } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";

/**
 * Split an assistant timeline into three regions for the conductor-style fold:
 *   preface — text items before the first process item
 *   middle  — everything from the first to the last process item (inclusive),
 *             including thinking/tool/error rows and explicit commentary text
 *   final   — text items after the last process item
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
  const isProcessItem = (item: ChatTimelineItem) =>
    item.type !== "text" || item.text_phase === "commentary";

  const firstProcessIdx = items.findIndex(isProcessItem);
  if (firstProcessIdx === -1) {
    return { preface: [], middle: [], final: items };
  }
  let lastProcessIdx = items.length - 1;
  while (lastProcessIdx >= 0 && !isProcessItem(items[lastProcessIdx]!)) {
    lastProcessIdx--;
  }
  return {
    preface: items.slice(0, firstProcessIdx),
    middle: items.slice(firstProcessIdx, lastProcessIdx + 1),
    final: items.slice(lastProcessIdx + 1),
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
