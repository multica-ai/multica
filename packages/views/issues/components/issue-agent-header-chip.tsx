"use client";

import { memo, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { AgentTask } from "@multica/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { useT } from "../../i18n";

// Per-issue "is an agent working on this right now?" chip for the issue
// detail header. Lives in the header (not the scrollable body) so the live
// signal stays in one fixed place and never competes with future sticky
// banners in the content column. Replaces the in-body sticky live card.
//
// Derives state from the workspace-wide agent task snapshot filtered by
// issue id — the same single source of truth that powers the board-card /
// list-row IssueAgentActivityIndicator, so the chip is always consistent
// with those surfaces and costs zero extra network.
//
// Collapsed display stays intentionally shallow:
//   - one active agent   → avatar + "{name} is working"
//   - multiple agents   → avatar stack + "N agents working"
//   - queued only       → same copy, half-opacity avatars / muted text
//
// Events, elapsed time, transcript, and stop controls belong to the right
// panel ExecutionLogSection. The header is a scan-only status segment.

interface IssueAgentHeaderChipProps {
  issueId: string;
}

export const IssueAgentHeaderChip = memo(function IssueAgentHeaderChip({
  issueId,
}: IssueAgentHeaderChipProps) {
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const { running, queued } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    for (const task of snapshot) {
      if (task.issue_id !== issueId) continue;
      if (task.status === "running") running.push(task);
      else if (
        task.status === "queued" ||
        task.status === "dispatched" ||
        // Daemon-parked on a busy local_directory — still active, just
        // waiting on a path lock. Belongs in the live chip, not dropped.
        task.status === "waiting_local_directory"
      )
        queued.push(task);
      // Terminal statuses are the execution log's story, not the live chip's.
    }
    return { running, queued };
  }, [snapshot, issueId]);

  // No active work → render nothing. Header details stay out of this component;
  // the right panel owns per-task timing, messages, transcript, and stop controls.
  if (running.length === 0 && queued.length === 0) return null;

  return <ActiveChip running={running} queued={queued} />;
});

interface ActiveChipProps {
  running: AgentTask[];
  queued: AgentTask[];
}

function ActiveChip({ running, queued }: ActiveChipProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();

  const activeTasks = [...running, ...queued];
  const agentIds = [...new Set(activeTasks.map((task) => task.agent_id))];
  const anyRunning = running.length > 0;
  const isSingle = agentIds.length === 1;
  const label = isSingle
    ? t(($) => $.agent_live.is_working, {
        name: getActorName("agent", agentIds[0] ?? ""),
      })
    : t(($) => $.agent_activity.hover_header, {
        count: agentIds.length,
      });

  return (
    <div className="flex items-center gap-1">
      <div
        role="status"
        aria-label={label}
        className="flex h-7 max-w-[11rem] items-center gap-1.5 rounded-md px-1.5 text-muted-foreground"
      >
        <AgentAvatarStack
          agentIds={agentIds}
          size={18}
          max={3}
          opacity={anyRunning ? "full" : "half"}
        />
        <span
          className={`min-w-0 truncate text-xs ${anyRunning ? "text-info" : "text-muted-foreground"}`}
        >
          {label}
        </span>
      </div>
      {/* Separator from the action buttons — the chip is a status segment,
          not another button, so a hairline keeps the two groups legible. */}
      <span className="h-4 w-px bg-border" aria-hidden="true" />
    </div>
  );
}
