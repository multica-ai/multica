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
  if (!item.id || !issueId || Number.isNaN(at)) return null;
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

/**
 * Resolves once no query under `queryKey` has a request out. A refresh that
 * waits for it instead of cancelling it keeps one read on the server at a
 * time: the client's cancel does not stop the query the server started.
 */
export function inboxQueriesIdle(qc: QueryClient, queryKey: QueryKey): Promise<void> {
  const cache = qc.getQueryCache();
  const busy = () => cache.findAll({ queryKey }).some((query) => query.state.fetchStatus !== "idle");
  if (!busy()) return Promise.resolve();
  return new Promise((resolve) => {
    const unsubscribe = cache.subscribe(() => {
      if (busy()) return;
      unsubscribe();
      resolve();
    });
  });
}

/** The shortest time between two starts of one coalesced refresh. */
export const INBOX_REFRESH_INTERVAL_MS = 1000;

interface RefreshGate {
  running: Promise<void> | null;
  startedAt: number;
  next: Promise<void> | null;
}

const gates = new WeakMap<object, Map<string, RefreshGate>>();

/**
 * Runs `refresh` at most once at a time per client and key, and starts it at
 * most once per `INBOX_REFRESH_INTERVAL_MS`. A call while a run is out or
 * the interval has not passed joins one follow-up run, which starts once both
 * have passed: a burst of events costs a leading and a trailing request
 * instead of one each. The returned promise settles with a run that started
 * after the call.
 */
export function coalescedRefresh(
  client: object,
  key: string,
  refresh: () => Promise<unknown>,
): Promise<void> {
  let byKey = gates.get(client);
  if (!byKey) gates.set(client, (byKey = new Map()));
  let gate = byKey.get(key);
  if (!gate) byKey.set(key, (gate = { running: null, startedAt: -Infinity, next: null }));
  if (gate.next) return gate.next;
  const g = gate;
  if (!g.running && Date.now() - g.startedAt >= INBOX_REFRESH_INTERVAL_MS) {
    return startRefresh(g, refresh);
  }
  g.next = (async () => {
    await g.running;
    const wait = g.startedAt + INBOX_REFRESH_INTERVAL_MS - Date.now();
    if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
    g.next = null;
    await startRefresh(g, refresh);
  })();
  return g.next;
}

function startRefresh(gate: RefreshGate, refresh: () => Promise<unknown>): Promise<void> {
  gate.startedAt = Date.now();
  const run: Promise<void> = refresh()
    .then(() => undefined, () => undefined)
    .finally(() => {
      if (gate.running === run) gate.running = null;
    });
  gate.running = run;
  return run;
}
