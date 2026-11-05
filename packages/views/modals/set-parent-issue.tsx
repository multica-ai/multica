"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  childIssuesOptions,
} from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useParentIssueRecents } from "@multica/core/issues/use-parent-issue-recents";
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
  const recentIssues = useParentIssueRecents(wsId, true);

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
      suggestions={[{
        key: "recent",
        heading: t(($) => $.issue_picker.suggested_recent),
        issues: recentIssues,
      }]}
      onSelect={async (selected) => {
        try {
          // The picker closes immediately after selection. Await the mutation
          // so its result still reaches the user after this modal unmounts.
          await updateIssue.mutateAsync({
            id: issueId,
            parent_issue_id: selected.id,
          });
          toast.success(
            t(($) => $.set_parent.toast_success, {
              identifier: selected.identifier,
            }),
          );
        } catch (err) {
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.set_parent.toast_failed),
          );
        }
      }}
    />
  );
}
