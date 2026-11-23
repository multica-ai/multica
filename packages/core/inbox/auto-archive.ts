import type { IssueAssigneeType } from "../types";

/**
 * Issues whose notifications the server just archived because the user
 * handled them (e.g. completed the issue or triggered an agent,
 * `inbox:batch-archived` with one of these reasons). The Inbox reads this to
 * tell such an archive apart from any other way the open notification left,
 * such as another window archiving it, and to distinguish triggering an agent
 * (which stays in place so the user sees the run start) from completing an
 * issue or other handled edits.
 */
export type InboxAutoArchiveReason =
  | "issue_status_terminal"
  | "actor_acted"
  | "agent_triggered";

const AUTO_ARCHIVE_REASONS = new Set<string>([
  "issue_status_terminal",
  "actor_acted",
  "agent_triggered",
]);

function resolveAutoArchiveReason(payload: {
  reason?: string;
  trigger?: string;
}): InboxAutoArchiveReason | null {
  if (!payload.reason || !AUTO_ARCHIVE_REASONS.has(payload.reason)) return null;
  if (payload.reason === "actor_acted" && payload.trigger === "agent_triggered") {
    return "agent_triggered";
  }
  return payload.reason as InboxAutoArchiveReason;
}

/** The archive lands within moments of the action; anything older is stale. */
const AUTO_ARCHIVE_TTL_MS = 30_000;

const archivedEntries = new Map<string, { at: number; reason: InboxAutoArchiveReason }>();

/** Record an `inbox:batch-archived` payload, if it is an archive the user caused. */
export function noteInboxAutoArchive(
  payload: { issue_id?: string; reason?: string; trigger?: string },
  now = Date.now(),
): void {
  const reason = resolveAutoArchiveReason(payload);
  if (!payload.issue_id || !reason) return;
  for (const [issueId, entry] of archivedEntries) {
    if (now - entry.at > AUTO_ARCHIVE_TTL_MS) archivedEntries.delete(issueId);
  }
  archivedEntries.set(payload.issue_id, { at: now, reason });
}

/** The user-caused reason that just archived `issueId`'s notifications, if any. Consumes the record. */
export function takeInboxAutoArchiveReason(
  issueId: string,
  now = Date.now(),
): InboxAutoArchiveReason | null {
  const entry = archivedEntries.get(issueId);
  archivedEntries.delete(issueId);
  return entry && now - entry.at <= AUTO_ARCHIVE_TTL_MS ? entry.reason : null;
}

/** Whether the user's own action just archived `issueId`'s notifications. Consumes the record. */
export function takeInboxAutoArchive(issueId: string, now = Date.now()): boolean {
  return takeInboxAutoArchiveReason(issueId, now) !== null;
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
