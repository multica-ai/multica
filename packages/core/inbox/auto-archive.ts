/**
 * Issues whose notifications the server just archived because the user
 * handled them: completed the issue, commented on it, changed its status or reassigned it
 * (`inbox:batch-archived` with one of these reasons). The Inbox reads this to
 * tell such an archive apart from any other way the open notification left,
 * such as another window archiving it.
 */
const AUTO_ARCHIVE_REASONS = new Set(["issue_status_terminal", "actor_acted"]);

/** The archive lands within moments of the action; anything older is stale. */
const AUTO_ARCHIVE_TTL_MS = 30_000;

const archivedAt = new Map<string, number>();

/** Record an `inbox:batch-archived` payload, if it is an archive the user caused. */
export function noteInboxAutoArchive(
  payload: { issue_id?: string; reason?: string },
  now = Date.now(),
): void {
  if (!payload.issue_id || !payload.reason || !AUTO_ARCHIVE_REASONS.has(payload.reason)) return;
  for (const [issueId, at] of archivedAt) {
    if (now - at > AUTO_ARCHIVE_TTL_MS) archivedAt.delete(issueId);
  }
  archivedAt.set(payload.issue_id, now);
}

/** Whether the user's own action just archived `issueId`'s notifications. Consumes the record. */
export function takeInboxAutoArchive(issueId: string, now = Date.now()): boolean {
  const at = archivedAt.get(issueId);
  archivedAt.delete(issueId);
  return at !== undefined && now - at <= AUTO_ARCHIVE_TTL_MS;
}
