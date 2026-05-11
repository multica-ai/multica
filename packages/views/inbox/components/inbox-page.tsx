"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import {
  inboxListOptions,
  inboxArchivedListOptions,
  activeIssueTasksOptions,
  deduplicateInboxItems,
  useInboxUnreadCount,
} from "@multica/core/inbox/queries";
import {
  useMarkInboxRead,
  useArchiveInbox,
  useMarkAllInboxRead,
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
} from "@multica/core/inbox/mutations";
// CEREBRO-PATCH(channels-flag-gate): hide channel/dm view options + new-message when feature is disabled
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
// CEREBRO-PATCH(inbox-keyboard-shortcuts): cerebro keyboard shortcuts (e = archive)
import { useInboxKeyboardShortcuts, CerebroSwipeArchive } from "@multica/cerebro-inbox";
// CEREBRO-PATCH(inbox-channel-archive-import): JEH-851 — per-user channel archive mutation.
import { useArchiveChannel } from "@multica/cerebro-channels";
import { useInboxViewStore, INBOX_VIEW_OPTIONS, type InboxView } from "@multica/core/inbox";
import { channelListOptions } from "@multica/core/channels";
import { useChatStore } from "@multica/core/chat";
import { ChannelDetail, NewMessageModal } from "../../channels";
import { IssueDetail } from "../../issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useNavigation } from "../../navigation";
import { SidebarTrigger } from "@multica/ui/components/ui/sidebar";
import { toast } from "sonner";
import {
  MoreHorizontal,
  Inbox,
  CheckCheck,
  Archive,
  BookCheck,
  ListChecks,
  ArrowLeft,
  Plus,
  Bot,
  ChevronRight,
  ChevronDown,
} from "lucide-react";
import type { Channel, InboxItem, InboxItemType } from "@multica/core/types";
import {
  chatSessionsOptions,
  archivedChatSessionsOptions,
  allChatSessionsOptions,
  pendingChatTasksOptions,
} from "@multica/core/chat/queries";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { projectListOptions } from "@multica/core/projects";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { PageHeader } from "../../layout/page-header";
import { ChannelListItem, InboxListItem, useTimeAgo } from "./inbox-list-item";
import { useTypeLabels } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";
import { useT } from "../../i18n";
import { InboxChatPanel } from "./inbox-chat-panel";

type ViewMode = { kind: "inbox" } | { kind: "archived" };

type GroupByMode = "none" | "project" | "agent" | "type";

const GROUP_BY_OPTIONS: { value: GroupByMode; label: string }[] = [
  { value: "none", label: "None" },
  { value: "project", label: "Project" },
  { value: "agent", label: "Agent" },
  { value: "type", label: "Type" },
];

function isGroupByMode(s: string | null): s is GroupByMode {
  return s === "none" || s === "project" || s === "agent" || s === "type";
}

