"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { AgentTask, Issue } from "@multica/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { WorkspaceAgentActivityHoverContent } from "../../agents/components/agent-activity-hover-content";
import { useT } from "../../i18n";

interface WorkspaceAgentWorkingChipProps {
  // Controlled toggle binding. Different surfaces (Issues page singleton
  // hook, My Issues vanilla store) own the underlying state, so the chip
  // stays presentational and accepts both forms via plain props.
  value: boolean;
  onToggle: () => void;
  // The rows this filter leaves on screen, computed by the surface from the
  // same pipeline that renders them (see `workingScopeIssues`).
  //
  // The chip does not count these — it counts the agents working ON them. The
  // list decides WHICH agents count: an agent whose only running task sits on
  // an issue this filter would hide is not part of the number. Taking scope
  // from the render pipeline instead of re-deriving it from the task snapshot
  // is what keeps that judgement in step with the list (MUL-4884).
  //
  // `undefined` means the scope is UNKNOWN — the table's ids-facet window is
  // still resolving, failed, or is too large to materialize. The chip then
  // shows an indeterminate "—" instead of a number: publishing a count from
  // some other, incomplete window would be a precise-looking wrong answer
  // (round-5 review P2). The toggle keeps working in that state.
  workingIssues: readonly Issue[] | undefined;
}

export interface WorkingChipView {
  /** Running tasks on `workingIssues`, keyed by issue id. */
  tasksByIssueId: Map<string, AgentTask[]>;
  /** Distinct agents behind the counted work. This IS the chip's number,
   *  and the avatar stack renders the same list. */
  agentIds: string[];
  /** Total running tasks across the counted issues — the hover card's
   *  second figure. */
  taskCount: number;
}

/**
 * Bucket the workspace task snapshot against the issues the filter would
 * show. Exported for tests: the counting rule is the entire point of this
 * component, so it is a pure function rather than a hook-bound useMemo.
 *
 * A running task only counts when its issue is on screen. Two kinds are
 * skipped, both silently — they have no visual presence on this page, so
 * announcing them would explain an absence the user never noticed:
 *   - no `issue_id` — chat/autopilot runs, which are not issue work at all
 *   - issue not on screen — filtered out, or past the page the list loaded
 *
 * `issue_id` is an EMPTY STRING for chat/autopilot tasks, not null — see
 * packages/core/types/agent.ts. The guard is data correctness, not
 * presentation: without it those tasks collapse into one `""` bucket and
 * their agents would join the count for work that isn't on any issue
 * (MUL-4884). Mirrors `deriveIssueSurfaceActivity`.
 */
export function deriveWorkingChipView(
  snapshot: readonly AgentTask[],
  workingIssues: readonly Issue[],
): WorkingChipView {
  const onScreen = new Set(workingIssues.map((issue) => issue.id));
  const tasksByIssueId = new Map<string, AgentTask[]>();
  const agentIds: string[] = [];
  const seenAgents = new Set<string>();
  let taskCount = 0;

  for (const task of snapshot) {
    if (task.status !== "running") continue;
    if (!task.issue_id) continue;
    if (!onScreen.has(task.issue_id)) continue;
    const bucket = tasksByIssueId.get(task.issue_id);
    if (bucket) bucket.push(task);
    else tasksByIssueId.set(task.issue_id, [task]);
    taskCount += 1;
    if (!seenAgents.has(task.agent_id)) {
      seenAgents.add(task.agent_id);
      agentIds.push(task.agent_id);
    }
  }

  return { tasksByIssueId, agentIds, taskCount };
}

/**
 * Which colour tier the chip wears, and the only classes allowed alongside
 * it. Exported so the tier rules are testable without a DOM.
 *
 * The tier lives entirely in the Button variant. `className` carries layout
 * only — with ONE exception: the idle tier needs muted text, which `outline`
 * does not set. That exception is gated on `!value`, and the gate matters:
 * `filter ON + 0 agents` is a real state (the filter stays on after the last
 * agent finishes), and there the variant is `brand`. Appending
 * `text-muted-foreground` there does not lose to the variant — it WINS, because
 * tailwind-merge keeps the last class in a group and `className` is appended
 * after the variant. The result would be muted grey text on a brand-blue
 * fill. Colour classes in `className` are how this component keeps breaking;
 * the safe rule is that a tier's colours only ever come from its variant.
 */
