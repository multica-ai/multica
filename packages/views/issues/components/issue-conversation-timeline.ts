import type { TaskMessagePayload } from "@multica/core/types/events";
import { buildTimeline, type TimelineItem } from "../../common/task-transcript/build-timeline";
import { redactSecrets } from "../../common/task-transcript/redact";
import type { TaskInteraction } from "@multica/core/chat";

export interface HumanTimelineEntry { id: string; text: string; chatText?: string; createdAt: string }

export interface ConversationTimelineItem extends TimelineItem { humanId?: string; humanContent?: string; runId?: string; divider?: boolean }

export function conversationHumanEntries(record: TaskInteraction | null | undefined, createdAt: string,
  labels: { human: string; interrupt: string; finish: string; pending: string }): HumanTimelineEntry[] {
  return [
    ...(record?.openingInput ? [{ id: "opening", createdAt, text: `**${labels.human}**\n\n${record.openingInput}`, chatText: record.openingInput }] : []),
    ...(record?.commands ?? []).filter((command) => command.kind !== "complete").map((command) => ({ id: command.id, createdAt: command.createdAt,
      text: `**${labels.human}**\n\n${command.kind === "interrupt" ? labels.interrupt : command.kind === "finish" ? labels.finish : command.text}\n\n${command.error || (!command.receipt ? labels.pending : "")}`,
      chatText: `${command.kind === "interrupt" ? labels.interrupt : command.kind === "finish" ? labels.finish : command.text}\n\n${command.error || (!command.receipt ? labels.pending : "")}`,
    })),
  ];
}

function timestamp(value: string | undefined): number {
  return value ? Date.parse(value) : NaN;
}

/** Keep native sequence order and split coalescing at human-input boundaries. */
export function issueConversationTimeline(messages: TaskMessagePayload[], human: HumanTimelineEntry[]): ConversationTimelineItem[] {
  const pending = [...human].sort((a, b) => timestamp(a.createdAt) - timestamp(b.createdAt));
  const items: ConversationTimelineItem[] = [];
  let buffer: TaskMessagePayload[] = [];
  let cursor = 0;
  const appendHuman = () => {
    items.push(...buildTimeline(buffer));
    buffer = [];
    const entry = pending[cursor]!;
    items.push({ seq: -cursor - 1, humanId: entry.id, humanContent: redactSecrets(entry.chatText ?? entry.text), type: "text", content: redactSecrets(entry.text), created_at: entry.createdAt });
    cursor++;
  };
  for (const message of [...messages].sort((a, b) => a.seq - b.seq)) {
    // RFC3339 offsets and fractional precision differ between the daemon and
    // server. Compare instants, not their wire-format strings.
    while (cursor < pending.length && timestamp(pending[cursor]!.createdAt) <= timestamp(message.created_at)) appendHuman();
    buffer.push(message);
  }
  items.push(...buildTimeline(buffer));
  buffer = [];
  while (cursor < pending.length) appendHuman();
  return items;
}
