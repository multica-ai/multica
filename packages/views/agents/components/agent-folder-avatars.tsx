"use client";

import type { AgentPresenceDetail } from "@multica/core/agents";
import type { Agent } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { availabilityConfig } from "../presence";

export function AgentFolderAvatars({
  agents,
  count,
  emptyLabel,
  presenceByAgentId,
  leaderId,
}: {
  agents: readonly Agent[];
  count: number;
  emptyLabel: string;
  presenceByAgentId?: ReadonlyMap<string, AgentPresenceDetail | null>;
  leaderId?: string | null;
}) {
  const { t } = useT("agents");
  if (count === 0) {
    return <span className="shrink-0 rounded-full border border-dashed px-2 text-caption text-muted-foreground">{emptyLabel}</span>;
  }

  const priority = (agent: Agent) => {
    const presence = presenceByAgentId?.get(agent.id);
    if (presence?.workload === "working") return 0;
    if (agent.id === leaderId) return 1;
    if (presence?.availability === "online") return 2;
    return 3;
  };
  const orderedAgents = agents
    .map((agent, index) => ({ agent, index }))
    .sort((a, b) => priority(a.agent) - priority(b.agent) || a.index - b.index)
    .map(({ agent }) => agent);
  const ring = "ring-2 ring-[var(--folder-row-background)]";
  const overflow = "size-6 shrink-0 items-center justify-center rounded-full bg-border text-caption tabular-nums text-muted-foreground";
  return (
    <span className="flex shrink-0 items-center -space-x-1.5">
      {orderedAgents.slice(0, 4).map((agent) => {
        const presence = presenceByAgentId?.get(agent.id);
        const visual = presence ? availabilityConfig[presence.availability] : null;
        return (
          <span
            key={agent.id}
            title={agent.name}
            className={cn("relative inline-flex rounded-full", ring)}
          >
            <ActorAvatar
              name={agent.name}
              initials={agent.name.slice(0, 1)}
              avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
              isAgent
              size="md"
            />
            {presence && visual && (
              <span
                aria-label={t(($) => $.presence_status_aria, {
                  status: t(($) => $.availability[presence.availability]),
                })}
                className={cn(
                  "absolute bottom-0 right-0 size-1.5 rounded-full ring-1 ring-background",
                  visual.dotClass,
                )}
              />
            )}
          </span>
        );
      })}
      {count > 4 && <span className={cn(overflow, ring, "inline-flex")}>+{count - 4}</span>}
    </span>
  );
}
