import type {
  InboxItem,
  Issue,
  IssuePriority,
  IssueStatusCategory,
} from "../types";
import { PRIORITY_ORDER } from "../issues/config/priority";
import { dateOnlyToUTCDate, todayDateOnly } from "../issues/date";
import { statusCategoryOfKey } from "../issues/status-category";

/**
 * The "needs me" queue on Home: every entry is one action the viewer owes.
 *
 * Nothing here is stored. Each kind is an existing object seen from the
 * viewer's side:
 * - `in_review` / `blocked`: an issue in that status that the viewer is on
 *   (assignee, creator, or the owner of the assigned agent).
 * - `mentioned`: an unarchived inbox mention.
 * - `action_required`: an unarchived inbox item the server already marks
 *   `severity: "action_required"` (assignment, failed run, quick create).
 *
 * An entry leaves the queue when its source changes: the issue leaves the
 * status, or the inbox item is archived.
 */
export type NeedsMeKind = "in_review" | "blocked" | "mentioned" | "action_required";

/** Status keys that put an issue in the queue. Built-in keys only. */
export const NEEDS_ME_STATUSES = ["in_review", "blocked"] as const;

/**
 * Inbox-derived entries older than this are left to the activity list. An
 * inbox that was never archived would otherwise flood the queue on first
 * open with months-old mentions and run failures.
 */
export const NEEDS_ME_INBOX_WINDOW_DAYS = 14;

export interface NeedsMeActor {
  type: "member" | "agent" | "squad" | "system";
  id: string;
}

export interface NeedsMeItem {
  /** Stable across refetches: one entry per issue, or per issue-less inbox item. */
  key: string;
  kind: NeedsMeKind;
  issueId: string | null;
  /** Present for status-derived entries. */
  issue: Issue | null;
  /** The newest inbox item folded into this entry, if any. */
  inbox: InboxItem | null;
  /** Every inbox item folded into this entry; archived once the action is done. */
  inboxIds: string[];
  title: string;
  identifier: string | null;
  priority: IssuePriority;
  dueDate: string | null;
  /** ISO timestamp the viewer has been owing this action since. */
  since: string;
  /** Who is waiting on the viewer: the assignee for status entries, the sender for inbox ones. */
  actor: NeedsMeActor | null;
}

export interface DeriveNeedsMeInput {
  /** Issues the viewer is on, already narrowed server-side to NEEDS_ME_STATUSES. */
  statusIssues: readonly Issue[];
  /** The unarchived inbox list (deduplicated or not). */
  inboxItems: readonly InboxItem[];
  now?: number;
}

const DAY_MS = 24 * 60 * 60 * 1000;

function isNeedsMeStatus(status: string): status is NeedsMeKind {
  return (NEEDS_ME_STATUSES as readonly string[]).includes(status);
}

function isTerminalCategory(category: IssueStatusCategory): boolean {
  return category === "done" || category === "closed";
}

function inboxActor(item: InboxItem): NeedsMeActor | null {
  if (!item.actor_type) return null;
  if (item.actor_type === "system") return { type: "system", id: "" };
  return item.actor_id ? { type: item.actor_type, id: item.actor_id } : null;
}

function inboxKind(item: InboxItem): NeedsMeKind | null {
  if (item.type === "mentioned") return "mentioned";
  if (item.severity === "action_required") return "action_required";
  return null;
}

function earlier(a: string, b: string): string {
  return new Date(a).getTime() <= new Date(b).getTime() ? a : b;
}

function later(a: InboxItem, b: InboxItem): InboxItem {
  return new Date(a.created_at).getTime() >= new Date(b.created_at).getTime() ? a : b;
}

