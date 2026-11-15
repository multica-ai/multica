"use client";

import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useHeldUntilArrival } from "../../navigation";

/**
 * Whether the open issue was opened from the Work panel's Inbox section: the
 * issue this page last opened from it, or one it opened in another tab.
 *
 * A row click or the issue header moving on to the next notification records
 * that issue before its page arrives; until then the open issue keeps the
 * record it had. `holding` keeps it for another click the caller knows has
 * named the next issue.
 */
export function useSidebarInboxIssue(wsId: string | undefined, openIssueId: string | null, holding = false) {
  const live = useInboxSelectionStore((s) => (wsId ? (s.sidebarIssueByWorkspace[wsId] ?? null) : null));
  const openedInTab = useInboxSelectionStore(
    (s) => !!wsId && !!openIssueId && !!s.sidebarTabIssuesByWorkspace[wsId]?.includes(openIssueId),
  );
  const held = useHeldUntilArrival(openIssueId, live, holding || (!!live && live !== openIssueId));
  return { fromInbox: !!openIssueId && (held.value === openIssueId || openedInTab), movingOn: held.movingOn };
}
