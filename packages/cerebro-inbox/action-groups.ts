// "Group by → Action" — cerebro-only inbox grouping (FIR-2115).
//
// Buckets each inbox entry by what the user is expected to DO with it, rather
// than by a workflow status field. Top-to-bottom precedence is fixed; every
// entry lands in exactly one bucket:
//
//   act_now   — unread: any unread inbox item.
//   reminders — reminders and date-arrival notifications (FIR-2445): keeps
//               time-driven items in their own section so they don't get lost
//               in Unread, regardless of read state.
//   watching  — running: an agent is currently running on it.
//   calm      — done: the issue is terminal (done/cancelled), or a non-issue
//               chat/channel has been read.
//   waiting   — an open thread to follow up: you've seen it, but it is not
//               running or done yet.
//
// The grouping logic lives here (cerebro zone) so the upstream inbox page only
// needs a few marked CEREBRO-PATCH touchpoints that delegate into it.

import type { InboxItem, InboxItemType } from "@multica/core/types";

export type InboxActionCategory = "act_now" | "reminders" | "watching" | "pending" | "waiting" | "calm";

/** Render order, top → bottom. Drives group sorting (not alphabetical). */
export const INBOX_ACTION_ORDER: InboxActionCategory[] = [
  "act_now",
  "reminders",
  "watching",
  "pending",
  "calm",
  "waiting",
];

const REMINDER_TYPES: ReadonlySet<InboxItemType> = new Set<InboxItemType>([
  "reminder",
  "due_date_reminder",
  "start_date_reminder",
]);

/** Zero-based render index for a category. Unknown categories sort last. */
export function inboxActionOrderIndex(category: InboxActionCategory): number {
  const i = INBOX_ACTION_ORDER.indexOf(category);
  return i === -1 ? INBOX_ACTION_ORDER.length : i;
}

/**
 * The "Action" entry for the inbox Group-by menu. English label by product
 * decision (FIR-2115) — kept stable across locales for now.
 */
export const INBOX_ACTION_GROUP_BY_OPTION = {
  value: "action" as const,
  label: "Action",
};

/**
 * Workspace-wide signals the classifier needs that don't live on the entry.
 * Run-state maps and the mentioned-channel set are owned by the inbox page;
 * we read membership only, so the value type is intentionally loose.
 */
export interface InboxActionContext {
  /** The viewing user's id — distinguishes "last comment from you" vs others. */
  userId: string;
  /** Issue ids with an in-flight agent run. */
  issueRunStates: ReadonlyMap<string, unknown>;
  /** Parent issue ids that have an in-flight agent run on a sub-issue (FIR-2326). */
  subIssueRunStates: ReadonlyMap<string, unknown>;
  /** Chat session ids with an in-flight agent run. */
  chatRunStates: ReadonlyMap<string, unknown>;
  /** Channel ids with an unread @mention for the viewing user. */
  mentionedChannels: ReadonlySet<string>;
}

/** Structural subset of the inbox page's MergedEntry we classify against. */
export type InboxActionEntry =
  | { kind: "notif"; item: InboxItem }
  | { kind: "chat"; session: { id: string; has_unread?: boolean } }
  | {
      kind: "channel";
      channel: {
        id: string;
        unread_count: number;
        last_message?: { author_type: "member" | "agent"; author_id: string } | null;
      };
    };

function classifyNotif(item: InboxItem, ctx: InboxActionContext): InboxActionCategory {
  // 1. Reminders and date-arrival items get their own bucket (FIR-2445),
  //    checked before Unread so they don't disappear into the Unread pile
  //    the moment they fire.
  if (REMINDER_TYPES.has(item.type)) return "reminders";

  // 2. Unread is a literal bucket: any unread notification belongs here.
  if (!item.read) return "act_now";

  // 3. An agent is actively working the issue — watch, don't act. Checked
  //    before "waiting" so a running thread isn't mistaken for a stalled one.
  //    FIR-2326: a parent whose sub-issue is running counts as watching too,
  //    so the umbrella surfaces in Running even when its own row is idle.
  if (item.issue_id && (ctx.issueRunStates.has(item.issue_id) || ctx.subIssueRunStates.has(item.issue_id)))
    return "watching";

  // 4. Done is also literal for issue notifications. A read item on an open
  //    issue is still follow-up, not done.
  if (item.issue_status === "done" || item.issue_status === "cancelled") return "calm";

  // 5. Everything else is open and already seen.
  return "waiting";
}

/**
 * Map a single inbox entry to its action category. Pure — unit-tested in
 * action-groups.test.ts.
 */
export function classifyInboxAction(
  entry: InboxActionEntry,
  ctx: InboxActionContext,
): InboxActionCategory {
  switch (entry.kind) {
    case "notif":
      return classifyNotif(entry.item, ctx);
    case "chat":
      // A 1:1 agent chat: unread is literal, then live run, then read/settled.
      if (entry.session.has_unread) return "act_now";
      if (ctx.chatRunStates.has(entry.session.id)) return "watching";
      return "calm";
    case "channel":
      // Channel unread counts are literal; mention state no longer changes the
      // bucket label because the bucket itself is named Unread.
      if (entry.channel.unread_count > 0) return "act_now";
      // A read channel is Pending when we sent the last message (waiting for a
      // reply) or when an agent sent it (still in flight). It is Done only when
      // another human had the last word — we've seen their response.
      if (entry.channel.last_message != null) {
        const { author_type, author_id } = entry.channel.last_message;
        if (author_type === "agent" || (author_type === "member" && author_id === ctx.userId))
          return "pending";
      }
      return "calm";
    default:
      // Enum drift downgrades, not crashes (see API Response Compatibility).
      return "calm";
  }
}

/**
 * Adapter for the inbox page's `bucketize`. Returns the same shape the other
 * group-by modes return, plus a fixed `order` so action buckets render in
 * precedence order instead of alphabetically.
 */
export function bucketizeInboxAction(
  entry: InboxActionEntry,
  ctx: InboxActionContext,
  labels: Record<InboxActionCategory, string>,
): { key: string; label: string; isFallback: boolean; order: number } {
  const category = classifyInboxAction(entry, ctx);
  return {
    key: category,
    label: labels[category],
    isFallback: false,
    order: inboxActionOrderIndex(category),
  };
}
