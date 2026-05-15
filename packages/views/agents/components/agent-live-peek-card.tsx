"use client";

import { useQuery } from "@tanstack/react-query";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  agentTaskSnapshotOptions,
  useAgentPresenceDetail,
} from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues";
import { timeAgo } from "@multica/core/utils";
import type { AgentTask } from "@multica/core/types";
import { AlertTriangle } from "lucide-react";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { workloadConfig } from "../presence";

interface AgentLivePeekCardProps {
  agentId: string;
}

// Live "peek" card for an agent avatar — shows the three live signals the
// squad members tab cares about (workload, current issue, last activity).
// Companion to AgentProfileCard, which surfaces static identity (description,
// runtime, skills, owner). Keeping them separate avoids polluting the 23+
// existing AgentProfileCard call sites with live-only concerns.
export function AgentLivePeekCard({ agentId }: AgentLivePeekCardProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const presence = useAgentPresenceDetail(wsId, agentId);

  const agent = agents.find((a) => a.id === agentId);

  if (agentsLoading && !agent) {
    return (
      <div className="flex items-center gap-3">
        <Skeleton className="h-10 w-10 rounded-full" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-20" />
        </div>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="text-xs text-muted-foreground">
        {t(($) => $.profile_card.unavailable)}
      </div>
    );
  }

  const agentTasks = snapshot.filter((t) => t.agent_id === agentId);
  const runningTask = agentTasks.find(
    (t) => t.status === "running" && !!t.issue_id,
  );
  const currentIssueId = runningTask?.issue_id ?? null;
  const lastTerminal = pickLatestTerminal(agentTasks);

  const initials = agent.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  const workload = presence === "loading" ? null : presence.workload;
  const workloadVisual = workload ? workloadConfig[workload] : null;

  return (
    <div className="flex flex-col gap-3 text-left">
      {/* Header — avatar + name. */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={agent.name}
          initials={initials}
          avatarUrl={agent.avatar_url}
          isAgent
          size={40}
          className="rounded-md"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold">{agent.name}</p>
          <div className="mt-0.5 inline-flex items-center gap-1.5">
            {workloadVisual ? (
              <>
                <workloadVisual.icon
                  className={`h-3 w-3 shrink-0 ${workloadVisual.textClass}`}
                />
                <span className={`text-xs ${workloadVisual.textClass}`}>
                  {t(($) => $.workload[workload!])}
                </span>
              </>
            ) : (
              <Skeleton className="h-3 w-12" />
            )}
          </div>
        </div>
      </div>

      {/* Meta rows. */}
      <div className="flex flex-col gap-1.5 text-xs">
        <CurrentIssueRow
          wsId={wsId}
          issueId={currentIssueId}
          label={t(($) => $.live_peek.current_issue_label)}
          emptyLabel={t(($) => $.live_peek.no_current_issue)}
          issueHref={(id) => p.issueDetail(id)}
        />
        <LastActivityRow
          task={lastTerminal}
          label={t(($) => $.live_peek.last_activity_label)}
          emptyLabel={t(($) => $.live_peek.no_recent_activity)}
          failedLabel={t(($) => $.live_peek.failed_indicator)}
        />
      </div>
    </div>
  );
}

// Pick the most recent terminal task for last-activity display. Snapshot
// already caps this to one terminal row per agent (see queries.ts header),
// but a defensive max-by-completed_at keeps the card honest if that shape
// ever changes.
function pickLatestTerminal(tasks: readonly AgentTask[]): AgentTask | null {
  let best: AgentTask | null = null;
  for (const t of tasks) {
    if (t.status !== "completed" && t.status !== "failed" && t.status !== "cancelled") {
      continue;
    }
    if (!t.completed_at) continue;
    if (!best || (best.completed_at && t.completed_at > best.completed_at)) {
      best = t;
    }
  }
  return best;
}

function CurrentIssueRow({
  wsId,
  issueId,
  label,
  emptyLabel,
  issueHref,
}: {
  wsId: string;
  issueId: string | null;
  label: string;
  emptyLabel: string;
  issueHref: (id: string) => string;
}) {
  // Lazy issue detail — only enabled while the card is mounted AND we have
  // a running issue id. snapshot already gives us the id; this hook just
  // resolves the human identifier (MUL-123) + title.
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId ?? ""),
    enabled: !!issueId,
  });

  return (
    <div className="flex items-center gap-1.5">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      {issueId ? (
        issue ? (
          <AppLink
            href={issueHref(issueId)}
            className="min-w-0 truncate text-brand hover:underline"
            title={`${issue.identifier} ${issue.title}`}
          >
            <span className="mr-1 font-mono text-[11px]">{issue.identifier}</span>
            <span>{issue.title}</span>
          </AppLink>
        ) : (
          <Skeleton className="h-3 w-24" />
        )
      ) : (
        <span className="text-muted-foreground">{emptyLabel}</span>
      )}
    </div>
  );
}

function LastActivityRow({
  task,
  label,
  emptyLabel,
  failedLabel,
}: {
  task: AgentTask | null;
  label: string;
  emptyLabel: string;
  failedLabel: string;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      {task && task.completed_at ? (
        <span className="inline-flex min-w-0 items-center gap-1 truncate">
          <span className="truncate">{timeAgo(task.completed_at)}</span>
          {task.status === "failed" && (
            // Failed terminal state shows here only — workload above stays a
            // clean "what's on the plate now" reading (working/queued/idle),
            // matching the project's deliberate split between current and
            // historical state.
            <span
              className="inline-flex items-center gap-0.5 rounded bg-warning/10 px-1 py-0.5 text-[10px] font-medium text-warning"
              title={failedLabel}
            >
              <AlertTriangle className="h-2.5 w-2.5" />
              {failedLabel}
            </span>
          )}
        </span>
      ) : (
        <span className="text-muted-foreground">{emptyLabel}</span>
      )}
    </div>
  );
}
