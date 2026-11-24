"use client";

import { useLayoutEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  type InboxAutoArchiveReason,
  type InboxBarAction,
  takeInboxAutoArchiveReason,
  takeInboxBarAction,
} from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { Issue } from "@multica/core/types";

/**
 * Resolves why an open Inbox notification was auto-archived in place: the
 * Inbox bar action taken in this window (`cause`), if any, and the server's
 * user-caused auto-archive `reason` (`issue_status_terminal`,
 * `agent_triggered`, or `actor_acted`).
 *
 * Auto-archives always stay on the open issue (`keepOpenInboxRow`) so the
 * user sees what happened — and confirms a triggered agent started — before
 * moving on with E / Next. Only explicit Inbox actions (Archive, Snooze,
 * Unsubscribe) advance immediately.
 */
export function useAutoArchiveCause(
  settled: KeptInboxRow | null,
): { cause: InboxBarAction | null; reason: InboxAutoArchiveReason | null } {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const archivedKey = settled?.reason === "archived" ? settled.key : null;
  const [settledMeta, setSettledMeta] = useState<{
    key: string;
    action: InboxBarAction | null;
    reason: InboxAutoArchiveReason | null;
  } | null>(null);

  useLayoutEffect(() => {
    if (!archivedKey) return;
    const reason = takeInboxAutoArchiveReason(archivedKey);
    const own = reason !== null;
    const action = takeInboxBarAction(archivedKey);
    const shown =
      own &&
      action !== null &&
      reflects(qc.getQueryData(issueDetailOptions(wsId, archivedKey).queryKey), action);
    setSettledMeta({ key: archivedKey, action: shown ? action : null, reason });
  }, [archivedKey, wsId, qc]);

  if (!settledMeta || settledMeta.key !== archivedKey) {
    return { cause: null, reason: null };
  }
  return { cause: settledMeta.action, reason: settledMeta.reason };
}

/**
 * Whether the issue shows the change the bar asked for. Agent assignments
 * confirm first, and one dismissed there never happened.
 */
function reflects(issue: Issue | undefined, action: InboxBarAction): boolean {
  if (!issue) return true;
  switch (action.type) {
    case "status":
      return issue.status === action.status;
    case "assignee":
      return (issue.assignee_id ?? null) === (action.assignee?.id ?? null);
    case "quick_action":
      return true;
  }
}
