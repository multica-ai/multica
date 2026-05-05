"use client";

// CEREBRO-PATCH(chat-status-line): cerebro modification of upstream file

import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import type { ChatTimelineItem } from "@multica/core/chat";
import type { TaskMessagePayload } from "@multica/core/types";
import { getToolSummary } from "./tool-summary";

/**
 * Slim "doing X now" status line shown above ChatInput while a task is
 * streaming. Mirrors the latest event from the live task timeline so the
 * user has an at-a-glance signal that the agent is alive — even when all
 * tool groups are collapsed by the time they look. When the latest event
 * is text, we render nothing (the streaming text in the timeline is the
 * status).
 */
export function ChatStatusLine({ pendingTaskId }: { pendingTaskId: string | null }) {
  const { data: messages } = useQuery({
    ...taskMessagesOptions(pendingTaskId ?? ""),
    enabled: !!pendingTaskId,
  });

  if (!pendingTaskId) return null;

  const latest = pickLatestStatusEvent(messages ?? []);
  // No interesting status to show yet (just text streaming or nothing) —
  // skip the line entirely instead of taking up vertical space with a
  // permanent placeholder.
  if (!latest) return null;

  return (
    <div
      className="mx-auto flex w-full max-w-4xl items-center gap-1.5 truncate px-5 pb-1 text-[11px] text-muted-foreground"
      role="status"
      aria-live="polite"
    >
      <Loader2 className="size-3 shrink-0 animate-spin" />
      {latest.kind === "thinking" ? (
        <span className="italic truncate">Thinking…</span>
      ) : (
        <>
          <span className="font-medium text-foreground shrink-0">{latest.tool}</span>
          {latest.summary && <span className="truncate">{latest.summary}</span>}
        </>
      )}
    </div>
  );
}

type LatestStatus =
  | { kind: "tool"; tool: string; summary: string }
  | { kind: "thinking" };

/**
 * Pick the line to render in the status bar. To avoid flicker between
 * "Thinking…" and a tool name on every event, we lock onto the most
 * recent `tool_use` when one exists in the task and only fall back to
 * "Thinking…" when no tool has been called yet. `tool_result` is always
 * skipped — its triggering `tool_use` already supplies the label —
 * and a trailing `text` (final answer) suppresses the line entirely
 * because the streaming text in the timeline is the status itself.
 */
function pickLatestStatusEvent(messages: TaskMessagePayload[]): LatestStatus | null {
  // If the most recent event is plain text the agent has moved on to
  // emitting its reply — drop the line so we don't outrank the answer.
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]!;
    if (m.type === "text") return null;
    if (m.type === "tool_use" || m.type === "thinking" || m.type === "tool_result") break;
  }

  // Otherwise prefer the latest tool_use as the stable label.
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]!;
    if (m.type !== "tool_use") continue;
    const item: ChatTimelineItem = {
      seq: m.seq,
      type: m.type,
      tool: m.tool,
      input: m.input,
    };
    return {
      kind: "tool",
      tool: m.tool ?? "tool",
      summary: getToolSummary(item),
    };
  }

  // No tool_use yet — show "Thinking…" if any thinking event exists.
  if (messages.some((m) => m.type === "thinking")) return { kind: "thinking" };
  return null;
}