export function chipAppearance(
  value: boolean,
  hasAgents: boolean,
): { variant: "brand" | "brandSubtle" | "outline"; className: string } {
  const layout = "h-8 px-2 md:h-7 md:px-2.5";
  if (value) return { variant: "brand", className: layout };
  if (hasAgents) return { variant: "brandSubtle", className: layout };
  return { variant: "outline", className: `${layout} text-muted-foreground` };
}

/**
 * Filter chip on the issues / my-issues header, sitting to the left of the
 * Filter button. Always rendered so the filter toggle never disappears
 * mid-flight (a previous design hid the chip when no agents were running,
 * which trapped users in an active-but-invisible filter state).
 *
 * It says one thing: "N agents working". The number counts agents, which is
 * exactly what the avatar stack next to it shows — one control, one unit.
 * Earlier versions counted issues (or tasks) beside a stack of agent heads,
 * and two units sitting side by side is what made the chip read as broken
 * no matter which one was "right" (MUL-4884).
 *
 * Counting agents also settles the subject: only agents produce a runtime
 * signal, so "N agents working" cannot be misread as "N issues someone is
 * working on" — human work has no signal here and is not being claimed.
 *
 * Accepted trade-off: the number no longer predicts the row count of the
 * click. One agent on two issues reads "1 agent working" and opens two
 * rows. The chip answers WHO, the list answers WHERE — different questions,
 * so the two numbers do not compete; the hover card's issue grouping shows
 * the mapping on the spot.
 *
 * Colour is three-tier and each tier is its own Button variant rather than
 * classes layered over `outline` — see the `brand` / `brandSubtle` variants
 * for why layering silently loses in dark mode.
 */
export function WorkspaceAgentWorkingChip({
  value,
  onToggle,
  workingIssues,
}: WorkspaceAgentWorkingChipProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const scopeKnown = workingIssues !== undefined;
  const view = useMemo(
    () => deriveWorkingChipView(snapshot, workingIssues ?? []),
    [snapshot, workingIssues],
  );

  // The number and the avatar stack are the same list, so they cannot
  // disagree.
  const agentCount = view.agentIds.length;
  const hasAgents = scopeKnown && agentCount > 0;

  const label = scopeKnown
    ? t(($) => $.agent_activity.chip_agents_working, { count: agentCount })
    : t(($) => $.agent_activity.chip_agents_working_unknown);

  // Three tiers: filter ON is the loud filled state, activity without the
  // filter is a tint, nothing running is a plain control. An unknown scope
  // uses the plain tier — it makes no activity claim to colour by.
  const appearance = chipAppearance(value, hasAgents);

  const trigger = (
    <Button
      variant={appearance.variant}
      size="sm"
      className={appearance.className}
      onClick={onToggle}
      aria-pressed={value}
      // The narrow layout shows the bare number, so pin the full
      // sentence as the accessible name in every layout.
      aria-label={label}
    >
      {hasAgents && (
        <AgentAvatarStack agentIds={view.agentIds} size="sm" max={3} />
      )}
      <span className="tabular-nums md:hidden">
        {scopeKnown ? agentCount : "—"}
      </span>
      <span className="hidden tabular-nums md:inline">{label}</span>
    </Button>
  );

  // No hover card while the scope is unknown: there is no issue list to
  // show, and an empty card would read as "nothing running" — the exact
  // claim the unknown state exists to avoid.
  if (!scopeKnown) return trigger;

  return (
    <HoverCard>
      <HoverCardTrigger render={trigger} />
      <HoverCardContent align="end" className="w-80">
        <WorkspaceAgentActivityHoverContent
          issues={workingIssues}
          tasksByIssueId={view.tasksByIssueId}
          taskCount={view.taskCount}
        />
      </HoverCardContent>
    </HoverCard>
  );
}
