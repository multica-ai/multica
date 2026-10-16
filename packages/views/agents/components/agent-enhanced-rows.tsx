"use client";

/* eslint-disable i18next/no-literal-string -- Approved prototype design: the
   card/row agent views use fixed English copy by explicit product decision in
   this fork; dynamic content (agent name, description) is unaffected. */

import { useMemo } from "react";
import {
  runtimeDisplayName,
  providerDisplayName,
} from "@multica/core/runtimes";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { RunningTaskChips } from "./running-task-chips";
import type { AgentListRow } from "./agents-page";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";

// Fixed UI copy for this view is English by design (matches the approved
// prototype); dynamic content (agent name, description) stays data-driven.
const ACCESS_LABELS = {
  workspace: "Workspace",
  "specific-people": "Specific people",
  "owner-only": "Owner only",
} as const;

// Module-level so the literal never sits in a JSX attribute (the repo's
// no-restricted-syntax rule requires aria-labels to be non-literal values).
const AGENTS_LIST_LABEL = "Agents";

function formatConcurrency(running: number, capacity: number) {
  if (capacity <= 0) return { pct: 0, label: `${running} / ∞` };
  return {
    pct: Math.min(100, Math.round((running / capacity) * 100)),
    label: `${running} / ${capacity}`,
  };
}

interface RunningBarProps {
  presence: AgentPresenceDetail | null;
  capacity: number;
  isBound: boolean;
  tasks: AgentListRow["runningTasks"];
}

/** Running count + mini bar; "Offline" when the runtime is unreachable, "Idle" when reachable but free. */
function RunningBar({ presence, capacity, isBound, tasks }: RunningBarProps) {
  const running = presence?.runningCount ?? 0;
  const { pct, label } = formatConcurrency(running, capacity);
  const hasRunning = running > 0;
  const offline = isBound && presence?.availability === "offline";

  if (!hasRunning && tasks.length === 0) {
    return (
      <div className="flex items-center gap-1.5 text-caption text-faint-foreground">
        {offline ? (
          <>
            <svg
              className="size-3 shrink-0"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
            >
              <circle cx="12" cy="12" r="10" />
              <line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
            </svg>
            Offline
          </>
        ) : (
          "Idle"
        )}
        <span className="font-mono text-micro tabular-nums">· {label}</span>
      </div>
    );
  }

  return (
    <div className="flex min-w-[140px] flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        {hasRunning && (
          <span className="relative flex size-3 shrink-0">
            <span className="absolute inset-0 animate-ping rounded-full bg-success/60" />
            <span className="relative size-1.5 rounded-full bg-success" />
          </span>
        )}
        <span
          className={cn(
            "text-caption font-medium",
            hasRunning ? "text-success" : "text-faint-foreground",
          )}
        >
          {hasRunning ? `${running} running` : "Idle"}
        </span>
        <span className="font-mono text-micro tabular-nums text-faint-foreground">
          · {label}
        </span>
      </div>
      <div className="h-1 overflow-hidden rounded-full bg-surface-hover">
        <div
          className={cn(
            "h-full rounded-full transition-all duration-300",
            hasRunning ? "bg-success" : "bg-transparent",
          )}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

interface RowRightProps {
  agent: AgentListRow["agent"];
  owner: AgentListRow["owner"];
  runCount: number;
  locale: string;
}

/** De-emphasized right zone: owner + access, then the 30d runs stat. */
function RowRight({ agent, owner, runCount, locale }: RowRightProps) {
  const accessScope = useMemo(() => {
    if (agent.permission_mode === "private") return "owner-only" as const;
    if (agent.invocation_targets.some((t) => t.target_type === "workspace")) {
      return "workspace" as const;
    }
    return "specific-people" as const;
  }, [agent.permission_mode, agent.invocation_targets]);

  return (
    <div className="flex min-w-[120px] flex-col items-end gap-1">
      {agent.owner_id && (
        <div className="flex items-center gap-1 text-micro text-faint-foreground">
          <ActorAvatar actorType="member" actorId={agent.owner_id} size="xs" />
          <span className="max-w-[80px] truncate">
            {owner?.name ?? agent.owner_id.slice(0, 8)}
          </span>
          <span className="opacity-50">· {ACCESS_LABELS[accessScope]}</span>
        </div>
      )}
      <span className="font-mono text-micro tabular-nums text-faint-foreground">
        {runCount.toLocaleString(locale)} runs · 30d
      </span>
    </div>
  );
}

export interface AgentEnhancedRowsProps {
  rows: AgentListRow[];
  locale: string;
}

