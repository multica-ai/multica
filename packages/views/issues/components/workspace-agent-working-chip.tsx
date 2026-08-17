"use client";

import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@multica/ui/components/ui/hover-card";
import { useActorName } from "@multica/core/workspace/hooks";
import type { WorkingAgentSummary } from "@multica/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { useT } from "../../i18n";

interface WorkspaceAgentWorkingChipProps {
  value: boolean;
  onToggle: () => void;
  /** Agents working inside the surface this header belongs to, already narrowed
   *  by its scope and every active filter. `undefined` = not resolved yet. */
  agents: readonly WorkingAgentSummary[] | undefined;
}

/** What the projection says about activity in this surface. `unknown` is a
 *  first-class case, not a synonym for `none`. */
export type ChipActivity = "unknown" | "none" | "some";

export function chipActivity(
  agents: readonly WorkingAgentSummary[] | undefined,
): ChipActivity {
  if (agents === undefined) return "unknown";
  return agents.length > 0 ? "some" : "none";
}

/**
 * Which colour tier the chip wears, and the only classes allowed alongside
 * it. Activity uses a tint, the active filter uses the filled brand tier,
 * and an idle surface stays neutral.
 *
 * The muted text on the neutral tier is what reads as "nothing is happening
 * here", so an unresolved projection gets the neutral tier WITHOUT it: we do
 * not yet know whether the surface is idle, and dimming it would claim so.
 */
export function chipAppearance(
  value: boolean,
  activity: ChipActivity,
): { variant: "brand" | "brandSubtle" | "outline"; className: string } {
  const layout = "h-8 px-2 md:h-7 md:px-2.5";
  if (value) return { variant: "brand", className: layout };
  if (activity === "some") return { variant: "brandSubtle", className: layout };
  if (activity === "unknown") return { variant: "outline", className: layout };
  return { variant: "outline", className: `${layout} text-muted-foreground` };
}

/**
 * Hover body for every surface that reads a working-agents projection — the
 * surface filter chip here and the sub-issues header chip on issue detail.
 * Shared so a narrowed read and an unnarrowed one describe activity the same
 * way; the only difference between the two is the projection's scope.
 *
 * Identity comes from the workspace agent directory rather than the payload:
 * the surface projection is a facet of ids and counts, and resolving names
 * here keeps one definition of an agent's name/avatar across both callers.
 *
 * Three distinct states, and the first two must never collapse into each other:
 * `undefined` = the projection has not resolved, `[]` = it resolved and this
 * surface really has nobody working, a non-empty list = the roster. Rendering
 * the empty sentence for an unresolved projection would assert "no agents
 * working right now" on no evidence — the same class of unearned claim the chip
 * count itself had (MUL-5525).
 */
export function WorkingAgentsHoverContent({
  agents,
}: {
  agents: readonly WorkingAgentSummary[] | undefined;
}) {
  const { t } = useT("issues");
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();

  if (agents === undefined) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.agent_activity.unknown_hover)}
      </p>
    );
  }

  if (agents.length === 0) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.agent_activity.empty_hover)}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="text-caption font-medium text-muted-foreground">
        {t(($) => $.agent_activity.hover_header, { count: agents.length })}
      </div>
      <div className="flex flex-col gap-1.5">
        {agents.map((agent) => (
          <div key={agent.id} className="flex items-center gap-2 text-caption">
            <ActorAvatar
              name={getActorName("agent", agent.id)}
              initials={getActorInitials("agent", agent.id)}
              avatarUrl={getActorAvatarUrl("agent", agent.id) ?? undefined}
              isAgent
              size="sm"
            />
            <span className="min-w-0 flex-1 truncate font-medium">
              {getActorName("agent", agent.id)}
            </span>
            <span className="shrink-0 tabular-nums text-muted-foreground">
              {t(($) => $.agent_activity.tasks_count, {
                count: agent.running_task_count,
              })}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * Agents-working filter chip for an issue surface header.
 *
 * The number IS the post-click row count's authority: it counts the agents
 * working on rows this surface's scope AND active filters would show, resolved
 * by the surface controller from the server-side `working_agents` facet — the
 * same compiled query the rows come from. Before MUL-5525 it ran its own
 * workspace-wide `/api/working-agents` read, so on a project page it could
 * advertise agents working nowhere near that project and open an empty list.
 *
 * `agents === undefined` means the projection has not resolved. Every surface of
 * the chip then stays indeterminate — label, compact number, colour tier and
 * hover body alike — because "0" and "not known yet" are different claims and
 * the reader cannot tell them apart once one of them is rendered as the other.
 *
 * Clicking only toggles view state; the controller turns the running-issue set
 * into the query's `working_issue_ids` filter.
 */
export function WorkspaceAgentWorkingChip({
  value,
  onToggle,
  agents,
}: WorkspaceAgentWorkingChipProps) {
  const { t } = useT("issues");
  const activity = chipActivity(agents);
  const agentIds = agents?.map((agent) => agent.id) ?? [];
  const label =
    activity === "unknown"
      ? t(($) => $.agent_activity.chip_agents_working_unknown)
      : t(($) => $.agent_activity.chip_agents_working, { count: agentIds.length });
  const appearance = chipAppearance(value, activity);

  const trigger = (
    <Button
      variant={appearance.variant}
      size="sm"
      className={appearance.className}
      onClick={onToggle}
      aria-pressed={value}
      aria-label={label}
    >
      {activity === "some" && (
        <AgentAvatarStack agentIds={agentIds} size="sm" max={3} />
      )}
      <span className="tabular-nums md:hidden">
        {activity === "unknown" ? "—" : agentIds.length}
      </span>
      <span className="hidden tabular-nums md:inline">{label}</span>
    </Button>
  );

  return (
    <HoverCard>
      <HoverCardTrigger render={trigger} />
      <HoverCardContent align="end" className="w-72">
        {/* Pass the projection through untouched. Defaulting to [] here would
            hand the hover card a definite "nobody is working" for a state we
            have not resolved. */}
        <WorkingAgentsHoverContent agents={agents} />
      </HoverCardContent>
    </HoverCard>
  );
}
