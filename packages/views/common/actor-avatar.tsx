"use client";

import { useEffect, useRef, useState } from "react";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { useActorName } from "@multica/core/workspace/hooks";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useCurrentWorkspace } from "@multica/core/paths";
import { AgentProfileCard } from "../agents/components/agent-profile-card";
import { AgentLivePeekCard } from "../agents/components/agent-live-peek-card";
import { MemberProfileCard } from "../members/member-profile-card";
import { availabilityConfig } from "../agents/presence";

/**
 * Selects which agent hover-card payload to render when `enableHoverCard` is
 * on. Two surfaces, two intents:
 * - `"profile"` (default) — static identity (description, runtime, skills,
 *   owner). Used by 20+ "who is this agent?" surfaces (comment authors,
 *   pickers, list rows).
 * - `"live"` — live activity peek (workload, current issue, last activity).
 *   Used where the user already knows the identity and wants the live state,
 *   e.g. the squad members tab.
 *
 * Has no effect for non-agent actors (members always render the member card).
 */
export type AgentHoverCardVariant = "profile" | "live";

interface ActorAvatarProps {
  actorType: string;
  actorId: string;
  size?: number;
  className?: string;
  /**
   * Wrap the avatar in a hover-card preview on dwell. Use for "who is this?"
   * surfaces — comment authors, list rows, subscriber chips. Independent of
   * `showStatusDot`: a surface can have one, both, or neither.
   */
  enableHoverCard?: boolean;
  /**
   * Overlay an agent-presence dot at the avatar's bottom-right. Use at
   * decision moments (picker rows, current-assignee display, agent-centric
   * surfaces). Has no effect for non-agent actors. Independent of
   * `enableHoverCard` so picker rows can show the dot without nesting a
   * popover inside the dropdown.
   */
  showStatusDot?: boolean;
  /**
   * When `enableHoverCard` is on for an agent, choose which payload to
   * render. See {@link AgentHoverCardVariant}. Defaults to `"profile"` so
   * existing call sites keep their identity-card behaviour.
   */
  hoverCardVariant?: AgentHoverCardVariant;
}

const FOCUSABLE_ANCESTOR_SELECTOR =
  'a[href], button:not([disabled]), [role="button"]:not([aria-disabled="true"]), [tabindex]:not([tabindex="-1"])';

export function ActorAvatar({
  actorType,
  actorId,
  size,
  className,
  enableHoverCard,
  showStatusDot,
  hoverCardVariant = "profile",
}: ActorAvatarProps) {
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  const avatar = (
    <ActorAvatarBase
      name={getActorName(actorType, actorId)}
      initials={getActorInitials(actorType, actorId)}
      avatarUrl={getActorAvatarUrl(actorType, actorId)}
      isAgent={actorType === "agent"}
      isSystem={actorType === "system"}
      isSquad={actorType === "squad"}
      size={size}
      className={className}
    />
  );

  // Optional presence dot overlay. Only meaningful for agents — members have
  // no presence backbone. Wrapping unconditionally with relative inline-flex
  // would create extra DOM for every avatar; we only wrap when a dot is asked
  // for.
  const wrapDot = showStatusDot && actorType === "agent";
  const dotted = wrapDot ? (
    <span className="relative inline-flex">
      {avatar}
      <AgentStatusDot agentId={actorId} size={size} />
    </span>
  ) : (
    avatar
  );

  if (!enableHoverCard) {
    return dotted;
  }
  if (actorType === "agent") {
    return (
      <AgentAvatarHoverCard agentId={actorId} variant={hoverCardVariant}>
        {dotted}
      </AgentAvatarHoverCard>
    );
  }
  if (actorType === "member") {
    return <MemberAvatarHoverCard userId={actorId}>{dotted}</MemberAvatarHoverCard>;
  }
  return dotted;
}

// Small presence indicator overlaid on the bottom-right of an agent avatar.
// Only renders on hover-enabled surfaces so dense decorative chips (e.g. the
// 14 px owner sub-avatar in agents-list rows) stay visually clean. The dot
// scales with the avatar size — anything ≥24 px gets the standard 8 px dot,
// smaller avatars use a 6 px dot so the indicator doesn't overwhelm them.
function AgentStatusDot({ agentId, size }: { agentId: string; size?: number }) {
  const ws = useCurrentWorkspace();
  const detail = useAgentPresenceDetail(ws?.id, agentId);
  if (detail === "loading") return null;

  const { dotClass, label } = availabilityConfig[detail.availability];
  const dotSize = (size ?? 24) >= 24 ? "h-1.5 w-1.5" : "h-1 w-1";

  return (
    <span
      aria-label={`Status: ${label}`}
      title={label}
      className={`absolute bottom-0 right-0 rounded-full ring-1 ring-background ${dotClass} ${dotSize}`}
    />
  );
}

/**
 * Wraps an agent avatar in a hover-card. The trigger is keyboard-focusable
 * only when no focusable ancestor (link/button) already provides a tab stop —
 * this prevents nested tabbable descendants and keyboard-nav bloat at sites
 * where the avatar lives inside a row link or click target.
 */
function AgentAvatarHoverCard({
  agentId,
  variant,
  children,
}: {
  agentId: string;
  variant: AgentHoverCardVariant;
  children: React.ReactNode;
}) {
  const content =
    variant === "live" ? (
      <AgentLivePeekCard agentId={agentId} />
    ) : (
      <AgentProfileCard agentId={agentId} />
    );
  return (
    <ActorAvatarHoverCardShell content={content}>
      {children}
    </ActorAvatarHoverCardShell>
  );
}

function MemberAvatarHoverCard({
  userId,
  children,
}: {
  userId: string;
  children: React.ReactNode;
}) {
  return (
    <ActorAvatarHoverCardShell content={<MemberProfileCard userId={userId} />}>
      {children}
    </ActorAvatarHoverCardShell>
  );
}

// Common chrome shared between agent and member hover cards. Keeps focus
// behaviour and width consistent so the two surfaces feel structurally
// parallel — content varies, frame doesn't.
function ActorAvatarHoverCardShell({
  content,
  children,
}: {
  content: React.ReactNode;
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
            ? "inline-flex cursor-pointer rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            : "inline-flex cursor-pointer"
        }
      >
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        {content}
      </HoverCardContent>
    </HoverCard>
  );
}
