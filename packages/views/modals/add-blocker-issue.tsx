"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useUpdateIssueBlockers, useUpdateIssueBlocking } from "@multica/core/issues/blockers";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

/**
 * Adds a manual blocker edge (MUL-8860). `blocked_by` picks an issue this one
 * waits for; `blocking` picks an issue that waits for this one, written on
 * that issue (MUL-10050). Shared by the sidebar's Relations section, its add
 * row and the Relations submenu of the issue actions menu, so every entry
 * point writes through the same path. The picker stays open while the write
 * is in flight and shows the server's error in place (a cycle, an issue the
 * caller can't see), closing only once the edge is saved.
 */
export function AddBlockerIssueModal({
  open = true,
  onClose,
  onOpenChangeComplete,
  data,
  direction = "blocked_by",
}: {
  open?: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
  data: Record<string, unknown> | null;
  direction?: "blocked_by" | "blocking";
}) {
  const { t } = useT("issues");
  const issueId = (data?.issueId as string) || "";
  const wsId = useWorkspaceId();
  const blockedBy = useUpdateIssueBlockers(wsId, issueId);
  const blocking = useUpdateIssueBlocking(wsId, issueId);
  const mutation = direction === "blocking" ? blocking : blockedBy;

  // List rows don't carry blocked_by or blocking, so read the detail for
  // what to exclude.
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const linked = direction === "blocking" ? issue?.blocking : issue?.blocked_by;
  const excludeIds = [
    issueId,
    ...(issue?.parent_issue_id ? [issue.parent_issue_id] : []),
    ...(linked ?? []).filter((b) => b.via === "manual").map((b) => b.id),
  ];

  return (
    <IssuePickerModal
      open={open}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      onOpenChangeComplete={onOpenChangeComplete}
      title={direction === "blocking" ? t(($) => $.relations.block_title) : t(($) => $.relations.wait_title)}
      description={direction === "blocking" ? t(($) => $.relations.blocking_picker_hint) : t(($) => $.waiting.picker_hint)}
      error={mutation.isError ? mutation.error.message : undefined}
      pending={mutation.isPending}
      closeOnSelect={false}
      excludeIds={excludeIds}
      // A parent and its sub-issues can't block each other either way.
      isSelectable={(candidate) => candidate.parent_issue_id !== issueId}
      onSelect={(candidate) =>
        direction === "blocking"
          ? blocking.mutate({ dependentId: candidate.id }, { onSuccess: onClose })
          : blockedBy.mutate({ blocked_by: [candidate.id] }, { onSuccess: onClose })
      }
    />
  );
}
