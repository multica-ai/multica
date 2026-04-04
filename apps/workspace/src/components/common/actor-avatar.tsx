"use client";

import { useState, useEffect } from "react";
import { Bot } from "lucide-react";
import { cn } from "@/lib/utils";
import { useActorName } from "@/features/workspace";

interface ActorAvatarProps {
  actorType: string;
  actorId: string;
  size?: number;
  avatarUrl?: string | null;
  getName?: (type: string, id: string) => string;
  getInitials?: (type: string, id: string) => string;
  getAvatarUrl?: (type: string, id: string) => string | null;
  className?: string;
}

function ActorAvatar({
  actorType,
  actorId,
  size = 20,
  avatarUrl,
  getName,
  getInitials,
  getAvatarUrl,
  className,
}: ActorAvatarProps) {
  const actorNameHook = useActorName();
  const resolveName = getName ?? actorNameHook.getActorName;
  const resolveInitials = getInitials ?? actorNameHook.getActorInitials;
  const resolveAvatarUrl = getAvatarUrl ?? actorNameHook.getActorAvatarUrl;

  const name = resolveName(actorType, actorId);
  const initials = resolveInitials(actorType, actorId);
  const isAgent = actorType === "agent";
  const resolvedUrl = avatarUrl !== undefined ? avatarUrl : resolveAvatarUrl(actorType, actorId);

  const [imgError, setImgError] = useState(false);

  // Reset error state when URL changes (e.g. user uploads new avatar)
  useEffect(() => {
    setImgError(false);
  }, [resolvedUrl]);

  return (
    <div
      data-slot="avatar"
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-full font-medium overflow-hidden",
        "bg-muted text-muted-foreground",
        className
      )}
      style={{ width: size, height: size, fontSize: size * 0.45 }}
      title={name}
    >
      {resolvedUrl && !imgError ? (
        <img
          src={resolvedUrl}
          alt={name}
          className="h-full w-full object-cover"
          onError={() => setImgError(true)}
        />
      ) : isAgent ? (
        <Bot style={{ width: size * 0.55, height: size * 0.55 }} />
      ) : (
        initials
      )}
    </div>
  );
}

export { ActorAvatar, type ActorAvatarProps };
