"use client";

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { isOwnIssue } from "@multica/core/issues/ownership";
import type { InboxItem } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  undoUnsubscribeArchiveInbox,
  useUnsubscribeArchiveInbox,
  type InboxUnsubscribeScope,
} from "@multica/core/inbox/mutations";
import { useT } from "../../i18n";

/**
 * Whether a list row offers "unsubscribe & archive": it is about an issue
 * that is not the viewer's own (see `isOwnIssue`). Rows from a server that
 * predates the issue projection carry no assignee and are not offered, since
 * an own issue cannot be told apart there.
 */
export function useInboxUnsubscribable(wsId: string) {
  const { data: agents, isSuccess } = useQuery(agentListOptions(wsId));
  return useCallback(
    (item: InboxItem) =>
      !!item.issue_id &&
      isSuccess &&
      item.recipient_type === "member" &&
      item.issue_assignee_type !== undefined &&
      !isOwnIssue(
        { assignee_type: item.issue_assignee_type, assignee_id: item.issue_assignee_id },
        item.recipient_id,
        agents ?? [],
      ),
    [isSuccess, agents],
  );
}

export interface UnsubscribeArchiveTarget {
  /** The inbox item to archive. */
  id: string;
  /** The issue to leave. */
  issueId: string;
}

/**
 * "Unsubscribe & archive" with the confirmation every surface shares. Undo is
 * offered for a single issue only: a subtree opt-out tombstones every
 * descendant, and one issue's undo would not bring those back.
 *
 * The toasts hang off the mutation itself, not the `mutate` call: every
 * surface moves on to the next notification straight away, which unmounts the
 * caller before the request settles.
 */
export function useUnsubscribeArchive() {
  const { t } = useT("inbox");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const failed = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);
  const { mutate } = useUnsubscribeArchiveInbox({
    onSuccess: ({ id, issueId, scope }) =>
      toast.success(
        scope === "subtree"
          ? t(($) => $.toasts.unsubscribed_archived_subtree)
          : t(($) => $.toasts.unsubscribed_archived),
        {
          description: t(($) => $.toasts.unsubscribed_hint),
          action:
            scope === "issue"
              ? {
                  label: t(($) => $.toasts.undo),
                  onClick: () => {
                    undoUnsubscribeArchiveInbox(qc, wsId, { id, issueId }).catch(
                      failed(t(($) => $.errors.undo_unsubscribe_failed)),
                    );
                  },
                }
              : undefined,
        },
      ),
    onError: failed(t(($) => $.errors.unsubscribe_archive_failed)),
  });
  return useCallback(
    (target: UnsubscribeArchiveTarget, scope: InboxUnsubscribeScope = "issue") =>
      mutate({ ...target, scope }),
    [mutate],
  );
}
