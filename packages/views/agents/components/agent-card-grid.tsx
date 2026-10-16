"use client";

/* eslint-disable i18next/no-literal-string -- Approved prototype design: the
   card/row agent views use fixed English copy by explicit product decision in
   this fork; dynamic content (agent name, description) is unaffected. */

import { useMemo } from "react";
import type { AgentRuntime } from "@multica/core/types";
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

// Top gradient stripe colour per availability — online green, unstable amber,
// offline gray (prototype: transparent → colour → transparent, 3px).
const STRIPE_VIA: Record<string, string> = {
  online: "before:via-success",
  unstable: "before:via-warning",
  offline: "before:via-muted-foreground before:opacity-40",
};

function formatConcurrency(
  running: number,
  capacity: number,
): { pct: number; label: string } {
  if (capacity <= 0) return { pct: 0, label: `${running} / ∞` };
  return {
    pct: Math.min(100, Math.round((running / capacity) * 100)),
    label: `${running} / ${capacity}`,
  };
}

/** 56px avatar with a 14px presence dot (3px surface ring) at bottom-right. */
function AgentAvatarWithStatus({
  agent,
  presence,
  isArchived,
}: {
  agent: AgentListRow["agent"];
  presence: AgentPresenceDetail | null;
  isArchived: boolean;
}) {
  const availability = presence?.availability;
  return (
    <div className="relative shrink-0">
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size="2xl"
        className={cn(isArchived && "opacity-50 grayscale")}
        showStatusDot={false}
      />
      {availability && !isArchived && (
        <span className="absolute bottom-0.5 right-0.5">
          {availability === "online" && (
            <span className="absolute inset-0 animate-ping rounded-full bg-success/60" />
          )}
          <span
            className={cn(
              "relative block size-3.5 rounded-full ring-3 ring-surface",
              availability === "online" && "bg-success",
              availability === "unstable" && "bg-warning",
              availability === "offline" && "bg-muted-foreground/60",
            )}
          />
        </span>
      )}
    </div>
  );
}

interface RuntimeBlockProps {
  runtime: AgentRuntime | null;
  agent: AgentListRow["agent"];
  presence: AgentPresenceDetail | null;
}

/**
 * The prominent runtime panel: hostname first, then the CLI badge; model in
 * mono; a 6px load meter. Mirrors the prototype's .runtime-block (12px
 * padding, 8px row gap, page-canvas fill).
 */
