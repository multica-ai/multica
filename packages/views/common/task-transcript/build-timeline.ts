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
  /**
   * Whether the stored `output` dropped bytes at the source (`tool_result`
   * only). `undefined` means unknown — the record predates the flag or came
   * from an older daemon — and must never be rendered as "complete".
   */
  output_truncated?: boolean;
  created_at?: string;
}

function canMergeStreamingText(prev: TimelineItem, next: TimelineItem): boolean {
  return (prev.type === "thinking" || prev.type === "text") && prev.type === next.type;
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
  return coalesceTimelineItems([...items, item]);
}

/**
 * Run the display safety net over every message body.
 *
 * This is the whole cost of building a timeline — coalescing a 3000-message
 * transcript is ~0.1ms, redacting it is ~23ms, because every pattern scans
 * every byte of every `content` and `output`. That is fine once per opened
 * transcript and ruinous on a live run, whose timeline is rebuilt on each
 * 100ms flush window (MUL-7227). Callers that redact the bounded strings they
 * actually render should build the structure and skip this.
 */
export function redactTimelineItems(items: TimelineItem[]): TimelineItem[] {
  return items.map(redactTimelineItem);
}

/**
 * Redact one record's bodies.
 *
 * Whole bodies, before anything downstream clips or summarizes them. Every
 * consumer of a body cuts first and redacts second — `StepBody` at its display
 * clip, the step summaries at 200 characters — so a pattern that spans the cut
 * loses the tail it needs to match and the head renders. Redacting the record
 * first is what made that ordering harmless, and it stays harmless as long as
 * a raw record never reaches a renderer.
 */
export function redactTimelineItem(item: TimelineItem): TimelineItem {
  return {
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
  };
}

/**
 * Whether this record's stored output is known to have dropped bytes.
 *
 * Only tool results carry the measurement, and an empty output has nothing to
 * be missing — truncation keeps the first 8 KiB, so a preview that dropped
 * bytes is never empty.
 */
export function isOutputTruncated(item: TimelineItem): boolean {
  return (
    item.type === "tool_result" && (item.output?.length ?? 0) > 0 && item.output_truncated === true
  );
}

/**
 * Chronological timeline of the raw message bodies, without redaction.
 *
 * Only for callers that redact at every point where they put one of these
 * strings on screen — a caller that renders `content` or `output` directly
 * wants `buildTimeline`. `input` and `output_truncated` are untouched by
 * redaction either way.
 */
export function buildTimelineStructure(msgs: TaskMessagePayload[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
    items.push({
      seq: msg.seq,
      type: msg.type,
      tool: msg.tool,
      content: msg.content,
      input: msg.input,
      output: msg.output,
      output_truncated: msg.output_truncated,
      created_at: msg.created_at,
    });
  }
  return coalesceTimelineItems(items);
}

/** Build a chronologically ordered, redacted timeline from raw task messages. */
export function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  return redactTimelineItems(buildTimelineStructure(msgs));
}
