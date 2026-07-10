"use client";

import type { ReactNode } from "react";
import { Users } from "lucide-react";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { AgentProfileCard } from "../agents/components/agent-profile-card";
import { MemberProfileCard } from "../members/member-profile-card";
import { SquadProfileCard } from "../squads/components/squad-profile-card";
import { useT } from "../i18n";

/**
 * MentionHoverCard — hover preview for an inline actor mention chip.
 *
 * member/agent/squad reuse the SAME profile cards as the views ActorAvatar's
 * hover (AgentProfileCard / MemberProfileCard / SquadProfileCard), so hovering
 * a mention matches hovering that actor anywhere else in the product (comment
 * authors, assignees, …). @all has no profile, so it renders a small static
 * "All members" card.
 *
 * The trigger span is intentionally non-focusable — the chip itself owns
 * focusability (editor: focusable; readonly: not), so this wrapper never adds
 * a keyboard tab stop (R14).
 */
export function MentionHoverCard({
  type,
  id,
  children,
}: {
  type: string;
  id: string;
  children: ReactNode;
}) {
  const { t } = useT("editor");

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
