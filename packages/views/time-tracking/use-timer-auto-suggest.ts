"use client";

import { useEffect, useRef } from "react";
import type { Issue } from "@multica/core/types";
import { suggestTimerForIssue } from "./suggest-timer-for-issue";

/**
 * Shows a timer-start suggestion toast in two cases:
 *  1. A new in_progress issue is opened (navigation or initial load).
 *  2. The current issue's status transitions to in_progress.
 *
 * Consistent across all views — the toast fires regardless of the route the
 * issue was opened from.
 */
export function useTimerAutoSuggest(issue: Issue | undefined) {
  const prevIssueId = useRef<string | undefined>(undefined);
  const prevStatus = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!issue) {
      // Reset refs during loading gaps so the next load is treated as fresh.
      prevIssueId.current = undefined;
      prevStatus.current = undefined;
      return;
    }

    const prevId = prevIssueId.current;
    const prev = prevStatus.current;
    prevIssueId.current = issue.id;
    prevStatus.current = issue.status;

    const openedDifferentIssue = prevId !== issue.id;
    const viewedInProgressIssue = openedDifferentIssue && issue.status === "in_progress";
    const becameInProgress =
      !openedDifferentIssue &&
      prev !== undefined &&
      prev !== "in_progress" &&
      issue.status === "in_progress";

    if (viewedInProgressIssue || becameInProgress) {
      suggestTimerForIssue(issue);
    }
  }, [issue?.status, issue?.id, issue?.identifier, issue?.title]);
}