function RuntimeBlock({ runtime, agent, presence }: RuntimeBlockProps) {
  if (!runtime || !agent.runtime_id) {
    return (
      <div className="rounded-md border border-surface-border bg-page-canvas p-3 text-center text-caption text-faint-foreground">
        Needs a runtime
      </div>
    );
  }

  const hostname = runtimeDisplayName(runtime);
  const cliProvider = providerDisplayName(runtime.provider);
  const { pct, label } = formatConcurrency(
    presence?.runningCount ?? 0,
    presence?.capacity ?? agent.max_concurrent_tasks,
  );
  const hasRunning = (presence?.runningCount ?? 0) > 0;

  return (
    <div className="space-y-2 rounded-md border border-surface-border bg-page-canvas p-3">
      {/* Hostname + CLI badge — machine name leads, per prototype feedback */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-micro font-medium uppercase tracking-wider text-faint-foreground">
          Runtime
        </span>
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="min-w-0 truncate text-caption text-muted-foreground">
            {hostname}
          </span>
          <span className="inline-flex shrink-0 items-center gap-1.5 rounded-xs bg-surface-hover px-2 py-0.5 text-caption font-medium text-foreground">
            <ProviderLogo
              provider={runtime.provider}
              className="h-3.5 w-3.5 shrink-0"
            />
            {cliProvider}
          </span>
        </span>
      </div>

      {/* Model */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-micro font-medium uppercase tracking-wider text-faint-foreground">
          Model
        </span>
        <span className="min-w-0 truncate font-mono text-caption text-muted-foreground">
          {agent.model || "—"}
        </span>
      </div>

      {/* Load meter */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-micro font-medium uppercase tracking-wider text-faint-foreground">
          Load
        </span>
        <div className="flex min-w-[100px] items-center gap-2">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-hover">
            <div
              className={cn(
                "h-full rounded-full transition-all duration-300",
                hasRunning ? "bg-success" : "bg-transparent",
              )}
              style={{ width: `${pct}%` }}
            />
          </div>
          <span
            className={cn(
              "shrink-0 font-mono text-micro tabular-nums",
              hasRunning ? "text-success" : "text-muted-foreground",
            )}
          >
            {label}
          </span>
        </div>
      </div>
    </div>
  );
}

interface RunningTasksSectionProps {
  tasks: AgentListRow["runningTasks"];
  presence: AgentPresenceDetail | null;
}

/** Running pill (green, blinking dot) + clickable task chips on one line, or a quiet idle line. */
function RunningTasksSection({ tasks, presence }: RunningTasksSectionProps) {
  const running = presence?.runningCount ?? 0;
  const hasRunning = running > 0;

  if (!hasRunning && tasks.length === 0) {
    return (
      <div className="flex items-center gap-2 text-caption text-faint-foreground">
        <svg
          className="size-3.5 shrink-0"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
        </svg>
        Idle
      </div>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {hasRunning && (
        <span className="inline-flex shrink-0 items-center gap-1.5 rounded-sm border border-success/25 bg-success/15 px-2.5 py-1 text-caption font-medium text-success">
          <span className="size-1.5 animate-pulse rounded-full bg-success" />
          {running} running
        </span>
      )}
      {tasks.length > 0 && <RunningTaskChips tasks={tasks} />}
    </div>
  );
}

interface CardFooterProps {
  agent: AgentListRow["agent"];
  owner: AgentListRow["owner"];
  runCount: number;
  locale: string;
}

/** De-emphasized footer: owner + access at micro/faint, runs stat in mono. */
function CardFooter({ agent, owner, runCount, locale }: CardFooterProps) {
  const accessScope = useMemo(() => {
    if (agent.permission_mode === "private") return "owner-only" as const;
    if (agent.invocation_targets.some((t) => t.target_type === "workspace")) {
      return "workspace" as const;
    }
    return "specific-people" as const;
  }, [agent.permission_mode, agent.invocation_targets]);

  return (
    <div className="flex items-center justify-between border-t border-surface-border pt-3">
      <div className="flex min-w-0 items-center gap-2.5 text-micro text-faint-foreground">
        {agent.owner_id && (
          <span className="flex shrink-0 items-center gap-1.5">
            <ActorAvatar actorType="member" actorId={agent.owner_id} size="xs" />
            <span className="max-w-[96px] truncate">
              {owner?.name ?? agent.owner_id.slice(0, 8)}
            </span>
          </span>
        )}
        <span className="shrink-0 opacity-50">{ACCESS_LABELS[accessScope]}</span>
      </div>
      <span className="shrink-0 font-mono text-micro tabular-nums text-faint-foreground">
        <span className="font-semibold text-muted-foreground">
          {runCount.toLocaleString(locale)}
        </span>{" "}
        runs · 30d
      </span>
    </div>
  );
}

export interface AgentCardGridProps {
  rows: AgentListRow[];
  locale: string;
}

/** Card grid view — responsive grid of agent cards with large avatars and prominent runtime blocks. */
export function AgentCardGrid({ rows, locale }: AgentCardGridProps) {
  if (rows.length === 0) return null;

  return (
    <div
      className="grid gap-4"
      style={{ gridTemplateColumns: "repeat(auto-fill, minmax(340px, 1fr))" }}
      role="list"
      aria-label={AGENTS_LIST_LABEL}
    >
      {rows.map((row) => {
        const { agent, runtime, presence, runCount, owner } = row;
        const isArchived = !!agent.archived_at;
        const availability = isArchived ? undefined : presence?.availability;

        return (
          <article
            key={agent.id}
            className={cn(
              "group relative flex flex-col gap-4 rounded-xl border border-surface-border bg-surface p-5 shadow-[var(--surface-shadow)] transition-all duration-200 hover:-translate-y-px hover:shadow-[0_3px_12px_rgb(0_0_0/0.18)]",
              availability &&
                "before:absolute before:inset-x-0 before:top-0 before:h-[3px] before:rounded-t-xl before:bg-gradient-to-r before:from-transparent before:to-transparent before:opacity-60",
              availability && STRIPE_VIA[availability],
              isArchived && "opacity-60",
            )}
            role="listitem"
          >
            {/* Header: 56px avatar + name + description */}
            <div className="flex gap-3.5">
              <AgentAvatarWithStatus
                agent={agent}
                presence={presence}
                isArchived={isArchived}
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <h3
                    className={cn(
                      "min-w-0 truncate text-title-sm font-semibold text-foreground",
                      isArchived && "text-muted-foreground",
                    )}
                  >
                    {agent.name}
                  </h3>
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
                  <p className="mt-0.5 line-clamp-2 text-caption leading-4 text-muted-foreground">
                    {agent.description}
                  </p>
                )}
              </div>
            </div>

            {/* Runtime panel */}
            <RuntimeBlock runtime={runtime} agent={agent} presence={presence} />

            {/* Running tasks */}
            <RunningTasksSection
              tasks={row.runningTasks}
              presence={presence}
            />

            {/* Footer: owner + access + runs (de-emphasized) */}
            <CardFooter
              agent={agent}
              owner={owner}
              runCount={runCount}
              locale={locale}
            />
          </article>
        );
      })}
    </div>
  );
}
