// "Group by → Action" — cerebro-only inbox grouping (FIR-2115).
//
// Buckets each inbox entry by what the user is expected to DO with it, rather
// than by a workflow status field. Top-to-bottom precedence is fixed; every
// entry lands in exactly one bucket:
//
//   act_now  — needs you now: a fresh, unread action-required item (new
//              @mention, review request, blocked-on-you, failure, assignment).
//   watching — an agent is currently running on it; nothing to do but watch.
//   waiting  — an open thread to follow up: you've seen it but the last comment
//              isn't yours, or you were @mentioned and haven't replied yet.
//   calm     — settled: completed/done, reactions, info you've already seen.
//
// The grouping logic lives here (cerebro zone) so the upstream inbox page only
// needs a few marked CEREBRO-PATCH touchpoints that delegate into it.

import type { InboxItem } from "@multica/core/types";

export type InboxActionCategory = "act_now" | "watching" | "waiting" | "calm";

/** Render order, top → bottom. Drives group sorting (not alphabetical). */
export const INBOX_ACTION_ORDER: InboxActionCategory[] = [
  "act_now",
  "watching",
  "waiting",
  "calm",
];

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
  /** Chat session ids with an in-flight agent run. */
  chatRunStates: ReadonlyMap<string, unknown>;
  /** Channel ids with an unread @mention for the viewing user. */
  mentionedChannels: ReadonlySet<string>;
}

/** Structural subset of the inbox page's MergedEntry we classify against. */
export type InboxActionEntry =
  | { kind: "notif"; item: InboxItem }
  | { kind: "chat"; session: { id: string; has_unread?: boolean } }
  | { kind: "channel"; channel: { id: string; unread_count: number } };

function classifyNotif(item: InboxItem, ctx: InboxActionContext): InboxActionCategory {
  // 1. Fresh, action-required items demand attention now.
  if (item.severity === "action_required" && !item.read) return "act_now";

  // 2. An agent is actively working the issue — watch, don't act. Checked
  //    before "waiting" so a running thread isn't mistaken for a stalled one.
  if (item.issue_id && ctx.issueRunStates.has(item.issue_id)) return "watching";

  // 3. Open threads to follow up on:
  //    - an action-required item you've already seen (mention you haven't closed),
  //    - the server's "attention" tier,
  //    - someone else's new comment (i.e. the last comment isn't yours).
  if (item.severity === "action_required") return "waiting";
  if (item.severity === "attention") return "waiting";
  if (
    item.type === "new_comment" &&
    !(item.actor_type === "member" && item.actor_id === ctx.userId)
  ) {
    return "waiting";
  }

  // 4. Everything else is settled.
  return "calm";
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
      // A 1:1 agent chat: a live run is "watching"; an unread reply means the
      // last message isn't yours → "waiting"; otherwise it's settled.
      if (ctx.chatRunStates.has(entry.session.id)) return "watching";
      return entry.session.has_unread ? "waiting" : "calm";
    case "channel":
      // An unread @mention in a channel needs you; other unread is follow-up.
      if (ctx.mentionedChannels.has(entry.channel.id)) return "act_now";
      return entry.channel.unread_count > 0 ? "waiting" : "calm";
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
