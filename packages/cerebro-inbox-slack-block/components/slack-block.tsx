// TECH-3422 / TECH-3494
"use client";

// Slack-block left rail for the dynamic inbox: a presentational client
// component that lists conversations — channels, people (with live online
// dots + DMs) and, opt-in, agents — as ONE ranked list. Clicking a row calls
// the parent so it opens the conversation in its detail panel; this component
// never navigates itself.
//
// TECH-3494 — the list is unified: the default sort is "most recent
// conversation" across all kinds, and the user picks whether to group by type
// or see one flat list. No per-group scroll. All UI text is English.
// TECH-3665 — the row limit is per kind when grouped by type (so the Channels
// group is always shown by default) and a single shared total when flat.
//
// Styling: light-mode, Tailwind semantic tokens only (bg-card,
// text-muted-foreground, bg-success for the online dot, etc.). No hardcoded
// colors — see CLAUDE.md CSS Architecture.
import { useMemo, useState } from "react";
import { Hash, CornerDownRight, Star, Settings2, X, Search, Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  channelListOptions,
  channelRosterListOptions,
  useCreateChannel,
} from "@multica/core/channels";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { inboxListOptions } from "@multica/core/inbox/queries";
import {
  memberListOptions,
  agentListOptions,
} from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import {
  buildUnreadChannelThreads,
  channelChatUnreadBadge,
  conversationKindOfChannel,
  useFeatureFlag,
} from "@multica/cerebro-feature-flags";
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
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { canAssignAgent } from "@multica/views/issues/components";
import type { Channel } from "@multica/core/types";
import { useMemberPresence } from "../hooks/use-member-presence";
import { useChannelTyping } from "../hooks/use-channel-typing";
import {
  applyChatRailLimit,
  partitionChatRail,
  railLimitKind,
} from "../partition-chat-rail";

export type TeamSort = "name" | "recent";
export type TeamGroupBy = "type" | "none";

export interface SlackBlockProps {
  wsId: string;
  selectedChannelId: string | null;
  /** FIR-4649 — when a thread row is open, its root id (null otherwise). */
  selectedThreadRootId?: string | null;
  onOpenChannel: (channel: Channel) => void;
  /** FIR-4649 — open an unread channel/DM thread in the detail pane. */
  onOpenThread?: (args: {
    channel: Channel;
    threadRootId: string;
    commentId?: string;
  }) => void;
  /** Rows to show. Per kind when grouped by type, a shared total when flat.
   *  0 = all. Default 10. */
  limit?: number;
  onSetLimit: (n: number) => void;
  /** Sort order across all kinds. Starred float to the top. Default "recent". */
  sort?: TeamSort;
  onSetSort: (s: TeamSort) => void;
  /** Keep unread conversations first inside the chosen grouping. Default true. */
  unreadFirst?: boolean;
  onSetUnreadFirst: (v: boolean) => void;
  /** Group the list by kind, or show one flat list. Default "type". */
  groupBy?: TeamGroupBy;
  onSetGroupBy: (g: TeamGroupBy) => void;
  /** TECH-3494 — also list the workspace's agents (opt-in). Default off. */
  showAgents?: boolean;
  /** FIR-4350 — list channels. Default on; the Chat page turns it off when the
   *  user has not placed channels there. */
  showChannels?: boolean;
  /** FIR-4350 — list people (DMs). Default on; see showChannels. */
  showPeople?: boolean;
  onSetShowAgents: (v: boolean) => void;
  /** TECH-3769 — show the search field by default instead of behind the header
   *  search button. Default off (search revealed via the button). */
  searchDefaultOpen?: boolean;
  onSetSearchDefaultOpen: (v: boolean) => void;
  /** FIR-4350 — show the display-settings dropdown (Sort / Group / Show /
   *  Search). On for both the dynamic inbox and the Chat page. */
  showSectionControls?: boolean;
  /** FIR-4350 — show the Remove-block "X". Default on for the dynamic inbox; the
   *  Chat page turns it off — it is a surface, not a removable block. */
  showRemove?: boolean;
  /** FIR-4350 — show the "Show agents" item in the settings dropdown. Default
   *  on; the Chat page turns it off because agents are inbox-only there. */
  showAgentsToggle?: boolean;
  /** FIR-4350 — render as a full-height page column instead of an inbox card:
   *  no rounded border, no drag-handle inset on the header, and the header stays
   *  pinned while the list scrolls. Default off (the dynamic inbox card). */
  flush?: boolean;
  /** FIR-4350 — when set, render a "+" button in the header that opens the
   *  new-conversation flow. The Chat page passes it; the inbox does not (it has
   *  the sidebar "New message" button). */
  onCreate?: () => void;
  /** FIR-4350 — when set, render a settings gear in the header that calls it.
   *  The Chat page passes it to open the placement matrix on the page instead
   *  of sending the user to the global Settings tab. */
  onOpenSettings?: () => void;
  /** Opens an agent chat in the parent's detail panel (no DM channel). */
  onOpenAgentChat: (agentId: string) => void;
  /** TECH-3664 — opens an EXISTING (unread) agent chat session so clicking an
   *  agent that shows "New" lands on the unread conversation instead of a blank
   *  new chat. Falls back to onOpenAgentChat when absent or no unread session. */
  onOpenAgentSession?: (sessionId: string) => void;
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
  { label: "By type", value: "type" },
  { label: "Flat list", value: "none" },
];

