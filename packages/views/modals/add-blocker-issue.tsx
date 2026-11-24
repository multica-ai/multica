"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useUpdateIssueBlockers } from "@multica/core/issues/blockers";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

/**
 * Adds a manual blocker to an issue (MUL-8860). Shared by the sidebar's
 * Waiting section and the Relations submenu of the issue actions menu, so
 * both entry points write through the same path. The picker stays open while
 * the write is in flight and shows the server's error in place (a cycle, a
 * blocker the caller can't see), closing only once the blocker is saved.
 */
export function AddBlockerIssueModal({
  open = true,
  onClose,
  onOpenChangeComplete,
  data,
}: {
  open?: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("issues");
  const issueId = (data?.issueId as string) || "";
  const wsId = useWorkspaceId();
  const mutation = useUpdateIssueBlockers(wsId, issueId);

  // List rows don't carry blocked_by, so read the detail for what to exclude.
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const excludeIds = [
    issueId,
    ...(issue?.parent_issue_id ? [issue.parent_issue_id] : []),
    ...(issue?.blocked_by ?? []).filter((b) => b.via === "manual").map((b) => b.id),
  ];

  return (
    <IssuePickerModal
      open={open}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      onOpenChangeComplete={onOpenChangeComplete}
      title={t(($) => $.waiting.add)}
      description={t(($) => $.waiting.picker_hint)}
      error={mutation.isError ? mutation.error.message : undefined}
      pending={mutation.isPending}
      closeOnSelect={false}
      excludeIds={excludeIds}
      // A sub-issue can't block its own parent.
      isSelectable={(candidate) => candidate.parent_issue_id !== issueId}
      onSelect={(candidate) =>
        mutation.mutate({ blocked_by: [candidate.id] }, { onSuccess: onClose })
      }
    />
  );
}
