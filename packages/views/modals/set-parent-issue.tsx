"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { childIssuesOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";

export function SetParentIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const issueId = (data?.issueId as string) || "";
  const wsId = useWorkspaceId();
  const updateIssue = useUpdateIssue();

  const { data: children = [] } = useQuery({
    ...childIssuesOptions(wsId, issueId),
    enabled: !!issueId,
  });

  const excludeIds = [issueId, ...children.map((c) => c.id)];

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title="Set parent issue"
      description="Search for an issue to set as the parent of this issue"
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateIssue.mutate(
          { id: issueId, parent_issue_id: selected.id },
          { onError: () => toast.error("Failed to update issue") },
        );
        toast.success(`Set ${selected.identifier} as parent issue`);
      }}
    />
  );
}
