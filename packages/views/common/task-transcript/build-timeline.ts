import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactInputValues, redactSecrets } from "./redact";

/** Live status of a tool call, derived from whether its result has landed. */
export type ToolStatus = "running" | "done" | "error";

/** A unified timeline entry: tool calls, thinking, text, and errors in chronological order. */
export interface TimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
  /** Tool call id, pairs tool_use ↔ tool_result. Null on legacy rows. */
  call_id?: string;
  /** Set on a paired tool_result, and copied onto its tool_use. */
  is_error?: boolean;
  /**
   * On a tool_use: live status derived by pairToolCalls — "running" until its
   * result lands, then "done"/"error". Undefined on non-tool items.
   */
  status?: ToolStatus;
  /** On a paired tool_use: the seq of the tool_result it absorbed. */
  resultSeq?: number;
  /** On a paired tool_use: call→result wall-clock, when both timestamps exist. */
  duration_ms?: number;
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

function parseTimestamp(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const t = Date.parse(value);
  return Number.isNaN(t) ? undefined : t;
}

/**
 * Pair each tool_use with its tool_result and derive a live status, so the
 * renderer can show one merged card (running · done · error) instead of two
 * separate rows.
 *
 * Pairing prefers the real `call_id` (correct even for concurrent or
 * duplicate-named tools in one turn); when a result has no call_id — a legacy
 * row written before the id was threaded through the wire boundary (MUL-27) —
 * it falls back to FIFO positional matching (oldest still-open call), which is
 * safe because tools execute sequentially.
 *
 * The paired result's output is copied onto the tool_use and the standalone
 * tool_result item is dropped from the array (not just hidden at render time),
 * so `items.length`-based step counts stay in sync with the visible rows. This
 * MUST run before redactTimelineItems so the copied output is redacted in the
 * same pass.
 */
export function pairToolCalls(items: TimelineItem[]): TimelineItem[] {
  const out: TimelineItem[] = items.map((item) => ({ ...item }));
  // Indices of tool_use items still awaiting a result, in seq order.
  const open: number[] = [];
  const droppedResultSeqs = new Set<number>();

  for (let i = 0; i < out.length; i++) {
    const item = out[i];
    if (!item) continue;
    if (item.type === "tool_use") {
      item.status = "running";
      open.push(i);
      continue;
    }
    if (item.type !== "tool_result") continue;

    // Find the matching open tool_use: by call_id first, else FIFO oldest.
    let matchPos = -1;
    if (item.call_id) {
      matchPos = open.findIndex((idx) => out[idx]?.call_id === item.call_id);
    }
    if (matchPos === -1) matchPos = open.length > 0 ? 0 : -1;
    if (matchPos === -1) continue; // orphan result — leave it as a standalone row

    const callIdx = open[matchPos];
    open.splice(matchPos, 1);
    const call = callIdx !== undefined ? out[callIdx] : undefined;
    if (!call) continue;

    call.status = item.is_error ? "error" : "done";
    call.is_error = item.is_error ?? false;
    call.output = item.output;
    call.resultSeq = item.seq;
    const start = parseTimestamp(call.created_at);
    const end = parseTimestamp(item.created_at);
    if (start !== undefined && end !== undefined && end >= start) {
      call.duration_ms = end - start;
    }
    droppedResultSeqs.add(item.seq);
  }

  return out.filter((item) => !(item.type === "tool_result" && droppedResultSeqs.has(item.seq)));
}

function redactTimelineItems(items: TimelineItem[]): TimelineItem[] {
  return items.map((item) => ({
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
    // Tool `input` is rendered both as a summary (raw field reads) and as
    // pretty-printed JSON by the chat renderer, neither of which redacts on
    // its own. Deep-redact the values here so secrets passed as tool args
    // never reach either path. See redactInputValues.
    input: item.input ? redactInputValues(item.input) : item.input,
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
      call_id: msg.call_id,
      is_error: msg.is_error,
    });
  }
  // Pair tool calls to results BEFORE redaction so the output copied onto the
  // tool_use is deep-redacted in the same pass (MUL-27).
  return redactTimelineItems(pairToolCalls(coalesceTimelineItems(items)));
}
