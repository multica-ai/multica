import type { InboxItem } from "../types";

/**
 * The list row for an `inbox:new` item, or null when it cannot be built from
 * what the client holds.
 *
 * The pushed item is the notification, not the listed row: a listed row also
 * mirrors its issue's identifier, priority, project, assignee, due date and
 * labels. The Inbox filters read those fields, and a row without them reads
 * as an older server (`inboxIssueFieldsSupport`). Every row of one issue
 * mirrors the same issue, so `known` rows of that issue supply them. A row
 * without an issue mirrors none, as the server lists it.
 */
export function inboxRowFromPush(
  item: InboxItem,
  known: Iterable<InboxItem>,
): InboxItem | null {
  if (!item.issue_id) {
    return {
      issue_identifier: null,
      issue_priority: null,
      issue_project_id: null,
      issue_assignee_type: null,
      issue_assignee_id: null,
      issue_due_date: null,
      issue_label_ids: [],
      ...item,
      issue_status: item.issue_status ?? null,
    };
  }
  for (const row of known) {
    if (row.issue_id !== item.issue_id) continue;
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
  return null;
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
