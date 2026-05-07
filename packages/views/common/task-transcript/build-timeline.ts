import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";

/** A unified timeline entry: tool calls, thinking, text, errors, and activity reports. */
// CEREBRO-PATCH(timeline-activity-types): widen type union with cerebro report_activity kinds
export type TimelineItemType =
  | "tool_use"
  | "tool_result"
  | "thinking"
  | "text"
  | "error"
  | "decision"
  | "verification"
  | "blocker"
  | "dependency"
  | "note";

export interface TimelineItem {
  seq: number;
  type: TimelineItemType;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}

/** Build a chronologically ordered timeline from raw task messages. */
export function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
    items.push({
      seq: msg.seq,
      type: msg.type,
      tool: msg.tool,
      content: msg.content ? redactSecrets(msg.content) : msg.content,
      input: msg.input,
      output: msg.output ? redactSecrets(msg.output) : msg.output,
    });
  }
  return items.sort((a, b) => a.seq - b.seq);
}
