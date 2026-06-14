// TECH-3422 / TECH-3494
"use client";

// Slack-block left rail for the dynamic inbox: a presentational client
// component that lists conversations — channels, people (with live online
// dots + DMs) and, opt-in, agents — as ONE ranked list. Clicking a row calls
// the parent so it opens the conversation in its detail panel; this component
// never navigates itself.
//
// TECH-3494 — the list is unified: a single total limit applies across all
// three kinds, the default sort is "most recent conversation" across all kinds,
// and the user picks whether to group by type or see one flat list. No
// per-group scroll. All UI text is English.
//
// Styling: light-mode, Tailwind semantic tokens only (bg-card,
// text-muted-foreground, bg-success for the online dot, etc.). No hardcoded
// colors — see CLAUDE.md CSS Architecture.
import { useMemo, useState } from "react";
import { Hash, Star, Settings2, X, Search } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { channelListOptions, useCreateChannel } from "@multica/core/channels";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import {
  memberListOptions,
  agentListOptions,
} from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  useChannelFavoritesStore,
  actorKey,
  channelKey,
  type FavoriteKey,
} from "@multica/cerebro-channels";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { canAssignAgent } from "@multica/views/issues/components";
import type { Channel } from "@multica/core/types";
import { useMemberPresence } from "../hooks/use-member-presence";
import { useChannelTyping } from "../hooks/use-channel-typing";

export type TeamSort = "name" | "recent";
export type TeamGroupBy = "unread" | "type" | "none";

export interface SlackBlockProps {
  wsId: string;
  selectedChannelId: string | null;
  onOpenChannel: (channel: Channel) => void;
  /** Total rows to show across channels + people + agents. 0 = all. Default 10. */
  limit?: number;
  onSetLimit: (n: number) => void;
  /** Sort order across all kinds. Starred float to the top. Default "recent". */
  sort?: TeamSort;
  onSetSort: (s: TeamSort) => void;
  /** Group the list by kind, or show one flat list. Default "type". */
  groupBy?: TeamGroupBy;
  onSetGroupBy: (g: TeamGroupBy) => void;
  /** TECH-3494 — also list the workspace's agents (opt-in). Default off. */
  showAgents?: boolean;
  onSetShowAgents: (v: boolean) => void;
  /** Opens an agent chat in the parent's detail panel (no DM channel). */
  onOpenAgentChat: (agentId: string) => void;
  onRemove: () => void;
}

const DEFAULT_LIMIT = 10;

/** Total-rows options. 0 = show all. */
const LIMIT_OPTIONS: Array<{ label: string; value: number }> = [
  { label: "10", value: 10 },
  { label: "15", value: 15 },
  { label: "25", value: 25 },
  { label: "All", value: 0 },
];

const SORT_OPTIONS: Array<{ label: string; value: TeamSort }> = [
  { label: "Recent conversation", value: "recent" },
  { label: "Name", value: "name" },
];

const GROUP_OPTIONS: Array<{ label: string; value: TeamGroupBy }> = [
  { label: "Unread first", value: "unread" },
  { label: "By type", value: "type" },
  { label: "Flat list", value: "none" },
];

type ChatItem = {
  key: string;
  kind: "channel" | "person" | "agent";
  name: string;
  recency: number;
  unread: number;
  starred: boolean;
  favKey?: FavoriteKey;
  channel?: Channel;
  userId?: string;
  agentId?: string;
  online?: boolean;
};

