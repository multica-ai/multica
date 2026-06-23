"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { IssueAssigneeType, IssueStatus, IssueTriggerPreviewItem } from "@multica/core/types";

export interface UseIssueTriggerPreviewParams {
  /** Existing issues to evaluate (single assign/status or batch). */
  issueIds?: string[];
  /** Preview a not-yet-persisted issue from assignee/status (create modal). */
  isCreate?: boolean;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  status?: IssueStatus;
  /** Caller gate — e.g. only fetch while a picker/modal is open. */
  enabled?: boolean;
}

export interface UseIssueTriggerPreviewResult {
  triggers: IssueTriggerPreviewItem[];
  totalCount: number;
  isLoading: boolean;
  /** True when every trigger's target runtime can render a handoff note, so
   *  the note box is safe to enable. False if any started run would drop it. */
  handoffSupported: boolean;
}

const EMPTY: IssueTriggerPreviewItem[] = [];

function previewSignature(params: UseIssueTriggerPreviewParams): string {
  return JSON.stringify({
    ids: [...(params.issueIds ?? [])].sort(),
    create: params.isCreate ?? false,
    at: params.assigneeType ?? null,
    aid: params.assigneeId ?? null,
    status: params.status ?? null,
  });
}

/** Reads the unified backend predicate via POST /api/issues/preview-trigger so
 *  the four entry points never re-implement "will this start a run" (MUL-3375).
 *  The answer is queue-dependent (status-source pending dedup), so it is never
 *  cached stale: staleTime 0, and WS task events invalidate issueTriggerPreviewAll. */
export function useIssueTriggerPreview(
  params: UseIssueTriggerPreviewParams,
): UseIssueTriggerPreviewResult {
  const hasTarget =
    (!!params.assigneeType && !!params.assigneeId) ||
    !!params.status ||
    (params.isCreate ?? false);
  const enabled = (params.enabled ?? true) && hasTarget;

  const signature = useMemo(() => previewSignature(params), [params]);

  const previewQuery = useQuery({
    queryKey: issueKeys.issueTriggerPreview(signature),
    queryFn: () =>
      api.previewIssueTrigger({
        issueIds: params.issueIds,
        isCreate: params.isCreate,
        assigneeType: params.assigneeType,
        assigneeId: params.assigneeId,
        status: params.status,
      }),
    enabled,
    retry: false,
    staleTime: 0,
  });

  const triggers = previewQuery.data?.triggers ?? EMPTY;
  return {
    triggers,
    totalCount: previewQuery.data?.total_count ?? 0,
    isLoading: enabled && previewQuery.isFetching,
    handoffSupported: triggers.length > 0 && triggers.every((t) => t.handoff_supported === true),
  };
}
