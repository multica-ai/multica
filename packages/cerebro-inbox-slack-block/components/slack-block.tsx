// TECH-3422
"use client";

// Slack-block left rail for the dynamic inbox: a presentational client
// component that lists people (with live online dots), their DMs, and channels.
// Clicking a row calls `onOpenChannel` so the parent opens the conversation in
// its detail panel — this component never navigates itself.
//
// It carries the same section "control box" as the other dynamic-inbox sections
// (move up/down, settings, remove) so it reads as one of the stack. The section
// chrome is driven by plain callbacks from the parent (not the InboxSectionConfig
// type) to avoid a circular dependency with @multica/cerebro-inbox-dynamic.
//
// Styling: light-mode, Tailwind semantic tokens only (bg-card,
// text-muted-foreground, bg-success for the online dot, etc.). No hardcoded
// colors — see CLAUDE.md CSS Architecture.
import { useMemo } from "react";
import { Hash, Star, ChevronUp, ChevronDown, Settings2, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { channelListOptions, useCreateChannel } from "@multica/core/channels";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  useChannelFavoritesStore,
  actorKey,
  channelKey,
} from "@multica/cerebro-channels";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import type { Channel } from "@multica/core/types";
import { useMemberPresence } from "../hooks/use-member-presence";
import { useChannelTyping } from "../hooks/use-channel-typing";

export interface SlackBlockProps {
  wsId: string;
  selectedChannelId: string | null;
  onOpenChannel: (channel: Channel) => void;
  /** How many people to show (0 / undefined = all). Starred people count first. */
  maxPeople?: number;
  onSetMaxPeople: (n: number | undefined) => void;
  onRemove: () => void;
  onMove: (dir: -1 | 1) => void;
  isFirst: boolean;
  isLast: boolean;
}

