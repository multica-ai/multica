"use client";

// FIR-3901 — a "dead" failed run is one that failed and that nothing is going
// to pick up again on its own. The server owns that definition (see
// cerebrodb.ListDeadFailedTasksForIssue); this module is the read side for the
// two surfaces that show it: the red bar on an issue and the red pip in the
// inbox.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";

export interface DeadFailedRun {
  task_id: string;
  agent_id?: string;
  issue_id: string;
  parent_issue_id?: string;
  failure_reason?: string;
  error?: string;
  completed_at?: string;
  attempt: number;
  max_attempts: number;
  /** False when the daemon could not pick the conversation back up. */
  resume_possible: boolean;
  /** Plain-words explanation shown when resume_possible is false. */
  blocked_reason?: string;
  runtime_name?: string;
}

interface FailedRunsPayload {
  runs?: DeadFailedRun[];
}

// Lenient on purpose: only the fields the two surfaces actually branch on are
// required. A server that adds a field, drops an optional one, or widens
// failure_reason must still render — see CLAUDE.md "API Response Compatibility".
const deadFailedRunSchema = z.object({
  task_id: z.string(),
  issue_id: z.string(),
  attempt: z.number(),
  max_attempts: z.number(),
  resume_possible: z.boolean().optional(),
}).loose();

const failedRunsPayloadSchema = z.object({
  runs: z.array(deadFailedRunSchema).optional(),
}).loose();

/** Parse a failed-runs body, degrading to an empty list rather than throwing. */
export function parseFailedRunsPayload(data: unknown, endpoint: string): DeadFailedRun[] {
  const parsed = parseWithFallback<FailedRunsPayload>(
    data,
    failedRunsPayloadSchema,
    {},
    { endpoint },
  );
  return Array.isArray(parsed.runs) ? parsed.runs : [];
}

export const ISSUE_FAILED_RUNS_KEY = (issueId: string) =>
  ["cerebro-issue-failed-runs", issueId] as const;

// NOTE: the literal "cerebro-inbox-failed-runs" key is mirrored in the
// CEREBRO-PATCH(inbox-failed-run-realtime) patch in
// packages/core/realtime/use-realtime-sync.ts, which invalidates it on
// task:failed so a fresh failure lights the pip without a manual refresh.
// packages/core cannot import this package, so renaming the key means
// updating that patch too.
export const INBOX_FAILED_RUNS_KEY = (wsId: string) =>
  ["cerebro-inbox-failed-runs", wsId] as const;

/**
 * Unresolved failed runs on one issue, newest first. Drives the red bar.
 * `enabled` gates the fetch behind the feature flag.
 */
export function useIssueFailedRuns(issueId: string, enabled: boolean): DeadFailedRun[] {
  const { data } = useQuery({
    queryKey: ISSUE_FAILED_RUNS_KEY(issueId),
    queryFn: () => api.listIssueFailedRuns<unknown>(issueId),
    enabled: enabled && !!issueId,
    staleTime: 15_000,
  });
  return useMemo(
    () => parseFailedRunsPayload(data, "GET /api/issues/:id/failed-runs"),
    [data],
  );
}

/**
 * One failed-run hint per issue for the inbox. Pure reducer over the workspace
 * list — exported separately so it can be unit-tested without a query client.
 *
 * A parent issue inherits its sub-issue's failure only when the parent has no
 * failure of its own, mirroring how the running-pip treats sub-issues: the
 * row's own state always wins.
 */
export function buildInboxFailedRunHints(
  runs: DeadFailedRun[],
): Map<string, { title: string; resumePossible: boolean }> {
  const map = new Map<string, { title: string; resumePossible: boolean }>();
  for (const run of runs) {
    if (!run?.issue_id) continue;
    // Newest-first from the server, so the first entry for an issue wins.
    if (map.has(run.issue_id)) continue;
    map.set(run.issue_id, {
      title: run.resume_possible ? "Run failed — can be continued" : "Run failed",
      resumePossible: run.resume_possible === true,
    });
  }
  return map;
}

/** Issues in this workspace with an unresolved failed run. Drives the red pip. */
export function useInboxFailedRunStates(
  wsId: string,
  enabled: boolean,
): Map<string, { title: string; resumePossible: boolean }> {
  const { data } = useQuery({
    queryKey: INBOX_FAILED_RUNS_KEY(wsId),
    queryFn: () => api.listWorkspaceFailedRuns<unknown>(),
    enabled: enabled && !!wsId,
    staleTime: 30_000,
  });
  return useMemo(
    () =>
      buildInboxFailedRunHints(
        parseFailedRunsPayload(data, "GET /api/inbox/failed-issue-tasks"),
      ),
    [data],
  );
}
