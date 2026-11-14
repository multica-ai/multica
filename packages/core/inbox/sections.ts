import type { AgentTask, InboxItem } from "../types";
import { groupInboxRows } from "./group";

/**
 * The Inbox lists every issue once, under whose turn it is (MUL-9215):
 *
 * - "Needs you": a notification addressed to the user is waiting on the issue.
 *   It stays there after it is read, until the user hands the issue on,
 *   archives it or snoozes it.
 * - "Working": an agent run the user started is active on the issue.
 * - "Snoozed": the user put it away until a time.
 * - "Updates": the user only follows the issue. Reading one is all there is
 *   to do with it, so a read update leaves for the archive.
 *
 * An issue that qualifies for several is listed in the first of that order.
 */
export type InboxItemSection = "needs_you" | "updates" | "snoozed";

/** Where one issue's notifications put it. */
export interface InboxPlacement {
  section: InboxItemSection;
  /**
   * The notification that explains a "Needs you" row: the newest one addressed
   * to the user that is not snoozed. Null in the other sections.
   */
  reason: InboxItem | null;
  /** When a snoozed issue comes back. Null unless `section` is "snoozed". */
  snoozedUntil: string | null;
  /** When a snooze that ran out brought the issue back, so it sorts as new. */
  wokeAt: number | null;
  /** The newest notification that carries text, for the row's latest line. */
  preview: InboxItem | null;
  /** Whether the issue's newest notification is read. */
  read: boolean;
}

/** One issue's row in a section. */
export interface InboxSectionRow {
  /** The notification the row opens and acts on. */
  item: InboxItem;
  reason: InboxItem | null;
  snoozedUntil: string | null;
  preview: InboxItem | null;
}

/** An issue an agent is working on for the user. It has no notification. */
export interface InboxWorkingRow {
  issueId: string;
  taskId: string;
  agentId: string;
  /** Waiting for a runtime rather than running. */
  queued: boolean;
  /** When the run started, or was queued while it has not. */
  since: string;
}

export interface InboxSections {
  needsYou: InboxSectionRow[];
  working: InboxWorkingRow[];
  updates: InboxSectionRow[];
  snoozed: InboxSectionRow[];
  /**
   * Keys (`issue_id ?? id`) of the issues whose updates the user has read.
   * They are not listed, and are due to move to the archive.
   */
  readUpdates: string[];
}

export interface InboxSectionOptions {
  /** Where "Working" is read from. Left out, the section is empty. */
  working?: InboxWorkingSource;
  /**
   * Key of the row that is open. Opening an update reads it, and it stays
   * listed until the user moves on.
   */
  openKey?: string | null;
}

/** The runs "Working" is read from, and whose they have to be. */
export interface InboxWorkingSource {
  tasks: readonly AgentTask[];
  userId: string | null | undefined;
}

function time(value: string | null | undefined): number {
  if (!value) return Number.NaN;
  return new Date(value).getTime();
}

function isSnoozed(item: InboxItem, now: number): boolean {
  return time(item.snoozed_until) > now;
}

/**
 * Whether a notification is addressed to the user rather than an update on
 * something they follow.
 *
 * A row from a server that predates sections carries none. Its Inbox was one
 * list of everything addressed to the user, which is what "Needs you" is.
 */
export function inboxItemNeedsUser(item: Pick<InboxItem, "section">): boolean {
  return (item.section ?? "needs_you") === "needs_you";
}

/**
 * Place one issue's notifications, newest first.
 *
 * Snoozing is issue-level and ends early by itself: a notification addressed
 * to the user that arrives later carries no snooze, so the issue is back in
 * "Needs you" the moment it lands. One that only reports activity does not
 * bring it back. This is the rule the server counts the badge by.
 */
export function placeInboxGroup(group: readonly InboxItem[], now: number): InboxPlacement {
  let wokeAt: number | null = null;
  let snoozedUntil: string | null = null;
  for (const item of group) {
    const until = time(item.snoozed_until);
    if (Number.isNaN(until)) continue;
    if (until > now) {
      if (snoozedUntil == null || until > time(snoozedUntil)) snoozedUntil = item.snoozed_until ?? null;
    } else if (wokeAt == null || until > wokeAt) {
      wokeAt = until;
    }
  }
  const preview = group.find((item) => !!item.body?.trim()) ?? null;
  const read = group[0]?.read === true;
  const reason = group.find((item) => inboxItemNeedsUser(item) && !isSnoozed(item, now)) ?? null;
  if (reason) return { section: "needs_you", reason, snoozedUntil: null, wokeAt, preview, read };
  if (snoozedUntil) return { section: "snoozed", reason: null, snoozedUntil, wokeAt: null, preview, read };
  return { section: "updates", reason: null, snoozedUntil: null, wokeAt, preview, read };
}

/** Placement of every issue in `items`, keyed like the list (`issue_id ?? id`). */
export function inboxPlacements(
  items: readonly InboxItem[],
  now: number,
): Map<string, InboxPlacement> {
  const placements = new Map<string, InboxPlacement>();
  for (const group of groupInboxRows(items)) {
    const first = group[0];
    if (first) placements.set(first.issue_id ?? first.id, placeInboxGroup(group, now));
  }
  return placements;
}

/**
 * Split the list's rows into sections.
 *
 * `rows` is the list as filtered: one notification per issue. `items` is every
 * active notification, which is what decides the section — a filter picks
 * which issues are listed, never where an issue belongs.
 *
 * Read updates are left out whether or not they pass the filter, and named
 * in `readUpdates`: reading an update is what clears it.
 */
