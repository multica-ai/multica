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
 * Redact everything one record can put on screen.
 *
 * Whole values, before anything downstream clips, summarizes, splits or
 * highlights them. Every consumer cuts first and redacts second — `StepBody`
 * at its display clip, the argument summary at 120 characters, the diff
 * surface line by line — and a pattern only matches while both of its ends are
 * present. Redacting the record first is what makes all of that harmless, and
 * it only stays harmless while no raw record reaches a renderer.
 *
 * `input` is included, and it is not decoration: a tool's arguments are
 * displayed as prominently as its output — as the row summary, and as the diff
 * body for an edit, which is built by splitting `old_string` / `new_string`
 * into lines. Redacting after that split can never match a rule spanning lines,
 * such as a PEM block; redacting the source before it is split does.
 */
export function redactTimelineItem(item: TimelineItem): TimelineItem {
  return {
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
    input: item.input ? (redactUnknown(item.input) as Record<string, unknown>) : item.input,
  };
}

/**
 * Redact the strings inside an arbitrary tool-argument value.
 *
 * Tool inputs are whatever JSON the agent sent, so the shape is not ours to
 * assume: `patch_apply` nests per-file bodies, `Edit` keeps them flat. Only
 * strings are rewritten, and objects are rebuilt only when something in them
 * changed, so an unaffected input keeps its identity and the memos downstream
 * keep their hits.
 */
function redactUnknown(value: unknown): unknown {
  if (typeof value === "string") return redactSecrets(value);
  if (Array.isArray(value)) {
    let changed = false;
    const next = value.map((entry) => {
      const redacted = redactUnknown(entry);
      if (redacted !== entry) changed = true;
      return redacted;
    });
    return changed ? next : value;
  }
  if (value && typeof value === "object") {
    let changed = false;
    const next: Record<string, unknown> = {};
    for (const [key, entry] of Object.entries(value)) {
      const redacted = redactUnknown(entry);
      if (redacted !== entry) changed = true;
      next[key] = redacted;
    }
    return changed ? next : value;
  }
  return value;
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
