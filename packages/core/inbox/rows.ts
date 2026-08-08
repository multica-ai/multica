import { useMemo } from "react";
import { useQuery, queryOptions } from "@tanstack/react-query";
import { useFlag, INBOX_V2_FLAG } from "../feature-flags";
import { api } from "../api";
import type { InboxGroup } from "../api/schemas";
import type { InboxItem, InboxItemType, InboxSeverity } from "../types";
import type { IssueStatus } from "../types/issue";
import {
  inboxKeys,
  inboxListOptions,
  archivedInboxListOptions,
  deduplicateInboxItems,
  deduplicateArchivedInboxItems,
} from "./queries";

/**
 * The row the inbox renders.
 *
 * Structurally an `InboxItem`, so every existing list/detail component keeps
 * working unchanged — plus `group`, which is present only when the row came
 * from the v2 endpoints.
 *
 * Both sources produce the same shape ON PURPOSE. The v1 client had to fold
 * events into "one row per issue" itself; v2 gets that row from the server.
 * Projecting the server's row into the shape the UI already renders means the
 * switch is a data-source change, not a rewrite, and the two paths cannot
 * drift into rendering different things.
 */
export interface InboxRow extends InboxItem {
  group?: InboxRowGroup;
}

export interface InboxRowGroup {
  /** Concurrency tokens the v2 read endpoint requires. */
  seq: number;
  stateVersion: number;
  /**
   * Where clicking this row should land, resolved by the server from the
   * representative event. The v1 client borrowed this from whatever row it
   * happened to be rendering, which is why a group could open the wrong
   * comment.
   */
  targetKind: string | null;
  targetId: string | null;
  sourceKind: string;
  sourceId: string;
}

/** True when writes for this row must go to the v2 group endpoints. */
export function isGroupRow(row: InboxRow): boolean {
  return row.group !== undefined;
}

/**
 * The comment a row should scroll to and highlight.
 *
 * v2 answers from the server's resolved target; v1 falls back to digging it out
 * of the details blob, which is all it ever had.
 */
export function inboxRowHighlightCommentId(
  row: InboxRow | null | undefined,
): string | undefined {
  if (!row) return undefined;
  if (row.group) {
    return row.group.targetKind === "comment" && row.group.targetId
      ? row.group.targetId
      : undefined;
  }
  return row.details?.comment_id ?? undefined;
}

/** The URL/selection key for a row — the issue when there is one. */
export function inboxRowKey(row: InboxRow): string {
  return row.issue_id ?? row.id;
}

function toRow(item: InboxItem): InboxRow {
  return item;
}

/**
 * Project a server group into the row shape.
 *
 * `id` is the GROUP's id, not the event's. Every write the page issues goes
 * through `row.id`, and under v2 the thing being marked read or archived is the
 * group — pointing writes at the representative event would move one event's
 * booleans and leave the group, and therefore every other client, untouched.
 */
export function inboxRowFromGroup(group: InboxGroup): InboxRow {
  return {
    id: group.id,
    workspace_id: group.workspace_id,
    recipient_type: "member",
    recipient_id: group.recipient_id,
    actor_type: (group.actor_type as InboxRow["actor_type"]) ?? null,
    actor_id: group.actor_id ?? null,
    type: group.type as InboxItemType,
    severity: group.severity as InboxSeverity,
    issue_id: group.issue_id ?? null,
    title: group.title,
    body: group.body ?? null,
    issue_status: (group.issue_status as IssueStatus | null) ?? null,
    // The one place the two models genuinely differ: v1 stores a boolean per
    // event, v2 derives it from a cursor. `read` is the negation so the
    // existing components — which all ask "is this read" — keep working.
    read: !group.unread,
    archived: group.archived,
    created_at: group.created_at,
    details: normalizeDetails(group.details),
    group: {
      seq: group.seq,
      stateVersion: group.state_version,
      targetKind: group.target_kind ?? null,
      targetId: group.target_id ?? null,
      sourceKind: group.source_kind,
      sourceId: group.source_id,
    },
  };
}

