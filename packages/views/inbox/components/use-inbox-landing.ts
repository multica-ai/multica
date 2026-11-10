import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueUnreadOptions, issueUnreadTarget } from "@multica/core/issues/unread";
import type { InboxItem, IssueUnread } from "@multica/core/types";

/**
 * Where opening an Inbox row lands in the issue's timeline. A mention lands on
 * the comment that named the viewer, and a Connected Apps row on its request.
 * Any other row lands on the issue's unread result (what its last run left),
 * so the newest row on an issue still lands on something. Without one it
 * lands on what the row itself points at, or at the top.
 */
export function inboxLandingTarget(item: InboxItem, unread: IssueUnread | null): string | null {
  const commentId = item.details?.comment_id ?? null;
  if (landsOnItsOwn(item)) return commentId;
  return issueUnreadTarget(unread) ?? commentId ?? item.details?.task_id ?? null;
}

function landsOnItsOwn(item: InboxItem): boolean {
  return item.type.startsWith("app_") || (item.type === "mentioned" && !!item.details?.comment_id);
}

/**
 * The landing for the open row, settled once per opened issue: the detail
 * clears the unread mark as soon as it shows the issue, and the landing must
 * not move when it does. Undefined until the unread list has loaded, for the
 * rows that depend on it.
 */
export function useInboxLanding(wsId: string, item: InboxItem | null): string | undefined {
  const issueId = item?.issue_id ?? "";
  const select = useCallback(
    (items: IssueUnread[]) => items.find((entry) => entry.issue_id === issueId) ?? null,
    [issueId],
  );
  const { data: unread, isError } = useQuery({
    ...issueUnreadOptions(wsId),
    select,
    enabled: !!issueId,
  });
  const [landing, setLanding] = useState<{ issueId: string; target: string | null } | null>(null);

  const settled = !!item && (landsOnItsOwn(item) || unread !== undefined || isError);
  if (item && issueId && landing?.issueId !== issueId && settled) {
    setLanding({ issueId, target: inboxLandingTarget(item, unread ?? null) });
  }
  return landing?.issueId === issueId ? (landing.target ?? undefined) : undefined;
}
