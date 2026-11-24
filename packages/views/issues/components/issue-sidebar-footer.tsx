"use client";

import type { ReactNode } from "react";
import type { Issue } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { AgentProfileCard } from "../../agents/components/agent-profile-card";
import { MemberProfileCard } from "../../members/member-profile-card";
import { SquadProfileCard } from "../../squads/components/squad-profile-card";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { useLocale, useT } from "../../i18n";

// Placeholders the translated sentence is split on, so each language keeps its
// own word order while names and the date render as interactive nodes.
const slot = (name: string) => `\u2063${name}\u2063`;
const SLOT_PATTERN = /\u2063(\w+)\u2063/;

function fillSlots(sentence: string, nodes: Record<string, ReactNode>): ReactNode[] {
  return sentence.split(SLOT_PATTERN).map((part, index) => (index % 2 === 1 ? <span key={index}>{nodes[part]}</span> : part));
}

const nameClass =
  "cursor-default rounded-xs text-foreground focus-visible:outline-2 focus-visible:outline-ring";

function ActorName({ type, id }: { type: string; id: string }) {
  const { getActorName } = useActorName();
  const name = getActorName(type, id);
  const card =
    type === "agent" ? <AgentProfileCard agentId={id} />
      : type === "member" ? <MemberProfileCard userId={id} />
        : type === "squad" ? <SquadProfileCard squadId={id} />
          : null;
  if (!card) return <span className="text-foreground">{name}</span>;
  return (
    <HoverCard>
      <HoverCardTrigger render={<span />} tabIndex={0} className={nameClass}>
        {name}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        {card}
      </HoverCardContent>
    </HoverCard>
  );
}

/**
 * Who created the issue and when, as one line under the sidebar's sections.
 * The date's tooltip carries the exact time and the last update.
 */
export function IssueSidebarFooter({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const viewTZ = useViewingTimezone();
  const requesterId = issue.creator_type === "agent" ? issue.requester_id : null;
  const format = (value: string, options: Intl.DateTimeFormatOptions) =>
    new Intl.DateTimeFormat(locale, { ...options, timeZone: viewTZ }).format(new Date(value));
  const exact = (value: string) => format(value, { dateStyle: "medium", timeStyle: "short" });
  const date = (
    <Tooltip>
      <TooltipTrigger render={<span />} tabIndex={0} className={nameClass}>
        {format(issue.created_at, { month: "short", day: "numeric" })}
      </TooltipTrigger>
      <TooltipContent className="flex-col items-start gap-0">
        <span>{t(($) => $.detail.footer.created_at, { time: exact(issue.created_at) })}</span>
        <span className="text-muted-foreground">{t(($) => $.detail.footer.updated_at, { time: exact(issue.updated_at) })}</span>
      </TooltipContent>
    </Tooltip>
  );
  const sentence = requesterId
    ? t(($) => $.detail.footer.created_for, { creator: slot("creator"), requester: slot("requester"), date: slot("date") })
    : t(($) => $.detail.footer.created, { creator: slot("creator"), date: slot("date") });
  return (
    <p data-testid="issue-sidebar-footer" className="px-4 text-caption text-muted-foreground">
      {fillSlots(sentence, {
        creator: <ActorName type={issue.creator_type} id={issue.creator_id} />,
        requester: requesterId ? <ActorName type="member" id={requesterId} /> : null,
        date,
      })}
    </p>
  );
}
