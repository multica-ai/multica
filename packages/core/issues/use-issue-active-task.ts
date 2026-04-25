"use client";

import { useState, useEffect, useCallback } from "react";
import { api } from "../api";
import { useWSEvent } from "../realtime";

export function isTaskPayloadForIssue(payload: unknown, issueId: string): boolean {
  if (!payload || typeof payload !== "object") return false;
  const payloadIssueId = (payload as { issue_id?: unknown }).issue_id;
  return typeof payloadIssueId === "string" && payloadIssueId.length > 0 && payloadIssueId === issueId;
}

export function useIssueActiveTask(issueId: string): { isAgentRunning: boolean } {
  const [isAgentRunning, setIsAgentRunning] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api.getActiveTasksForIssue(issueId).then(({ tasks }) => {
      if (cancelled) return;
      setIsAgentRunning(tasks.some((t) => t.status === "running" || t.status === "dispatched"));
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [issueId]);

  useWSEvent(
    "task:dispatch",
    useCallback((payload: unknown) => {
      if (!isTaskPayloadForIssue(payload, issueId)) return;
      setIsAgentRunning(true);
    }, [issueId]),
  );

  const handleTaskEnd = useCallback((payload: unknown) => {
    if (!isTaskPayloadForIssue(payload, issueId)) return;
    api.getActiveTasksForIssue(issueId).then(({ tasks }) => {
      setIsAgentRunning(tasks.some((t) => t.status === "running" || t.status === "dispatched"));
    }).catch(() => {});
  }, [issueId]);

  useWSEvent("task:completed", handleTaskEnd);
  useWSEvent("task:failed", handleTaskEnd);
  useWSEvent("task:cancelled", handleTaskEnd);

  return { isAgentRunning };
}
