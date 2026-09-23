"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueDetailOptions,
  issueDuplicatesOptions,
  issueKeys,
} from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import {
  selectRecentIssues,
  useRecentIssuesStore,
} from "@multica/core/issues/stores/recent-issues-store";
import type { Issue } from "@multica/core/types";
import { isDuplicateIssue } from "../issues/components/issue-duplicates";
import { IssuePickerModal, type IssuePickerSuggestionGroup } from "./issue-picker-modal";
import { useT } from "../i18n";

const SUGGESTION_LIMIT = 6;

const notDuplicate = (candidate: Issue) => !isDuplicateIssue(candidate);

/**
 * Marks an issue as a duplicate of the picked one (MUL-7349). A duplicate is a
 * cancelled issue that remembers its original, so the write also cancels it.
 *
 * The picker opens with candidates instead of an empty search: issues whose
 * title matches this one's (the same bug filed twice) and the issues viewed
 * most recently, since the original is usually the one that prompted the
 * mark. Issues that are duplicates themselves cannot be originals and are
 * left out of candidates and search results alike.
 */
export function MarkDuplicateIssueModal({
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

  // An issue's own duplicates can never be its original.
  const { data: relations } = useQuery({
    ...issueDuplicatesOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const excludeIds = [issueId, ...(relations?.duplicates.map((d) => d.id) ?? [])];

  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const title = issue?.title.trim() ?? "";
  const { data: sameTitle } = useQuery({
    queryKey: [...issueKeys.all(wsId), "duplicate-candidates", issueId, title],
    queryFn: () => api.searchIssues({ q: title, limit: SUGGESTION_LIMIT, include_closed: true }),
    enabled: title !== "",
    staleTime: 60_000,
  });

  const recentEntries = useRecentIssuesStore(selectRecentIssues(wsId));
  const recentQueries = useQueries({
    queries: recentEntries
      .filter((entry) => entry.id !== issueId)
      .slice(0, SUGGESTION_LIMIT)
      .map((entry) => issueDetailOptions(wsId, entry.id)),
  });
  const recent = useMemo(
    () => recentQueries.flatMap((q) => (q.data ? [q.data] : [])),
    [recentQueries],
  );

  const suggestions = useMemo<IssuePickerSuggestionGroup[]>(() => {
    const similar = sameTitle?.issues ?? [];
    const seen = new Set(similar.map((i) => i.id));
    return [
      { key: "similar", heading: t(($) => $.issue_picker.suggested_similar), issues: similar },
      {
        key: "recent",
        heading: t(($) => $.issue_picker.suggested_recent),
        issues: recent.filter((i) => !seen.has(i.id)),
      },
    ];
  }, [sameTitle, recent, t]);

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.mark_duplicate.title)}
      description={t(($) => $.mark_duplicate.description)}
      excludeIds={excludeIds}
      suggestions={suggestions}
      isSelectable={notDuplicate}
      onSelect={(selected) => {
        updateIssue.mutate(
          {
            id: issueId,
            status: "cancelled",
            duplicate_of_issue_id: selected.id,
          },
          {
            onSuccess: () =>
              toast.success(
                t(($) => $.mark_duplicate.toast_success, {
                  identifier: selected.identifier,
                }),
              ),
            onError: (err) => {
              switch (errorCode(err)) {
                case "duplicate_target_is_duplicate":
                  toast.error(
                    t(($) => $.mark_duplicate.error_target_is_duplicate, {
                      identifier: selected.identifier,
                    }),
                  );
                  return;
                case "issue_has_duplicates":
                  toast.error(t(($) => $.mark_duplicate.error_has_duplicates));
                  return;
                default:
                  toast.error(
                    err instanceof Error && err.message
                      ? err.message
                      : t(($) => $.mark_duplicate.toast_failed),
                  );
              }
            },
          },
        );
      }}
    />
  );
}