export function deriveNeedsMe({
  statusIssues,
  inboxItems,
  now = Date.now(),
}: DeriveNeedsMeInput): NeedsMeItem[] {
  const byKey = new Map<string, NeedsMeItem>();

  for (const issue of statusIssues) {
    // Re-check the status: an optimistic patch can move a cached row out of
    // the status before the server-filtered list refetches.
    if (!isNeedsMeStatus(issue.status)) continue;
    const key = `issue:${issue.id}`;
    if (byKey.has(key)) continue;
    byKey.set(key, {
      key,
      kind: issue.status,
      issueId: issue.id,
      issue,
      inbox: null,
      inboxIds: [],
      title: issue.title,
      identifier: issue.identifier,
      priority: issue.priority,
      dueDate: issue.due_date,
      // Status changes bump updated_at; it is the closest existing signal for
      // "entered this status" without reading every issue's timeline.
      since: issue.updated_at,
      actor: issue.assignee_type && issue.assignee_id
        ? { type: issue.assignee_type, id: issue.assignee_id }
        : null,
    });
  }

  const cutoff = now - NEEDS_ME_INBOX_WINDOW_DAYS * DAY_MS;
  for (const item of inboxItems) {
    if (item.archived) continue;
    const kind = inboxKind(item);
    if (!kind) continue;
    if (new Date(item.created_at).getTime() < cutoff) continue;
    // A failed run or an assignment on an issue that has since finished
    // leaves nothing to do. A mention still might.
    if (
      kind === "action_required" &&
      item.issue_status &&
      isTerminalCategory(statusCategoryOfKey(item.issue_status))
    ) {
      continue;
    }

    const key = item.issue_id ? `issue:${item.issue_id}` : `inbox:${item.id}`;
    const existing = byKey.get(key);
    if (existing) {
      // Same issue already queued (by status, or by an earlier inbox row):
      // fold the row in so finishing the action archives it too.
      existing.inboxIds.push(item.id);
      existing.inbox = existing.inbox ? later(existing.inbox, item) : item;
      if (!existing.issue) {
        existing.since = earlier(existing.since, item.created_at);
        // A mention outranks a generic action on the same issue: replying is
        // the concrete thing being asked.
        if (kind === "mentioned" && existing.kind === "action_required") {
          existing.kind = "mentioned";
          existing.actor = inboxActor(item);
        }
      }
      continue;
    }
    byKey.set(key, {
      key,
      kind,
      issueId: item.issue_id,
      issue: null,
      inbox: item,
      inboxIds: [item.id],
      title: item.title,
      identifier: null,
      priority: item.issue_priority ?? "none",
      dueDate: null,
      since: item.created_at,
      actor: inboxActor(item),
    });
  }

  return [...byKey.values()].sort((a, b) => compareNeedsMe(a, b));
}

/** 0 overdue, 1 today, 2 tomorrow, 3 later, 4 no due date. */
export type DueBucket = 0 | 1 | 2 | 3 | 4;

export function dueBucket(dueDate: string | null, today = todayDateOnly()): DueBucket {
  const due = dateOnlyToUTCDate(dueDate);
  const base = dateOnlyToUTCDate(today);
  if (!due || !base) return 4;
  const days = Math.round((due.getTime() - base.getTime()) / DAY_MS);
  if (days < 0) return 0;
  if (days === 0) return 1;
  if (days === 1) return 2;
  return 3;
}

function priorityRank(priority: IssuePriority): number {
  const index = PRIORITY_ORDER.indexOf(priority);
  return index === -1 ? PRIORITY_ORDER.length : index;
}

/** An agent that stopped and waits on the viewer outranks work that is merely ready. */
function blockingRank(kind: NeedsMeKind): number {
  return kind === "blocked" ? 0 : 1;
}

/**
 * Queue order. The issue's own priority and due date come first — the kind of
 * action says nothing about business urgency — then whether someone is stopped
 * on the viewer, then who has waited longest.
 */
export function compareNeedsMe(
  a: NeedsMeItem,
  b: NeedsMeItem,
  today = todayDateOnly(),
): number {
  return (
    priorityRank(a.priority) - priorityRank(b.priority) ||
    dueBucket(a.dueDate, today) - dueBucket(b.dueDate, today) ||
    blockingRank(a.kind) - blockingRank(b.kind) ||
    new Date(a.since).getTime() - new Date(b.since).getTime() ||
    a.key.localeCompare(b.key)
  );
}

export type NeedsMeReason =
  | { kind: "priority"; priority: IssuePriority }
  | { kind: "overdue" }
  | { kind: "due_today" }
  | { kind: "due_tomorrow" }
  | { kind: "waiting"; ms: number };

/**
 * Why an entry sits where it does, built from the same fields the order uses
 * so the "look at this first" line never contradicts the ranking.
 */
export function needsMeReasons(
  item: NeedsMeItem,
  now = Date.now(),
  today = todayDateOnly(),
): NeedsMeReason[] {
  const reasons: NeedsMeReason[] = [];
  if (item.priority !== "none") reasons.push({ kind: "priority", priority: item.priority });
  const bucket = dueBucket(item.dueDate, today);
  if (bucket === 0) reasons.push({ kind: "overdue" });
  else if (bucket === 1) reasons.push({ kind: "due_today" });
  else if (bucket === 2) reasons.push({ kind: "due_tomorrow" });
  reasons.push({ kind: "waiting", ms: Math.max(0, now - new Date(item.since).getTime()) });
  return reasons;
}
