"use client";

import type { ReactNode } from "react";
import { Users } from "lucide-react";
import { HoverCard, HoverCardTrigger, HoverCardContent } from "@multica/ui/components/ui/hover-card";
import { ActorAvatar } from "./actor-avatar";

interface MentionHoverCardProps {
  type: string;
  id: string;
  name: string;
  initials: string;
  avatarUrl?: string | null;
  role?: string;
  children: ReactNode;
}

function MentionHoverCard({
  type,
  id: _id,
  name,
  initials,
  avatarUrl,
  role,
  children,
}: MentionHoverCardProps) {
  if (type === "all") {
    return (
      <HoverCard>
        <HoverCardTrigger render={<span />} className="cursor-default">
          {children}
        </HoverCardTrigger>
        <HoverCardContent align="start" className="w-auto min-w-48 max-w-72">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary/10">
              <Users className="h-4 w-4 text-primary" />
            </div>
            <div className="min-w-0">
              <p className="text-sm font-medium">All members</p>
              <p className="text-xs text-muted-foreground">Notifies all workspace members</p>
            </div>
          </div>
        </HoverCardContent>
      </HoverCard>
    );
  }

  return (
    <HoverCard>
      <HoverCardTrigger render={<span />} className="cursor-default">
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-auto min-w-48 max-w-72">
        <div className="flex items-center gap-2.5">
          <ActorAvatar
            name={name}
            initials={initials}
            avatarUrl={avatarUrl}
            isAgent={type === "agent"}
            size={32}
          />
          <div className="min-w-0">
            <p className="text-sm font-medium truncate">{name}</p>
            {role && (
              <p className="text-xs text-muted-foreground truncate">{role}</p>
            )}
          </div>
        </div>
      </HoverCardContent>
    </HoverCard>
  );
}

export { MentionHoverCard, type MentionHoverCardProps };
