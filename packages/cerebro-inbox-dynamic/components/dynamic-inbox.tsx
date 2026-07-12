// TECH-3413 — the Dynamic inbox: one outer box holding tabs + stackable,
// configurable sections over the same data as the classic inbox. Light mode and
// rows match the classic inbox (reused row components). Switching back to the
// classic inbox lives in the ⋯ menu.
"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Plus, MoreHorizontal, LayoutList, Search, X, Inbox, GripVertical, ArrowLeft, Pencil, Save, Trash2, Archive } from "lucide-react";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useWorkspaceId } from "@multica/core/hooks";
import { useQueries } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores/create-mode-store";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuLabel,
  DropdownMenuGroup,
} from "@multica/ui/components/ui/dropdown-menu";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  useMarkInboxRead,
  useArchiveInbox,
  useMarkAllInboxRead,
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
} from "@multica/core/inbox/mutations";
import { listenForInboxDefaultTab } from "@multica/core/inbox";
import { useArchiveChannel } from "@multica/cerebro-channels";
import { useMarkThreadRead } from "../use-mark-thread-read";
import {
  GlobalInboxReminderDialog,
  formatPlannedDateTime,
  useInboxActionGroupLabels,
  useSelectedNotifChannel,
  useSetInboxMode,
} from "@multica/cerebro-inbox";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useModalStore } from "@multica/core/modals";
import { IssueDetail } from "@multica/views/issues/components";
import { ChannelDetail, NewMessageModal } from "@multica/views/channels";
import { InboxChatPanel } from "@multica/views/inbox/components/inbox-list-item";
import { useTypeLabels } from "@multica/views/inbox/components/inbox-detail-label";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { SlackBlock } from "@multica/cerebro-inbox-slack-block";
import type { Channel, InboxItem } from "@multica/core/types";
import { useDynamicInboxData } from "../use-dynamic-inbox-data";
import { useInboxScrollAnchor } from "../use-inbox-scroll-anchor";
import { INBOX_MESSAGE_PARAM, messageKeyForEntry, findEntryByMessageKey, findEntryByInboxIssueParam, nextInboxMessageUrl, noteMentionTarget, noteMentionUrl } from "../message-link";
import { useInboxLayout } from "../use-inbox-layout";
import {
  SECTION_CATALOG,
  makeId,
  type InboxLayout,
  type InboxSectionConfig,
  type InboxTabConfig,
  type SectionKind,
} from "../layout";
import type { DynInboxEntry } from "../section-filter";
import { DynamicInboxCreateMenu } from "./dynamic-inbox-create-menu";
import { DynamicInboxSection, runStateFor } from "./dynamic-inbox-section";
import { DynamicInboxRow } from "./dynamic-inbox-row";
import { ArchivedInboxView } from "./archived-inbox-view";
// FIR-1645 — Archived box: archived messages as a foldable block (its own
// archived queries), dispatched here like the Chat block.
import { ArchivedInboxBlock } from "./archived-inbox-block";
import { SecretarySection, secretaryEntryKey } from "./secretary-section";
// TECH-3421 — the Notes box renders its own data (recent notes), so it is
// dispatched here instead of going through DynamicInboxSection.
import { NotesInboxBox, NoteInboxBox, NoteInboxDetail } from "@multica/cerebro-notes/views";
import { SkillChangeInboxDetail } from "@multica/cerebro-skill-ownership/views";
import { opensInDynamicInboxPane } from "../notification-routing";
import { AddToRoundAction, ConnectedRoundManager, RoundsBlock, roundIssueIdsToExclude, useRoundStatuses, useStartRound } from "@multica/cerebro-rounds";

function replaceTab(layout: InboxLayout, tabId: string, fn: (t: InboxTabConfig) => InboxTabConfig): InboxLayout {
  return { ...layout, tabs: layout.tabs.map((t) => (t.id === tabId ? fn(t) : t)) };
}

// TECH-3413 #6 — stable identity of an entry so a frozen snapshot can be
// matched back to its live row after a refetch (ids survive read/state changes).
function entryIdentity(entry: DynInboxEntry): string {
  return `${entry.kind}:${entry.id}`;
}


// TECH-3413 #1 — wraps one section in a @dnd-kit sortable. The render prop
// receives the drag handle to place inside the section header so only the
// grip starts a drag (the rest of the box stays clickable).
function SortableSection({
  id,
  highlight,
  children,
}: {
  id: string;
  /** TECH-3502 #5 — flash a ring around a just-added box. */
  highlight?: boolean;
  children: (handle: React.ReactNode) => React.ReactNode;
}) {
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging } =
    useSortable({ id });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 20 : undefined,
    opacity: isDragging ? 0.85 : undefined,
  };
  const handle = (
    <button
      ref={setActivatorNodeRef}
      type="button"
      {...attributes}
      {...listeners}
      className="-ml-1 cursor-grab touch-none rounded p-0.5 text-muted-foreground/50 hover:bg-muted hover:text-muted-foreground active:cursor-grabbing"
      title="Drag to reorder"
      aria-label="Drag to reorder section"
    >
      <GripVertical className="size-3.5" />
    </button>
  );
  return (
    <div
      ref={setNodeRef}
      style={style}
      className={
        highlight
          ? "rounded-xl ring-2 ring-primary ring-offset-2 ring-offset-background transition-shadow"
          : "transition-shadow"
      }
    >
      {children(handle)}
    </div>
  );
}

