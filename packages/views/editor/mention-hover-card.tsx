"use client";

import type { ReactNode } from "react";
import { Users } from "lucide-react";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { isActorMentionType } from "@multica/core/mention";
import type { MentionType } from "@multica/core/mention";
import { AgentProfileCard } from "../agents/components/agent-profile-card";
import { MemberProfileCard } from "../members/member-profile-card";
import { SquadProfileCard } from "../squads/components/squad-profile-card";
import { SkillProfileCard } from "./skill-profile-card";
import { useT } from "../i18n";

/**
 * MentionHoverCard — hover preview for an inline actor mention chip.
 *
 * Dispatch is registry-driven via `isActorMentionType` from
 * `@multica/core/mention`. member/agent/squad reuse the SAME profile cards as
 * the views ActorAvatar's hover (AgentProfileCard / MemberProfileCard /
 * SquadProfileCard), so hovering a mention matches hovering that actor anywhere
 * else in the product (comment authors, assignees, ...). @all has no profile,
 * so it renders a small static "All members" card. Skill mentions render a
 * SkillProfileCard showing the skill name, description, and bound agents.
 *
 * The trigger span is intentionally non-focusable — the chip itself owns
 * focusability (editor: focusable; readonly: not), so this wrapper never adds
 * a keyboard tab stop (R14).
 */
export function MentionHoverCard({
  type,
  id,
  label,
  description,
  children,
}: {
  type: string;
  id: string;
  /** Display label — used by skill hover cards for the skill name. */
  label?: string;
  /** Optional description — used by skill hover cards. */
  description?: string;
  children: ReactNode;
}) {
  const { t } = useT("editor");

  // Skill type: render a SkillProfileCard with agent affinity.
  if (type === "skill") {
    return (
      <HoverCard>
        <HoverCardTrigger render={<span />} className="cursor-default">
          {children}
        </HoverCardTrigger>
        <HoverCardContent align="start" className="w-72">
          <SkillProfileCard
            skillId={id}
            skillName={label ?? id}
            skillDescription={description}
          />
        </HoverCardContent>
      </HoverCard>
    );
  }

  // Registry-driven guard: only actor types get hover cards.
  if (!isActorMentionType(type as MentionType)) {
    return <>{children}</>;
  }

  const content: ReactNode =
    type === "agent" ? (
      <AgentProfileCard agentId={id} />
    ) : type === "member" ? (
      <MemberProfileCard userId={id} />
    ) : type === "squad" ? (
      <SquadProfileCard squadId={id} />
    ) : (
      <AllMembersContent label={t(($) => $.mention.all_members)} />
    );

  return (
    <HoverCard>
      <HoverCardTrigger render={<span />} className="cursor-default">
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        {content}
      </HoverCardContent>
    </HoverCard>
  );
}

function AllMembersContent({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2.5">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-warning/10">
        <Users className="h-4 w-4 text-warning" />
      </div>
      <p className="text-sm font-medium">{label}</p>
    </div>
  );
}
