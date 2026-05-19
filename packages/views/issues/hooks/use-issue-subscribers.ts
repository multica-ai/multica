"use client";

import { useIssueSubscribers as useCoreIssueSubscribers } from "@multica/core/issues/hooks";

export function useIssueSubscribers(issueId: string, userId?: string) {
  return useCoreIssueSubscribers("", issueId, userId);
}