export function buildInboxSections(
  rows: readonly InboxItem[],
  items: readonly InboxItem[],
  now: number,
  { working, openKey = null }: InboxSectionOptions = {},
): InboxSections {
  const placements = inboxPlacements(items, now);
  // Issues listed above "Working", which they would otherwise repeat.
  const ahead = new Set<string>();
  const readUpdates = new Set<string>();
  for (const [key, placement] of placements) {
    if (placement.section !== "updates") ahead.add(key);
    else if (placement.read && key !== openKey) readUpdates.add(key);
  }
  const workingRows = working ? inboxWorkingRows(working.tasks, working.userId, ahead) : [];
  const workingIssueIds = new Set(workingRows.map((row) => row.issueId));

  const sections: InboxSections = {
    needsYou: [],
    working: workingRows,
    updates: [],
    snoozed: [],
    readUpdates: [...readUpdates],
  };
  const sortAt = new Map<InboxSectionRow, number>();
  for (const item of rows) {
    const key = item.issue_id ?? item.id;
    if (readUpdates.has(key)) continue;
    const placement = placements.get(key) ?? placeInboxGroup([item], now);
    const row: InboxSectionRow = {
      item,
      reason: placement.reason,
      snoozedUntil: placement.snoozedUntil,
      preview: placement.preview,
    };
    if (placement.section === "needs_you") {
      sortAt.set(row, Math.max(time(item.created_at), placement.wokeAt ?? 0));
      sections.needsYou.push(row);
    } else if (placement.section === "snoozed") {
      sortAt.set(row, time(placement.snoozedUntil));
      sections.snoozed.push(row);
    } else if (!(item.issue_id && workingIssueIds.has(item.issue_id))) {
      sortAt.set(row, time(item.created_at));
      sections.updates.push(row);
    }
  }
  const at = (row: InboxSectionRow) => sortAt.get(row) ?? 0;
  sections.needsYou.sort((a, b) => at(b) - at(a));
  sections.updates.sort((a, b) => at(b) - at(a));
  // The one that comes back first leads.
  sections.snoozed.sort((a, b) => at(a) - at(b));
  return sections;
}

/**
 * The next moment a snooze runs out, or null. The list re-places its rows
 * then: nothing arrives from the server when a snooze simply elapses.
 */
export function nextInboxWake(items: readonly InboxItem[], now: number): number | null {
  let next: number | null = null;
  for (const item of items) {
    if (item.archived) continue;
    const until = time(item.snoozed_until);
    if (until > now && (next == null || until < next)) next = until;
  }
  return next;
}

function isQueued(task: AgentTask): boolean {
  return (
    task.status === "queued" ||
    task.status === "dispatched" ||
    task.status === "waiting_local_directory" ||
    (task.status === "deferred" && !!task.wakeup_id)
  );
}

/**
 * The issues an agent is working on because the user asked: a run the user
 * started themselves, by assigning, commenting or rerunning. Runs that
 * autopilots, other members or other agents started are not the user's
 * hand-over, and neither are chat runs, which have no issue.
 *
 * One row per issue, a running run before a queued one. Issues in `exclude`
 * are listed elsewhere: they need the user, or the user snoozed them.
 */
export function inboxWorkingRows(
  tasks: readonly AgentTask[],
  userId: string | null | undefined,
  exclude: ReadonlySet<string> = new Set(),
): InboxWorkingRow[] {
  if (!userId) return [];
  const byIssue = new Map<string, InboxWorkingRow>();
  for (const task of tasks) {
    if (!task.issue_id || exclude.has(task.issue_id)) continue;
    const running = task.status === "running";
    if (!running && !isQueued(task)) continue;
    if (task.attribution?.source !== "direct_human") continue;
    if (task.attribution.originator?.id !== userId) continue;
    const row: InboxWorkingRow = {
      issueId: task.issue_id,
      taskId: task.id,
      agentId: task.agent_id,
      queued: !running,
      since: (running ? task.started_at : null) ?? task.created_at,
    };
    const current = byIssue.get(task.issue_id);
    if (
      !current ||
      (current.queued && !row.queued) ||
      (current.queued === row.queued && time(row.since) < time(current.since))
    ) {
      byIssue.set(task.issue_id, row);
    }
  }
  return [...byIssue.values()].sort((a, b) =>
    a.queued === b.queued ? time(b.since) - time(a.since) : a.queued ? 1 : -1,
  );
}

/** Why an issue waits on the user, for the words a "Needs you" row leads with. */
export type InboxReasonKind =
  | "review"
  | "question"
  | "mention"
  | "failed"
  | "cancelled"
  | "assigned"
  | "comment"
  | "finished";

/**
 * What a "Needs you" row is waiting for, from the notification that put it
 * there. A notification that names its own reason keeps it; one that only
 * reports the agent handed over (a finished run, a status change, a comment)
 * is read through the issue's status, which says what the hand-over asks for:
 * `in_review` asks for a check, `blocked` for an answer. Null when neither
 * says, and the row falls back to the notification's own label.
 */
export function inboxReasonKind(reason: InboxItem): InboxReasonKind | null {
  switch (reason.type) {
    case "task_failed":
    case "quick_create_failed":
      return "failed";
    case "task_cancelled":
      return "cancelled";
    case "agent_blocked":
      return "question";
    case "mentioned":
      return "mention";
    case "issue_assigned":
      return "assigned";
    case "review_requested":
      return "review";
    case "agent_completed":
    case "task_completed":
    case "status_changed":
    case "new_comment":
      if (reason.issue_status === "in_review") return "review";
      if (reason.issue_status === "blocked") return "question";
      if (reason.type === "new_comment") return "comment";
      return reason.type === "status_changed" ? null : "finished";
    default:
      return null;
  }
}
