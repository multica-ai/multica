"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { deriveAgentAvailability } from "@multica/core/agents";
import type { AgentTask, Issue } from "@multica/core/types";
import { workloadConfig } from "../presence";
import { useT } from "../../i18n";

interface AgentActivityHoverContentProps {
  // Active tasks (running / queued / dispatched) to render — caller filters
  // by issue id or by workspace scope. Order is preserved; we render every
  // task as its own row.
  tasks: readonly AgentTask[];
}

/**
 * Tick `now` once per second so duration labels update live while a hover
 * card is open. setInterval only runs while the card is mounted (Base UI
 * portals the content but tears it down on close), so this costs nothing
 * when the card is closed.
 */
function useActivityNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

/**
 * O(1) agent + runtime lookups so each task row resolves without an N×M
 * scan. Cheap — agents/runtimes count in tens at most.
 */
function useActivityLookups() {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const agentById = new Map(agents.map((a) => [a.id, a] as const));
  const runtimeById = new Map(runtimes.map((r) => [r.id, r] as const));
  return { agentById, runtimeById };
}

type ActivityLookups = ReturnType<typeof useActivityLookups>;

/**
 * One task row: agent avatar, name, status dot, status label, duration.
 *
 * Status colour follows the workspace's existing composition rule:
 *   - running                       → brand (text-brand)
 *   - queued, runtime online        → muted gray (transient race)
 *   - queued, runtime offline/etc.  → warning amber (genuine stuck)
 * — same rule as agent-presence-indicator.tsx so users see a single,
 * consistent language for "agent is in trouble" vs "just enqueued".
 */
function AgentActivityTaskRow({
  task,
  now,
  agentById,
  runtimeById,
}: {
  task: AgentTask;
  now: number;
} & ActivityLookups) {
  const { t } = useT("issues");
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();

  const agent = agentById.get(task.agent_id);
  const runtime = runtimeFrom(agent?.runtime_id, runtimeById);
  const availability = deriveAgentAvailability(runtime, now);
  const isRunning = task.status === "running";
  // queued/dispatched both read as "queued" in the user-facing copy —
  // `dispatched` is the daemon-acked sub-state of queued and not
  // user-meaningful here.
  const wl = isRunning ? workloadConfig.working : workloadConfig.queued;
  // queued + online → muted gray (transient race, no warning);
  // queued + offline/unstable → keep warning amber from workloadConfig.
  // Mirrors agent-presence-indicator.tsx.
  const dotClass = isRunning
    ? "bg-brand"
    : availability === "online"
      ? "bg-muted-foreground/40"
      : "bg-warning";
  const labelClass = isRunning
    ? wl.textClass
    : availability === "online"
      ? "text-muted-foreground"
      : wl.textClass;
  const startedFrom = isRunning
    ? (task.started_at ?? task.dispatched_at ?? task.created_at)
    : task.created_at;

  return (
    <div className="flex items-center gap-2 text-xs">
      <ActorAvatarBase
        name={getActorName("agent", task.agent_id)}
        initials={getActorInitials("agent", task.agent_id)}
        avatarUrl={getActorAvatarUrl("agent", task.agent_id)}
        isAgent
        size="sm"
      />
      <span className="flex-1 truncate font-medium">
        {getActorName("agent", task.agent_id)}
      </span>
      <span className="flex shrink-0 items-center gap-1.5">
        <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
        <span className={labelClass}>
          {isRunning
            ? t(($) => $.agent_activity.status_running)
            : t(($) => $.agent_activity.status_queued)}
        </span>
        <span className="tabular-nums text-muted-foreground">
          {formatDuration(startedFrom, now)}
        </span>
      </span>
    </div>
  );
}

/**
 * Shared hover-card body for "what are these agents doing right now?" — used
 * by IssueAgentActivityIndicator (per-issue). One row per task.
 *
 * The workspace-wide chip uses WorkspaceAgentActivityHoverContent below,
 * which groups the same rows by issue.
 */
