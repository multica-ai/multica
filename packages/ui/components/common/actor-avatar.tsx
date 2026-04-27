"use client";

import { useState, useEffect } from "react";
import type React from "react";
import { Bot } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

interface ActorAvatarProps {
  name: string;
  initials: string;
  avatarUrl?: string | null;
  isAgent?: boolean;
  providerFallback?: React.ReactNode;
  size?: number;
  className?: string;
}

function ActorAvatar({
  name,
  initials,
  avatarUrl,
  isAgent,
  providerFallback,
  size = 20,
  className,
}: ActorAvatarProps) {
  const [imgError, setImgError] = useState(false);

  // Reset error state when URL changes (e.g. user uploads new avatar)
  useEffect(() => {
    setImgError(false);
  }, [avatarUrl]);

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
      {avatarUrl && !imgError ? (
        <img
          src={avatarUrl}
          alt={name}
          className="h-full w-full object-cover"
          onError={() => setImgError(true)}
        />
      ) : isAgent && providerFallback ? (
        providerFallback
      ) : isAgent ? (
        <Bot style={{ width: size * 0.55, height: size * 0.55 }} />
      ) : (
        initials
      )}
    </div>
  );
}

export { ActorAvatar, type ActorAvatarProps };
