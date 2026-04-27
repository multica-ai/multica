"use client";

import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import { ProviderLogo } from "../runtimes/components/provider-logo";

interface ActorAvatarProps {
  actorType: string;
  actorId: string;
  size?: number;
  className?: string;
}

export function ActorAvatar({ actorType, actorId, size, className }: ActorAvatarProps) {
  const { getActorName, getActorInitials, getActorAvatarUrl, getActorProvider } = useActorName();
  const isAgent = actorType === "agent";
  const provider = isAgent ? getActorProvider(actorId) : null;
  const iconSize = (size ?? 20) * 0.65;
  return (
    <ActorAvatarBase
      name={getActorName(actorType, actorId)}
      initials={getActorInitials(actorType, actorId)}
      avatarUrl={getActorAvatarUrl(actorType, actorId)}
      isAgent={isAgent}
      providerFallback={
        provider ? (
          <span style={{ width: iconSize, height: iconSize, display: "flex" }}>
            <ProviderLogo provider={provider} className="h-full w-full" />
          </span>
        ) : undefined
      }
      size={size}
      className={className}
    />
  );
}
