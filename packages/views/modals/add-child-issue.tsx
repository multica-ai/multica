"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueDetailOptions,
  childIssuesOptions,
} from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";

export function AddChildIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const issueId = (data?.issueId as string) || "";
  const wsId = useWorkspaceId();
  const updateIssue = useUpdateIssue();

  const { data: issue = null } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const { data: children = [] } = useQuery({
    ...childIssuesOptions(wsId, issueId),
    enabled: !!issueId,
  });

  const excludeIds = [
    issueId,
    ...(issue?.parent_issue_id ? [issue.parent_issue_id] : []),
    ...children.map((c) => c.id),
  ];

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title="Add sub-issue"
      description="Search for an issue to add as a sub-issue"
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateIssue.mutate(
          { id: selected.id, parent_issue_id: issueId },
          { onError: () => toast.error("Failed to add sub-issue") },
        );
        toast.success(`Added ${selected.identifier} as sub-issue`);
      }}
    />
  );
}
