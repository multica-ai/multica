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
import { useT } from "../i18n";

export function AddChildIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
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
      title={t(($) => $.add_child.title)}
      description={t(($) => $.add_child.description)}
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateIssue.mutate(
          { id: selected.id, parent_issue_id: issueId },
          { onError: () => toast.error(t(($) => $.add_child.toast_failed)) },
        );
        toast.success(t(($) => $.add_child.toast_success, { identifier: selected.identifier }));
      }}
    />
  );
}
