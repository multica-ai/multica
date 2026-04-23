"use client";

import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { useActorName } from "@multica/core/workspace/hooks";
import { AgentProfileCard } from "../agents/components/agent-profile-card";

interface ActorAvatarProps {
  actorType: string;
  actorId: string;
  size?: number;
  className?: string;
  /** Disable the hover-card preview (e.g. when the avatar is itself the page subject). */
  disableHoverCard?: boolean;
}

export function ActorAvatar({
  actorType,
  actorId,
  size,
  className,
  disableHoverCard,
}: ActorAvatarProps) {
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  const avatar = (
    <ActorAvatarBase
      name={getActorName(actorType, actorId)}
      initials={getActorInitials(actorType, actorId)}
      avatarUrl={getActorAvatarUrl(actorType, actorId)}
      isAgent={actorType === "agent"}
      size={size}
      className={className}
    />
  );

  if (disableHoverCard || actorType !== "agent") {
    return avatar;
  }

  return (
    <HoverCard>
      <HoverCardTrigger render={<span />} className="inline-flex cursor-default">
        {avatar}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        <AgentProfileCard agentId={actorId} />
      </HoverCardContent>
    </HoverCard>
  );
}