export function DynamicInbox() {
  const wsId = useWorkspaceId();
  // TECH-3618 — read sidebar "New message" intent off the URL (see effect below).
  const { searchParams, replace, replaceSilent, push } = useNavigation();
  const paths = useWorkspacePaths();
  const notesEnabled = useFeatureFlag("cerebro_notes");
  // TECH-3690 — open notes in the inbox detail pane instead of navigating away.
  const noteInboxPaneEnabled = useFeatureFlag("cerebro_note_inbox_pane");
  const { layout, setLayout, userPresets, saveUserPreset, deleteUserPreset } = useInboxLayout();
  const { entries, items, filterContext, projects, projectMap, agentMap, toggleFavorite, knownChannelIds, loading } =
    useDynamicInboxData(wsId);
  const actionLabels = useInboxActionGroupLabels();
  const typeLabels = useTypeLabels();
  const setMode = useSetInboxMode();
  const slackBlockEnabled = useFeatureFlag("cerebro_inbox_slack_block");
  const secretaryEnabled = useFeatureFlag("cerebro_inbox_secretary");
  const favoritesEnabled = useFeatureFlag("cerebro_inbox_favorites");
  const roundsEnabled = useFeatureFlag("cerebro_inbox_rounds");
  const { data: roundStatuses = [] } = useRoundStatuses(wsId);
  const startRound = useStartRound(wsId);
  const roundIssueIds = useMemo(() => [...new Set(roundStatuses.flatMap((status) => status.members.map((member) => member.issue_id)))], [roundStatuses]);
  const roundIssueQueries = useQueries({ queries: roundIssueIds.map((issueId) => issueDetailOptions(wsId, issueId)) });
  const roundIssueTitles = useMemo(() => Object.fromEntries(roundIssueQueries.flatMap((query, index) => {
    const issue = query.data;
    return issue ? [[roundIssueIds[index]!, `${issue.identifier} · ${issue.title}`]] : [];
  })), [roundIssueIds, roundIssueQueries]);
  // TECH-3541 #2 — same flags the classic inbox uses to gate its view options.
  const channelsEnabled = useFeatureFlag("cerebro_channels");
  const pinnedFilterEnabled = useFeatureFlag("cerebro_inbox_pinned_filter");
  const isMobile = useIsMobile();

  const markRead = useMarkInboxRead();
  // FIR-1854 — opening a thread row clears only that thread, not the channel.
  const markThreadRead = useMarkThreadRead();
  const archiveInbox = useArchiveInbox();
  const archiveChannel = useArchiveChannel();
  // TECH-3541 #2 — workspace-wide bulk maintenance, identical to the classic
  // inbox ⋯ menu, surfaced on the "All messages" box.
  const markAllRead = useMarkAllInboxRead();
  const archiveAll = useArchiveAllInbox();
  const archiveAllRead = useArchiveAllReadInbox();
  const archiveCompleted = useArchiveCompletedInbox();

  const [activeTabId, setActiveTabId] = useState<string>(
    layout.activeTabId ?? layout.tabs[0]?.id ?? "",
  );
  const activeTab =
    layout.tabs.find((t) => t.id === activeTabId) ??
    layout.tabs[0] ?? { id: "", title: "Inbox", sections: [] };
  const activeSections = useMemo(
    () =>
      activeTab.sections.filter(
        (section) =>
          (section.kind !== "secretary" || secretaryEnabled) &&
          // TECH-3579 — hide a persisted standalone Favorites box when the flag
          // is off, mirroring how Secretary/Chat boxes are gated.
          (section.kind !== "favorites" || favoritesEnabled),
      ),
    [activeTab.sections, secretaryEnabled, favoritesEnabled],
  );

  // TECH-3502 #2 — inline rename of the active tab (also reachable from ⋯).
  const [editingTabId, setEditingTabId] = useState<string | null>(null);
  // TECH-3502 #5 — id of the box just added, so we can flash it; cleared on a
  // timer. The list scrolls to the top (where new boxes land) on insert.
  const [justAddedId, setJustAddedId] = useState<string | null>(null);
  const sectionsScrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!justAddedId) return;
    const t = setTimeout(() => setJustAddedId(null), 1800);
    return () => clearTimeout(t);
  }, [justAddedId]);

  // TECH-3413 #9 — free-text search across all sections in the active tab.
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<DynInboxEntry | null>(null);
  // TECH-3690 — a note opened from the Notes box renders in the same detail
  // pane messages use. Held separately from `selected` (which is the inbox
  // feed union) so it doesn't disturb feed selection/snapshot logic.
  const [selectedNote, setSelectedNote] = useState<string | null>(null);
  // FIR-2826 — when a note is opened from a note-comment mention, carry the
  // mentioned comment id into the pane so NoteEditor opens the comments panel
  // and scrolls to that comment (parity with the classic inbox's `&comment=`
  // deep link). Undefined when the note is opened from the Notes box.
  const [selectedNoteComment, setSelectedNoteComment] = useState<
    string | null
  >(null);
  const [secretarySelectedKey, setSecretarySelectedKey] = useState<string | null>(null);
  const [secretaryCompletedKeys, setSecretaryCompletedKeys] = useState<Set<string>>(
    () => new Set(),
  );
  // JEH-901 — "new message" → picking an AGENT must open an agent chat, not a
  // one-agent DM. Held in local state because no ChatSession exists yet (the
  // session is created on first send); mirrors the classic inbox new-chat flow.
  const [newChat, setNewChat] = useState<{ agentId: string; sessionId: string | null } | null>(null);
  const [showNewMessage, setShowNewMessage] = useState(false);
  const [showReminder, setShowReminder] = useState(false);
  useEffect(
    () =>
      listenForInboxDefaultTab(() => {
        setActiveTabId(layout.tabs[0]?.id ?? "");
      }),
    [layout.tabs],
  );
  const [savePresetOpen, setSavePresetOpen] = useState(false);
  const [presetName, setPresetName] = useState("");
  // TECH-3541 #3 — archived messages are a VIEW reached from the ⋯ menu, not a
  // box. Toggling this swaps the section list for the archived list.
  const [archivedOpen, setArchivedOpen] = useState(false);
  // TECH-3413 #6 — frozen copy of the selected entry, captured at selection
  // time. While a row is open we render it from this snapshot so marking it
  // read doesn't move it out of its group/section until you pick another row.
  const [selectedSnapshot, setSelectedSnapshot] = useState<DynInboxEntry | null>(null);
  const selectedKey = selected
    ? selected.kind === "notif"
      ? selected.item.issue_id ?? selected.item.id
      : selected.id
    : null;

  // FIR-1702 — anchor the viewport so the list can re-sort without the page
  // jumping. Prefer the open row (where the user's eyes are); when nothing is
  // open, fall back to the first row whose agent is actively running — that is
  // the "active task" whose position moves when an agent is started. The key
  // shape matches entryKey() / selectedKey so it lines up with the row's
  // data-inbox-entry-key attribute.
  const runningAnchorKey = useMemo<string | null>(() => {
    const { issueRunStates, chatRunStates } = filterContext.action;
    if (!issueRunStates.size && !chatRunStates.size) return null;
    for (const e of entries) {
      if (e.kind === "notif" && e.item.issue_id && issueRunStates.has(e.item.issue_id)) {
        return e.item.issue_id;
      }
      if (e.kind === "chat" && chatRunStates.has(e.session.id)) return e.id;
    }
    return null;
  }, [entries, filterContext]);
  useInboxScrollAnchor(sectionsScrollRef, selectedKey ?? runningAnchorKey);

  const clearSelection = useCallback(() => {
    if (secretarySelectedKey) {
      setSecretaryCompletedKeys((keys) => {
        if (keys.has(secretarySelectedKey)) return keys;
        const next = new Set(keys);
        next.add(secretarySelectedKey);
        return next;
      });
    }
    setSecretarySelectedKey(null);
    setSelected(null);
    setSelectedSnapshot(null);
    setNewChat(null);
    setSelectedNote(null);
    setSelectedNoteComment(null);
  }, [secretarySelectedKey]);

  // TECH-3690 — open a note in the detail pane; clears any feed/chat selection
  // so the pane shows the note, not a stale message. FIR-2826 — an optional
  // commentId opens the note straight at that comment (note-comment mention).
  const openNoteInPane = useCallback(
    (noteId: string, commentId?: string | null) => {
      setSecretarySelectedKey(null);
      setSelected(null);
      setSelectedSnapshot(null);
      setNewChat(null);
      setSelectedNote(noteId);
      setSelectedNoteComment(commentId ?? null);
    },
    [],
  );

  // TECH-3598 #2 — deep-link parity for notifications that carry no issue_id.
  // Skill change-requests open the skill detail with the proposal focused;
  // note @-mentions open the note. Mirrors the classic inbox's handleSelect
  // (inbox-page.tsx) so clicking the row lands on what it is about instead of
  // the empty "Select a message" pane. Returns true when it routed away.
  const routeNonIssueNotif = useCallback(
    (item: InboxItem): boolean => {
      // FIR-3051 — skill change requests are messages, not navigation. They
      // stay selected and render SkillChangeInboxDetail in this pane.
      if (opensInDynamicInboxPane(item)) return false;
      // FIR-1587 — fork / agent-assignment items deep-link to the relevant skill detail.
      if (item.type === "skill_forked" && item.details?.forked_skill_id) {
        push(paths.skillDetail(String(item.details.forked_skill_id)));
        return true;
      }
      if (item.type === "skill_agent_assigned" && item.details?.skill_id) {
        push(paths.skillDetail(String(item.details.skill_id)));
        return true;
      }
      // FIR-1775 — agent-context change requests carry no issue_id; deep-link to
      // the agent's Instructions tab (?tab=instructions) with the proposal queue.
      if (item.type === "agent_context_change_request" && item.details?.agent_id) {
        const cr = item.details.change_request_id;
        push(`${paths.agentDetail(item.details.agent_id)}?tab=instructions${cr ? `&cr=${cr}` : ""}`);
        return true;
      }
      // TECH-3690 — open the note in the detail pane (parity with the Notes
      // box); fall back to the full Notes page when the pane is off. FIR-2826 —
      // carry the comment id (a note-comment mention) so the note opens at the
      // exact comment: the pane forwards it to NoteEditor, and the full-page
      // fallback appends `&comment=` like the classic inbox.
      const noteTarget = noteMentionTarget(item);
      if (noteTarget) {
        if (noteInboxPaneEnabled)
          openNoteInPane(noteTarget.noteId, noteTarget.commentId);
        else push(noteMentionUrl(paths.notes(), noteTarget));
        return true;
      }
      return false;
    },
    [push, paths, noteInboxPaneEnabled, openNoteInPane],
  );

  const onSelect = (entry: DynInboxEntry) => {
    // TECH-3413 #7 — selecting never auto-advances to the next row; we only
    // ever set the row the user clicked.
    if (entry.kind === "notif" && !entry.item.read) markRead.mutate(entry.item.id);
    // FIR-1854 — opening a thread row clears only that thread (independent of
    // the channel's own read state).
    if (entry.kind === "thread" && !entry.item.read)
      markThreadRead.mutate({ channelId: entry.channelId, rootId: entry.threadRootId });
    // TECH-3598 #2 — non-issue notifs deep-link away instead of opening the pane.
    if (entry.kind === "notif" && routeNonIssueNotif(entry.item)) return;
    setSecretarySelectedKey(null);
    setNewChat(null);
    setSelectedNote(null);
    setSelected(entry);
    setSelectedSnapshot(entry);
  };
  const onSecretarySelect = (entry: DynInboxEntry) => {
    if (entry.kind === "notif" && !entry.item.read) markRead.mutate(entry.item.id);
    if (entry.kind === "thread" && !entry.item.read)
      markThreadRead.mutate({ channelId: entry.channelId, rootId: entry.threadRootId });
    if (entry.kind === "notif" && routeNonIssueNotif(entry.item)) return;
    setSecretarySelectedKey(secretaryEntryKey(entry));
    setNewChat(null);
    setSelectedNote(null);
    setSelected(entry);
    setSelectedSnapshot(entry);
  };
  // TECH-3489 — issues and channels still archive via their host mutations.
  // Chat rows own archive/unarchive/delete inside CerebroChatSessionRowActions;
  // for a chat entry this only clears the selection if that row was open (the
  // row passes it as `onCleared`).
  const onArchive = useCallback(
    (entry: DynInboxEntry) => {
      // FIR-1854 — a thread row archives its backing inbox item, like a notif.
      if (entry.kind === "notif" || entry.kind === "thread") archiveInbox.mutate(entry.item.id);
      else if (entry.kind === "channel") archiveChannel.mutate(entry.channel.id);
      if (selected && selected.id === entry.id) clearSelection();
    },
    [archiveInbox, archiveChannel, selected, clearSelection],
  );

  // TECH-3541 #2 — lookup tables + classic-parity controls for the "All
  // messages" box. groupContext feeds project/agent/type grouping; the controls
  // (view options gating + bulk actions) are only wired to the "all" box.
  const groupContext = useMemo(
    () => ({ projectMap, agentMap, typeLabels }),
    [projectMap, agentMap, typeLabels],
  );
  const classicControls = useMemo(
    () => ({
      channelsEnabled,
      pinnedFilterEnabled,
      onMarkAllRead: () => markAllRead.mutate(undefined),
      onArchiveAll: () => {
        clearSelection();
        archiveAll.mutate(undefined);
      },
      onArchiveAllRead: () => {
        clearSelection();
        archiveAllRead.mutate(undefined);
      },
      onArchiveCompleted: () => {
        clearSelection();
        archiveCompleted.mutate(undefined);
      },
    }),
    [
      channelsEnabled,
      pinnedFilterEnabled,
      markAllRead,
      archiveAll,
      archiveAllRead,
      archiveCompleted,
      clearSelection,
    ],
  );

  // TECH-3413 #6 — substitute the frozen snapshot for its live row so the
  // section filter/sort sees the entry's selection-time state, not the
  // just-marked-read state. Matched on stable identity; falls through cleanly
  // if the row was archived/removed.
  const roundBlockActive = roundsEnabled && activeSections.some((section) => section.kind === "rounds") && roundStatuses.some(
    (status) => status.members.length > 0 || status.round.next_run_at || status.active_run,
  );
  const displayEntries = useMemo(() => {
    const excluded = roundIssueIdsToExclude(roundStatuses, roundBlockActive);
    const availableEntries = entries.filter((e) => e.kind !== "notif" || !e.item.issue_id || !excluded.has(e.item.issue_id));
    if (!selectedSnapshot) return availableEntries;
    const target = entryIdentity(selectedSnapshot);
    let swapped = false;
    const next = availableEntries.map((e) => {
      if (!swapped && entryIdentity(e) === target) {
        swapped = true;
        return selectedSnapshot;
      }
      return e;
    });
    return swapped ? next : availableEntries;
  }, [entries, selectedSnapshot, roundBlockActive, roundStatuses]);

  // TECH-3413 #1 — drag-to-reorder sections (5px threshold so clicks inside a
  // section still register).
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const handleSectionDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    updateActiveTab((t) => {
      const oldIndex = t.sections.findIndex((s) => s.id === active.id);
      const newIndex = t.sections.findIndex((s) => s.id === over.id);
      if (oldIndex < 0 || newIndex < 0) return t;
      return { ...t, sections: arrayMove(t.sections, oldIndex, newIndex) };
    });
  };
  // TECH-3422 — the Slack-block opens a channel/DM into the same detail panel.
  const onOpenChannel = (channel: Channel) => {
    setSecretarySelectedKey(null);
    setSelected({
      kind: "channel",
      id: channel.id,
      time: Date.parse(channel.updated_at) || 0,
      channel,
    });
  };
  const selectedChannelId = selected?.kind === "channel" ? selected.channel.id : null;
  // JEH-901 — picking an AGENT in "new message" starts an agent chat (the same
  // conversation panel the classic inbox uses), not a DM channel. A MEMBER
  // still flows through onCreated → onOpenChannel (a real DM channel).
  const handleAgentChatStarted = (agentId: string) => {
    setSelected(null);
    setSelectedSnapshot(null);
    setNewChat({ agentId, sessionId: null });
  };
  // TECH-3664 — opening an EXISTING (unread) agent session from the Chat block:
  // find its chat entry and select it so the unread conversation opens in the
  // detail panel (where it gets marked read), instead of a blank new chat.
  const handleOpenAgentSession = (sessionId: string) => {
    const entry = entries.find((e) => e.kind === "chat" && e.id === sessionId);
    if (entry) onSelect(entry);
  };

  // TECH-3618 — the sidebar "New message" button opens the GLOBAL picker, which
  // hands its result to the inbox via URL params: ?chat=new-chat&agent=<id> for
  // an agent chat, ?issue=<id> for a freshly created DM/channel. The classic
  // inbox reads these params directly; the dynamic inbox owns selection in local
  // state, so we consume the intent here — translate the param into a selection,
  // then strip it (silently) so it can't re-fire. Channel/chat rows wait until
  // the just-created conversation lands in `entries` after its query refetch.
  const urlChat = searchParams.get("chat") ?? "";
  const urlIssue = searchParams.get("issue") ?? "";
  const urlAgent = searchParams.get("agent") ?? "";
  // `replaceSilent` strips the param via history.replaceState without a re-read
  // of searchParams, so a ref (not the URL) is what guarantees a given intent is
  // applied exactly once — even while we wait for `entries` to include a
  // just-created conversation.
  const consumedIntentRef = useRef<string>("");
  useEffect(() => {
    const intent = urlChat
      ? `chat:${urlChat}:${urlAgent}`
      : urlIssue
        ? `issue:${urlIssue}`
        : "";
    if (!intent) {
      consumedIntentRef.current = "";
      return;
    }
    if (consumedIntentRef.current === intent) return;
    const consume = () => {
      consumedIntentRef.current = intent;
      (replaceSilent ?? replace)(paths.inbox());
    };
    if (urlChat === "new-chat" && urlAgent) {
      handleAgentChatStarted(urlAgent);
      consume();
      return;
    }
    if (urlChat) {
      const entry = entries.find((e) => e.kind === "chat" && e.id === urlChat);
      if (entry) {
        onSelect(entry);
        consume();
      }
      return;
    }
    const entry = findEntryByInboxIssueParam(entries, urlIssue, {
      deferIssueNotifications: loading,
    });
    if (entry) {
      onSelect(entry);
      consume();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlChat, urlIssue, urlAgent, entries, loading, replace, replaceSilent, paths]);

  // TECH-3708 — per-message deep links. The inbox owns selection in local
  // state, so `?message=<key>` is what makes the open message linkable: opening
  // a row writes the key to the URL (so it can be copied), and a pasted URL
  // re-opens that exact message once its row lands in `entries`.
  const urlMessage = searchParams.get(INBOX_MESSAGE_PARAM) ?? "";
  // Guards a given key from being re-applied once we've acted on it, so the
  // writer below (which mirrors the selection back into the URL) can't make the
  // reader re-select in a loop, and a deep-link that opened then got cleared
  // doesn't re-open on the next entries refetch.
  const appliedMessageRef = useRef<string>("");
  useEffect(() => {
    if (!urlMessage) {
      appliedMessageRef.current = "";
      return;
    }
    if (selected && messageKeyForEntry(selected) === urlMessage) {
      appliedMessageRef.current = urlMessage;
      return;
    }
    if (appliedMessageRef.current === urlMessage) return;
    if (loading) return;
    const entry = findEntryByMessageKey(entries, urlMessage);
    if (entry) {
      appliedMessageRef.current = urlMessage;
      onSelect(entry);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlMessage, entries, selected, loading]);

  // Keep the URL in sync with the open message so the browser address bar (and
  // the Copy link button) always reflects what is showing. `replaceSilent`
  // updates the URL without an RSC nav (FIR-2684), so this never reloads the
  // pane. A `newChat` (unsent agent chat) has no stable key, so selection being
  // null there correctly leaves the URL message-free.
  useEffect(() => {
    // FIR-2677 — nextInboxMessageUrl returns null (skip) while a sidebar
    // "New message" (?chat/?agent) or created-conversation (?issue) intent is
    // still in the URL, so this effect can't clobber that intent with a stale
    // selection when it re-runs on a fresh `replace` reference.
    const next = nextInboxMessageUrl({
      urlChat,
      urlIssue,
      selectedKey: selected ? messageKeyForEntry(selected) : null,
      currentMessageParam: searchParams.get(INBOX_MESSAGE_PARAM) ?? null,
      inboxPath: paths.inbox(),
    });
    if (next === null) return;
    (replaceSilent ?? replace)(next);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, paths, replace, replaceSilent, urlChat, urlIssue]);

  // ---- layout edits ----
  const updateActiveTab = (fn: (t: InboxTabConfig) => InboxTabConfig) =>
    setLayout(replaceTab(layout, activeTab.id, fn));

  const addSection = (kind: SectionKind) => {
    const id = makeId(kind);
    const section: InboxSectionConfig = {
      id,
      kind,
      groupBy: kind === "act_now" ? "action" : kind === "archived" ? "type" : "none",
      sort: "newest",
      ...(kind === "project" ? { projectId: projects[0]?.id } : {}),
      // FIR-1645 — the Archived block starts folded by default.
      ...(kind === "archived" ? { defaultCollapsed: true } : {}),
    };
    // TECH-3502 #5 — new boxes are inserted at the TOP so they're immediately
    // visible; flash a ring around the new box and scroll the list up so the
    // insert reads clearly.
    updateActiveTab((t) => ({ ...t, sections: [section, ...t.sections] }));
    setJustAddedId(id);
    requestAnimationFrame(() =>
      sectionsScrollRef.current?.scrollTo({ top: 0, behavior: "smooth" }),
    );
  };
  const removeSection = (id: string) =>
    // TECH-3541 (Jesper) — the permanent inbox block (first "all" box in the
    // tab) cannot be removed. TECH-3748 (Jesper) — this protection only applies
    // in its default placement (the first/home tab); on any other tab the
    // "All messages" box was inserted by the user and can be removed.
    updateActiveTab((t) => {
      const isDefaultTab = layout.tabs[0]?.id === t.id;
      const permanentId = isDefaultTab
        ? t.sections.find((s) => s.kind === "all")?.id ?? null
        : null;
      if (id === permanentId) return t;
      return { ...t, sections: t.sections.filter((s) => s.id !== id) };
    });
  const changeSection = (next: InboxSectionConfig) =>
    updateActiveTab((t) => ({ ...t, sections: t.sections.map((s) => (s.id === next.id ? next : s)) }));

  const addTab = () => {
    const id = makeId("tab");
    setLayout({
      ...layout,
      tabs: [...layout.tabs, { id, title: "New tab", sections: [{ id: makeId("all"), kind: "all" }] }],
    });
    setActiveTabId(id);
    // TECH-3502 #2 — a fresh tab opens straight into rename so it gets a name.
    setEditingTabId(id);
  };
  // TECH-3502 #2/#4 — rename a tab; empty input keeps the previous title.
  const renameTab = (id: string, title: string) => {
    const trimmed = title.trim();
    setLayout({
      ...layout,
      tabs: layout.tabs.map((t) => (t.id === id ? { ...t, title: trimmed || t.title } : t)),
    });
  };
  const removeTab = (id: string) => {
    if (layout.tabs.length <= 1) return;
    const remaining = layout.tabs.filter((t) => t.id !== id);
    const nextActive = remaining[0]?.id ?? "";
    setLayout({ ...layout, tabs: remaining, activeTabId: nextActive });
    if (activeTabId === id) setActiveTabId(nextActive);
  };
  const applyUserPreset = (presetLayout: InboxLayout) => {
    setLayout(presetLayout);
    setActiveTabId(presetLayout.activeTabId ?? presetLayout.tabs[0]?.id ?? "");
  };
  const onSavePreset = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const cleanName = presetName.trim();
    if (!cleanName) return;
    saveUserPreset(cleanName);
    setPresetName("");
    setSavePresetOpen(false);
  };

  // TECH-3598 #3 — snapshot the most recent unread notification's comment_id
  // for the selected channel so ChannelDetail opens at the unread thread, not
  // the top. Keyed on the channel id (a stable string), NOT on `items`: once
  // the channel opens its notifications get marked read and `items` gets a
  // fresh reference, which would recompute against the now-read items and clear
  // the id before ChannelDetail's auto-open effect runs. Mirrors the classic
  // inbox's selectedChannelCommentId (inbox-page.tsx). selectedChannelId is
  // already derived above.
  const selectedChannelCommentId = useMemo<string | undefined>(() => {
    if (!selectedChannelId) return undefined;
    const relevant = items
      .filter(
        (i) =>
          !i.read && !i.archived && i.issue_id === selectedChannelId && i.details?.comment_id,
      )
      .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
    return relevant[0]?.details?.comment_id ?? undefined;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedChannelId]);

  // FIR-1576 (follow-up) — route a notif by its issue's real kind, not channel-
  // list-cache membership; see useSelectedNotifChannel for the full rationale.
  const selectedNotifIssueId =
    selected?.kind === "notif" ? selected.item.issue_id ?? null : null;
  const { isChannel: selectedNotifIsChannel, kindPending: selectedNotifKindPending } =
    useSelectedNotifChannel(wsId, selectedNotifIssueId, knownChannelIds);

  const detail = useMemo(() => {
    // JEH-901 — a freshly started agent chat (from "new message"). No session
    // exists until the first message is sent; InboxChatPanel owns that, then
    // reports the id back so we keep showing the live session.
    if (newChat) {
      return (
        <ErrorBoundary resetKeys={[newChat.sessionId ?? newChat.agentId]}>
          <InboxChatPanel
            key={newChat.sessionId ?? "new-chat"}
            sessionId={newChat.sessionId}
            initialAgentId={newChat.agentId}
            onSessionCreated={(id) =>
              setNewChat((c) => (c ? { ...c, sessionId: id } : c))
            }
          />
        </ErrorBoundary>
      );
    }
    // TECH-3690 — a note opened from the Notes box (or a note @-mention) shows
    // in this same pane. Only when no feed row is selected; selecting a row
    // clears selectedNote, so the two never collide.
    if (!selected) {
      if (selectedNote) {
        return (
          // FIR-2826 — key on note + comment so re-opening the same note at a
          // different comment remounts NoteEditor and re-runs its open-comment
          // effect (which only fires once per mount).
          <ErrorBoundary resetKeys={[selectedNote, selectedNoteComment]}>
            <NoteInboxDetail
              key={`${selectedNote}:${selectedNoteComment ?? ""}`}
              noteId={selectedNote}
              initialCommentId={selectedNoteComment}
              onClose={() => {
                setSelectedNote(null);
                setSelectedNoteComment(null);
              }}
            />
          </ErrorBoundary>
        );
      }
      return null;
    }
    if (selected.kind === "channel") {
      return (
        <ErrorBoundary resetKeys={[selected.channel.id]}>
          <ChannelDetail
            key={selected.channel.id}
            channelId={selected.channel.id}
            initialChannel={selected.channel}
            // TECH-3598 #3 — open at the unread thread, classic parity.
            initialCommentId={selectedChannelCommentId}
            onArchive={clearSelection}
          />
        </ErrorBoundary>
      );
    }
    // FIR-1854 — a thread row opens its channel in the same pane and deep-links
    // straight into the thread via the reply's comment id (ChannelDetail opens
    // the thread side-panel when the initial comment is itself a reply).
    if (selected.kind === "thread") {
      return (
        <ErrorBoundary resetKeys={[selected.channelId, selected.threadRootId]}>
          <ChannelDetail
            key={selected.channelId}
            channelId={selected.channelId}
            initialCommentId={selected.item.details?.comment_id ?? undefined}
            // FIR-1854 — opening from a thread row must not auto-mark the whole
            // channel read; onSelect already cleared just this thread.
            suppressAutoMarkRead
            onArchive={clearSelection}
          />
        </ErrorBoundary>
      );
    }
    // FIR-1576 — a notification whose issue is a channel/DM opens in the
    // ChannelDetail pane, never IssueDetail (whose FIR-2452 effect would redirect
    // the user out of the inbox to /channels/<id>). Channel-ness is decided by
    // the issue's real `kind` (selectedNotifIsChannel), with knownChannelIds as a
    // zero-latency fast path; see the note where those are computed above.
    if (selected.kind === "notif" && selected.item.issue_id && selectedNotifIsChannel) {
      const channelId = selected.item.issue_id;
      return (
        <ErrorBoundary resetKeys={[channelId]}>
          <ChannelDetail
            key={channelId}
            channelId={channelId}
            initialCommentId={selectedChannelCommentId}
            onArchive={clearSelection}
          />
        </ErrorBoundary>
      );
    }
    // FIR-1576 — kind not yet resolved: hold the pane on a light placeholder
    // instead of mounting IssueDetail, so a channel notif never fires the
    // redirect during the window before its kind loads (the first-load / back-
    // navigation race that knownChannelIds alone could not close).
    if (selected.kind === "notif" && selected.item.issue_id && selectedNotifKindPending) {
      return (
        <div className="flex h-full items-center justify-center text-muted-foreground">
          <Inbox className="h-10 w-10 animate-pulse text-muted-foreground/30" />
        </div>
      );
    }
    if (selected.kind === "notif" && selected.item.issue_id) {
      return (
        <ErrorBoundary resetKeys={[selected.item.issue_id]}>
          <IssueDetail
            key={selected.item.issue_id}
            issueId={selected.item.issue_id}
            seedFromIssueList={false}
            defaultSidebarOpen={false}
            layoutId="multica_inbox_dynamic_issue_detail_layout"
            // TECH-3598 #1 — scroll to + highlight the comment that triggered
            // the notification instead of landing at the top, classic parity.
            highlightCommentId={selected.item.details?.comment_id ?? undefined}
            extensions={roundsEnabled ? <AddToRoundAction issueId={selected.item.issue_id} /> : undefined}
            linkSelfInBreadcrumb
            onDelete={clearSelection}
            // TECH-3549 — reuse the classic inbox's mark-done/archive toolbar
            // button: wiring onDone makes IssueDetail render the same
            // checkmark (active → done + archive) / Archive (already done)
            // affordance the classic inbox shows, so opening an issue from the
            // dynamic inbox can archive it from the open page too.
            onDone={() => onArchive(selected)}
          />
        </ErrorBoundary>
      );
    }
    // Agent chat session — open the same conversation panel the classic inbox
    // uses, so chats read identically here.
    if (selected.kind === "chat") {
      return (
        <ErrorBoundary resetKeys={[selected.session.id]}>
          <InboxChatPanel key={selected.session.id} sessionId={selected.session.id} />
        </ErrorBoundary>
      );
    }
    // TECH-3598 #2 — a notification with no issue_id that wasn't routed away
    // (reminder, quick-create-failed, runtime-auto-paused, private-agent run
    // request, …). Classic renders a real detail pane for these instead of the
    // empty placeholder; mirror it so the row opens something.
    if (selected.kind === "notif") {
      const item = selected.item;
      if (opensInDynamicInboxPane(item)) {
        return (
          <ErrorBoundary resetKeys={[item.id]}>
            <SkillChangeInboxDetail item={item} />
          </ErrorBoundary>
        );
      }
      return (
        <div className="p-3 sm:p-6">
          <h2 className="text-lg font-semibold">{item.title}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{typeLabels[item.type]}</p>
          {item.body && (
            <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
              {item.body}
            </div>
          )}
          {item.type === "reminder" && (
            <p className="mt-3 text-sm text-muted-foreground">
              {formatPlannedDateTime(item.details?.planned_at ?? item.muted_until) ?? ""}
            </p>
          )}
          {item.type === "quick_create_failed" && item.details?.original_prompt && (
            <div className="mt-4 rounded-md border bg-muted/40 p-3">
              <p className="text-xs font-medium text-muted-foreground">Original input</p>
              <p className="mt-1 whitespace-pre-wrap text-sm">{item.details.original_prompt}</p>
            </div>
          )}
          <div className="mt-4 flex gap-2">
            {item.type === "quick_create_failed" && (
              <Button
                size="sm"
                onClick={() => {
                  // Seed the advanced create-issue form with the original
                  // prompt so the user recovers their input instead of
                  // retyping; the agent hint becomes the assignee candidate.
                  const prompt = item.details?.original_prompt ?? "";
                  const agentId = item.details?.agent_id;
                  useIssueDraftStore.getState().setDraft({
                    description: prompt,
                    ...(agentId
                      ? { assigneeType: "agent" as const, assigneeId: agentId }
                      : {}),
                  });
                  useModalStore.getState().open("create-issue");
                }}
              >
                Edit as advanced form
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={() => onArchive(selected)}>
              <Archive className="mr-1.5 h-3.5 w-3.5" />
              Archive
            </Button>
          </div>
        </div>
      );
    }
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
        <p className="text-sm">Select a message to read it here.</p>
      </div>
    );
  }, [selected, selectedNote, selectedNoteComment, clearSelection, newChat, onArchive, selectedChannelCommentId, selectedNotifIsChannel, selectedNotifKindPending, typeLabels]); // FIR-1576 — re-render the channel-vs-issue branch when the selected notif's kind resolves

  const createMenuProps = {
    onNewMessage: () => setShowNewMessage(true),
    onNewIssue: () => openCreateIssueWithPreference(),
    onNewReminder: () => setShowReminder(true),
  };


  // TECH-3541 (Jesper) — the permanent inbox block is the first "All messages"
  // box in the active tab. The search bar is rendered as part of it. When a tab
  // has no "all" box, the search falls back to a standalone bar at the top so
  // search is never lost.
  const inboxBlockId = activeTab.sections.find((s) => s.kind === "all")?.id ?? null;
  // TECH-3748 (Jesper) — the "All messages" box is permanent (non-removable)
  // only in its default placement: the first/home tab. On every other tab it
  // was inserted by the user and can be removed like any other box.
  const isDefaultTab = layout.tabs[0]?.id === activeTab.id;
  const permanentBlockId = isDefaultTab ? inboxBlockId : null;

  const searchBar = (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-2.5 py-1.5">
      <Search className="size-3.5 flex-none text-muted-foreground" />
      <input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        autoComplete="off"
        autoCorrect="off"
        spellCheck={false}
        role="searchbox"
        aria-label="Search inbox"
        placeholder="Search inbox…"
        className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
      />
      {query && (
        <button
          type="button"
          className="flex-none rounded p-0.5 text-muted-foreground hover:bg-muted"
          onClick={() => setQuery("")}
          title="Clear search"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );

  const listColumn = (
    <div className="relative flex h-full min-h-0 flex-col">
      {/* tabs */}
      <div className="flex items-center gap-1 border-b border-border bg-muted/30 px-2 pt-1">
          {/* TECH-3502 — on mobile the dynamic inbox is full-screen with no app
              chrome, so it needs its own hamburger to open the navigation menu
              (the classic inbox gets this from PageHeader). md:hidden on desktop. */}
          <MobileSidebarTrigger className="-mb-1 mr-1" />
          {/* TECH-3494 — tabs scroll horizontally and tuck behind the fixed
              create/⋯ buttons when there are more than fit. */}
          <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {layout.tabs.map((tab) =>
            editingTabId === tab.id ? (
              // TECH-3502 #2/#4 — inline rename of a tab.
              <input
                key={tab.id}
                autoFocus
                defaultValue={tab.title}
                onBlur={(e) => {
                  renameTab(tab.id, e.target.value);
                  setEditingTabId(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    renameTab(tab.id, e.currentTarget.value);
                    setEditingTabId(null);
                  } else if (e.key === "Escape") {
                    setEditingTabId(null);
                  }
                }}
                className="-mb-px w-28 shrink-0 border-b-2 border-primary bg-transparent px-2 pb-2 pt-2 text-sm font-semibold text-foreground outline-none"
                aria-label="Tab name"
              />
            ) : (
              <button
                key={tab.id}
                type="button"
                onClick={() => setActiveTabId(tab.id)}
                onDoubleClick={() => setEditingTabId(tab.id)}
                title="Double-click to rename"
                className={`-mb-px shrink-0 whitespace-nowrap border-b-2 px-3 pb-2 pt-2 text-sm font-semibold ${
                  tab.id === activeTab.id
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                }`}
              >
                {tab.title}
              </button>
            ),
          )}
          </div>
          {/* create-menu (main) + ⋯ settings menu — tab add/rename/remove now
              lives in ⋯ (TECH-3502 #2) */}
          <div className="flex shrink-0 items-center gap-1 pb-1">
            <DynamicInboxCreateMenu {...createMenuProps} />
            <Dialog open={savePresetOpen} onOpenChange={setSavePresetOpen}>
              <DialogContent>
                <form onSubmit={onSavePreset} className="space-y-4">
                  <DialogHeader>
                    <DialogTitle>Save inbox preset</DialogTitle>
                    <DialogDescription>
                      Save the current tabs and sections as a shortcut in this menu.
                    </DialogDescription>
                  </DialogHeader>
                  <Input
                    value={presetName}
                    onChange={(e) => setPresetName(e.target.value)}
                    placeholder="Preset name"
                    autoFocus
                  />
                  <DialogFooter>
                    <Button type="button" variant="outline" onClick={() => setSavePresetOpen(false)}>
                      Cancel
                    </Button>
                    <Button type="submit" disabled={!presetName.trim()}>
                      <Save className="size-4" /> Save preset
                    </Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<button type="button" className="rounded p-1.5 text-muted-foreground hover:bg-muted" title="Inbox menu" />}
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Tabs</DropdownMenuLabel>
                  <DropdownMenuItem onClick={addTab}>
                    <Plus className="mr-2 size-4" /> Add tab
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setEditingTabId(activeTab.id)}>
                    <Pencil className="mr-2 size-4" /> Rename current tab
                  </DropdownMenuItem>
                  {layout.tabs.length > 1 && (
                    <DropdownMenuItem onClick={() => removeTab(activeTab.id)}>
                      <Trash2 className="mr-2 size-4" /> Remove current tab
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Add section</DropdownMenuLabel>
                  {SECTION_CATALOG.filter(
                    // TECH-3422 — Chat only when slack-block is on.
                    (c) => c.kind !== "team" || slackBlockEnabled,
                  )
                    // TECH-3421 — only offer the Notes box when Notes is enabled.
                    .filter((c) => c.kind !== "notes" || notesEnabled)
                    // TECH-3690 — the Quick note box shares the Notes flag.
                    .filter((c) => c.kind !== "note" || notesEnabled)
                    // TECH-3557 — only offer Secretary when its flag is enabled.
                    .filter((c) => c.kind !== "secretary" || secretaryEnabled)
                    // TECH-3579 — only offer the Favorites box when its flag is on.
                    .filter((c) => c.kind !== "favorites" || favoritesEnabled)
                    .filter((c) => c.kind !== "rounds" || roundsEnabled)
                    .map((c) => (
                    <DropdownMenuItem key={c.kind} onClick={() => addSection(c.kind)}>
                      {c.label}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Start from preset</DropdownMenuLabel>
                  <DropdownMenuItem onClick={() => setSavePresetOpen(true)}>
                    <Save className="mr-2 size-4" /> Save current as preset...
                  </DropdownMenuItem>
                  {userPresets.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuLabel>My presets</DropdownMenuLabel>
                      {userPresets.map((preset) => (
                        <DropdownMenuItem
                          key={preset.id}
                          className="pr-1"
                          onClick={() => applyUserPreset(preset.layout)}
                        >
                          <span className="min-w-0 flex-1 truncate">{preset.label}</span>
                          <button
                            type="button"
                            className="ml-2 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                            title={`Delete ${preset.label}`}
                            aria-label={`Delete ${preset.label}`}
                            onClick={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              deleteUserPreset(preset.id);
                            }}
                          >
                            <Trash2 className="size-3.5" />
                          </button>
                        </DropdownMenuItem>
                      ))}
                    </>
                  )}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => setArchivedOpen(true)}>
                  <Archive className="mr-2 size-4" /> Show archived
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => void setMode("classic")}>
                  <LayoutList className="mr-2 size-4" /> Switch to classic inbox
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {/* search (TECH-3413 #9) — TECH-3541 (Jesper): the inbox search lives
            inside the "All messages" block (rendered as its searchSlot). FIR-1502
            (Jesper): tabs without an "All messages" box no longer get a standalone
            search bar in the tab header — search lives in blocks (each block, e.g.
            the Chat block, carries its own search). */}

        {/* sections */}
        <div ref={sectionsScrollRef} className="flex-1 space-y-3 overflow-y-auto p-3">
          {loading && <p className="px-1 text-sm text-muted-foreground">Loading…</p>}
          {activeSections.length === 0 && !loading && (
            <p className="px-1 text-sm text-muted-foreground">
              Empty tab — add a section from the ⋯ menu.
            </p>
          )}
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleSectionDragEnd}>
            <SortableContext
              items={activeSections.map((s) => s.id)}
              strategy={verticalListSortingStrategy}
            >
              <div className="space-y-3">
                {activeSections.map((section) => (
                  <SortableSection key={section.id} id={section.id} highlight={section.id === justAddedId}>
                    {(handle) =>
                      // TECH-3421 — the Notes box renders its own data (recent
                      // notes); dispatched here like the Chat block, not via
                      // DynamicInboxSection.
                      section.kind === "rounds" && roundsEnabled ? (
                        <RoundsBlock
                          statuses={roundStatuses}
                          issueTitles={roundIssueTitles}
                          settings={<ConnectedRoundManager statuses={roundStatuses} issueTitles={roundIssueTitles} />}
                          dragHandle={handle}
                          defaultCollapsed={section.defaultCollapsed}
                          onRemove={() => removeSection(section.id)}
                          onStart={(roundId) => startRound.mutate(roundId)}
                          onSelectIssue={(issueId) => {
                            const entry = entries.find((candidate) => candidate.kind === "notif" && candidate.item.issue_id === issueId);
                            if (entry) onSelect(entry);
                          }}
                          renderIssue={(issueId) => {
                            const entry = entries.find((candidate) => candidate.kind === "notif" && candidate.item.issue_id === issueId);
                            if (!entry) return null;
                            return <DynamicInboxRow key={entry.id} entry={entry} isSelected={entry.id === selected?.id} agentRunState={runStateFor(entry, filterContext)} favoritesEnabled={favoritesEnabled} isFavorite={filterContext.isFavorite?.(entry) ?? false} onToggleFavorite={toggleFavorite} onSelect={onSelect} onArchive={onArchive} />;
                          }}
                        />
                      ) : section.kind === "notes" ? (
                        <NotesInboxBox
                          title={section.title}
                          dragHandle={handle}
                          limit={section.notesLimit}
                          pinnedOnly={section.notesPinnedOnly}
                          sort={section.notesSort}
                          visibility={section.notesVisibility}
                          onSetLimit={(n) => changeSection({ ...section, notesLimit: n })}
                          onSetPinnedOnly={(v) => changeSection({ ...section, notesPinnedOnly: v })}
                          onSetSort={(s) => changeSection({ ...section, notesSort: s })}
                          onSetVisibility={(v) => changeSection({ ...section, notesVisibility: v })}
                          onOpenNote={noteInboxPaneEnabled ? openNoteInPane : undefined}
                          onRemove={() => removeSection(section.id)}
                        />
                      ) : section.kind === "note" ? (
                        <NoteInboxBox
                          title={section.title}
                          dragHandle={handle}
                          noteId={section.noteId}
                          onSetNoteId={(id) =>
                            changeSection({ ...section, noteId: id ?? undefined })
                          }
                          onSetTitle={(t) =>
                            changeSection({ ...section, title: t ?? undefined })
                          }
                          onRemove={() => removeSection(section.id)}
                          fontSize={section.noteFontSize}
                          onSetFontSize={(size) =>
                            changeSection({ ...section, noteFontSize: size })
                          }
                          visibleLines={section.noteVisibleLines}
                          onSetVisibleLines={(n) =>
                            changeSection({ ...section, noteVisibleLines: n })
                          }
                          collapsible={section.collapsible}
                          defaultCollapsed={section.defaultCollapsed}
                          onSetCollapsible={(v) =>
                            changeSection({ ...section, collapsible: v })
                          }
                          onSetDefaultCollapsed={(v) =>
                            changeSection({ ...section, defaultCollapsed: v })
                          }
                        />
                      ) : section.kind === "team" ? (
                        <div className="relative">
                          <div className="absolute left-2 top-2.5 z-10">{handle}</div>
                          <SlackBlock
                            wsId={wsId}
                            selectedChannelId={selectedChannelId}
                            onOpenChannel={onOpenChannel}
                            limit={section.teamLimit}
                            onSetLimit={(n) => changeSection({ ...section, teamLimit: n })}
                            sort={section.teamSort}
                            onSetSort={(s) => changeSection({ ...section, teamSort: s })}
                            unreadFirst={section.teamUnreadFirst ?? true}
                            onSetUnreadFirst={(v) =>
                              changeSection({
                                ...section,
                                teamUnreadFirst: v,
                                teamGroupBy:
                                  section.teamGroupBy === "unread" ? "type" : section.teamGroupBy,
                              })
                            }
                            groupBy={section.teamGroupBy === "unread" ? "type" : section.teamGroupBy}
                            onSetGroupBy={(g) => changeSection({ ...section, teamGroupBy: g })}
                            showAgents={section.showAgents}
                            onSetShowAgents={(v) => changeSection({ ...section, showAgents: v })}
                            searchDefaultOpen={section.teamSearchDefaultOpen}
                            onSetSearchDefaultOpen={(v) =>
                              changeSection({ ...section, teamSearchDefaultOpen: v })
                            }
                            onOpenAgentChat={handleAgentChatStarted}
                            onOpenAgentSession={handleOpenAgentSession}
                            onRemove={() => removeSection(section.id)}
                          />
                        </div>
                      ) : section.kind === "archived" ? (
                        // FIR-1645 — archived messages as a foldable block over
                        // their own archived queries.
                        <div className="relative">
                          <ArchivedInboxBlock
                            wsId={wsId}
                            section={section}
                            selectedKey={selectedKey}
                            query={query}
                            dragHandle={handle}
                            onSelect={onSelect}
                            onChange={changeSection}
                            onRemove={() => removeSection(section.id)}
                          />
                        </div>
                      ) : section.kind === "secretary" && secretaryEnabled ? (
                        <SecretarySection
                          dragHandle={handle}
                          entries={displayEntries}
                          filterContext={filterContext}
                          selectedKey={selectedKey}
                          completedKeys={secretaryCompletedKeys}
                          query={query}
                          onSelect={onSecretarySelect}
                          onArchive={onArchive}
                          onResetCompleted={() => setSecretaryCompletedKeys(new Set())}
                          onRemove={() => removeSection(section.id)}
                        />
                      ) : (
                        <DynamicInboxSection
                          section={section}
                          entries={displayEntries}
                          filterContext={filterContext}
                          actionLabels={actionLabels}
                          projects={projects}
                          groupContext={groupContext}
                          classicControls={section.kind === "all" ? classicControls : undefined}
                          selectedKey={selectedKey}
                          query={query}
                          dragHandle={handle}
                          searchSlot={
                            section.id === inboxBlockId ? (
                              <div className="border-b border-border px-3 py-2">{searchBar}</div>
                            ) : undefined
                          }
                          removable={section.id !== permanentBlockId}
                          favoritesEnabled={favoritesEnabled}
                          onSelect={onSelect}
                          onArchive={onArchive}
                          onToggleFavorite={toggleFavorite}
                          onChange={changeSection}
                          onRemove={() => removeSection(section.id)}
                        />
                      )
                    }
                  </SortableSection>
                ))}
              </div>
            </SortableContext>
          </DndContext>
        </div>
        <div className="pointer-events-none absolute bottom-4 right-4 z-30 md:hidden">
          <div className="pointer-events-auto">
            <DynamicInboxCreateMenu {...createMenuProps} variant="floating" />
          </div>
        </div>
    </div>
  );

  // TECH-3541 #3 — when the archived view is open it replaces the section list
  // in the left column (the detail panel keeps working for opened rows).
  const leftColumn = archivedOpen ? (
    <ArchivedInboxView
      wsId={wsId}
      selectedKey={selectedKey}
      onSelect={onSelect}
      onBack={() => setArchivedOpen(false)}
    />
  ) : (
    listColumn
  );

  // TECH-3413 (Jesper feedback): mobile/PWA has no room for the side-by-side
  // split. It renders a single column — the section list, or (when a row is
  // open) that message full-screen with a Back button — mirroring how the
  // classic inbox behaves on a phone.
  //
  // TECH-3541 #1 — the list column stays MOUNTED while a message is open; the
  // detail is rendered as an opaque overlay on top of it. Unmounting the list
  // (the previous behaviour) discarded the scroll container, so pressing Back
  // dropped the user at the top of the inbox instead of where they were. By
  // keeping the same `sectionsScrollRef` element alive under the overlay, its
  // scrollTop is preserved and Back returns to the exact previous position.
  if (isMobile) {
    const detailOpen = Boolean(selected || newChat || selectedNote);
    return (
      <div className="relative flex h-full flex-col min-h-0">
        {leftColumn}
        {detailOpen && (
          <div className="absolute inset-0 z-40 flex flex-col min-h-0 bg-background">
            <div className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-2">
              <MobileSidebarTrigger className="mr-0" />
              <button
                type="button"
                onClick={clearSelection}
                className="flex shrink-0 items-center gap-1.5 rounded px-2 py-1 text-sm text-muted-foreground hover:bg-muted"
              >
                <ArrowLeft className="size-4" /> Back
              </button>
            </div>
            <div className="flex flex-1 flex-col min-h-0">{detail}</div>
          </div>
        )}
        <NewMessageModal
          open={showNewMessage}
          onClose={() => setShowNewMessage(false)}
          onCreated={onOpenChannel}
          onAgentChatStarted={handleAgentChatStarted}
        />
        <GlobalInboxReminderDialog open={showReminder} onOpenChange={setShowReminder} />
      </div>
    );
  }

  return (
    <>
      <ResizablePanelGroup orientation="horizontal">
        <ResizablePanel id="list" defaultSize={420} minSize={300} className="flex flex-col">
          {leftColumn}
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel id="detail" minSize="40%" className="flex flex-col">
          {detail ? (
            <div className="flex flex-1 flex-col min-h-0">{detail}</div>
          ) : (
            // Empty detail placeholder — mirrors the classic inbox (Inbox icon +
            // hint) so the right panel never reads as a blank/broken pane.
            <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
              <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
              <p className="text-sm">Select a message to read it here.</p>
            </div>
          )}
        </ResizablePanel>
      </ResizablePanelGroup>
      <NewMessageModal
        open={showNewMessage}
        onClose={() => setShowNewMessage(false)}
        onCreated={onOpenChannel}
        onAgentChatStarted={handleAgentChatStarted}
      />
      <GlobalInboxReminderDialog open={showReminder} onOpenChange={setShowReminder} />
    </>
  );
}
