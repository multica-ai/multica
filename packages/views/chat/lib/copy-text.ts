import type { ChatMessage } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { stripChatQuickActionsProtocol } from "./quick-actions";

/**
 * Split an assistant timeline into three regions for the conductor-style fold:
 *   preface — text items before the first thinking/tool/error item
 *   middle  — everything from the first to the last non-text item (inclusive),
 *             including any text items sandwiched between them
 *   final   — text items after the last non-text item
 *
 * While streaming, UI renders preface above the outer fold, middle inside the
 * fold, and final below it. Once settled, preface + middle become process
 * history and the canonical chat message replaces final.
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
 * Canonical completed answer from the persisted chat message. Surface-specific
 * transforms still apply so hidden protocols stay out of the rendered and
 * copied answer.
 */
export function canonicalAnswerText(
  message: ChatMessage,
  transformContent?: (content: string) => string,
): string {
  const content = stripChatQuickActionsProtocol(message.content ?? "");
  return transformContent ? transformContent(content) : content;
}

/**
 * Markdown source for Copy. Completed messages use canonical content instead
 * of inferring an answer from transcript position. Legacy rows with empty
 * content retain the previous visible-timeline fallback.
 */
export function extractCopyText(
  message: ChatMessage,
  timeline: ChatTimelineItem[],
  transformContent?: (content: string) => string,
): string {
  const canonical = canonicalAnswerText(message, transformContent);
  if (canonical.trim()) return canonical;

  const { preface, final } = splitTimeline(timeline);
  return [...preface, ...final]
    .map((item) => item.content ?? "")
    .filter((content) => content.length > 0)
    .join("\n\n");
}
