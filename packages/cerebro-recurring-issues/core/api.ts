import { api, parseWithFallback } from "@multica/core/api";

import { EMPTY_RUN_RESULT, issueRecurrenceSchema, runResultSchema } from "./api-schemas";
import type { IssueRecurrence, RecurrenceWriteInput } from "./types";

export async function fetchIssueRecurrence(issueId: string): Promise<IssueRecurrence | null> {
  try {
    const raw = await api.cerebroRequest<unknown>(`/api/cerebro/issues/${issueId}/recurrence`);
    return parseWithFallback(raw, issueRecurrenceSchema, null, {
      endpoint: "fetchIssueRecurrence",
    });
  } catch (err) {
    // 404 → the issue is not recurring yet; the panel shows the empty state.
    if (isNotFound(err)) return null;
    throw err;
  }
}

export async function upsertIssueRecurrence(
  issueId: string,
  payload: RecurrenceWriteInput,
): Promise<IssueRecurrence | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/issues/${issueId}/recurrence`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, issueRecurrenceSchema, null, {
    endpoint: "upsertIssueRecurrence",
  });
}

export async function deleteIssueRecurrence(issueId: string): Promise<void> {
  await api.cerebroRequest<void>(`/api/cerebro/issues/${issueId}/recurrence`, {
    method: "DELETE",
  });
}

export async function runIssueRecurrence(recurrenceId: string): Promise<{ spawned: boolean }> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/issue-recurrences/${recurrenceId}/run`,
    { method: "POST" },
  );
  return parseWithFallback(raw, runResultSchema, EMPTY_RUN_RESULT, {
    endpoint: "runIssueRecurrence",
  });
}

function isNotFound(err: unknown): boolean {
  if (typeof err !== "object" || err === null) return false;
  const status = (err as { status?: unknown }).status;
  return status === 404;
}