export function AgentActivityHoverContent({
  tasks,
}: AgentActivityHoverContentProps) {
  const { t } = useT("issues");
  const now = useActivityNow();
  const { agentById, runtimeById } = useActivityLookups();

  if (tasks.length === 0) return null;

  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-muted-foreground">
        {/* One row per task, so count tasks — not agents. A single agent can
            run several tasks at once, so an agent-worded header here would
            disagree with the row count below. */}
        {t(($) => $.agent_activity.hover_header_tasks, { count: tasks.length })}
      </div>
      <div className="flex flex-col gap-1.5">
        {tasks.map((task) => (
          <AgentActivityTaskRow
            key={task.id}
            task={task}
            now={now}
            agentById={agentById}
            runtimeById={runtimeById}
          />
        ))}
      </div>
    </div>
  );
}

interface WorkspaceAgentActivityHoverContentProps {
  /** Issues the working filter leaves on screen, in list order. Each has at
   *  least one running task. */
  issues: readonly Issue[];
  /** Running tasks for those issues, keyed by issue id. */
  tasksByIssueId: ReadonlyMap<string, readonly AgentTask[]>;
  /** Total running tasks across `issues` — the second header figure. */
  taskCount: number;
}

/**
 * Hover-card body for the workspace working chip (MUL-4884).
 *
 * The chip says WHO is working ("N agents working"); this card says WHERE.
 * The header carries the two figures the chip does not — how many issues
 * that work lands on, and how many tasks it takes — and the rows group by
 * issue, mirroring what clicking the chip does to the list.
 *
 * It says nothing about work it excludes. Chat/autopilot runs have no
 * linked issue and leave no trace anywhere on this page: no row, no head,
 * no indicator. A footnote about them would explain an absence the user
 * never perceived — inventing a discrepancy rather than resolving one.
 * Same for tasks on issues the current filters or the loaded page exclude.
 *
 * Deliberately not a dashboard: two figures and grouped rows.
 */
export function WorkspaceAgentActivityHoverContent({
  issues,
  tasksByIssueId,
  taskCount,
}: WorkspaceAgentActivityHoverContentProps) {
  const { t } = useT("issues");
  const now = useActivityNow();
  const { agentById, runtimeById } = useActivityLookups();

  if (issues.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        {t(($) => $.agent_activity.empty_hover)}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2.5">
      <div className="text-xs font-medium text-muted-foreground">
        {`${t(($) => $.agent_activity.issues_count, {
          count: issues.length,
        })} · ${t(($) => $.agent_activity.tasks_count, { count: taskCount })}`}
      </div>
      <div className="flex flex-col gap-2.5">
        {issues.map((issue) => (
          <div key={issue.id} className="flex flex-col gap-1.5">
            <div className="flex items-baseline gap-1.5 text-xs">
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                {issue.identifier}
              </span>
              <span className="truncate">{issue.title}</span>
            </div>
            <div className="flex flex-col gap-1.5">
              {(tasksByIssueId.get(issue.id) ?? []).map((task) => (
                <AgentActivityTaskRow
                  key={task.id}
                  task={task}
                  now={now}
                  agentById={agentById}
                  runtimeById={runtimeById}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function runtimeFrom<T extends { id: string }>(
  id: string | undefined,
  byId: Map<string, T>,
): T | null {
  if (!id) return null;
  return byId.get(id) ?? null;
}

// Compact `2m 14s` / `45s` / `1h 03m` duration since the given ISO string.
// Capped at hours — anything over a day for a running task is a sign of a
// stuck runtime, but the hover card is not the place to relitigate that;
// the row will read as `26h 12m` and the user can act.
//
// Exported so the issue-detail header live chip formats its collapsed
// single-agent elapsed with the same `2m 14s` / `1h 03m` rule used here.
export function formatDuration(fromIso: string, nowMs: number): string {
  const start = new Date(fromIso).getTime();
  if (!Number.isFinite(start)) return "";
  const sec = Math.max(0, Math.round((nowMs - start) / 1000));
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  const remSec = sec % 60;
  if (min < 60) return `${min}m ${pad2(remSec)}s`;
  const hr = Math.floor(min / 60);
  const remMin = min % 60;
  return `${hr}h ${pad2(remMin)}m`;
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}
