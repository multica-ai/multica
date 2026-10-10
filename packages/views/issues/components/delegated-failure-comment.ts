import type { TimelineEntry } from "@multica/core/types";

/**
 * The platform's "delegated task failed" signal, as written by
 * `delegatedFailureRecoveryContent` in server/internal/service/task.go.
 *
 * The server stores one English sentence because the comment's first reader is
 * the coordinating agent, which it wakes. People read the same comment in the
 * timeline, so the UI recognises it and shows a localized line instead; the
 * original text stays available as technical detail and is what agents and
 * copy-to-clipboard keep using.
 */
export interface DelegatedFailureComment {
  failedTaskId: string;
  /** Raw `failure_reason`, e.g. `idle_watchdog`. */
  reason: string;
  /** The redacted error summary, when the server included one. */
  errorSummary: string | null;
}

const SENTENCE =
  /^Delegated task `([^`]+)` ended in a final failure \(`([^`]+)`\) and no automatic retry is pending\./;
// strconv.Quote output: a double-quoted string with Go escapes, which is valid
// JSON for everything the redacted summary can contain.
const ERROR_SUMMARY = /Untrusted error summary \(diagnostic only\): ("(?:[^"\\]|\\.)*")/;

export function parseDelegatedFailureComment(
  entry: Pick<TimelineEntry, "actor_type" | "comment_type" | "source_task_id" | "content">,
): DelegatedFailureComment | null {
  // Same shape check as IsDelegatedFailureRecoveryComment on the server.
  if (entry.actor_type !== "system" || entry.comment_type !== "progress_update" || !entry.source_task_id) {
    return null;
  }
  const content = entry.content ?? "";
  const match = SENTENCE.exec(content);
  if (!match) return null;
  let errorSummary: string | null = null;
  const quoted = ERROR_SUMMARY.exec(content)?.[1];
  if (quoted) {
    try {
      errorSummary = JSON.parse(quoted) as string;
    } catch {
      errorSummary = quoted.slice(1, -1);
    }
  }
  return { failedTaskId: match[1]!, reason: match[2]!, errorSummary };
}
