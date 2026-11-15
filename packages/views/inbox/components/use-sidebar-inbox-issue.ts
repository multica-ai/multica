"use client";

import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useHeldUntilArrival } from "../../navigation";

/**
 * The issue last opened from the Work panel's Inbox section, as the open
 * issue sees it. A row click or the issue header moving on to the next
 * notification records that issue before its page arrives; until then the
 * open issue keeps the record it had. `holding` keeps it for another
 * click the caller knows has named the next issue.
 */
export function useSidebarInboxIssue(wsId: string | undefined, openIssueId: string | null, holding = false) {
  const live = useInboxSelectionStore((s) => (wsId ? (s.sidebarIssueByWorkspace[wsId] ?? null) : null));
  return useHeldUntilArrival(openIssueId, live, holding || (!!live && live !== openIssueId));
}
