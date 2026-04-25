import type { Issue } from "@multica/core/types";
import { isTerminalStatus } from "@multica/core/issues/config";

export const DEFAULT_AUTO_HIDE_DAYS = 7;

export function isAutoHidden(issue: Issue, autoHideDays = DEFAULT_AUTO_HIDE_DAYS): boolean {
  if (!isTerminalStatus(issue.status)) return false;
  if (!issue.completed_at) return false;
  const completedAt = new Date(issue.completed_at);
  const cutoff = new Date(Date.now() - autoHideDays * 24 * 60 * 60 * 1000);
  return completedAt < cutoff;
}
