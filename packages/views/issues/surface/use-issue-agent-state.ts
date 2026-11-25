"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { squadListOptions } from "@multica/core/workspace/queries";
import type { AgentTask } from "@multica/core/types";
import { selectIssueTasks } from "./activity";
import {
  groupExecutionTasksByActor,
  type ExecutionActorRef,
} from "./execution-actors";

export type IssueAgentState = {
  running: boolean;
  agentIds: string[];
  actors: ExecutionActorRef[];
} | null;

/** The execution units (squads / standalone agents) running on an issue, or queued when none is running yet. */
export function useIssueAgentState(wsId: string, issueId: string): IssueAgentState {
  const { data: squads } = useQuery({
    ...squadListOptions(wsId),
    enabled: Boolean(wsId && issueId),
  });
  const knownSquadIds = useMemo(
    () => (squads ? new Set(squads.map((s) => s.id)) : null),
    [squads],
  );
  const select = useCallback(
    (snapshot: AgentTask[]) => {
      if (!issueId) return null;
      const { running, queued } = selectIssueTasks(snapshot, issueId);
      const tasks = running.length > 0 ? running : queued;
      if (tasks.length === 0) return null;
      return { running: running.length > 0, tasks };
    },
    [issueId],
  );
  const { data = null } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    enabled: Boolean(issueId),
    select,
  });
  return useMemo(() => {
    if (!data) return null;
    const grouped = groupExecutionTasksByActor(data.tasks, knownSquadIds);
    return {
      running: data.running,
      agentIds: [...new Set(data.tasks.map((task) => task.agent_id))],
      actors: grouped.map(({ type, id }) => ({ type, id })),
    };
  }, [data, knownSquadIds]);
}
