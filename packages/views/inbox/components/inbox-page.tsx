"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  inboxListOptions,
  deduplicateInboxItems,
  useInboxUnreadCount,
} from "@multica/core/inbox/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import {
  useMarkInboxRead,
  useArchiveInbox,
  useMarkAllInboxRead,
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
} from "@multica/core/inbox/mutations";

import { IssueDetail } from "../../issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useNavigation } from "../../navigation";
import { toast } from "sonner";
import {
  MoreHorizontal,
  Inbox,
  CheckCheck,
  Archive,
  BookCheck,
  ListChecks,
  ArrowLeft,
} from "lucide-react";
import type { InboxItem } from "@multica/core/types";
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
import {
  getInboxItemRenderer,
  notificationEntry,
  conversationEntry,
  type InboxFeedEntry,
} from "../item-types";
import { useT } from "../../i18n";

export function InboxPage() {
  const { t } = useT("inbox");
  const { searchParams, replace } = useNavigation();
  const urlIssue = searchParams.get("issue") ?? "";
  const urlConversation = searchParams.get("conversation") ?? "";
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlIssue);
  const [selectedConversationId, setSelectedConversationId] = useState(
    () => urlConversation,
  );

  // Sync from URL when searchParams change (e.g. navigation)
  useEffect(() => {
    setSelectedKeyState(urlIssue);
  }, [urlIssue]);
  useEffect(() => {
    setSelectedConversationId(urlConversation);
  }, [urlConversation]);

  const wsId = useWorkspaceId();
  const { data: rawItems = [], isLoading: loading } = useQuery(inboxListOptions(wsId));
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);

  // Agent conversations are surfaced inline in the same feed as notifications.
  const { data: rawSessions = [] } = useQuery(chatSessionsOptions(wsId));
  const sessions = useMemo(
    () => rawSessions.filter((s) => s.status === "active"),
    [rawSessions],
  );

  // The merged typed feed: notifications + conversations, newest first. Each
  // entry dispatches to its item-type renderer for the row.
  const feed = useMemo<InboxFeedEntry[]>(() => {
    const entries = [
      ...items.map(notificationEntry),
      ...sessions.map(conversationEntry),
    ];
    return entries.sort((a, b) => b.sortAt.localeCompare(a.sortAt));
  }, [items, sessions]);

  const selected = items.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;
  const selectedConversation =
    sessions.find((s) => s.id === selectedConversationId) ?? null;

  // Notifications backed by an issue defer to the shared IssueDetail surface;
  // everything else renders the detail pane its item-type renderer provides.
  const SelectedDetail =
    selected && !selected.issue_id
      ? getInboxItemRenderer("issue_notification").Detail
      : undefined;

  // Track the last key we actually resolved against the inbox list. Lets the
  // fallback effect distinguish "shared-link to a notification not in our
  // inbox" (never resolved → redirect to the issue page) from "item was in
  // our inbox and just got removed" (was resolved → stay on /inbox).
  const lastResolvedKeyRef = useRef<string>("");
  useEffect(() => {
    if (selected) lastResolvedKeyRef.current = selectedKey;
  }, [selected, selectedKey]);

  const setSelectedKey = useCallback((key: string) => {
    setSelectedKeyState(key);
    setSelectedConversationId("");
    const inboxPath = wsPaths.inbox();
    const url = key ? `${inboxPath}?issue=${key}` : inboxPath;
    replace(url);
  }, [replace, wsPaths]);

  // Open a conversation in the detail pane (like opening an issue), clearing
  // any notification selection. The floating chat window is intentionally not
  // involved — it is being deprecated in favour of this inline surface.
  const selectConversation = useCallback((id: string) => {
    setSelectedConversationId(id);
    setSelectedKeyState("");
    replace(`${wsPaths.inbox()}?conversation=${id}`);
  }, [replace, wsPaths]);

  // Shared inbox links (?issue=<id>) may point to notifications not in this
  // user's inbox (archived, or never received). Fall back to the issue page
  // so the URL still resolves to something meaningful. But if the key was
  // previously resolvable (e.g. the issue was just deleted in another tab
  // and `onInboxIssueDeleted` pruned the cache), the issue detail would 404
  // too — clear the selection and stay on /inbox instead.
  useEffect(() => {
    if (loading) return;
    if (!selectedKey) return;
    if (selected) return;
    if (lastResolvedKeyRef.current === selectedKey) {
      setSelectedKey("");
      return;
    }
    replace(wsPaths.issueDetail(selectedKey));
  }, [loading, selectedKey, selected, replace, wsPaths, setSelectedKey]);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_inbox_layout",
  });

  const isMobile = useIsMobile();
  const unreadCount = useInboxUnreadCount(wsId);

  const markReadMutation = useMarkInboxRead();
  const archiveMutation = useArchiveInbox();
  const markAllReadMutation = useMarkAllInboxRead();
  const archiveAllMutation = useArchiveAllInbox();
  const archiveAllReadMutation = useArchiveAllReadInbox();
  const archiveCompletedMutation = useArchiveCompletedInbox();


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
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_read_failed),
        ),
    });
  }, [selectedId, selectedRead, markReadMutate, t]);

  const handleSelect = (item: InboxItem) => {
    setSelectedKey(item.issue_id ?? item.id);
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
      setSelectedKey(next ? (next.issue_id ?? next.id) : "");
    }
    archiveMutation.mutate(id, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_failed),
        ),
    });
  };

  // Batch operations
  const handleMarkAllRead = () => {
    markAllReadMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.mark_all_read_failed),
        ),
    });
  };

  const handleArchiveAll = () => {
    setSelectedKey("");
    archiveAllMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_all_failed),
        ),
    });
  };

  const handleArchiveAllRead = () => {
    const readKeys = items.filter((i) => i.read).map((i) => i.issue_id ?? i.id);
    if (readKeys.includes(selectedKey)) setSelectedKey("");
    archiveAllReadMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_all_read_failed),
        ),
    });
  };

  const handleArchiveCompleted = () => {
    setSelectedKey("");
    archiveCompletedMutation.mutate(undefined, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.errors.archive_completed_failed),
        ),
    });
  };

  // -- Shared sub-components --------------------------------------------------

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex items-center gap-2">
        <h1 className="text-sm font-semibold">{t(($) => $.page.title)}</h1>
        {unreadCount > 0 && (
          <span className="text-xs text-muted-foreground">
            {unreadCount}
          </span>
        )}
      </div>
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
          <DropdownMenuItem onClick={handleMarkAllRead}>
            <CheckCheck className="h-4 w-4" />
            {t(($) => $.menu.mark_all_read)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={handleArchiveAll}>
            <Archive className="h-4 w-4" />
            {t(($) => $.menu.archive_all)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={handleArchiveAllRead}>
            <BookCheck className="h-4 w-4" />
            {t(($) => $.menu.archive_all_read)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={handleArchiveCompleted}>
            <ListChecks className="h-4 w-4" />
            {t(($) => $.menu.archive_completed)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </PageHeader>
  );

  const listBody = feed.length === 0 ? (
    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
      <p className="text-sm">{t(($) => $.list.empty)}</p>
    </div>
  ) : (
    <div>
      {feed.map((entry) => {
        const { Row } = getInboxItemRenderer(entry.kind);
        const isNotification = entry.kind === "issue_notification";
        return (
          <Row
            key={`${entry.kind}:${entry.id}`}
            entry={entry}
            isSelected={
              isNotification
                ? (entry.notification.issue_id ?? entry.notification.id) === selectedKey
                : entry.id === selectedConversationId
            }
            onSelect={() => {
              // Both kinds open into the same detail pane.
              if (isNotification) handleSelect(entry.notification);
              else selectConversation(entry.id);
            }}
            onArchive={() => {
              if (isNotification) handleArchive(entry.notification.id);
            }}
          />
        );
      })}
    </div>
  );

  const ConversationDetailComp = getInboxItemRenderer("conversation").Detail;
  const detailContent = selectedConversation && ConversationDetailComp ? (
    <ConversationDetailComp
      entry={conversationEntry(selectedConversation)}
      onArchive={() => {}}
    />
  ) : selected?.issue_id ? (
    // Key by issue_id (not inbox-item id): a new comment/reaction generates a
    // new inbox notification for the same issue, and the dedup helper picks the
    // newest one — keying on its id would remount IssueDetail on every event,
    // wiping the comment composer draft and resetting scroll position.
    <ErrorBoundary resetKeys={[selected.issue_id]}>
      <IssueDetail
        key={selected.issue_id}
        issueId={selected.issue_id}
        defaultSidebarOpen={false}
        layoutId="multica_inbox_issue_detail_layout"
        highlightCommentId={selected.details?.comment_id ?? undefined}
        onDelete={() => {
          // Issue deletion CASCADE-deletes the inbox item server-side, and the
          // issue:deleted WS event prunes it from the inbox cache. Just clear
          // the selection — calling archive here would 404 on a row that no
          // longer exists.
          setSelectedKey("");
        }}
        onDone={() => {
          handleArchive(selected.id);
        }}
      />
    </ErrorBoundary>
  ) : selected && SelectedDetail ? (
    <SelectedDetail
      entry={notificationEntry(selected)}
      onArchive={() => handleArchive(selected.id)}
    />
  ) : null;

  // -- Mobile layout: list / detail toggle -----------------------------------

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

    // Mobile: show detail full-screen when an item is selected
    if (selected || selectedConversation) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSelectedKey("")}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              {t(($) => $.page.back)}
            </Button>
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto">
            {detailContent}
          </div>
        </div>
      );
    }

    // Mobile: full-screen list
    return (
      <div className="flex flex-1 flex-col min-h-0">
        {listHeader}
        <div className="flex-1 min-h-0 overflow-y-auto">
          {listBody}
        </div>
      </div>
    );
  }

  // -- Desktop layout: resizable two-panel -----------------------------------

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
              {feed.length === 0
                ? t(($) => $.detail.empty)
                : t(($) => $.detail.select_prompt)}
            </p>
          </div>
        )}
      </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
