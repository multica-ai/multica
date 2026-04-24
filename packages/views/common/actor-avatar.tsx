"use client";

import { useEffect, useRef, useState } from "react";
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

const FOCUSABLE_ANCESTOR_SELECTOR =
  'a[href], button:not([disabled]), [role="button"]:not([aria-disabled="true"]), [tabindex]:not([tabindex="-1"])';

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

  return <AgentAvatarHoverCard agentId={actorId}>{avatar}</AgentAvatarHoverCard>;
}

/**
 * Wraps an agent avatar in a hover-card. The trigger is keyboard-focusable
 * only when no focusable ancestor (link/button) already provides a tab stop —
 * this prevents nested tabbable descendants and keyboard-nav bloat at sites
 * where the avatar lives inside a row link or click target.
 */
function AgentAvatarHoverCard({
  agentId,
  children,
}: {
  agentId: string;
  children: React.ReactNode;
}) {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const [standalone, setStandalone] = useState(false);

  useEffect(() => {
    const el = triggerRef.current;
    if (!el) return;
    const ancestor = el.parentElement?.closest(FOCUSABLE_ANCESTOR_SELECTOR);
    setStandalone(!ancestor);
  }, []);

  return (
    <HoverCard>
      <HoverCardTrigger
        render={<span ref={triggerRef} />}
        tabIndex={standalone ? 0 : -1}
        className={
          standalone
            ? "inline-flex rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            : "inline-flex"
        }
      >
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        <AgentProfileCard agentId={agentId} />
      </HoverCardContent>
    </HoverCard>
  );
}
