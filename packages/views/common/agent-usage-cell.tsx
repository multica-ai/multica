"use client";

import { ActorAvatar } from "./actor-avatar";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";

interface AgentUsageCellProps {
  agentId: string;
  /** The resolved agent, or `undefined` when it was hard-deleted. */
  agent: { name: string } | undefined;
  /**
   * Whether the workspace agent list has finished loading. Until it has, an
   * absent `agent` only means the list is still resolving — not that the agent
   * was deleted — so we keep the live avatar instead of flashing the deleted
   * placeholder on every row.
   */
  agentsLoaded: boolean;
  /** Localised "Deleted agent" label, supplied by the caller's namespace. */
  deletedLabel: string;
}

// Agent identity cell shared by the dashboard and runtime usage leaderboards.
// A hard-deleted agent has no row in the workspace agent list, so the live
// ActorAvatar would link through to a now-removed profile (404) and resolve
// "UA" initials that contradict the label. Deleted agents therefore get a
// neutral, non-interactive placeholder instead. Only the loaded list can prove
// deletion, so an absent agent before then keeps the live avatar.
export function AgentUsageCell({
  agentId,
  agent,
  agentsLoaded,
  deletedLabel,
}: AgentUsageCellProps) {
  if (!agent && agentsLoaded) {
    return (
      <div className="flex min-w-0 items-center gap-2">
        <ActorAvatarBase name={deletedLabel} initials="" isAgent size={22} />
        <span className="truncate text-sm font-medium text-muted-foreground">
          {deletedLabel}
        </span>
      </div>
    );
  }
  return (
    <div className="flex min-w-0 items-center gap-2">
      <ActorAvatar actorType="agent" actorId={agentId} size={22} enableHoverCard />
      <span className="cursor-pointer truncate text-sm font-medium">
        {agent?.name ?? agentId}
      </span>
    </div>
  );
}
