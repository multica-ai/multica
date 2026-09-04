import type { AgentTask } from "@multica/core/types";
import { useT } from "../../i18n";
import { stripMentionMarkdown } from "../utils/strip-mention-markdown";

// Display labels shared by every surface that lists an issue's agent runs:
// the execution log rows and the usage-detail dialog. They live here rather
// than in either component so the two can't drift — a run must read the same
// in the sidebar and in the table it opens.

/**
 * Human label for what caused this run.
 *
 * Primary source: the canonical snapshot taken at task creation time
 * (comment text / autopilot title). Survives source edits/deletes and is
 * information-dense — far better than a structural label.
 *
 * Retry tasks inherit the parent's trigger_summary on the DB side (so the
 * snapshot survives across attempts), but a row that just shows the inherited
 * summary is indistinguishable from its parent. We prepend "Retry #N" when
 * parent_task_id is set so retries are scannable as retries even when their
 * summary is inherited.
 *
 * Fallback chain for legacy tasks created before the snapshot field shipped,
 * OR for sources we don't snapshot (direct assignment / chat): degrade to a
 * short structural label by trigger source. New tasks (post-061 migration)
 * almost always hit the snapshot path.
 */
export function useTriggerText(task: AgentTask): string {
  const { t } = useT("issues");
  const isRetry = !!task.parent_task_id;
  const retryPrefix = isRetry
    ? task.attempt && task.attempt > 1
      ? t(($) => $.execution_log.trigger_retry_attempt_prefix, { attempt: task.attempt })
      : t(($) => $.execution_log.trigger_retry_prefix)
    : "";

  if (task.trigger_summary) return retryPrefix + stripMentionMarkdown(task.trigger_summary);
  if (isRetry) {
    return task.attempt && task.attempt > 1
      ? t(($) => $.execution_log.trigger_retry_attempt, { attempt: task.attempt })
      : t(($) => $.execution_log.trigger_retry);
  }
  if (task.autopilot_run_id) return t(($) => $.execution_log.trigger_autopilot);
  if (task.trigger_comment_id) return t(($) => $.execution_log.trigger_comment);
  return t(($) => $.execution_log.trigger_initial);
}

export function useStatusLabel(status: AgentTask["status"]): string {
  const { t } = useT("issues");
  switch (status) {
    case "queued": return t(($) => $.execution_log.status_queued);
    case "dispatched": return t(($) => $.execution_log.status_dispatched);
    case "waiting_local_directory":
      return t(($) => $.execution_log.status_waiting_local_directory);
    case "running": return t(($) => $.execution_log.status_running);
    case "completed": return t(($) => $.execution_log.status_completed);
    case "failed": return t(($) => $.execution_log.status_failed);
    case "cancelled": return t(($) => $.execution_log.status_cancelled);
    default: return status;
  }
}

/**
 * Wall-clock duration of a finished run, or "" when the task never started or
 * never finished. Deliberately not computed for running tasks: the execution
 * log already ticks a live elapsed timer for those, and a second, frozen
 * figure next to it would contradict it.
 */
export function taskDurationMs(task: AgentTask): number | null {
  const startIso = task.started_at ?? task.dispatched_at;
  if (!startIso || !task.completed_at) return null;
  const start = new Date(startIso).getTime();
  const end = new Date(task.completed_at).getTime();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return null;
  return end - start;
}
