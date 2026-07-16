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
  // same pipeline that renders them (see `workingScopeIssues`). The chip's
  // number is this list's length — that is the whole point: the number and
  // the click result cannot disagree, because they are the same list.
  //
  // The chip used to take the PRE-filter issue set and count distinct running
  // `issue_id`s out of the task snapshot itself. That was a second derivation
  // of "what's on screen" and it drifted from the real one (MUL-4884).
  workingIssues: readonly Issue[];
}

export interface WorkingChipView {
  /** Running tasks on `workingIssues`, keyed by issue id. */
  tasksByIssueId: Map<string, AgentTask[]>;
  /** Distinct agents behind the counted work — the avatar stack. */
  agentIds: string[];
  /** Total running tasks across the counted issues. */
  taskCount: number;
  /** Running tasks with no linked issue (chat / autopilot). */
  unlinkedCount: number;
  /** Running tasks whose issue isn't on screen (filtered out, or past the
   *  50-per-status page the list loads). */
  outOfScopeCount: number;
}

/**
 * Bucket the workspace task snapshot against the issues the filter would
 * show. Exported for tests: the counting rule is the entire point of this
 * component, so it is a pure function rather than a hook-bound useMemo.
 *
 * Every running task lands in exactly one bucket:
 *   - no `issue_id`      → unlinked (chat/autopilot; never counted)
 *   - issue on screen    → counted, grouped under that issue
 *   - issue not on screen → out of scope (filtered out or past the page)
 *
 * `issue_id` is an EMPTY STRING for chat/autopilot tasks, not null — see
 * packages/core/types/agent.ts. Without the guard those tasks all collapse
 * into one `""` bucket and read as a phantom issue, inflating the count by
 * exactly one (MUL-4884). Mirrors `deriveIssueSurfaceActivity`.
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
  let unlinkedCount = 0;
  let outOfScopeCount = 0;

  for (const task of snapshot) {
    if (task.status !== "running") continue;
    if (!task.issue_id) {
      unlinkedCount += 1;
      continue;
    }
    if (!onScreen.has(task.issue_id)) {
      outOfScopeCount += 1;
      continue;
    }
    const bucket = tasksByIssueId.get(task.issue_id);
    if (bucket) bucket.push(task);
    else tasksByIssueId.set(task.issue_id, [task]);
    taskCount += 1;
    if (!seenAgents.has(task.agent_id)) {
      seenAgents.add(task.agent_id);
      agentIds.push(task.agent_id);
    }
  }

  return { tasksByIssueId, agentIds, taskCount, unlinkedCount, outOfScopeCount };
}

/**
 * Filter chip on the issues / my-issues header, sitting to the left of the
 * Filter button. Always rendered so the filter toggle never disappears
 * mid-flight (a previous design hid the chip when no agents were running,
 * which trapped users in an active-but-invisible filter state).
 *
 * It says one thing: "N issues in progress" — N being exactly the rows you
 * get when you click it. One number, one unit, self-verifiable.
 *
 * The avatar stack is ambience ("who's on it"), not a second statistic: it
 * carries no `+N`, because a rival number next to the main one is what made
 * this chip read as broken (it counted issues while the stack counted
 * agents). The precise roster, the task count, and everything the number
 * excludes live in the hover card.
 *
 * Colour is two-step on purpose: idle activity is a whisper of brand, and
 * the loud filled state is reserved for "this filter is ON" — the chip
 * should read as a quiet tool, not an alert.
 */
export function WorkspaceAgentWorkingChip({
  value,
  onToggle,
  workingIssues,
}: WorkspaceAgentWorkingChipProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const view = useMemo(
    () => deriveWorkingChipView(snapshot, workingIssues),
    [snapshot, workingIssues],
  );

  // The number. Not a re-derivation of "what's running" — the length of the
  // list the click produces.
  const issueCount = workingIssues.length;
  const hasAgents = issueCount > 0;

  // Active (brand-filled) class — must explicitly re-pin text and bg in
  // every interactive state. Button's `outline` variant ships
  // `hover:text-foreground` + `aria-expanded:bg-muted aria-expanded:text-foreground`,
  // which would otherwise repaint the brand chip back to neutral on hover
  // and while the HoverCard is open.
  const activeClass = value
    ? "border-brand bg-brand text-brand-foreground hover:bg-brand/90 hover:text-brand-foreground aria-expanded:bg-brand aria-expanded:text-brand-foreground"
    : hasAgents
      ? // Idle-with-activity: a brand tint, not a fill. Enough to read as
        // "something is happening here" while scanning, quiet enough that
        // the filled state still means something.
        "border-brand/30 bg-brand/5 text-foreground"
      : "text-muted-foreground";

  const label = t(($) => $.agent_activity.issues_in_progress, {
    count: issueCount,
  });

  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className={`h-8 px-2 md:h-7 md:px-2.5 ${activeClass}`}
            onClick={onToggle}
            aria-pressed={value}
            // The narrow layout shows the bare number, so pin the full
            // sentence as the accessible name in every layout.
            aria-label={label}
          >
            {hasAgents && (
              <AgentAvatarStack
                agentIds={view.agentIds}
                size="sm"
                max={3}
                opacity="full"
                overflow="fade"
              />
            )}
            <span className="tabular-nums md:hidden">{issueCount}</span>
            <span className="hidden tabular-nums md:inline">{label}</span>
          </Button>
        }
      />
      <HoverCardContent align="end" className="w-80">
        <WorkspaceAgentActivityHoverContent
          issues={workingIssues}
          tasksByIssueId={view.tasksByIssueId}
          taskCount={view.taskCount}
          unlinkedCount={view.unlinkedCount}
          outOfScopeCount={view.outOfScopeCount}
        />
      </HoverCardContent>
    </HoverCard>
  );
}
