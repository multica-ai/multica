import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useIssueStatuses } from "../issue-statuses/hooks";
import { inboxListOptions } from "../inbox/queries";
import { issueTableRowPageOptions } from "../issues/queries";
import { issueBehavesAsAny } from "../issues/status-category";
import type { InboxItem, Issue, IssueTableRowsRequest } from "../types";
import { isReplyPending, useNeedsMeRepliesStore } from "./replies-store";
import { deriveNeedsMe, NEEDS_ME_STATUSES, type NeedsMeItem } from "./needs-me";

const EMPTY_ISSUES: Issue[] = [];
const EMPTY_INBOX: InboxItem[] = [];

/**
 * The status half of the queue: in_review / blocked issues the viewer is on,
 * as one Table-channel query. The `my:any` relation is the server's union of
 * "assigned to me", "created by me" and "assigned to an agent I own", so the
 * three ways of being the person an agent is waiting on cost one request. It
 * lives under `issueKeys.tableAll`, which every issue event already refreshes.
 */
export function needsMeStatusRequest(): IssueTableRowsRequest {
  return {
    query: {
      scope: { kind: "my", relation: "any" },
      filters: { statuses: [...NEEDS_ME_STATUSES] },
      sort: { field: "updated_at", direction: "asc" },
    },
    group: { kind: "none" },
    group_key: null,
    hierarchy: { enabled: false },
    parent_id: null,
    page: { limit: 100, cursor: null },
  };
}

export interface NeedsMeResult {
  /** Queue order, with entries the viewer already replied to moved to the end. */
  items: NeedsMeItem[];
  /** Entries still waiting for an action — what the Home badge counts. */
  actionableCount: number;
  /** Keys of entries whose reply is still the latest thing on the issue. */
  replied: ReadonlySet<string>;
  isLoading: boolean;
  /** The raw inbox list, shared with the activity views. */
  inboxItems: InboxItem[];
}

export function useNeedsMe(wsId: string | null | undefined): NeedsMeResult {
  const statusQuery = useQuery({
    ...issueTableRowPageOptions(wsId ?? "", needsMeStatusRequest()),
    enabled: !!wsId,
  });
  const inboxQuery = useQuery({ ...inboxListOptions(wsId ?? ""), enabled: !!wsId });

  const statusIssues = useMemo(
    () => statusQuery.data?.rows.map((row) => row.issue) ?? EMPTY_ISSUES,
    [statusQuery.data],
  );
  const inboxItems = inboxQuery.data ?? EMPTY_INBOX;
  const derived = useMemo(
    () => deriveNeedsMe({ statusIssues, inboxItems }),
    [statusIssues, inboxItems],
  );

  const replies = useNeedsMeRepliesStore((s) => s.replies);
  const retainReplies = useNeedsMeRepliesStore((s) => s.retainReplies);
  const loaded = statusQuery.isSuccess && inboxQuery.isSuccess;
  useEffect(() => {
    if (loaded) retainReplies(new Set(derived.map((item) => item.key)));
  }, [loaded, derived, retainReplies]);

  const replied = useMemo(
    () =>
      new Set(
        derived
          .filter((item) => isReplyPending(replies[item.key], item.issue))
          .map((item) => item.key),
      ),
    [derived, replies],
  );
  const items = useMemo(() => {
    if (replied.size === 0) return derived;
    return [
      ...derived.filter((item) => !replied.has(item.key)),
      ...derived.filter((item) => replied.has(item.key)),
    ];
  }, [derived, replied]);

  return {
    items,
    actionableCount: derived.length - replied.size,
    replied,
    isLoading: statusQuery.isLoading || inboxQuery.isLoading,
    inboxItems,
  };
}

/**
 * "On my plate": open issues assigned to the viewer, earliest due date first.
 * Narrowed to the workspace's open status keys server-side so a long tail of
 * finished issues cannot push dated work out of the window.
 */
export function useMyPlate(wsId: string | null | undefined) {
  const catalog = useIssueStatuses(wsId ?? "");
  const openStatuses = useMemo(
    () =>
      catalog.statuses
        .filter((entry) => !issueBehavesAsAny({ status: entry.key, status_category: entry.category }, ["done", "closed"]))
        .map((entry) => entry.key),
    [catalog.statuses],
  );
  const request = useMemo<IssueTableRowsRequest>(
    () => ({
      query: {
        scope: { kind: "my", relation: "assigned" },
        filters: { statuses: openStatuses },
        sort: { field: "due_date", direction: "asc" },
      },
      group: { kind: "none" },
      group_key: null,
      hierarchy: { enabled: false },
      parent_id: null,
      page: { limit: 100, cursor: null },
    }),
    [openStatuses],
  );
  const query = useQuery({
    ...issueTableRowPageOptions(wsId ?? "", request),
    // An empty status list means "no filter" to the server; wait for the
    // catalog rather than briefly listing every issue ever assigned.
    enabled: !!wsId && openStatuses.length > 0,
  });
  const issues = useMemo(
    () => query.data?.rows.map((row) => row.issue) ?? EMPTY_ISSUES,
    [query.data],
  );
  return { issues, isLoading: query.isLoading || catalog.statuses.length === 0 };
}
