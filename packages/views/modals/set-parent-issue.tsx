"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { childIssuesOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

export function SetParentIssueModal({
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
      title={t(($) => $.set_parent.title)}
      description={t(($) => $.set_parent.description)}
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateIssue.mutate(
          { id: issueId, parent_issue_id: selected.id },
          { onError: () => toast.error(t(($) => $.set_parent.toast_failed)) },
        );
        toast.success(t(($) => $.set_parent.toast_success, { identifier: selected.identifier }));
      }}
    />
  );
}
