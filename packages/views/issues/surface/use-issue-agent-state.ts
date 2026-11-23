"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { AgentTask } from "@multica/core/types";
import { selectIssueTasks } from "./activity";

export type IssueAgentState = { running: boolean; agentIds: string[] } | null;

/** The agents running on an issue, or queued for it when none is running yet. */
export function useIssueAgentState(wsId: string, issueId: string): IssueAgentState {
  const select = useCallback(
    (snapshot: AgentTask[]): IssueAgentState => {
      if (!issueId) return null;
      const { running, queued } = selectIssueTasks(snapshot, issueId);
      const tasks = running.length > 0 ? running : queued;
      if (tasks.length === 0) return null;
      return { running: running.length > 0, agentIds: [...new Set(tasks.map((task) => task.agent_id))] };
    },
    [issueId],
  );
  const { data = null } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    enabled: Boolean(issueId),
    select,
  });
  return data;
}
