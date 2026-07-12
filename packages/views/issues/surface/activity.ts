"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentTask } from "@multica/core/types";

export interface IssueActivityState {
  isWorking: boolean;
  isQueued: boolean;
  runningTasks: AgentTask[];
  queuedTasks: AgentTask[];
}

export interface IssueSurfaceActivity {
  activityByIssueId: Map<string, IssueActivityState>;
  runningIssueIds: Set<string>;
}

function isQueuedTaskStatus(status: AgentTask["status"]) {
  return (
    status === "queued" ||
    status === "dispatched" ||
    status === "waiting_local_directory"
  );
}

export function deriveIssueSurfaceActivity(
  tasks: readonly AgentTask[],
): IssueSurfaceActivity {
  const activityByIssueId = new Map<string, IssueActivityState>();

  for (const task of tasks) {
    if (!task.issue_id) continue;
    if (task.status !== "running" && !isQueuedTaskStatus(task.status)) {
      continue;
    }

    const current =
      activityByIssueId.get(task.issue_id) ?? {
        isWorking: false,
        isQueued: false,
        runningTasks: [],
        queuedTasks: [],
      };

    if (task.status === "running") {
      current.runningTasks.push(task);
      current.isWorking = true;
    } else {
      current.queuedTasks.push(task);
      current.isQueued = true;
    }

    activityByIssueId.set(task.issue_id, current);
  }

  const runningIssueIds = new Set<string>();
  for (const [issueId, activity] of activityByIssueId) {
    if (activity.isWorking) runningIssueIds.add(issueId);
  }

  return { activityByIssueId, runningIssueIds };
}

export function useIssueSurfaceActivity(): IssueSurfaceActivity {
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  return useMemo(() => deriveIssueSurfaceActivity(snapshot), [snapshot]);
}
