import type { Issue } from "@multica/core/types";
import { issueBehavesAsAny } from "@multica/core/issues";

/**
 * Whether an issue is still waiting on unfinished tasks. A done or cancelled
 * issue is not waiting for anything even when an earlier stage is still open,
 * so the Blocked filter and the red marker skip it. Mirrors the server's
 * blocked filter; an unresolved custom status stays visible (fail-safe).
 */
export function isIssueWaiting(issue: Pick<Issue, "status" | "status_category" | "blocked_count">): boolean {
  return (issue.blocked_count ?? 0) > 0 && !issueBehavesAsAny(issue, ["done", "closed"]);
}
