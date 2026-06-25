"use client";

import { useState, useEffect } from "react";
import { Bot, Users } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { MulticaIcon } from "./multica-icon";

interface ActorAvatarProps {
  name: string;
  initials: string;
  avatarUrl?: string | null;
  isAgent?: boolean;
  isSystem?: boolean;
  isSquad?: boolean;
  size?: number;
  className?: string;
  /** When true, renders the actor name to the right of the avatar. */
  showName?: boolean;
}

function ActorAvatar({
  name,
  initials,
  avatarUrl,
  isAgent,
  isSystem,
  isSquad,
  size = 30,
  className,
  showName,
}: ActorAvatarProps) {
  const [imgError, setImgError] = useState(false);

  useEffect(() => {
    setImgError(false);
  }, [avatarUrl]);

  const avatarElement = (
    <div
      data-slot="avatar"
      className={cn(
        "inline-flex shrink-0 items-center justify-center font-medium overflow-hidden",
        // Squads (a group, non-human) get a square tile so they don't read as
        // a single person; everyone else stays round.
        isSquad ? "rounded-md" : "rounded-full",
        (!avatarUrl || imgError) && "bg-muted text-muted-foreground",
        className
      )}
      style={{ width: size, height: size, fontSize: size * 0.45 }}
      title={name}
    >
      {avatarUrl && !imgError ? (
        <img
          src={avatarUrl}
          alt={name}
          className="h-full w-full object-cover"
          onError={() => setImgError(true)}
        />
      ) : isSystem ? (
        <MulticaIcon noSpin style={{ width: size * 0.55, height: size * 0.55 }} />
      ) : isAgent ? (
        <Bot style={{ width: size * 0.55, height: size * 0.55 }} />
      ) : isSquad ? (
        <Users style={{ width: size * 0.55, height: size * 0.55 }} />
      ) : (
        initials
      )}
    </div>
  );

  if (!showName) {
    return avatarElement;
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      {avatarElement}
      <span className="text-sm truncate">{name}</span>
    </span>
  );
}

export { ActorAvatar, type ActorAvatarProps };