type ChatItem = {
  key: string;
  kind: "channel" | "person" | "agent" | "thread";
  name: string;
  recency: number;
  unread: number;
  starred: boolean;
  /** Cap bucket — threads count toward their parent channel/DM kind. */
  limitKind: "channel" | "person" | "agent";
  favKey?: FavoriteKey;
  channel?: Channel;
  userId?: string;
  agentId?: string;
  online?: boolean;
  /** TECH-3664 — id of the agent's most-recent unread chat session, if any.
   *  Set so clicking an unread agent row opens that session. */
  sessionId?: string;
  /** FIR-4649 — unread thread row fields. */
  threadRootId?: string;
  commentId?: string;
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
  selectedThreadRootId = null,
  onOpenChannel,
  onOpenThread,
  limit,
  onSetLimit,
  sort = "recent",
  onSetSort,
  unreadFirst = true,
  onSetUnreadFirst,
  groupBy = "type",
  onSetGroupBy,
  showAgents = false,
  showChannels = true,
  showPeople = true,
  onSetShowAgents,
  searchDefaultOpen = false,
  onSetSearchDefaultOpen,
  showSectionControls = true,
  showRemove = true,
  showAgentsToggle = true,
  flush = false,
  onCreate,
  onOpenSettings,
  onOpenAgentChat,
  onOpenAgentSession,
  onRemove,
}: SlackBlockProps) {
  const selfUserId = useAuthStore((s) => s.user?.id);
  const threadSplitEnabled = useFeatureFlag("cerebro_inbox_thread_split");
  // `channels` (non-archived) backs DM↔member matching so inbox-archived DMs
  // stay hidden from People. `rosterChannels` (include-archived) backs the
  // Channels group so a named channel persists in the chat roster even after
  // it is archived in the inbox — TECH-3758.
  const { data: channels = [] } = useQuery(channelListOptions(wsId));
  const { data: rosterChannels = [] } = useQuery(channelRosterListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  // FIR-4649 — same inbox feed the dynamic inbox uses to split unread threads.
  const { data: rawInbox = [] } = useQuery({
    ...inboxListOptions(wsId),
    enabled: !!wsId && threadSplitEnabled,
  });
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
  // Agent presence (online/offline dot), keyed by agent id. Only paid for
  // when the user opts into the agents list — pass undefined otherwise so the
  // runtime/snapshot queries stay idle. `availability === "online"` is the
  // single green-dot signal; every other state (offline/unstable/paused) reads
  // as the red offline dot, matching the binary member dot in this block.
  const { byAgent: agentPresence } = useWorkspacePresenceMap(
    showAgents ? wsId : undefined,
  );
  // Watch typing for the currently selected conversation so a member's DM row
  // can surface "typing…" when they type.
  const { typingUserIds } = useChannelTyping(selectedChannelId);
  // TECH-3769 — the search field starts visible when the box is set to show it
  // by default; otherwise it stays behind the header search button.
  const [searchOpen, setSearchOpen] = useState(searchDefaultOpen);
  const [search, setSearch] = useState("");

  // TECH-3769 — flipping "Search shown by default" persists the choice and
  // reflects it immediately: turning it on reveals the field, off hides + clears.
  const toggleSearchDefault = () => {
    const next = !searchDefaultOpen;
    onSetSearchDefaultOpen(next);
    setSearchOpen(next);
    if (!next) setSearch("");
  };

  const createChannel = useCreateChannel();

  const favorites = useChannelFavoritesStore((s) => s.favorites);
  const toggleFavorite = useChannelFavoritesStore((s) => s.toggle);

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === selfUserId)?.role,
    [members, selfUserId],
  );

  // Most-recent agent chat activity, keyed by agent id. TECH-3664 — also track
  // the id of the most-recent UNREAD session so clicking an agent that shows
  // "New" opens that conversation instead of a blank new chat.
  const agentActivity = useMemo(() => {
    const map = new Map<
      string,
      { recency: number; unread: boolean; unreadSessionId?: string; unreadRecency: number }
    >();
    for (const s of chatSessions) {
      if (s.status === "archived") continue;
      const recency = Date.parse(s.updated_at) || 0;
      const prev = map.get(s.agent_id);
      const next = {
        recency: Math.max(prev?.recency ?? 0, recency),
        unread: (prev?.unread ?? false) || s.has_unread,
        unreadSessionId: prev?.unreadSessionId,
        unreadRecency: prev?.unreadRecency ?? -1,
      };
      if (s.has_unread && recency >= next.unreadRecency) {
        next.unreadSessionId = s.id;
        next.unreadRecency = recency;
      }
      map.set(s.agent_id, next);
    }
    return map;
  }, [chatSessions]);

  const normalizedSearch = search.trim().toLowerCase();

  // One unified list of every conversation the block can show.
  const allItems = useMemo<ChatItem[]>(() => {
    const items: ChatItem[] = [];

    for (const c of rosterChannels) {
      if (c.kind !== "channel" && c.kind !== "group") continue;
      // FIR-4350 — one shared rule with the Inbox's hide logic: a channel is
      // gated by the channel placement, a group by the DM placement, exactly as
      // conversationKindOfChannel maps them for inbox hiding. Without listing
      // groups here a group could leave the Inbox (DM off) yet appear on neither
      // surface while its unread still counts.
      const show =
        conversationKindOfChannel(c.kind) === "channel" ? showChannels : showPeople;
      if (!show) continue;
      items.push({
        key: `channel:${c.id}`,
        kind: "channel",
        name: c.title || "Group",
        recency: channelRecency(c),
        // FIR-4649 — smart-unread activity (has_unread_activity) counts too.
        unread: channelChatUnreadBadge(c),
        starred: favorites.includes(channelKey(c.id)),
        limitKind: "channel",
        favKey: channelKey(c.id),
        channel: c,
      });
    }

    for (const m of members) {
      if (!showPeople) break;
      if (m.user_id === selfUserId) continue;
      const dm = dmWithMember(channels, selfUserId, m.user_id);
      items.push({
        key: `person:${m.user_id}`,
        kind: "person",
        name: m.name,
        recency: channelRecency(dm),
        unread: dm ? channelChatUnreadBadge(dm) : 0,
        starred: favorites.includes(actorKey("member", m.user_id)),
        limitKind: "person",
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
          limitKind: "agent",
          agentId: a.id,
          sessionId: act?.unreadSessionId,
          online: agentPresence.get(a.id)?.availability === "online",
        });
      }
    }

    // FIR-4649 — unread channel/DM threads as their own rows (parity with
    // dynamic inbox FIR-1854), so a reply is not invisible when the channel
    // lives only on the Chat page.
    if (threadSplitEnabled && onOpenThread) {
      const channelMap = new Map<string, Channel>();
      for (const c of rosterChannels) channelMap.set(c.id, c);
      for (const c of channels) channelMap.set(c.id, c);
      for (const t of buildUnreadChannelThreads(rawInbox, channelMap)) {
        const show =
          conversationKindOfChannel(t.channel.kind) === "channel"
            ? showChannels
            : showPeople;
        if (!show) continue;
        const parentTitle = t.channel.title?.trim() || (t.channel.kind === "dm" ? "DM" : "Channel");
        const preview = t.item.details?.thread_root_preview?.trim();
        items.push({
          key: `thread:${t.threadRootId}`,
          kind: "thread",
          name: preview ? `${parentTitle}: ${preview}` : `${parentTitle} thread`,
          recency: t.time,
          unread: t.unreadCount,
          starred: favorites.includes(channelKey(t.channelId)),
          limitKind: railLimitKind("thread", t.channel.kind),
          favKey: channelKey(t.channelId),
          channel: t.channel,
          threadRootId: t.threadRootId,
          commentId: t.item.details?.comment_id,
        });
      }
    }

    return items;
  }, [
    channels,
    rosterChannels,
    members,
    agents,
    showAgents,
    showChannels,
    showPeople,
    agentActivity,
    agentPresence,
    favorites,
    selfUserId,
    memberRole,
    onlineUserIds,
    threadSplitEnabled,
    onOpenThread,
    rawInbox,
  ]);

  // Search → sort (unread first when on, then starred, then chosen order) →
  // cap. FIR-4649 — unreads sort ahead of Favorites so the shared limit never
  // drops an unread behind a full starred/read quota (see applyChatRailLimit).
  const shownItems = useMemo(() => {
    const filtered = normalizedSearch
      ? allItems.filter((it) => it.name.toLowerCase().includes(normalizedSearch))
      : allItems;
    const sorted = filtered.slice().sort((a, b) => {
      if (unreadFirst && (a.unread > 0) !== (b.unread > 0)) {
        return a.unread > 0 ? -1 : 1;
      }
      if (a.starred !== b.starred) return a.starred ? -1 : 1;
      if (sort === "recent") {
        const d = b.recency - a.recency;
        if (d !== 0) return d;
      }
      return a.name.localeCompare(b.name);
    });
    const effective = limit == null ? DEFAULT_LIMIT : limit;
    return applyChatRailLimit(sorted, effective, groupBy, unreadFirst);
  }, [allItems, normalizedSearch, unreadFirst, sort, limit, groupBy]);

  const onlineCount = useMemo(
    () =>
      members.filter(
        (m) => m.user_id !== selfUserId && onlineUserIds.has(m.user_id),
      ).length,
    [members, selfUserId, onlineUserIds],
  );

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
    if (it.kind === "agent" && it.agentId) {
      // TECH-3664 — an agent with an unread session opens that conversation;
      // otherwise the row starts a fresh chat as before.
      if (it.sessionId && onOpenAgentSession) onOpenAgentSession(it.sessionId);
      else onOpenAgentChat(it.agentId);
    } else if (it.kind === "thread" && it.channel && it.threadRootId && onOpenThread) {
      onOpenThread({
        channel: it.channel,
        threadRootId: it.threadRootId,
        commentId: it.commentId,
      });
    } else if (it.kind === "channel" && it.channel) onOpenChannel(it.channel);
    else if (it.kind === "person" && it.userId) void openMember(it.userId);
  };

  const renderRow = (it: ChatItem) => {
    const isSelected =
      it.kind === "thread"
        ? it.threadRootId != null && it.threadRootId === selectedThreadRootId
        : it.channel != null &&
          it.channel.id === selectedChannelId &&
          !selectedThreadRootId;
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
            ) : it.kind === "thread" ? (
              <span className="flex size-6 items-center justify-center text-muted-foreground">
                <CornerDownRight className="size-4" />
              </span>
            ) : it.kind === "agent" ? (
              <ActorAvatar actorType="agent" actorId={it.agentId!} size={24} />
            ) : (
              <ActorAvatar actorType="member" actorId={it.userId!} size={24} />
            )}
            {(it.kind === "person" || it.kind === "agent") && (
              <span
                data-testid={`presence-dot-${it.userId ?? it.agentId}`}
                data-online={it.online ? "true" : "false"}
                className={`absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full border border-muted-foreground ${
                  it.online ? "bg-success" : "bg-background"
                }`}
                aria-label={it.online ? "online" : "offline"}
              />
            )}
          </span>
          <span className="flex min-w-0 flex-1 items-center gap-1.5">
            <span
              className={`truncate ${it.unread > 0 ? "font-semibold text-foreground" : "text-foreground"}`}
            >
              {it.name}
            </span>
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
            // FIR-4350 — a touch screen has no hover, so an unstarred row's
            // star was invisible and favouriting was unreachable on a phone.
            // Always visible below sm; hover-revealed from sm up as before.
            className={`shrink-0 rounded p-0.5 transition-opacity ${
              it.starred
                ? "text-warning opacity-100"
                : "text-muted-foreground opacity-100 hover:text-foreground sm:opacity-0 sm:group-hover:opacity-100"
            }`}
          >
            <Star className="size-3.5" fill={it.starred ? "currentColor" : "none"} />
          </button>
        )}
      </div>
    );
  };

  // FIR-4649 — Slack order: Unread (every type) → Favorites (starred+read) →
  // the rest grouped/flat. Same unread signal the Chat badge uses.
  const {
    unread: unreadItems,
    favorites: favoriteItems,
    rest: restItems,
  } = partitionChatRail(shownItems, unreadFirst);

  let groups: Array<{ label: string; items: ChatItem[] }>;
  if (groupBy === "type") {
    groups = [
      {
        label: "Channels",
        items: restItems.filter(
          (i) =>
            i.kind === "channel" ||
            (i.kind === "thread" && i.channel?.kind === "channel"),
        ),
      },
      {
        label: "People",
        items: restItems.filter(
          (i) =>
            i.kind === "person" ||
            (i.kind === "thread" &&
              (i.channel?.kind === "dm" || i.channel?.kind === "group")),
        ),
      },
      { label: "Agents", items: restItems.filter((i) => i.kind === "agent") },
    ].filter((g) => g.items.length > 0);
  } else {
    groups = restItems.length > 0 ? [{ label: "", items: restItems }] : [];
  }

  return (
    <section
      data-testid="slack-block"
      className={`bg-card text-sm ${
        flush
          ? "flex h-full min-h-0 flex-col"
          : "overflow-hidden rounded-xl border border-border"
      }`}
    >
      {/* Control box — matches the other dynamic-inbox sections. TECH-3494:
          pl-8 clears the drag handle the parent overlays at left-2. FIR-4350:
          the flush (page) variant has no drag handle, so that 32px of dead left
          padding would just push the header out of line with the rows below. */}
      <header
        className={`flex items-center gap-2 border-b border-border py-2 pr-3 ${
          flush ? "shrink-0 pl-3" : "pl-8"
        }`}
      >
        <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          Chat
        </span>
        <span className="text-xs text-muted-foreground">{onlineCount} online</span>
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          {onCreate && (
            <button
              type="button"
              className="rounded p-1 hover:bg-muted"
              onClick={onCreate}
              aria-label="New conversation"
              title="New conversation"
            >
              <Plus className="size-3.5" />
            </button>
          )}
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
          {showSectionControls && (
          <>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="rounded p-1 hover:bg-muted"
                  aria-label="Display settings"
                  title="Display settings"
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
                <DropdownMenuItem onClick={() => onSetUnreadFirst(!unreadFirst)}>
                  Unread first {unreadFirst ? "✓" : ""}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
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
                {showAgentsToggle && (
                  <DropdownMenuItem onClick={() => onSetShowAgents(!showAgents)}>
                    Show agents {showAgents ? "✓" : ""}
                  </DropdownMenuItem>
                )}
                {/* TECH-3769 — choose whether the search field is shown by
                    default or revealed via the header search button. */}
                <DropdownMenuItem onClick={toggleSearchDefault}>
                  Search shown by default {searchDefaultOpen ? "✓" : ""}
                </DropdownMenuItem>
              </DropdownMenuGroup>
              {/* FIR-4350 — on the Chat page the placement matrix lives inside
                  this one gear instead of a second, identical gear button. */}
              {onOpenSettings && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onClick={onOpenSettings}>
                    Chat placement…
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          {showRemove && (
            <button
              type="button"
              className="rounded p-1 hover:bg-muted"
              onClick={onRemove}
              title="Remove block"
            >
              <X className="size-3.5" />
            </button>
          )}
          </>
          )}
          {/* The standalone placement gear is only needed when the settings
              dropdown is hidden; otherwise placement lives inside it. */}
          {onOpenSettings && !showSectionControls && (
            <button
              type="button"
              className="rounded p-1 hover:bg-muted"
              onClick={onOpenSettings}
              aria-label="Chat settings"
              title="Chat settings"
            >
              <Settings2 className="size-3.5" />
            </button>
          )}
        </div>
      </header>

      <div
        className={`flex flex-col gap-3 p-3 ${
          flush ? "min-h-0 flex-1 overflow-y-auto" : ""
        }`}
      >
        {searchOpen && (
          // TECH-3769 — same design as the "All messages" box search bar.
          <div className="mx-1 flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-2.5 py-1.5">
            <Search className="size-3.5 flex-none text-muted-foreground" />
            <input
              type="search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              autoComplete="off"
              autoCorrect="off"
              spellCheck={false}
              role="searchbox"
              placeholder="Search in list..."
              aria-label="Search in list"
              className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
            {search && (
              <button
                type="button"
                className="flex-none rounded p-0.5 text-muted-foreground hover:bg-muted"
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
            {/* FIR-4649 — Unread first (Slack): every unread conversation of
                any type, above Favorites and the type groups. */}
            {unreadItems.length > 0 && (
              <div className="flex flex-col gap-1" data-testid="unread-group">
                <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Unread
                </h3>
                {unreadItems.map(renderRow)}
                {(favoriteItems.length > 0 || groups.length > 0) && (
                  <hr
                    data-testid="unread-divider"
                    className="mt-2 border-t border-border"
                  />
                )}
              </div>
            )}
            {/* FIR-4350 / FIR-4649 — Favorites = starred that are already read;
                starred+unread live in the Unread block above. */}
            {favoriteItems.length > 0 && (
              <div className="flex flex-col gap-1" data-testid="favorites-group">
                <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Favorites
                </h3>
                {favoriteItems.map(renderRow)}
                {groups.length > 0 && (
                  <hr className="mt-2 border-t border-border" />
                )}
              </div>
            )}
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