export function InboxPage() {
  const { t } = useT("inbox");
  const { searchParams, replace } = useNavigation();
  // ?chat=<id> selects a chat session (preferred). ?issue=<id> selects a
  // notification or issue. Both are also accepted as legacy aliases for
  // each other so old bookmarks of `?issue=<chat-id>` keep landing on the
  // chat — the lookup further down decides which one matches.
  const urlChat = searchParams.get("chat") ?? "";
  const urlIssue = searchParams.get("issue") ?? "";
  const urlSelected = urlChat || urlIssue;
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlSelected);
  const [viewMode, setViewMode] = useState<ViewMode>({ kind: "inbox" });

  useEffect(() => {
    setSelectedKeyState(urlSelected);
  }, [urlSelected]);

  const wsId = useWorkspaceId();
  const isArchivedView = viewMode.kind === "archived";

  const inboxDefaultQuery = useQuery({
    ...inboxListOptions(wsId),
    enabled: !isArchivedView,
  });
  const inboxArchivedQuery = useQuery({
    ...inboxArchivedListOptions(wsId),
    enabled: isArchivedView,
  });
  const rawItems = isArchivedView
    ? inboxArchivedQuery.data ?? []
    : inboxDefaultQuery.data ?? [];
  const loading = isArchivedView
    ? inboxArchivedQuery.isLoading
    : inboxDefaultQuery.isLoading;
  const items = useMemo(
    () => (isArchivedView ? rawItems : deduplicateInboxItems(rawItems)),
    [rawItems, isArchivedView],
  );

  const chatDefaultQuery = useQuery({
    ...chatSessionsOptions(wsId),
    enabled: !isArchivedView,
  });
  const chatArchivedQuery = useQuery({
    ...archivedChatSessionsOptions(wsId),
    enabled: isArchivedView,
  });
  const chatSessions = isArchivedView
    ? chatArchivedQuery.data ?? []
    : chatDefaultQuery.data ?? [];

  // Workspace-wide "agent is working" signals: which issues and chat sessions
  // currently have an in-flight task. Used to render the small live indicator
  // on each row in the inbox list.
  const { data: activeIssueTasksData } = useQuery(activeIssueTasksOptions(wsId));
  const activeIssueIds = useMemo(
    () => new Set(activeIssueTasksData?.issue_ids ?? []),
    [activeIssueTasksData],
  );
  const { data: pendingChatTasksData } = useQuery(pendingChatTasksOptions(wsId));
  const activeChatSessionIds = useMemo(
    () => new Set((pendingChatTasksData?.tasks ?? []).map((t) => t.chat_session_id)),
    [pendingChatTasksData],
  );

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agentMap = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const projectMap = useMemo(() => new Map(projects.map((p) => [p.id, p])), [projects]);

  const userId = useAuthStore((s) => s.user?.id ?? "");
  const { getActorName } = useActorName();
  const groupByStorageKey = `multica_inbox_groupby_${wsId}_${userId}`;
  const [groupBy, setGroupBy] = useState<{ primary: GroupByMode; secondary: GroupByMode }>(() => {
    if (typeof window === "undefined") return { primary: "none", secondary: "none" };
    const saved = window.localStorage.getItem(groupByStorageKey);
    if (!saved) return { primary: "none", secondary: "none" };
    const [p, s] = saved.split(",");
    return {
      primary: isGroupByMode(p ?? null) ? (p as GroupByMode) : "none",
      secondary: isGroupByMode(s ?? null) ? (s as GroupByMode) : "none",
    };
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(groupByStorageKey, `${groupBy.primary},${groupBy.secondary}`);
  }, [groupBy, groupByStorageKey]);
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});
  const toggleGroup = useCallback((key: string) => {
    setCollapsedGroups((c) => ({ ...c, [key]: !c[key] }));
  }, []);

  // All sessions (active + archived). Used purely for URL-driven lookup so a
  // pasted/bookmarked chat URL still resolves when the active list doesn't
  // contain that session — e.g. an archived chat opened via deep link.
  const { data: allSessions = [] } = useQuery(allChatSessionsOptions(wsId));

  // Channels (multi-party + DMs) — these are issues with kind != 'issue', so
  // any inbox row with the same issue_id is folded into the channel row.
  // CEREBRO-PATCH(channels-flag-gate): channelsEnabledHere skips the fetch when feature is disabled
  const channelsEnabledForFetch = useFeatureFlag("cerebro_channels");
  // CEREBRO-PATCH(inbox-page-channel-fallback): expose isLoading so the redirect effect can wait for channels
  const { data: channels = [], isLoading: channelsLoading } = useQuery({
    ...channelListOptions(wsId),
    enabled: channelsEnabledForFetch && !isArchivedView,
  });
  const channelMap = useMemo(
    () => new Map(channels.map((c) => [c.id, c])),
    [channels],
  );

  // Per-channel "@you was mentioned" flag — true when at least one unread
  // inbox item attached to the channel has type = 'mentioned'. The channel
  // row uses this to render the small "@ YOU" badge from the mockup.
  const mentionedChannels = useMemo(() => {
    const set = new Set<string>();
    for (const item of items) {
      if (item.read) continue;
      if (item.type !== "mentioned") continue;
      if (!item.issue_id) continue;
      if (!channelMap.has(item.issue_id)) continue;
      set.add(item.issue_id);
    }
    return set;
  }, [items, channelMap]);

  const view = useInboxViewStore((s) => s.view);
  const setView = useInboxViewStore((s) => s.setView);
  const [showNewMessage, setShowNewMessage] = useState(false);

  // CEREBRO-PATCH(channels-flag-gate): hide channel/dm view options when disabled,
  // and snap any persisted "channels"/"dms" view back to "all" when the user toggles off.
  const channelsEnabled = useFeatureFlag("cerebro_channels");
  const inboxViewOptions = useMemo(
    () =>
      channelsEnabled
        ? INBOX_VIEW_OPTIONS
        : INBOX_VIEW_OPTIONS.filter((o) => o.value !== "channels" && o.value !== "dms"),
    [channelsEnabled],
  );
  useEffect(() => {
    if (!channelsEnabled && (view === "channels" || view === "dms")) {
      setView("all");
    }
  }, [channelsEnabled, view, setView]);

  const selectedChatSession =
    chatSessions.find((s) => s.id === selectedKey) ??
    allSessions.find((s) => s.id === selectedKey) ??
    null;
  const selected = selectedChatSession
    ? null
    : items.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;
  const qc = useQueryClient();

  const pendingChatIdRef = useRef<string | null>(null);

  // CEREBRO-PATCH(inbox-page-resolved-tracking): Track the last key we actually resolved against the inbox list. Lets the
  // fallback effect distinguish "shared-link to a notification not in our
  // inbox" (never resolved → redirect to the issue page) from "item was in
  // our inbox and just got removed" (was resolved → stay on /inbox).
  const lastResolvedKeyRef = useRef<string>("");
  useEffect(() => {
    if (selected) lastResolvedKeyRef.current = selectedKey;
  }, [selected, selectedKey]);

  useEffect(() => {
    if (pendingChatIdRef.current && chatSessions.some((s) => s.id === pendingChatIdRef.current)) {
      pendingChatIdRef.current = null;
    }
  }, [chatSessions]);

  // `kind` decides which query param the URL uses: `?chat=` for chat
  // sessions (incl. the new-chat sentinel), `?issue=` for inbox items.
  // Picking the right one is what makes shared URLs land on the right
  // panel without the inbox redirecting chats into "issue does not exist".
  const setSelectedKey = useCallback((kind: "chat" | "issue" | null, key: string) => {
    setSelectedKeyState(key);
    const inboxPath = wsPaths.inbox();
    const url = !key
      ? inboxPath
      : kind === "chat"
        ? `${inboxPath}?chat=${key}`
        : `${inboxPath}?issue=${key}`;
    replace(url);
  }, [replace, wsPaths]);

  // CEREBRO-PATCH(inbox-page-archive-chat): chat-session archive helper
  // Upstream replaced archiveChatSession with deleteChatSession (#2115); preserve the
  // cerebro "archive from inbox" UX by hard-deleting instead.
  const handleArchiveChat = useCallback(async (sessionId: string) => {
    if (sessionId === selectedKey) setSelectedKey(null, "");
    await api.deleteChatSession(sessionId);
    qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
  }, [selectedKey, setSelectedKey, wsId, qc]);

  // Shared inbox links (?issue=<id>) may point to notifications not in this
  // user's inbox (archived, or never received). Fall back to the issue page
  // so the URL still resolves to something meaningful. But if the key was
  // previously resolvable (e.g. the issue was just deleted in another tab
  // and `onInboxIssueDeleted` pruned the cache), the issue detail would 404
  // too — clear the selection and stay on /inbox instead.
  useEffect(() => {
    if (loading) return;
    if (channelsLoading) return; // CEREBRO-PATCH(inbox-page-channel-fallback): wait for channels query before deciding to redirect
    if (!selectedKey) return;
    if (selected) return;
    if (channelMap.has(selectedKey)) return; // CEREBRO-PATCH(inbox-page-channel-fallback): channel/DM URLs render via ChannelDetail in-place
    // CEREBRO-PATCH(inbox-page-chat-fallback): don't redirect chat URLs to issue page
    if (selectedChatSession || selectedKey === "new-chat") return;
    if (pendingChatIdRef.current === selectedKey) return;
    if (urlChat && !urlIssue) return;
    if (lastResolvedKeyRef.current === selectedKey) {
      setSelectedKey(null, "");
      return;
    }
    replace(wsPaths.issueDetail(selectedKey));
  }, [loading, channelsLoading, selectedKey, selected, channelMap, selectedChatSession, replace, wsPaths, setSelectedKey, urlChat, urlIssue]);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_inbox_layout",
  });

  const isMobile = useIsMobile();
  const unreadCount = useInboxUnreadCount(wsId);

  const markReadMutation = useMarkInboxRead();
  const archiveMutation = useArchiveInbox();
  // CEREBRO-PATCH(inbox-channel-archive-mut): JEH-851 — per-user channel archive.
  const archiveChannelMutation = useArchiveChannel();
  const markAllReadMutation = useMarkAllInboxRead();
  const archiveAllMutation = useArchiveAllInbox();
  const archiveAllReadMutation = useArchiveAllReadInbox();
  const archiveCompletedMutation = useArchiveCompletedInbox();
  const timeAgo = useTimeAgo();
  const typeLabels = useTypeLabels();


  // Auto-mark-read whenever a selected item is unread — covers both click-
  // to-select and URL-param-select (e.g. OS notification click on desktop).
  // The mutation flips `read: true` optimistically, so this effect settles
  // in one pass and can't loop. Kept in a `useEffect` rather than inlined
  // in handleSelect so URL-driven selection triggers it too.
  const markReadMutate = markReadMutation.mutate;
  const selectedId = selected?.id;
  const selectedRead = selected?.read;
  useEffect(() => {
    if (!selectedId || selectedRead) return;
    markReadMutate(selectedId, {
      onError: () => toast.error(t(($) => $.errors.mark_read_failed)),
    });
  }, [selectedId, selectedRead, markReadMutate, t]);

  const handleSelect = (item: InboxItem) => {
    setSelectedKey("issue", item.issue_id ?? item.id);
  };

  const handleArchive = (id: string) => {
    const idx = items.findIndex((i) => i.id === id);
    const archived = idx >= 0 ? items[idx] : null;
    const wasSelected =
      !!archived && (archived.issue_id ?? archived.id) === selectedKey;
    if (wasSelected) {
      // List is sorted newest-first; prefer the next (older) item, fall back
      // to the previous (newer) one when archiving at the bottom, and only
      // clear the selection when nothing else is left.
      const next = items[idx + 1] ?? items[idx - 1] ?? null;
      setSelectedKey(next ? "issue" : null, next ? (next.issue_id ?? next.id) : "");
    }
    archiveMutation.mutate(id, {
      onError: () => toast.error(t(($) => $.errors.archive_failed)),
    });
  };
  // CEREBRO-PATCH(inbox-keyboard-shortcuts): wires `e` = archive selection.
  useInboxKeyboardShortcuts({ selectedId: selected?.id ?? null, onArchive: handleArchive });

  const handleMarkAllRead = () => {
    markAllReadMutation.mutate(undefined, {
      onError: () => toast.error(t(($) => $.errors.mark_all_read_failed)),
    });
  };

  const handleArchiveAll = () => {
    setSelectedKey(null, "");
    archiveAllMutation.mutate(undefined, {
      onError: () => toast.error(t(($) => $.errors.archive_all_failed)),
    });
  };

  const handleArchiveAllRead = () => {
    const readKeys = items.filter((i) => i.read).map((i) => i.issue_id ?? i.id);
    if (readKeys.includes(selectedKey)) setSelectedKey(null, "");
    archiveAllReadMutation.mutate(undefined, {
      onError: () => toast.error(t(($) => $.errors.archive_all_read_failed)),
    });
  };

  const handleArchiveCompleted = () => {
    setSelectedKey(null, "");
    archiveCompletedMutation.mutate(undefined, {
      onError: () => toast.error(t(($) => $.errors.archive_completed_failed)),
    });
  };

  const handleNewChat = () => {
    setSelectedKey("chat", "new-chat");
  };

  // Cmd/Ctrl+Shift+M opens the new-message modal globally. Shift is added
  // because plain Cmd+M is the macOS minimize-window shortcut and Cmd+N
  // collides with new-window/new-doc bindings in the desktop app.
  // CEREBRO-PATCH(channels-flag-gate): no-op when channels are disabled.
  useEffect(() => {
    if (!channelsEnabled) return;
    const handler = (e: KeyboardEvent) => {
      if (
        e.key.toLowerCase() === "m" &&
        e.shiftKey &&
        (e.metaKey || e.ctrlKey)
      ) {
        e.preventDefault();
        setShowNewMessage(true);
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [channelsEnabled]);

  const headerTitle = isArchivedView ? "Archived" : "Inbox";
  const showBack = isArchivedView;
  const showViewSwitch = !isArchivedView;
  const currentViewLabel =
    INBOX_VIEW_OPTIONS.find((o) => o.value === view)?.label ?? "All";

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex min-w-0 items-center gap-2">
        {showBack && (
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground"
            onClick={() => setViewMode({ kind: "inbox" })}
            title="Back to Inbox"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
        )}
        <h1 className="truncate text-sm font-semibold">{headerTitle}</h1>
        {showViewSwitch && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:bg-accent/60 hover:text-foreground"
                />
              }
            >
              {currentViewLabel}
              <ChevronDown className="size-3" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-auto">
              {inboxViewOptions.map((opt) => (
                <DropdownMenuItem
                  key={opt.value}
                  onClick={() => setView(opt.value as InboxView)}
                >
                  <span className="w-4 text-muted-foreground">
                    {view === opt.value ? "✓" : ""}
                  </span>
                  {opt.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        {showViewSwitch && unreadCount > 0 && (
          <span className="text-xs text-muted-foreground">{unreadCount}</span>
        )}
      </div>
      <div className="flex items-center gap-1">
        {/* CEREBRO-PATCH(inbox-plus-direct): JEH-718 — '+' opens the unified
         * actor picker directly. The picker dispatches single-agent taps to
         * the chat flow, removing the need for a "New message" / "Chat with
         * agent" dropdown. When channels are disabled the button falls back
         * to the legacy chat-with-agent path. */}
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          title={channelsEnabled ? "New message" : "Chat with agent"}
          onClick={() =>
            channelsEnabled ? setShowNewMessage(true) : handleNewChat()
          }
        >
          <Plus className="h-4 w-4" />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
              />
            }
          >
            <MoreHorizontal className="h-4 w-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem disabled>Group by</DropdownMenuItem>
            {GROUP_BY_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={`p-${opt.value}`}
                onClick={() =>
                  setGroupBy((g) => ({
                    primary: opt.value,
                    // Drop secondary when it would equal new primary or when primary is none.
                    secondary:
                      opt.value === "none" || g.secondary === opt.value ? "none" : g.secondary,
                  }))
                }
              >
                <span className="w-4 text-muted-foreground">
                  {groupBy.primary === opt.value ? "✓" : ""}
                </span>
                {opt.label}
              </DropdownMenuItem>
            ))}
            {groupBy.primary !== "none" && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled>Then by</DropdownMenuItem>
                {GROUP_BY_OPTIONS.filter((opt) => opt.value !== groupBy.primary).map((opt) => (
                  <DropdownMenuItem
                    key={`s-${opt.value}`}
                    onClick={() =>
                      setGroupBy((g) => ({ ...g, secondary: opt.value }))
                    }
                  >
                    <span className="w-4 text-muted-foreground">
                      {groupBy.secondary === opt.value ? "✓" : ""}
                    </span>
                    {opt.label}
                  </DropdownMenuItem>
                ))}
              </>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => {
                setSelectedKey(null, "");
                setViewMode(isArchivedView ? { kind: "inbox" } : { kind: "archived" });
              }}
            >
              <Archive className="h-4 w-4" />
              {isArchivedView ? "Show active" : "Show archived"}
            </DropdownMenuItem>
            {!isArchivedView && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleMarkAllRead}>
                  <CheckCheck className="h-4 w-4" />
                  Mark all as read
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleArchiveAll}>
                  <Archive className="h-4 w-4" />
                  Archive all
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleArchiveAllRead}>
                  <BookCheck className="h-4 w-4" />
                  Archive all read
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleArchiveCompleted}>
                  <ListChecks className="h-4 w-4" />
                  Archive completed
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </PageHeader>
  );

  type MergedEntry =
    | { kind: "chat"; id: string; time: number; session: typeof chatSessions[number] }
    | { kind: "notif"; id: string; time: number; item: InboxItem }
    | { kind: "channel"; id: string; time: number; channel: Channel };

  const mergedEntries = useMemo<MergedEntry[]>(() => {
    const entries: MergedEntry[] = [];
    // Channels first so we know which inbox items to swallow.
    for (const channel of channels) {
      entries.push({
        kind: "channel",
        id: channel.id,
        time: new Date(channel.updated_at).getTime(),
        channel,
      });
    }
    for (const session of chatSessions) {
      entries.push({
        kind: "chat",
        id: session.id,
        time: new Date(session.updated_at).getTime(),
        session,
      });
    }
    for (const item of items) {
      // Don't double-count: inbox notifications about a channel-issue's
      // comments are surfaced via the channel row + unread_count badge.
      if (item.issue_id && channelMap.has(item.issue_id)) continue;
      entries.push({
        kind: "notif",
        id: item.id,
        time: new Date(item.created_at).getTime(),
        item,
      });
    }
    entries.sort((a, b) => b.time - a.time);
    return entries;
  }, [chatSessions, items, channels, channelMap]);

  const filteredEntries = useMemo<MergedEntry[]>(
    () => mergedEntries.filter((entry) => matchesView(entry, view)),
    [mergedEntries, view],
  );

  const renderEntry = (entry: MergedEntry) => {
    if (entry.kind === "channel") {
      const channel = entry.channel;
      const lastMsg = channel.last_message;
      // "You" beats the user's full name when the latest message is from
      // them — Slack/iMessage style. Markdown lives in the snippet, but a
      // single-line preview only needs the first line, so we collapse
      // whitespace runs to keep the row tidy.
      const preview = lastMsg
        ? {
            author:
              lastMsg.author_type === "member" && lastMsg.author_id === userId
                ? "You"
                : getActorName(lastMsg.author_type, lastMsg.author_id),
            text: lastMsg.content.replace(/\s+/g, " ").trim(),
          }
        : null;
      return (
        <ChannelListItem
          key={`channel:${channel.id}`}
          channel={channel}
          mentioned={mentionedChannels.has(channel.id)}
          preview={preview}
          isSelected={channel.id === selectedKey}
          onClick={() => setSelectedKey("issue", channel.id)}
          onArchive={() => {
            // CEREBRO-PATCH(inbox-channel-archive): JEH-851 — channels/DMs
            // archive via per-user `cerebro_channel_archived` row, not via
            // inbox_item.archived. Also archive any open inbox notifications
            // for the channel so unread badges clear in the same gesture.
            if (channel.id === selectedKey) setSelectedKey(null, "");
            archiveChannelMutation.mutate(channel.id, {
              onError: () => toast.error(t(($) => $.errors.archive_failed)),
            });
            items
              .filter((i) => i.issue_id === channel.id && !i.archived)
              .forEach((i) => archiveMutation.mutate(i.id));
          }}
        />
      );
    }
    if (entry.kind === "chat") {
      const session = entry.session;
      const agent = agentMap.get(session.agent_id);
      const isAgentActive = activeChatSessionIds.has(session.id);
      return (
        <div
          key={`chat:${session.id}`}
          // CEREBRO-PATCH(inbox-chat-row-swipe): `relative` is required so the
          // cerebro swipe overlay (absolute inset-0) covers the row.
          className={`group/chat relative flex items-center gap-3 px-4 py-2.5 cursor-pointer transition-colors hover:bg-accent/50 ${
            session.id === selectedKey ? "bg-accent" : ""
          }`}
          onClick={() => setSelectedKey("chat", session.id)}
        >
          <Avatar className="size-7 shrink-0">
            {agent?.avatar_url && <AvatarImage src={agent.avatar_url} />}
            <AvatarFallback className="bg-purple-100 text-purple-700 text-xs">
              <Bot className="size-3.5" />
            </AvatarFallback>
          </Avatar>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-1.5">
              {session.has_unread && (
                <span className="size-1.5 shrink-0 rounded-full bg-brand" />
              )}
              <span
                className={`truncate text-sm ${session.has_unread ? "font-medium" : "text-muted-foreground"}`}
              >
                {session.title || agent?.name || "Chat"}
              </span>
            </div>
            <span className={`flex items-center gap-1.5 text-xs ${session.has_unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}>
              {isAgentActive && <AgentRunningPip />}
              {agent?.name} · {timeAgo(session.updated_at)}
            </span>
          </div>
          <button
            type="button"
            className="flex h-9 w-9 sm:h-6 sm:w-6 sm:hidden shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent sm:group-hover/chat:flex"
            onClick={(e) => {
              e.stopPropagation();
              handleArchiveChat(session.id);
            }}
            title="Archive"
            aria-label="Archive chat"
          >
            <Archive className="size-4 sm:size-3" />
          </button>
          {/* CEREBRO-PATCH(inbox-chat-row-swipe): swipe-right archives chat
              sessions, matching issue/channel rows. hideOnDesktop avoids
              stacking with the existing hover archive button above. */}
          <CerebroSwipeArchive
            hideOnDesktop
            onArchive={() => handleArchiveChat(session.id)}
          />
        </div>
      );
    }
    const item = entry.item;
    const isAgentActive = !!item.issue_id && activeIssueIds.has(item.issue_id);
    return (
      <InboxListItem
        key={`notif:${item.id}`}
        item={item}
        isSelected={(item.issue_id ?? item.id) === selectedKey}
        isAgentActive={isAgentActive}
        onClick={() => handleSelect(item)}
        onArchive={() => handleArchive(item.id)}
      />
    );
  };

  const bucketize = useCallback(
    (mode: GroupByMode, entry: MergedEntry): { key: string; label: string; isFallback: boolean } => {
      if (mode === "project") {
        if (entry.kind === "notif" && entry.item.project_id) {
          const proj = projectMap.get(entry.item.project_id);
          return {
            key: entry.item.project_id,
            label: proj?.title ?? "Unknown project",
            isFallback: false,
          };
        }
        if (entry.kind === "channel" && entry.channel.project_id) {
          const proj = projectMap.get(entry.channel.project_id);
          return {
            key: entry.channel.project_id,
            label: proj?.title ?? "Unknown project",
            isFallback: false,
          };
        }
        return { key: "__no_project__", label: "No project", isFallback: true };
      }
      if (mode === "agent") {
        if (entry.kind === "chat") {
          const agent = agentMap.get(entry.session.agent_id);
          return {
            key: entry.session.agent_id,
            label: agent?.name ?? "Unknown agent",
            isFallback: false,
          };
        }
        if (entry.kind === "notif") {
          if (entry.item.actor_type === "agent" && entry.item.actor_id) {
            const agent = agentMap.get(entry.item.actor_id);
            return {
              key: entry.item.actor_id,
              label: agent?.name ?? "Unknown agent",
              isFallback: false,
            };
          }
          return { key: "__no_agent__", label: "No agent", isFallback: true };
        }
        // Channels — bucket by the (single) agent participant if any.
        const agentParticipant = entry.channel.participants.find(
          (p) => p.user_type === "agent",
        );
        if (agentParticipant) {
          const agent = agentMap.get(agentParticipant.user_id);
          return {
            key: agentParticipant.user_id,
            label: agent?.name ?? "Unknown agent",
            isFallback: false,
          };
        }
        return { key: "__no_agent__", label: "No agent", isFallback: true };
      }
      if (mode === "type") {
        if (entry.kind === "chat") {
          return { key: "__chat__", label: "Chats", isFallback: true };
        }
        if (entry.kind === "channel") {
          return entry.channel.kind === "dm"
            ? { key: "__dm__", label: "Direct messages", isFallback: true }
            : { key: "__channel__", label: "Channels", isFallback: true };
        }
        const t = entry.item.type;
        return {
          key: t,
          label: typeLabels[t as InboxItemType] ?? t,
          isFallback: false,
        };
      }
      return { key: "__none__", label: "", isFallback: true };
    },
    [projectMap, agentMap],
  );

  type Group = {
    key: string;
    label: string;
    isFallback: boolean;
    entries: MergedEntry[];
    children?: Group[];
  };

  const sortGroups = (groups: Group[]) => {
    groups.sort((a, b) => {
      if (a.isFallback !== b.isFallback) return a.isFallback ? 1 : -1;
      return a.label.localeCompare(b.label);
    });
  };

  const groupedEntries = useMemo<Group[] | null>(() => {
    if (groupBy.primary === "none") return null;
    const primaryMap = new Map<string, Group>();
    for (const entry of filteredEntries) {
      const p = bucketize(groupBy.primary, entry);
      let pg = primaryMap.get(p.key);
      if (!pg) {
        pg = { ...p, entries: [], children: groupBy.secondary !== "none" ? [] : undefined };
        primaryMap.set(p.key, pg);
      }
      pg.entries.push(entry);
    }

    if (groupBy.secondary !== "none") {
      for (const pg of primaryMap.values()) {
        const subMap = new Map<string, Group>();
        for (const entry of pg.entries) {
          const s = bucketize(groupBy.secondary, entry);
          let sg = subMap.get(s.key);
          if (!sg) {
            sg = { ...s, entries: [] };
            subMap.set(s.key, sg);
          }
          sg.entries.push(entry);
        }
        const subGroups = Array.from(subMap.values());
        sortGroups(subGroups);
        pg.children = subGroups;
      }
    }

    const top = Array.from(primaryMap.values());
    sortGroups(top);
    return top;
  }, [filteredEntries, groupBy, bucketize]);

  const renderGroup = (
    group: Group,
    depth: number,
    parentKey: string,
  ): React.ReactNode => {
    const fullKey = `${parentKey}/${group.key}`;
    const collapsed = collapsedGroups[fullKey] ?? false;
    return (
      <div key={fullKey}>
        <button
          type="button"
          onClick={() => toggleGroup(fullKey)}
          className="flex w-full items-center gap-1.5 py-1.5 pr-4 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground hover:bg-accent/50"
          style={{ paddingLeft: 16 + depth * 12 }}
        >
          {collapsed ? (
            <ChevronRight className="size-3" />
          ) : (
            <ChevronDown className="size-3" />
          )}
          <span className="truncate">{group.label}</span>
          <span className="text-muted-foreground/60">{group.entries.length}</span>
        </button>
        {!collapsed && (
          group.children
            ? group.children.map((child) => renderGroup(child, depth + 1, fullKey))
            : group.entries.map(renderEntry)
        )}
      </div>
    );
  };

  const emptyMessage = isArchivedView ? "No archived items" : "No notifications";

  const listBody = (
    <div>
      {filteredEntries.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm">
            {mergedEntries.length === 0
              ? emptyMessage
              : `Nothing in ${INBOX_VIEW_OPTIONS.find((o) => o.value === view)?.label.toLowerCase()}`}
          </p>
        </div>
      ) : groupedEntries ? (
        groupedEntries.map((group) => renderGroup(group, 0, ""))
      ) : (
        filteredEntries.map(renderEntry)
      )}
    </div>
  );

  const selectedChannel = selectedKey ? channelMap.get(selectedKey) ?? null : null;

  // Key by issue_id (not inbox-item id): a new comment/reaction generates a
  // new inbox notification for the same issue, and the dedup helper picks the
  // newest one — keying on its id would remount IssueDetail on every event,
  // wiping the comment composer draft and resetting scroll position.
  const detailContent = selectedChannel ? (
    <ChannelDetail
      key={selectedChannel.id}
      channelId={selectedChannel.id}
      initialChannel={selectedChannel}
      onArchive={() => setSelectedKey(null, "")}
    />
  ) : selectedChatSession || selectedKey === "new-chat" ? (
    <InboxChatPanel
      key={selectedChatSession?.id ?? "new"}
      sessionId={selectedChatSession?.id ?? null}
      onSessionCreated={(id) => {
        pendingChatIdRef.current = id;
        setSelectedKey("chat", id);
      }}
    />
  ) : selected?.issue_id ? (
    <ErrorBoundary resetKeys={[selected.issue_id]}>
      <IssueDetail
        key={selected.issue_id}
        issueId={selected.issue_id}
        defaultSidebarOpen={false}
        layoutId="multica_inbox_issue_detail_layout"
        highlightCommentId={selected.details?.comment_id ?? undefined}
        linkSelfInBreadcrumb
        onDelete={() => {
          // Issue deletion CASCADE-deletes the inbox item server-side, and the
          // issue:deleted WS event prunes it from the inbox cache. Just clear
          // the selection — calling archive here would 404 on a row that no
          // longer exists.
          setSelectedKey(null, "");
        }}
        onDone={() => {
          handleArchive(selected.id);
        }}
      />
    </ErrorBoundary>
  ) : selected ? (
    <div className="p-3 sm:p-6">
      <h2 className="text-lg font-semibold">{getInboxDisplayTitle(selected)}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {typeLabels[selected.type]} · {timeAgo(selected.created_at)}
      </p>
      {selected.body && (
        <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
          {selected.body}
        </div>
      )}
      {selected.type === "quick_create_failed" && selected.details?.original_prompt && (
        <div className="mt-4 rounded-md border bg-muted/40 p-3">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.detail.original_input)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-sm">{selected.details.original_prompt}</p>
        </div>
      )}
      <div className="mt-4 flex gap-2">
        {selected.type === "quick_create_failed" && (
          <Button
            size="sm"
            onClick={() => {
              // Seed the legacy advanced form with the original prompt so the
              // user can recover their input in the full editor instead of
              // retyping. The agent picker hint becomes the assignee
              // candidate (still editable).
              const prompt = selected.details?.original_prompt ?? "";
              const agentId = selected.details?.agent_id;
              useIssueDraftStore.getState().setDraft({
                description: prompt,
                ...(agentId
                  ? { assigneeType: "agent" as const, assigneeId: agentId }
                  : {}),
              });
              useModalStore.getState().open("create-issue");
            }}
          >
            {t(($) => $.detail.edit_advanced)}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() => handleArchive(selected.id)}
        >
          <Archive className="mr-1.5 h-3.5 w-3.5" />
          {t(($) => $.detail.archive)}
        </Button>
      </div>
    </div>
  ) : null;

  if (isMobile) {
    if (loading) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-4">
            <Skeleton className="h-5 w-16" />
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-3/4" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        </div>
      );
    }

    if (detailContent) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-2 gap-1">
            <SidebarTrigger />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSelectedKey(null, "")}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              {t(($) => $.page.back)}
            </Button>
          </div>
          {/* CEREBRO-PATCH(inbox-mobile-detail-flex-height): IssueDetail/ChannelDetail/InboxChatPanel rely on h-full + flex-1 for their internal scroll regions, so the wrapper must be a flex column (not an overflow-y-auto block) or the body collapses to zero height (JEH-697). */}
          <div className="flex flex-1 flex-col min-h-0">
            {detailContent}
          </div>
        </div>
      );
    }

    return (
      <>
        <div className="flex flex-1 flex-col min-h-0">
          {listHeader}
          <div className="flex-1 min-h-0 overflow-y-auto">
            {listBody}
          </div>
        </div>
        <NewMessageModal
          open={showNewMessage}
          onClose={() => setShowNewMessage(false)}
          onCreated={(channel) => setSelectedKey("issue", channel.id)}
          onAgentChatStarted={(agentId) => {
            useChatStore.getState().setSelectedAgentId(agentId);
            setSelectedKey("chat", "new-chat");
          }}
        />
      </>
    );
  }

  if (loading) {
    return (
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
        <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
          <div className="flex flex-col border-r h-full">
            <div className="flex h-12 shrink-0 items-center border-b px-4">
              <Skeleton className="h-5 w-16" />
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                  <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-3 w-1/2" />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="p-6">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="mt-4 h-4 w-32" />
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    );
  }

  return (
    <>
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
        <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
          <div className="flex flex-col border-r h-full">
            {listHeader}
            <div className="flex-1 min-h-0 overflow-y-auto">
              {listBody}
            </div>
          </div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="flex flex-col min-h-0 h-full">
            {detailContent ?? (
              <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
                <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
                <p className="text-sm">
                  {items.length === 0 && channels.length === 0
                    ? "Your inbox is empty"
                    : "Select a conversation to view it"}
                </p>
              </div>
            )}
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
      {/* CEREBRO-PATCH(inbox-desktop-agent-chat-start): JEH-846 — mirror mobile onAgentChatStarted so agent taps don't fall through to a rejected one-agent DM. */}
      <NewMessageModal
        open={showNewMessage}
        onClose={() => setShowNewMessage(false)}
        onCreated={(channel) => setSelectedKey("issue", channel.id)}
        onAgentChatStarted={(agentId) => {
          useChatStore.getState().setSelectedAgentId(agentId);
          setSelectedKey("chat", "new-chat");
        }}
      />
    </>
  );
}

/**
 * Filter rule for the view-switch dropdown. Returns true when the entry
 * should be visible in the given view.
 *
 * - `unread`: anything with at least one unread signal — inbox dots, channel
 *   unread_count, chat has_unread.
 * - `mentioned`: only inbox items of type 'mentioned'.
 * - `issues|channels|dms`: by entry kind.
 */
function matchesView(
  entry:
    | { kind: "chat"; session: { has_unread?: boolean } }
    | { kind: "notif"; item: InboxItem }
    | { kind: "channel"; channel: Channel },
  view: InboxView,
): boolean {
  switch (view) {
    case "all":
      return true;
    case "unread":
      if (entry.kind === "chat") return !!entry.session.has_unread;
      if (entry.kind === "notif") return !entry.item.read;
      return entry.channel.unread_count > 0;
    case "mentioned":
      return entry.kind === "notif" && entry.item.type === "mentioned";
    case "issues":
      return entry.kind === "notif";
    case "channels":
      return entry.kind === "channel" && entry.channel.kind === "channel";
    case "dms":
      // Chat sessions are 1:1 agent chats — group them with DMs since
      // mentally the user's "DM" view is "every 1:1 thread I have".
      if (entry.kind === "chat") return true;
      return entry.kind === "channel" && entry.channel.kind === "dm";
  }
}

// -----------------------------------------------------------------------------
// Active agent indicator — tiny pulsing dot
// -----------------------------------------------------------------------------

function AgentRunningPip() {
  return (
    <span
      title="Agent is working"
      className="relative inline-flex size-2 shrink-0 items-center justify-center"
    >
      <span className="absolute inline-flex size-2 animate-ping rounded-full bg-emerald-500/60" />
      <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
    </span>
  );
}
