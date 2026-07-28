import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";

/** A unified timeline entry: tool calls, thinking, text, and errors in chronological order. */
export interface TimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

function canMergeStreamingText(prev: TimelineItem, next: TimelineItem): boolean {
  return (prev.type === "thinking" || prev.type === "text") && prev.type === next.type;
}

function splitTextThinkingTags(item: TimelineItem): TimelineItem[] {
  if (item.type !== "text" || !item.content) return [item];

  const tagPattern = /<\/?thinking>/gi;
  const matches = Array.from(item.content.matchAll(tagPattern));
  if (matches.length === 0) return [item];

  const hasBalancedPair = matches.some((match, index) => {
    if (match[0].toLowerCase() !== "<thinking>") return false;
    return matches.slice(index + 1).some((next) => next[0].toLowerCase() === "</thinking>");
  });
  if (!hasBalancedPair) return [item];

  const out: TimelineItem[] = [];
  let cursor = 0;
  let inThinking = false;
  let segment = 0;

  const pushSegment = (type: "text" | "thinking", content: string) => {
    if (!content) return;
    out.push({
      ...item,
      seq: item.seq + segment / 1_000_000,
      type,
      content,
    });
    segment += 1;
  };

  for (const match of matches) {
    const tag = match[0].toLowerCase();
    const index = match.index ?? 0;

    pushSegment(inThinking ? "thinking" : "text", item.content.slice(cursor, index));
    cursor = index + match[0].length;

    if (tag === "<thinking>") {
      inThinking = true;
    } else {
      inThinking = false;
    }
  }

  pushSegment(inThinking ? "thinking" : "text", item.content.slice(cursor));

  return out.length > 0 ? out : [item];
}

function normalizeTimelineItems(items: TimelineItem[]): TimelineItem[] {
  const coalesced = coalesceTimelineItems(items);
  return coalesceTimelineItems(coalesced.flatMap(splitTextThinkingTags));
}

/** Merge adjacent text/thinking fragments that were split only by daemon flush timing. */
export function coalesceTimelineItems(items: TimelineItem[]): TimelineItem[] {
  const sorted = [...items].sort((a, b) => a.seq - b.seq);
  const out: TimelineItem[] = [];

  for (const item of sorted) {
    const prev = out[out.length - 1];
    if (prev && canMergeStreamingText(prev, item)) {
      out[out.length - 1] = {
        ...prev,
        content: `${prev.content ?? ""}${item.content ?? ""}`,
        created_at: item.created_at ?? prev.created_at,
      };
      continue;
    }
    out.push(item);
  }

  return out;
}

export function appendTimelineItem(items: TimelineItem[], item: TimelineItem): TimelineItem[] {
  return normalizeTimelineItems([...items, item]);
}

function redactTimelineItems(items: TimelineItem[]): TimelineItem[] {
  return items.map((item) => ({
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
  }));
}

/** Build a chronologically ordered timeline from raw task messages. */
export function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
    items.push({
      seq: msg.seq,
      type: msg.type,
      tool: msg.tool,
      content: msg.content,
      input: msg.input,
      output: msg.output,
      created_at: msg.created_at,
    });
  }
  return redactTimelineItems(normalizeTimelineItems(items));
}