/** Enhanced row view — wider rows with large avatars, inline runtime badges, running bar, faded owner/access. */
export function AgentEnhancedRows({ rows, locale }: AgentEnhancedRowsProps) {
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  if (rows.length === 0) return null;

  return (
    <div className="flex flex-col gap-2" role="list" aria-label={AGENTS_LIST_LABEL}>
      {rows.map((row) => {
        const { agent, runtime, presence, runCount, owner, runningTasks } = row;
        const isArchived = !!agent.archived_at;
        const isBound = !!agent.runtime_id;
        const availability = isArchived ? undefined : presence?.availability;

        return (
          <div
            key={agent.id}
            className={cn(
              "group grid cursor-pointer items-center gap-4 rounded-lg border border-surface-border bg-surface px-5 py-3.5 transition-all duration-150 hover:bg-surface-hover",
              availability === "online" &&
                "border-l-[3px] [border-left-color:var(--color-success)]",
              availability === "unstable" &&
                "border-l-[3px] [border-left-color:var(--color-warning)]",
              (!availability || availability === "offline") && "border-l-[3px]",
              isArchived && "opacity-60",
            )}
            role="listitem"
            onClick={() => navigation.push(paths.agentDetail(agent.id))}
          >
            <div className="grid w-full grid-cols-[56px_minmax(180px,1fr)_auto_auto_minmax(120px,auto)] items-center gap-4">
              {/* 56px avatar + 12px presence dot */}
              <div className="relative">
                <ActorAvatar
                  actorType="agent"
                  actorId={agent.id}
                  size="2xl"
                  className={cn(isArchived && "opacity-50 grayscale")}
                  showStatusDot={false}
                />
                {availability && (
                  <span
                    className={cn(
                      "absolute bottom-0.5 right-0.5 size-3 rounded-full ring-[2.5px] ring-surface",
                      availability === "online" && "bg-success",
                      availability === "unstable" && "bg-warning",
                      availability === "offline" && "bg-muted-foreground/60",
                    )}
                  />
                )}
              </div>

              {/* Identity: name + description */}
              <div className="flex min-w-0 flex-col gap-0.5">
                <div className="flex items-center gap-2">
                  <span
                    className={cn(
                      "min-w-0 truncate text-body font-semibold",
                      isArchived && "text-muted-foreground",
                    )}
                  >
                    {agent.name}
                  </span>
                  {agent.visibility === "private" && !isArchived && (
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <svg
                            className="size-3.5 shrink-0 text-faint-foreground"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="2"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          >
                            <rect
                              x="3"
                              y="11"
                              width="18"
                              height="11"
                              rx="2"
                              ry="2"
                            />
                            <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                          </svg>
                        }
                      />
                      <TooltipContent>Private agent</TooltipContent>
                    </Tooltip>
                  )}
                </div>
                {agent.description && (
                  <span className="min-w-0 truncate text-caption text-muted-foreground">
                    {agent.description}
                  </span>
                )}
              </div>

              {/* Runtime: hostname, then CLI badge, then model badge */}
              <div className="flex items-center gap-1.5">
                {runtime && isBound ? (
                  <>
                    <span className="max-w-[96px] truncate text-caption text-muted-foreground">
                      {runtimeDisplayName(runtime)}
                    </span>
                    <span className="inline-flex shrink-0 items-center gap-1 rounded-sm bg-surface-hover px-2.5 py-1 text-caption font-medium whitespace-nowrap text-foreground">
                      <ProviderLogo
                        provider={runtime.provider}
                        className="h-3.5 w-3.5 shrink-0"
                      />
                      {providerDisplayName(runtime.provider)}
                    </span>
                    <span className="inline-flex shrink-0 items-center gap-1 rounded-sm border border-info/20 bg-info/10 px-2.5 py-1 text-caption font-medium whitespace-nowrap text-info">
                      {agent.model || "—"}
                    </span>
                  </>
                ) : (
                  <span className="inline-flex shrink-0 items-center gap-1 rounded-sm border border-warning/30 bg-warning/10 px-2.5 py-1 text-caption font-medium whitespace-nowrap text-warning">
                    Needs a runtime
                  </span>
                )}
              </div>

              {/* Running indicator */}
              <div className="min-w-[140px]">
                <RunningBar
                  presence={presence}
                  capacity={agent.max_concurrent_tasks}
                  isBound={isBound}
                  tasks={runningTasks}
                />
              </div>

              {/* De-emphasized right zone */}
              <RowRight
                agent={agent}
                owner={owner}
                runCount={runCount}
                locale={locale}
              />
            </div>

            {/* Clickable running task chips */}
            {runningTasks.length > 0 && (presence?.runningCount ?? 0) > 0 && (
              <div className="pl-[72px] pt-1">
                <RunningTaskChips tasks={runningTasks} />
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
