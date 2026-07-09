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
}

function ActorAvatar({
  name,
  initials,
  avatarUrl,
  isAgent,
  isSystem,
  isSquad,
  size = 20,
  className,
}: ActorAvatarProps) {
  const [imgError, setImgError] = useState(false);

  useEffect(() => {
    setImgError(false);
  }, [avatarUrl]);

  // Only humans (members) read as a circle. Non-human actors — agents, squads,
  // and the system identity — get a rounded square so they don't read as a
  // single person. This is the single source of truth for avatar shape; the
  // upload editors mirror it (packages/views/common/avatar-upload-control.tsx).
  const isHuman = !isAgent && !isSystem && !isSquad;

  return (
    <div
      data-slot="avatar"
      className={cn(
        "inline-flex shrink-0 items-center justify-center font-medium overflow-hidden",
        isHuman ? "rounded-full" : "rounded-md",
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
}

export { ActorAvatar, type ActorAvatarProps };
