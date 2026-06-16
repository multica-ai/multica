import type { AgentTask, ChatMessage } from "@multica/core/types";
import { stripMentionMarkdown } from "../../issues/utils/strip-mention-markdown";
import { redactSecrets } from "../../common/task-transcript/redact";

/**
 * Adapter: an agent run (`AgentTask`) → the `ChatMessage[]` the chat renderer
 * (`ChatMessageList`) consumes, so the visual execution-history view can reuse
 * the live-chat renderer for a read-only run transcript.
 *
 * Shape rules (load-bearing — see the plan's Eng review):
 *
 *  - Every run opens with exactly ONE synthetic `role:"user"` bubble carrying
 *    the run's `trigger_summary` ("here's what was asked"). Falls back to empty
 *    when there's no snapshot; the caller decides whether to render an empty
 *    state.
 *
 *  - A TERMINAL run also emits exactly ONE `role:"assistant"` message keyed by
 *    `task_id`. `AssistantMessage` re-fetches the whole timeline by `task_id`
 *    and does its own preface/process-fold/final splitting, so emitting more
 *    than one assistant message would re-render the entire run once per bubble.
 *
 *  - An IN-FLIGHT run emits the user bubble ONLY. The live stream is rendered
 *    through `ChatMessageList`'s `pendingTask` slot; emitting the assistant
 *    message early would trip `pendingAlreadyPersisted` and suppress the live
 *    timeline.
 */

// Terminal = the run has reached a final state and will receive no further
// task:message writes. Everything else is still in flight.
const TERMINAL_STATUSES: ReadonlySet<AgentTask["status"]> = new Set([
  "completed",
  "failed",
  "cancelled",
]);

export function isTerminalRun(task: AgentTask): boolean {
  return TERMINAL_STATUSES.has(task.status);
}

/**
 * Wall-clock run duration in ms, derived from the task timestamps (issue runs
 * have no chat_message carrying a server-computed `elapsed_ms`). Returns null
 * when either endpoint is missing or the math is non-positive, so the footer
 * simply omits the duration rather than showing "0s" / a negative value.
 */
export function deriveElapsedMs(task: AgentTask): number | null {
  const start = task.started_at ?? task.dispatched_at ?? task.created_at;
  const end = task.completed_at;
  if (!start || !end) return null;
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (!Number.isFinite(ms) || ms <= 0) return null;
  return ms;
}

function triggerText(task: AgentTask): string {
  // Mirror the execution-log row: raw mention markdown reads oddly as chat
  // prose, so strip it before rendering the user bubble.
  return task.trigger_summary ? stripMentionMarkdown(task.trigger_summary) : "";
}

export interface BuildRunChatOptions {
  /**
   * Whether to include the trigger user bubble. Defaults to true. The bubble
   * is skipped automatically when there's no trigger text to show.
   */
  includeTrigger?: boolean;
}

/**
 * Build the `ChatMessage[]` for a single run. See the module doc for the
 * terminal-vs-live shape contract.
 */
export function buildRunChat(
  task: AgentTask,
  options: BuildRunChatOptions = {},
): ChatMessage[] {
  const { includeTrigger = true } = options;
  const messages: ChatMessage[] = [];

  const trigger = triggerText(task);
  if (includeTrigger && trigger) {
    messages.push({
      id: `run-${task.id}-trigger`,
      chat_session_id: task.chat_session_id ?? "",
      role: "user",
      content: trigger,
      task_id: null,
      created_at: task.created_at,
    });
  }

  // In-flight: user bubble only; the live timeline comes through pendingTask.
  if (!isTerminalRun(task)) {
    return messages;
  }

  // Terminal: exactly one assistant message keyed by task_id. AssistantMessage
  // re-fetches and renders the full timeline itself.
  const failureReason =
    task.status === "failed" && task.failure_reason
      ? task.failure_reason
      : null;

  messages.push({
    id: `run-${task.id}-assistant`,
    chat_session_id: task.chat_session_id ?? "",
    role: "assistant",
    // `content` is only a fallback when the timeline is empty; for a failed
    // run it's the raw error the FailureBubble shows under "Show details".
    // `task.error` is a top-level field that never passes through
    // `buildTimeline`'s deep redaction, and agent errors routinely echo the
    // failing command, env, or connection string — so run it through the
    // client-side secret net here, at the source, before it reaches the bubble.
    content: task.error ? redactSecrets(task.error) : "",
    task_id: task.id,
    created_at: task.completed_at ?? task.created_at,
    failure_reason: failureReason,
    elapsed_ms: deriveElapsedMs(task),
  });

  return messages;
}
