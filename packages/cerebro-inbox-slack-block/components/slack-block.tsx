// TECH-3422
"use client";

// Slack-block left rail for the dynamic inbox: a presentational client
// component that lists people (with live online dots), their DMs, and channels.
// Clicking a row calls `onOpenChannel` so the parent opens the conversation in
// its detail panel — this component never navigates itself.
//
// Styling: light-mode, Tailwind semantic tokens only (bg-card,
// text-muted-foreground, bg-success for the online dot, etc.). No hardcoded
// colors — see CLAUDE.md CSS Architecture.
import { useMemo } from "react";
import { Hash } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { channelListOptions, useCreateChannel } from "@multica/core/channels";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import type { Channel } from "@multica/core/types";
import { useMemberPresence } from "../hooks/use-member-presence";
import { useChannelTyping } from "../hooks/use-channel-typing";

export interface SlackBlockProps {
  wsId: string;
  selectedChannelId: string | null;
  onOpenChannel: (channel: Channel) => void;
}

/** A DM channel is "with" a member when its participants are exactly
 *  {self, member} (two members, the current user plus that one). */
function dmWithMember(
  channels: Channel[],
  selfUserId: string | undefined,
  memberUserId: string,
): Channel | undefined {
  if (!selfUserId) return undefined;
  return channels.find((c) => {
    if (c.kind !== "dm") return false;
    const memberParticipants = c.participants.filter(
      (p) => p.user_type === "member",
    );
    if (memberParticipants.length !== 2) return false;
    const ids = new Set(memberParticipants.map((p) => p.user_id));
    return ids.has(selfUserId) && ids.has(memberUserId);
  });
}

export function SlackBlock({
  wsId,
  selectedChannelId,
  onOpenChannel,
}: SlackBlockProps) {
  // Gate the whole block behind its cerebro feature flag. Reading it keeps the
  // flag wired even though the parent also gates the "Add section" entry point.
  const enabled = useFeatureFlag("cerebro_inbox_slack_block");

  const selfUserId = useAuthStore((s) => s.user?.id);
  const { data: channels = [] } = useQuery(channelListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { onlineUserIds } = useMemberPresence(wsId);
  // Watch typing for the currently selected conversation so a member's DM row
  // can surface "skriver…" when they type.
  const { typingUserIds } = useChannelTyping(selectedChannelId);

  const createChannel = useCreateChannel();

  // People = workspace members excluding self, sorted by name.
  const people = useMemo(
    () =>
      members
        .filter((m) => m.user_id !== selfUserId)
        .slice()
        .sort((a, b) => a.name.localeCompare(b.name)),
    [members, selfUserId],
  );

  const channelRooms = useMemo(
    () =>
      channels
        .filter((c) => c.kind === "channel")
        .slice()
        .sort((a, b) => a.title.localeCompare(b.title)),
    [channels],
  );

  const onlineCount = useMemo(
    () => people.filter((m) => onlineUserIds.has(m.user_id)).length,
    [people, onlineUserIds],
  );

  if (!enabled) return null;

  const openMember = async (memberUserId: string): Promise<void> => {
    const existing = dmWithMember(channels, selfUserId, memberUserId);
    if (existing) {
      onOpenChannel(existing);
      return;
    }
    const created = await createChannel.mutateAsync({
      kind: "dm",
      name: "",
      member_ids: [memberUserId],
      agent_ids: [],
    });
    onOpenChannel(created);
  };

  return (
    <div className="flex flex-col gap-4 bg-card p-3 text-sm">
      <div className="flex items-center justify-between px-1">
        <h2 className="font-semibold text-foreground">Team &amp; kanaler</h2>
        <span className="text-xs text-muted-foreground">
          {onlineCount} online
        </span>
      </div>

      <section className="flex flex-col gap-1">
        <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
          Direkte beskeder
        </h3>
        {people.length === 0 ? (
          <p className="px-1 py-2 text-xs text-muted-foreground">
            Ingen personer endnu.
          </p>
        ) : (
          people.map((m) => {
            const isOnline = onlineUserIds.has(m.user_id);
            const dm = dmWithMember(channels, selfUserId, m.user_id);
            const unread = dm?.unread_count ?? 0;
            const isSelected = dm != null && dm.id === selectedChannelId;
            const isTyping =
              dm != null &&
              dm.id === selectedChannelId &&
              typingUserIds.has(m.user_id);
            return (
              <button
                key={m.user_id}
                type="button"
                onClick={() => void openMember(m.user_id)}
                className={`flex items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left transition-colors ${
                  isSelected ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                <span className="relative shrink-0">
                  <ActorAvatar actorType="member" actorId={m.user_id} size={24} />
                  <span
                    data-testid={`presence-dot-${m.user_id}`}
                    data-online={isOnline ? "true" : "false"}
                    className={`absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full border-2 border-card ${
                      isOnline ? "bg-success" : "bg-muted-foreground"
                    }`}
                    aria-label={isOnline ? "online" : "offline"}
                  />
                </span>
                <span className="flex min-w-0 flex-1 items-center gap-1.5">
                  <span className="truncate text-foreground">{m.name}</span>
                  {isTyping && (
                    <span className="shrink-0 text-xs italic text-brand">
                      skriver…
                    </span>
                  )}
                </span>
                {unread > 0 && (
                  <span className="shrink-0 rounded-full bg-warning px-1.5 py-0.5 text-[10px] font-semibold text-warning-foreground">
                    {unread}
                  </span>
                )}
              </button>
            );
          })
        )}
      </section>

      <section className="flex flex-col gap-1">
        <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
          Kanaler
        </h3>
        {channelRooms.length === 0 ? (
          <p className="px-1 py-2 text-xs text-muted-foreground">
            Ingen kanaler endnu.
          </p>
        ) : (
          channelRooms.map((c) => {
            const isSelected = c.id === selectedChannelId;
            return (
              <button
                key={c.id}
                type="button"
                onClick={() => onOpenChannel(c)}
                className={`flex items-center gap-2 rounded-md px-1.5 py-1.5 text-left transition-colors ${
                  isSelected ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                <Hash className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate text-foreground">
                  {c.title}
                </span>
                {c.unread_count > 0 && (
                  <span className="shrink-0 rounded-full bg-warning px-1.5 py-0.5 text-[10px] font-semibold text-warning-foreground">
                    {c.unread_count}
                  </span>
                )}
              </button>
            );
          })
        )}
      </section>
    </div>
  );
}