/**
 * `details` is typed as an all-strings map because that is what the mobile
 * client's schema requires; a value that is not a string is dropped rather
 * than rendered, matching what every consumer already assumes.
 */
function normalizeDetails(raw: unknown): Record<string, string> | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (typeof value === "string") out[key] = value;
  }
  return out;
}

export const inboxV2Keys = {
  all: (wsId: string) => ["inbox-v2", wsId] as const,
  list: (wsId: string) => [...inboxV2Keys.all(wsId), "list"] as const,
  archived: (wsId: string) => [...inboxV2Keys.all(wsId), "archived"] as const,
  unreadSummary: () => ["inbox-v2", "unread-summary"] as const,
};

export function inboxV2ListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxV2Keys.list(wsId),
    queryFn: () => api.listInboxGroups(),
  });
}

export function inboxV2ArchivedOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxV2Keys.archived(wsId),
    queryFn: () => api.listArchivedInboxGroups(),
  });
}

export interface InboxRowsResult {
  rows: InboxRow[];
  archivedRows: InboxRow[];
  loading: boolean;
  archivedLoading: boolean;
  archivedError: boolean;
  /** True when the rows on screen came from the group endpoints. */
  grouped: boolean;
}

/**
 * The inbox's data source, routed by the `inbox_v2` flag.
 *
 * Both branches always run their hooks — React requires a stable hook order,
 * and `enabled` is what actually keeps the unused branch from fetching.
 *
 * The v2 branch falls back to v1 on `ready: false`, which is not an error
 * state: it means this user's history has not been folded into groups yet.
 * v1 is completely correct in the meantime, so the fallback is invisible
 * rather than degraded — the whole reason the server answers that way instead
 * of blocking the request behind a migration.
 */
export function useInboxRows(wsId: string): InboxRowsResult {
  const v2Enabled = useFlag(INBOX_V2_FLAG, false);

  const v2List = useQuery({ ...inboxV2ListOptions(wsId), enabled: v2Enabled && !!wsId });
  const v2Archived = useQuery({
    ...inboxV2ArchivedOptions(wsId),
    enabled: v2Enabled && !!wsId,
  });

  // `ready` is per-user, not per-view, so one not-ready answer routes both
  // lists back to v1 together. Splitting them would render a grouped inbox
  // beside an ungrouped archive, and moving a row between the two would change
  // its identity mid-action.
  const v2Ready =
    v2Enabled && v2List.data?.ready === true && v2Archived.data?.ready === true;

  const v1List = useQuery({ ...inboxListOptions(wsId), enabled: !v2Ready && !!wsId });
  const v1Archived = useQuery({
    ...archivedInboxListOptions(wsId),
    enabled: !v2Ready && !!wsId,
  });

  const rows = useMemo(() => {
    if (v2Ready) return (v2List.data?.items ?? []).map(inboxRowFromGroup);
    return deduplicateInboxItems(v1List.data ?? []).map(toRow);
  }, [v2Ready, v2List.data, v1List.data]);

  const archivedRows = useMemo(() => {
    if (v2Ready) return (v2Archived.data?.items ?? []).map(inboxRowFromGroup);
    return deduplicateArchivedInboxItems(v1Archived.data ?? []).map(toRow);
  }, [v2Ready, v2Archived.data, v1Archived.data]);

  return {
    rows,
    archivedRows,
    loading: v2Enabled ? v2List.isLoading || v1List.isLoading : v1List.isLoading,
    archivedLoading: v2Enabled
      ? v2Archived.isLoading || v1Archived.isLoading
      : v1Archived.isLoading,
    // A v2 failure is not surfaced as an error: the fallback already covers it.
    // Only the archive's own failure gets an error state, matching v1.
    archivedError: v2Ready ? v2Archived.isError : v1Archived.isError,
    grouped: v2Ready,
  };
}

/** Query keys both generations write, for a mutation that must invalidate both. */
export function allInboxKeys(wsId: string) {
  return [inboxKeys.all(wsId), inboxV2Keys.all(wsId)];
}
