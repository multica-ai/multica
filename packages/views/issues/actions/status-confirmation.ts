import type { IssueStatus } from "@multica/core/types";

export const CONFIRMABLE_ISSUE_STATUSES = [
  "cancelled",
  "archive",
] as const satisfies readonly IssueStatus[];

export type ConfirmableIssueStatus =
  (typeof CONFIRMABLE_ISSUE_STATUSES)[number];

export function requiresIssueStatusConfirmation(
  status: IssueStatus | undefined | null,
): status is ConfirmableIssueStatus {
  return status === "cancelled" || status === "archive";
}
