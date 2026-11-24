import type { QueryClient, QueryKey } from "@tanstack/react-query";
import type { InboxItem } from "../types";

/**
 * The list row for an `inbox:new` item, or null when the client cannot know
 * it is the row the server would list.
 *
 * The pushed item is the notification, not the listed row: a listed row also
 * mirrors its issue's identifier, priority, project, assignee, due date and
 * labels. The Inbox filters read those fields, and a row without them reads
 * as an older server (`inboxIssueFieldsSupport`). Every row of one issue
 * mirrors the same issue, so another row of it (in `listed`, else in
 * `elsewhere`) supplies them, and also shows that the reader may see the
 * issue. An issue-less notice has no such row: the list hides it from a guest
 * (everything outside a project is hidden from them).
 *
 * Times are to the second, and the list keeps the server's order for rows of
 * one second. A listed row of the issue from the same second leaves the order
 * of the two unknown, since pushes may arrive out of order.
 */
export function inboxRowFromPush(
  item: InboxItem,
  listed: readonly InboxItem[],
  elsewhere: readonly InboxItem[] = [],
): InboxItem | null {
  const issueId = item.issue_id;
  const at = Date.parse(item.created_at);
  if (!issueId || Number.isNaN(at) || !isListableItem(item)) return null;
  const sameIssue = (row: InboxItem) => row.issue_id === issueId;
  if (listed.some((row) => sameIssue(row) && Date.parse(row.created_at) === at)) return null;
  const row = listed.find(sameIssue) ?? elsewhere.find(sameIssue);
  if (!row) return null;
  return {
    ...item,
    issue_identifier: row.issue_identifier,
    // The push carries the status at the time of the notification, when the
    // writer knew it.
    issue_status: item.issue_status ?? row.issue_status,
    issue_priority: row.issue_priority,
    issue_project_id: row.issue_project_id,
    issue_assignee_type: row.issue_assignee_type,
    issue_assignee_id: row.issue_assignee_id,
    issue_due_date: row.issue_due_date,
    issue_label_ids: row.issue_label_ids,
  };
}

// What the list and its grouping read on every row. The push is not parsed
// like a list response, so a row missing any of it is not written.
function isListableItem(item: InboxItem): boolean {
  return (
    typeof item.id === "string" && item.id !== "" &&
    typeof item.type === "string" &&
    typeof item.title === "string" &&
    typeof item.read === "boolean" &&
    item.archived === false
  );
}

// Whether a query under `queryKey` has a read on the wire. A paused read is
// not: it waits for focus or the network, which a refresh must not wait on
// (the desktop dock badge reads the summary while the window is hidden).
function readingOut(qc: QueryClient, queryKey: QueryKey): boolean {
  return qc.getQueryCache().findAll({ queryKey }).some((query) => query.state.fetchStatus === "fetching");
}

function readsSettled(qc: QueryClient, queryKey: QueryKey): Promise<void> {
  return new Promise((resolve) => {
    const unsubscribe = qc.getQueryCache().subscribe(() => {
      if (readingOut(qc, queryKey)) return;
      unsubscribe();
      resolve();
    });
  });
}

/** The shortest time between two starts of one coalesced refresh. */
export const INBOX_REFRESH_INTERVAL_MS = 1000;

interface RefreshGate {
  run: Promise<void> | null;
  // Whether the current run has started its read. Until it has, a call is
  // answered by that read.
  reading: boolean;
  startedAt: number;
  next: Promise<void> | null;
}

const gates = new WeakMap<QueryClient, Map<string, RefreshGate>>();

/**
 * Runs `refresh`, which re-reads the queries under `queryKey`, so that the
 * server sees one read at a time and a burst of calls a leading and a
 * trailing one instead of one each.
 *
 * A read already on the wire is waited for, not cancelled: the client's
 * cancel does not stop the query the server started, and that read may
 * predate the call, so `refresh` follows it. Calls that arrive before the
 * refresh starts its read share it; later ones share one follow-up, started
 * once the run is done and `INBOX_REFRESH_INTERVAL_MS` has passed since the
 * last read began. The returned promise settles with a read that started
 * after the call.
 */
export function coalescedRefresh(
  qc: QueryClient,
  queryKey: QueryKey,
  refresh: () => Promise<unknown>,
): Promise<void> {
  let byKey = gates.get(qc);
  if (!byKey) gates.set(qc, (byKey = new Map()));
  const key = JSON.stringify(queryKey);
  let gate = byKey.get(key);
  if (!gate) byKey.set(key, (gate = { run: null, reading: false, startedAt: -Infinity, next: null }));
  const g = gate;
  if (g.next) return g.next;
  if (g.run && !g.reading) return g.run;
  if (!g.run && Date.now() - g.startedAt >= INBOX_REFRESH_INTERVAL_MS) {
    return startRefresh(qc, queryKey, g, refresh);
  }
  g.next = (async () => {
    await g.run;
    const wait = g.startedAt + INBOX_REFRESH_INTERVAL_MS - Date.now();
    if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
    g.next = null;
    await startRefresh(qc, queryKey, g, refresh);
  })();
  return g.next;
}

function startRefresh(
  qc: QueryClient,
  queryKey: QueryKey,
  gate: RefreshGate,
  refresh: () => Promise<unknown>,
): Promise<void> {
  gate.reading = false;
  const read = () => {
    gate.reading = true;
    gate.startedAt = Date.now();
    return refresh();
  };
  const run: Promise<void> = (readingOut(qc, queryKey) ? readsSettled(qc, queryKey).then(read) : read())
    .then(() => undefined, () => undefined)
    .finally(() => {
      if (gate.run === run) gate.run = null;
    });
  gate.run = run;
  return run;
}