/** Most-recent activity timestamp for a conversation (ms), 0 when none. */
function channelRecency(c: Channel | undefined): number {
  if (!c) return 0;
  const ts = c.last_message?.created_at ?? c.updated_at;
  return ts ? Date.parse(ts) || 0 : 0;
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
  limit,
  onSetLimit,
  sort = "recent",
  onSetSort,
  groupBy = "unread",
  onSetGroupBy,
  showAgents = false,
  onSetShowAgents,
  onOpenAgentChat,
  onRemove,
}: SlackBlockProps) {
  // Gate the whole block behind its cerebro feature flag. Reading it keeps the
  // flag wired even though the parent also gates the "Add section" entry point.
  const enabled = useFeatureFlag("cerebro_inbox_slack_block");

  const selfUserId = useAuthStore((s) => s.user?.id);
  const { data: channels = [] } = useQuery(channelListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  // Agents + their chat sessions are only fetched when the user opts in.
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: !!wsId && showAgents,
  });
  const { data: chatSessions = [] } = useQuery({
    ...chatSessionsOptions(wsId),
    enabled: !!wsId && showAgents,
  });
  const { onlineUserIds } = useMemberPresence(wsId);
  // Watch typing for the currently selected conversation so a member's DM row
  // can surface "typing…" when they type.
  const { typingUserIds } = useChannelTyping(selectedChannelId);
  const [searchOpen, setSearchOpen] = useState(false);
  const [search, setSearch] = useState("");

  const createChannel = useCreateChannel();

  const favorites = useChannelFavoritesStore((s) => s.favorites);
  const toggleFavorite = useChannelFavoritesStore((s) => s.toggle);

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === selfUserId)?.role,
    [members, selfUserId],
  );

  // Most-recent agent chat activity, keyed by agent id.
  const agentActivity = useMemo(() => {
    const map = new Map<string, { recency: number; unread: boolean }>();
    for (const s of chatSessions) {
      if (s.status === "archived") continue;
      const recency = Date.parse(s.updated_at) || 0;
      const prev = map.get(s.agent_id);
      map.set(s.agent_id, {
        recency: Math.max(prev?.recency ?? 0, recency),
        unread: (prev?.unread ?? false) || s.has_unread,
      });
    }
    return map;
  }, [chatSessions]);

  const normalizedSearch = search.trim().toLowerCase();

  // One unified list of every conversation the block can show.
  const allItems = useMemo<ChatItem[]>(() => {
    const items: ChatItem[] = [];

    for (const c of channels) {
      if (c.kind !== "channel") continue;
      items.push({
        key: `channel:${c.id}`,
        kind: "channel",
        name: c.title,
        recency: channelRecency(c),
        unread: c.unread_count ?? 0,
        starred: favorites.includes(channelKey(c.id)),
        favKey: channelKey(c.id),
        channel: c,
      });
    }

    for (const m of members) {
      if (m.user_id === selfUserId) continue;
      const dm = dmWithMember(channels, selfUserId, m.user_id);
      items.push({
        key: `person:${m.user_id}`,
        kind: "person",
        name: m.name,
        recency: channelRecency(dm),
        unread: dm?.unread_count ?? 0,
        starred: favorites.includes(actorKey("member", m.user_id)),
        favKey: actorKey("member", m.user_id),
        channel: dm,
        userId: m.user_id,
        online: onlineUserIds.has(m.user_id),
      });
    }

    if (showAgents) {
      for (const a of agents) {
        if (a.archived_at || !canAssignAgent(a, selfUserId, memberRole)) continue;
        const act = agentActivity.get(a.id);
        items.push({
          key: `agent:${a.id}`,
          kind: "agent",
          name: a.name,
          recency: act?.recency ?? 0,
          unread: act?.unread ? 1 : 0,
          starred: false,
          agentId: a.id,
        });
      }
    }

    return items;
  }, [
    channels,
    members,
    agents,
    showAgents,
    agentActivity,
    favorites,
    selfUserId,
    memberRole,
    onlineUserIds,
  ]);

  // Search → sort (starred first, then by chosen order) → cap to the total limit.
  const shownItems = useMemo(() => {
    const filtered = normalizedSearch
      ? allItems.filter((it) => it.name.toLowerCase().includes(normalizedSearch))
      : allItems;
    const sorted = filtered.slice().sort((a, b) => {
      if (a.starred !== b.starred) return a.starred ? -1 : 1;
      if (sort === "recent") {
        const d = b.recency - a.recency;
        if (d !== 0) return d;
      }
      return a.name.localeCompare(b.name);
    });
    const effective = limit == null ? DEFAULT_LIMIT : limit;
    return effective > 0 ? sorted.slice(0, effective) : sorted;
  }, [allItems, normalizedSearch, sort, limit]);

  const onlineCount = useMemo(
    () =>
      members.filter(
        (m) => m.user_id !== selfUserId && onlineUserIds.has(m.user_id),
      ).length,
    [members, selfUserId, onlineUserIds],
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

  const openItem = (it: ChatItem) => {
    if (it.kind === "agent" && it.agentId) onOpenAgentChat(it.agentId);
    else if (it.kind === "channel" && it.channel) onOpenChannel(it.channel);
    else if (it.kind === "person" && it.userId) void openMember(it.userId);
  };

  const renderRow = (it: ChatItem) => {
    const isSelected = it.channel != null && it.channel.id === selectedChannelId;
    const isTyping =
      it.kind === "person" &&
      it.channel != null &&
      it.channel.id === selectedChannelId &&
      it.userId != null &&
      typingUserIds.has(it.userId);
    return (
      <div
        key={it.key}
        className={`group flex items-center gap-2.5 rounded-md px-1.5 py-1.5 transition-colors ${
          isSelected ? "bg-accent" : "hover:bg-accent/50"
        }`}
      >
        <button
          type="button"
          onClick={() => openItem(it)}
          className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
        >
          <span className="relative shrink-0">
            {it.kind === "channel" ? (
              <span className="flex size-6 items-center justify-center text-muted-foreground">
                <Hash className="size-4" />
              </span>
            ) : it.kind === "agent" ? (
              <ActorAvatar actorType="agent" actorId={it.agentId!} size={24} />
            ) : (
              <ActorAvatar actorType="member" actorId={it.userId!} size={24} />
            )}
            {it.kind === "person" && (
              <span
                data-testid={`presence-dot-${it.userId}`}
                data-online={it.online ? "true" : "false"}
                className={`absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full border-2 border-card ${
                  it.online ? "bg-success" : "bg-muted-foreground"
                }`}
                aria-label={it.online ? "online" : "offline"}
              />
            )}
          </span>
          <span className="flex min-w-0 flex-1 items-center gap-1.5">
            <span className="truncate text-foreground">{it.name}</span>
            {isTyping && (
              <span className="shrink-0 text-xs italic text-brand">typing…</span>
            )}
          </span>
        </button>
        {it.unread > 0 && (
          <span className="shrink-0 rounded-full bg-warning px-1.5 py-0.5 text-[10px] font-semibold text-warning-foreground">
            {it.kind === "agent" ? "New" : it.unread}
          </span>
        )}
        {it.favKey && (
          <button
            type="button"
            aria-label={it.starred ? "Unstar" : "Star"}
            aria-pressed={it.starred}
            onClick={() => toggleFavorite(it.favKey!)}
            className={`shrink-0 rounded p-0.5 transition-opacity ${
              it.starred
                ? "text-warning opacity-100"
                : "text-muted-foreground opacity-0 hover:text-foreground group-hover:opacity-100"
            }`}
          >
            <Star className="size-3.5" fill={it.starred ? "currentColor" : "none"} />
          </button>
        )}
      </div>
    );
  };

  let groups: Array<{ label: string; items: ChatItem[] }>;
  if (groupBy === "type") {
    groups = [
      { label: "Channels", items: shownItems.filter((i) => i.kind === "channel") },
      { label: "People", items: shownItems.filter((i) => i.kind === "person") },
      { label: "Agents", items: shownItems.filter((i) => i.kind === "agent") },
    ].filter((g) => g.items.length > 0);
  } else if (groupBy === "unread") {
    groups = [
      { label: "Unread", items: shownItems.filter((i) => i.unread > 0) },
      { label: "Read", items: shownItems.filter((i) => i.unread === 0) },
    ].filter((g) => g.items.length > 0);
  } else {
    groups = [{ label: "", items: shownItems }];
  }

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card text-sm">
      {/* Control box — matches the other dynamic-inbox sections. TECH-3494:
          pl-8 clears the drag handle the parent overlays at left-2. */}
      <header className="flex items-center gap-2 border-b border-border py-2 pl-8 pr-3">
        <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          Chat
        </span>
        <span className="text-xs text-muted-foreground">{onlineCount} online</span>
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          <button
            type="button"
            className={`rounded p-1 hover:bg-muted ${
              searchOpen ? "bg-muted text-foreground" : ""
            }`}
            onClick={() => {
              setSearchOpen((open) => !open);
              if (searchOpen) setSearch("");
            }}
            aria-label="Search"
            title="Search"
          >
            <Search className="size-3.5" />
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="rounded p-1 hover:bg-muted"
                  title="Settings"
                />
              }
            >
              <Settings2 className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Sort by</DropdownMenuLabel>
                {SORT_OPTIONS.map((opt) => (
                  <DropdownMenuItem key={opt.value} onClick={() => onSetSort(opt.value)}>
                    {opt.label} {sort === opt.value ? "✓" : ""}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Group by</DropdownMenuLabel>
                {GROUP_OPTIONS.map((opt) => (
                  <DropdownMenuItem key={opt.value} onClick={() => onSetGroupBy(opt.value)}>
                    {opt.label} {groupBy === opt.value ? "✓" : ""}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Show</DropdownMenuLabel>
                {LIMIT_OPTIONS.map((opt) => {
                  const current = limit == null ? DEFAULT_LIMIT : limit;
                  return (
                    <DropdownMenuItem key={opt.label} onClick={() => onSetLimit(opt.value)}>
                      {opt.label} {current === opt.value ? "✓" : ""}
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem onClick={() => onSetShowAgents(!showAgents)}>
                  Show agents {showAgents ? "✓" : ""}
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            onClick={onRemove}
            title="Remove block"
          >
            <X className="size-3.5" />
          </button>
        </div>
      </header>

      <div className="flex flex-col gap-3 p-3">
        {searchOpen && (
          <div className="relative px-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search conversations..."
              aria-label="Search conversations"
              className="h-8 pl-8 pr-7 text-xs"
            />
            {search && (
              <button
                type="button"
                className="absolute right-2 top-1/2 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                onClick={() => setSearch("")}
                aria-label="Clear search"
                title="Clear search"
              >
                <X className="size-3.5" />
              </button>
            )}
          </div>
        )}

        {shownItems.length === 0 ? (
          <p className="px-1 py-2 text-xs text-muted-foreground">
            {normalizedSearch ? "No matches." : "No conversations yet."}
          </p>
        ) : (
          <div className="flex flex-col gap-3" data-testid="chat-list">
            {groups.map((g) => (
              <div key={g.label || "flat"} className="flex flex-col gap-1">
                {g.label && (
                  <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                    {g.label}
                  </h3>
                )}
                {g.items.map(renderRow)}
              </div>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
