import type { IssueAssigneeType } from "../types";

/**
 * Issues whose notifications the server just archived because the user
 * handled them (e.g. completed the issue or triggered an agent,
 * `inbox:batch-archived` with one of these reasons). The Inbox reads this to
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

/**
 * What the user did to an issue from its Inbox bar in this window. These
 * actions archive the notification on the server, so once the archive comes
 * back as the user's own (`takeInboxAutoArchive`), the bar can name the
 * action instead of a bare "Notification archived".
 */
export type InboxBarAction =
  | { type: "status"; status: string }
  | { type: "assignee"; assignee: { type: IssueAssigneeType; id: string } | null }
  | { type: "quick_action"; name: string };

const barActions = new Map<string, { action: InboxBarAction; at: number }>();

/** Record an action taken from `issueId`'s Inbox bar; a later one replaces it. */
export function noteInboxBarAction(issueId: string, action: InboxBarAction, now = Date.now()): void {
  for (const [id, entry] of barActions) {
    if (now - entry.at > AUTO_ARCHIVE_TTL_MS) barActions.delete(id);
  }
  barActions.set(issueId, { action, at: now });
}

/** The action just taken from `issueId`'s Inbox bar, if any. Consumes the record. */
export function takeInboxBarAction(issueId: string, now = Date.now()): InboxBarAction | null {
  const entry = barActions.get(issueId);
  barActions.delete(issueId);
  return entry && now - entry.at <= AUTO_ARCHIVE_TTL_MS ? entry.action : null;
}
