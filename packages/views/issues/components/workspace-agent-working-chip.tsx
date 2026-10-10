"use client";

import { useId } from "react";
import { Zap, ZapOff } from "lucide-react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@multica/ui/components/ui/hover-card";
import { useActorName } from "@multica/core/workspace/hooks";
import type { WorkingAgentSummary } from "@multica/core/types";
import type { AgentWorkingMode } from "@multica/core/issues/stores/view-store";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { useT } from "../../i18n";

interface WorkspaceAgentWorkingChipProps {
  /** `all` = filter off, `working` / `not_working` = the two directions. */
  value: AgentWorkingMode;
  /** Advances the mode: all -> working -> not_working -> all. */
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
  value: AgentWorkingMode,
  activity: ChipActivity,
): { variant: "brand" | "brandSubtle" | "outline"; className: string } {
  const layout = "h-8 px-2 md:h-7 md:px-2.5";
  if (value !== "all") return { variant: "brand", className: layout };
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
 * Clicking only advances view state (all -> working -> not working -> all); the
 * controller turns the running-issue set into the query's `working_issue_ids`
 * or `not_working_issue_ids` filter. The count always describes the agents
 * working, in every mode, so it does not move when you click the chip.
 */
export function WorkspaceAgentWorkingChip({
  value,
  onToggle,
  agents,
}: WorkspaceAgentWorkingChipProps) {
  const { t } = useT("issues");
  const activity = chipActivity(agents);
  const agentIds = agents?.map((agent) => agent.id) ?? [];
  // The count always names the agents working, whichever way the filter
  // points, so it never moves when the chip is clicked.
  const workingLabel =
    activity === "unknown"
      ? t(($) => $.agent_activity.chip_agents_working_unknown)
      : t(($) => $.agent_activity.chip_agents_working, { count: agentIds.length });
  const label = workingLabel;
  const stateLabel =
    value === "working"
      ? t(($) => $.agent_activity.chip_state_working)
      : value === "not_working"
        ? t(($) => $.agent_activity.chip_state_not_working)
        : t(($) => $.agent_activity.chip_state_all);
  // The accessible NAME stays the count label in every state (unchanged for
  // existing consumers); the three-way state is its description.
  const stateId = useId();
  const appearance = chipAppearance(value, activity);

  const trigger = (
    <Button
      variant={appearance.variant}
      size="sm"
      className={appearance.className}
      onClick={onToggle}
      // Three states do not fit aria-pressed's two, so the current state is
      // announced as the button's description instead.
      aria-label={label}
      aria-describedby={stateId}
      data-working-mode={value}
    >
      <span id={stateId} className="sr-only">
        {stateLabel}
      </span>
      {/* The selected state, by SHAPE rather than colour, so it reads at every
          width (the text label beside the chip is hidden below md) and without
          a tooltip: a bolt means "only issues with agents working", a struck
          bolt means "only issues without". All is the plain chip. The accessible
          state lives in the description above, so the glyph is decorative. */}
      {value === "working" && (
        <Zap aria-hidden="true" data-state-icon="working" className="size-3.5 shrink-0" />
      )}
      {value === "not_working" && (
        <ZapOff aria-hidden="true" data-state-icon="not_working" className="size-3.5 shrink-0" />
      )}
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
