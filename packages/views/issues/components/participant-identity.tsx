"use client";

import { useMemo, type CSSProperties, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { useCurrentWorkspace } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT, useTimeAgo } from "../../i18n";
import { assignParticipantColors, participantColor, participantStyle, type ParticipantKey } from "./participant-color";
import { collectThreadParticipants } from "./thread-utils";

/**
 * Comment-thread identity cues (participant colour on avatars, names and
 * comment accents), kept out of comment-card.tsx so it only gains a few call
 * sites.
 */

const byCreatedAt = (a: { created_at: string }, b: { created_at: string }) =>
  a.created_at.localeCompare(b.created_at);

/**
 * Colour slots for the current workspace roster, oldest member/agent first.
 * Reuses the member and agent lists useActorName already caches, so it adds
 * no requests. Until they load, colours fall back to the bare hash.
 */
function useParticipantSlots(): ReadonlyMap<string, number> {
  // Comment cards also render outside a workspace route (previews, tests);
  // there the colours fall back to the bare hash instead of throwing.
  const wsId = useCurrentWorkspace()?.id ?? "";
  const { data: members } = useQuery({ ...memberListOptions(wsId), enabled: wsId !== "" });
  const { data: agents } = useQuery({ ...agentListOptions(wsId), enabled: wsId !== "" });
  return useMemo(() => {
    const roster: ParticipantKey[] = [
      ...(Array.isArray(members) ? members : []).map((m) => ({ type: "member", id: m.user_id, created_at: m.created_at })),
      ...(Array.isArray(agents) ? agents : []).map((a) => ({ type: "agent", id: a.id, created_at: a.created_at })),
    ].sort(byCreatedAt);
    return assignParticipantColors(roster);
  }, [members, agents]);
}

/** The author's colour as a `--participant` style, or undefined for system actors. */
export function useParticipantStyle(actorType: string, actorId: string): CSSProperties | undefined {
  const slots = useParticipantSlots();
  return participantStyle(participantColor(actorType, actorId, slots));
}

/** Ring in the participant's colour around an avatar: solid for people, dashed for agents. */
export function ParticipantRing({ actorType, actorId, children }: {
  actorType: string;
  actorId: string;
  children: ReactNode;
}) {
  const style = useParticipantStyle(actorType, actorId);
  if (!style) return <>{children}</>;
  // An outline rather than a ring so agents can be dashed, matching the solid
  // (person) / dashed (agent) left border on comments.
  return (
    <span
      style={style}
      className={cn(
        "inline-flex shrink-0 rounded-full outline-[1.5px] outline-offset-1 outline-(--participant)",
        actorType === "agent" ? "outline-dashed" : "outline-solid",
      )}
    >
      {children}
    </span>
  );
}

/**
 * Left border in the author's colour for one comment block, plus a background
 * tint for agents.
 * `--comment-bg` is inherited by the block's sticky header so the header keeps
 * the same tint while it pins over the body.
 */
export function useParticipantBlockStyle(actorType: string, actorId: string): CSSProperties | undefined {
  const style = useParticipantStyle(actorType, actorId);
  if (!style) return undefined;
  // People keep a transparent background (only their border is coloured);
  // agents get a translucent wash of their colour. Discussions are mostly
  // one person with several agents, so this makes "who is a person" the
  // first thing you see, while each agent stays distinguishable.
  if (actorType !== "agent") return style;
  return { ...style, "--comment-bg": "color-mix(in srgb, var(--participant) var(--participant-tint), transparent)" } as CSSProperties;
}

/**
 * Classes that paint a block styled by useParticipantBlockStyle. Members get a
 * solid left border and agents a dashed one, so people and agents read as two
 * kinds at a glance while each keeps their own colour.
 */
export function participantBlockClassName(actorType: string): string {
  return cn("border-l-4 border-l-(--participant)", actorType === "agent" && "border-dashed bg-(--comment-bg)");
}

/** Inline style colouring an author name; for spans that must stay native elements. */
export function useParticipantNameStyle(actorType: string, actorId: string): CSSProperties | undefined {
  const style = useParticipantStyle(actorType, actorId);
  return style ? { ...style, color: "var(--participant)" } : undefined;
}

/** Author name in the participant's colour. */
export function ParticipantName({ actorType, actorId, className, children }: {
  actorType: string;
  actorId: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={className} style={useParticipantNameStyle(actorType, actorId)}>
      {children}
    </span>
  );
}

const MAX_SUMMARY_AVATARS = 5;

/**
 * One-line thread summary between the root comment and its replies:
 * participants, reply count and last activity. Marks where a discussion's
 * replies begin, so consecutive threads no longer read as one stream.
 */
export function ThreadSummary({ root, replies }: {
  root: TimelineEntry;
  replies: TimelineEntry[];
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  if (replies.length === 0) return null;
  const participants = collectThreadParticipants(root, replies);
  const last = replies.reduce((a, b) => (b.created_at > a.created_at ? b : a));
  return (
    <div
      data-thread-summary
      className="flex items-center gap-2 border-t border-border/50 px-4 max-md:px-3 py-1.5 text-label text-muted-foreground"
    >
      <span className="flex items-center -space-x-1">
        {participants.slice(0, MAX_SUMMARY_AVATARS).map((p) => (
          <ParticipantRing key={`${p.actor_type}:${p.actor_id}`} actorType={p.actor_type} actorId={p.actor_id}>
            <ActorAvatar
              actorType={p.actor_type}
              actorId={p.actor_id}
              name={p.actor_name}
              avatarUrl={p.actor_avatar_url}
              size="xs"
            />
          </ParticipantRing>
        ))}
      </span>
      {participants.length > MAX_SUMMARY_AVATARS && (
        <span>+{participants.length - MAX_SUMMARY_AVATARS}</span>
      )}
      <span>{t(($) => $.comment.reply_count, { count: replies.length })}</span>
      <span aria-hidden>·</span>
      <span>{timeAgo(last.created_at)}</span>
    </div>
  );
}