/** Options offered in the "Show people" setting. `undefined` = all. */
const PEOPLE_LIMITS: Array<{ label: string; value: number | undefined }> = [
  { label: "10", value: 10 },
  { label: "15", value: 15 },
  { label: "25", value: 25 },
  { label: "Alle", value: undefined },
];

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
  maxPeople,
  onSetMaxPeople,
  onRemove,
  onMove,
  isFirst,
  isLast,
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

  // Starred people + channels float to the top. Selecting the favorites array
  // (not a derived value) keeps the rows reactive to star toggles.
  const favorites = useChannelFavoritesStore((s) => s.favorites);
  const toggleFavorite = useChannelFavoritesStore((s) => s.toggle);

  // People = workspace members excluding self, starred first then by name.
  const people = useMemo(() => {
    const isStarred = (userId: string) =>
      favorites.includes(actorKey("member", userId));
    return members
      .filter((m) => m.user_id !== selfUserId)
      .slice()
      .sort((a, b) => {
        const sa = isStarred(a.user_id);
        const sb = isStarred(b.user_id);
        if (sa !== sb) return sa ? -1 : 1;
        return a.name.localeCompare(b.name);
      });
  }, [members, selfUserId, favorites]);

  const visiblePeople =
    maxPeople && maxPeople > 0 ? people.slice(0, maxPeople) : people;
  const hiddenPeople = people.length - visiblePeople.length;

  const channelRooms = useMemo(() => {
    const isStarred = (id: string) => favorites.includes(channelKey(id));
    return channels
      .filter((c) => c.kind === "channel")
      .slice()
      .sort((a, b) => {
        const sa = isStarred(a.id);
        const sb = isStarred(b.id);
        if (sa !== sb) return sa ? -1 : 1;
        return a.title.localeCompare(b.title);
      });
  }, [channels, favorites]);

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
    <section className="overflow-hidden rounded-xl border border-border bg-card text-sm">
      {/* Control box — matches the other dynamic-inbox sections. */}
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          Team &amp; kanaler
        </span>
        <span className="text-xs text-muted-foreground">{onlineCount} online</span>
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          <button
            type="button"
            className="rounded p-1 hover:bg-muted disabled:opacity-30"
            disabled={isFirst}
            onClick={() => onMove(-1)}
            title="Flyt op"
          >
            <ChevronUp className="size-3.5" />
          </button>
          <button
            type="button"
            className="rounded p-1 hover:bg-muted disabled:opacity-30"
            disabled={isLast}
            onClick={() => onMove(1)}
            title="Flyt ned"
          >
            <ChevronDown className="size-3.5" />
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="rounded p-1 hover:bg-muted"
                  title="Indstillinger"
                />
              }
            >
              <Settings2 className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Vis personer</DropdownMenuLabel>
                {PEOPLE_LIMITS.map((opt) => (
                  <DropdownMenuItem
                    key={opt.label}
                    onClick={() => onSetMaxPeople(opt.value)}
                  >
                    {opt.label} {maxPeople === opt.value ? "✓" : ""}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            onClick={onRemove}
            title="Fjern blok"
          >
            <X className="size-3.5" />
          </button>
        </div>
      </header>

      <div className="flex flex-col gap-4 p-3">
        <div className="flex flex-col gap-1">
          <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
            Direkte beskeder
          </h3>
          {visiblePeople.length === 0 ? (
            <p className="px-1 py-2 text-xs text-muted-foreground">
              Ingen personer endnu.
            </p>
          ) : (
            visiblePeople.map((m) => {
              const isOnline = onlineUserIds.has(m.user_id);
              const dm = dmWithMember(channels, selfUserId, m.user_id);
              const unread = dm?.unread_count ?? 0;
              const isSelected = dm != null && dm.id === selectedChannelId;
              const isTyping =
                dm != null &&
                dm.id === selectedChannelId &&
                typingUserIds.has(m.user_id);
              const favKey = actorKey("member", m.user_id);
              const starred = favorites.includes(favKey);
              return (
                <div
                  key={m.user_id}
                  className={`group flex items-center gap-2.5 rounded-md px-1.5 py-1.5 transition-colors ${
                    isSelected ? "bg-accent" : "hover:bg-accent/50"
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => void openMember(m.user_id)}
                    className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
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
                  </button>
                  {unread > 0 && (
                    <span className="shrink-0 rounded-full bg-warning px-1.5 py-0.5 text-[10px] font-semibold text-warning-foreground">
                      {unread}
                    </span>
                  )}
                  <button
                    type="button"
                    aria-label={starred ? "Fjern stjerne" : "Stjernemarkér"}
                    aria-pressed={starred}
                    onClick={() => toggleFavorite(favKey)}
                    className={`shrink-0 rounded p-0.5 transition-opacity ${
                      starred
                        ? "text-warning opacity-100"
                        : "text-muted-foreground opacity-0 hover:text-foreground group-hover:opacity-100"
                    }`}
                  >
                    <Star
                      className="size-3.5"
                      fill={starred ? "currentColor" : "none"}
                    />
                  </button>
                </div>
              );
            })
          )}
          {hiddenPeople > 0 && (
            <p className="px-1.5 pt-0.5 text-[10px] text-muted-foreground">
              +{hiddenPeople} flere skjult — skru op under Indstillinger.
            </p>
          )}
        </div>

        <div className="flex flex-col gap-1">
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
              const favKey = channelKey(c.id);
              const starred = favorites.includes(favKey);
              return (
                <div
                  key={c.id}
                  className={`group flex items-center gap-2 rounded-md px-1.5 py-1.5 transition-colors ${
                    isSelected ? "bg-accent" : "hover:bg-accent/50"
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => onOpenChannel(c)}
                    className="flex min-w-0 flex-1 items-center gap-2 text-left"
                  >
                    <Hash className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate text-foreground">
                      {c.title}
                    </span>
                  </button>
                  {c.unread_count > 0 && (
                    <span className="shrink-0 rounded-full bg-warning px-1.5 py-0.5 text-[10px] font-semibold text-warning-foreground">
                      {c.unread_count}
                    </span>
                  )}
                  <button
                    type="button"
                    aria-label={starred ? "Fjern stjerne" : "Stjernemarkér"}
                    aria-pressed={starred}
                    onClick={() => toggleFavorite(favKey)}
                    className={`shrink-0 rounded p-0.5 transition-opacity ${
                      starred
                        ? "text-warning opacity-100"
                        : "text-muted-foreground opacity-0 hover:text-foreground group-hover:opacity-100"
                    }`}
                  >
                    <Star
                      className="size-3.5"
                      fill={starred ? "currentColor" : "none"}
                    />
                  </button>
                </div>
              );
            })
          )}
        </div>
      </div>
    </section>
  );
}
