"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import {
  inboxListOptions,
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
import {
  useInboxFilterStore,
} from "@multica/core/inbox/inbox-filter-store";

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
  ArrowDown,
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
import { useTypeLabels } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";
import { useTimeAgo } from "./inbox-list-item-hooks";
import { InboxToolbar } from "./inbox-toolbar";
import { groupInboxItems } from "./inbox-grouping";
import { InboxGroupSection } from "./inbox-group-section";
import { InboxBulkBar } from "./inbox-bulk-bar";
import { InboxEmptyState, type EmptyStateType } from "./inbox-empty-state";
import { useT } from "../../i18n";

// Tablet breakpoint: < 1024px
function useIsTablet(): boolean {
  const [isTablet, setIsTablet] = useState(false);
  useEffect(() => {
    const check = () => setIsTablet(window.innerWidth < 1024);
    check();
    window.addEventListener("resize", check);
    return () => window.removeEventListener("resize", check);
  }, []);
  return isTablet;
}

export function InboxPage() {
  const { t } = useT("inbox");
  const { searchParams, replace } = useNavigation();
  const urlIssue = searchParams.get("issue") ?? "";
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlIssue);

  // Sync from URL when searchParams change
  useEffect(() => {
    setSelectedKeyState(urlIssue);
  }, [urlIssue]);

  const wsId = useWorkspaceId();
  const { data: rawItems = [], isLoading: loading } = useQuery(inboxListOptions(wsId));
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);

  // Filter store state
  const groupMode = useInboxFilterStore((s) => s.groupMode);
  const density = useInboxFilterStore((s) => s.density);
  const unreadOnly = useInboxFilterStore((s) => s.unreadOnly);
  const searchQuery = useInboxFilterStore((s) => s.searchQuery);
  const selectedIds = useInboxFilterStore((s) => s.selectedIds);
  const collapsedGroups = useInboxFilterStore((s) => s.collapsedGroups);
  const multiselectActive = useInboxFilterStore((s) => s.multiselectActive);
  const toggleSelect = useInboxFilterStore((s) => s.toggleSelect);
  const selectAll = useInboxFilterStore((s) => s.selectAll);
  const clearSelection = useInboxFilterStore((s) => s.clearSelection);
  const toggleGroupCollapse = useInboxFilterStore((s) => s.toggleGroupCollapse);
  const setSearchQuery = useInboxFilterStore((s) => s.setSearchQuery);

  // Apply filters: unreadOnly, searchQuery
  const filteredItems = useMemo(() => {
    let result = items;
    if (unreadOnly) {
      result = result.filter((i) => !i.read);
    }
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter(
        (i) =>
          i.title.toLowerCase().includes(q) ||
          (i.body?.toLowerCase().includes(q) ?? false),
      );
    }
    return result;
  }, [items, unreadOnly, searchQuery]);

  // Group filtered items
  const groups = useMemo(
    () => groupInboxItems(filteredItems, groupMode),
    [filteredItems, groupMode],
  );

  // Flatten groups for j/k navigation and selection lookup
  const flatItems = useMemo(
    () => groups.flatMap((g) => g.items),
    [groups],
  );

  const selected = flatItems.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;

  // Track the last key we actually resolved against the inbox list
  const lastResolvedKeyRef = useRef<string>("");
  useEffect(() => {
    if (selected) lastResolvedKeyRef.current = selectedKey;
  }, [selected, selectedKey]);

  const setSelectedKey = useCallback(
    (key: string) => {
      setSelectedKeyState(key);
      const inboxPath = wsPaths.inbox();
      const url = key ? `${inboxPath}?issue=${key}` : inboxPath;
      replace(url);
    },
    [replace, wsPaths],
  );

  // Fallback redirect for unresolvable keys
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
  const isTablet = useIsTablet();
  const unreadCount = useInboxUnreadCount(wsId);

  const markReadMutation = useMarkInboxRead();
  const archiveMutation = useArchiveInbox();
  const markAllReadMutation = useMarkAllInboxRead();
  const archiveAllMutation = useArchiveAllInbox();
  const archiveAllReadMutation = useArchiveAllReadInbox();
  const archiveCompletedMutation = useArchiveCompletedInbox();
  const timeAgo = useTimeAgo();
  const typeLabels = useTypeLabels();

  // Auto-mark-read when selected item is unread
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

  const handleArchive = useCallback(
    (id: string) => {
      const idx = items.findIndex((i) => i.id === id);
      const archived = idx >= 0 ? items[idx] : null;
      const wasSelected =
        !!archived && (archived.issue_id ?? archived.id) === selectedKey;
      if (wasSelected) {
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
    },
    [items, selectedKey, archiveMutation, setSelectedKey, t],
  );

  const handleMarkRead = useCallback(
    (id: string) => {
      markReadMutation.mutate(id, {
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.errors.mark_read_failed),
          ),
      });
    },
    [markReadMutation, t],
  );

  const handleOpenIssue = useCallback(
    (issueId: string) => {
      setSelectedKey(issueId);
    },
    [setSelectedKey],
  );

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

  // Bulk selection operations
  const handleBulkMarkRead = useCallback(() => {
    const ids = Array.from(selectedIds);
    for (const id of ids) {
      const item = flatItems.find((i) => (i.issue_id ?? i.id) === id);
      if (item && !item.read) {
        markReadMutation.mutate(item.id);
      }
    }
    clearSelection();
  }, [selectedIds, flatItems, markReadMutation, clearSelection]);

  const handleBulkArchive = useCallback(() => {
    const ids = Array.from(selectedIds);
    for (const id of ids) {
      const item = flatItems.find((i) => (i.issue_id ?? i.id) === id);
      if (item) {
        archiveMutation.mutate(item.id);
      }
    }
    if (ids.includes(selectedKey)) setSelectedKey("");
    clearSelection();
  }, [selectedIds, flatItems, archiveMutation, clearSelection, selectedKey, setSelectedKey]);

  // Group selection helpers
  const handleSelectAllInGroup = useCallback(
    (groupItems: InboxItem[]) => {
      const ids = groupItems.map((i) => i.issue_id ?? i.id);
      selectAll(ids);
    },
    [selectAll],
  );

  const handleDeselectAllInGroup = useCallback(
    (groupItems: InboxItem[]) => {
      const ids = groupItems.map((i) => i.issue_id ?? i.id);
      const next = new Set(selectedIds);
      for (const id of ids) next.delete(id);
      useInboxFilterStore.setState({ selectedIds: next });
    },
    [selectedIds],
  );

  // --- Real-time notification banner ---
  const [showNewNotifBanner, setShowNewNotifBanner] = useState(false);
  const prevItemCountRef = useRef(items.length);
  const listScrollRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const prevCount = prevItemCountRef.current;
    const newCount = items.length;
    if (newCount > prevCount && prevCount > 0) {
      // New items arrived while user was viewing the list
      const scrollTop = listScrollRef.current?.scrollTop ?? 0;
      if (scrollTop > 80) {
        setShowNewNotifBanner(true);
      }
    }
    prevItemCountRef.current = newCount;
  }, [items.length]);

  const handleScrollToTop = useCallback(() => {
    listScrollRef.current?.scrollTo({ top: 0, behavior: "smooth" });
    setShowNewNotifBanner(false);
  }, []);

  // --- Keyboard navigation ---
  const [keyFocusedIndex, setKeyFocusedIndex] = useState(-1);
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") {
        if (e.key === "Escape") {
          (e.target as HTMLElement).blur();
          setSearchQuery("");
        }
        return;
      }

      switch (e.key) {
        case "j":
        case "ArrowDown": {
          e.preventDefault();
          setKeyFocusedIndex((prev) => Math.min(prev + 1, flatItems.length - 1));
          break;
        }
        case "k":
        case "ArrowUp": {
          e.preventDefault();
          setKeyFocusedIndex((prev) => Math.max(prev - 1, 0));
          break;
        }
        case "e": {
          e.preventDefault();
          if (keyFocusedIndex >= 0 && keyFocusedIndex < flatItems.length) {
            const focused = flatItems[keyFocusedIndex];
            if (focused) handleArchive(focused.id);
          }
          break;
        }
        case "/": {
          e.preventDefault();
          searchInputRef.current?.focus();
          break;
        }
        case "Escape": {
          e.preventDefault();
          if (multiselectActive) {
            useInboxFilterStore.setState({ multiselectActive: false });
            clearSelection();
          } else {
            setSearchQuery("");
          }
          break;
        }
      }
    },
    [flatItems, keyFocusedIndex, handleArchive, searchInputRef, setSearchQuery, multiselectActive, clearSelection],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  // Sync keyboard focus to selection
  useEffect(() => {
    if (keyFocusedIndex >= 0 && keyFocusedIndex < flatItems.length) {
      const item = flatItems[keyFocusedIndex];
      if (item) handleSelect(item);
    }
  }, [keyFocusedIndex]); // eslint-disable-line react-hooks/exhaustive-deps

  // --- Determine empty state ---
  const emptyStateType: EmptyStateType | null = useMemo(() => {
    if (items.length === 0) return "empty";
    if (filteredItems.length === 0) {
      if (unreadOnly && items.every((i) => i.read)) return "no_unread";
      if (searchQuery.trim()) return "no_search_results";
      return "no_filter_results";
    }
    return null;
  }, [items, filteredItems, unreadOnly, searchQuery]);

  // --- Shared sub-components ---

  const listHeader = (
    <PageHeader className="justify-between shrink-0">
      <div className="flex items-center gap-2">
        <h1 className="text-sm font-semibold">{t(($) => $.page.title)}</h1>
        {unreadCount > 0 && (
          <span className="rounded-full bg-brand/10 px-1.5 py-0.5 text-[10px] font-semibold text-brand">
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

  const listBody = (
    <>
      {/* Real-time notification banner */}
      {showNewNotifBanner && (
        <button
          type="button"
          onClick={handleScrollToTop}
          className="flex w-full items-center justify-center gap-1.5 border-b bg-brand/10 py-1.5 text-xs font-medium text-brand transition-colors hover:bg-brand/20"
        >
          <ArrowDown className="h-3 w-3" />
          {t(($) => $.realtime.new_notifications)}
        </button>
      )}

      {emptyStateType ? (
        <InboxEmptyState type={emptyStateType} />
      ) : (
        <div>
          {groups.map((group) => {
            const isCollapsed = collapsedGroups.has(group.key);
            return (
              <InboxGroupSection
                key={group.key}
                group={group}
                isCollapsed={isCollapsed}
                density={density}
                selectedKey={selectedKey}
                selectedIds={selectedIds}
                showCheckbox={multiselectActive}
                onToggleCollapse={() => toggleGroupCollapse(group.key)}
                onSelectAll={() => handleSelectAllInGroup(group.items)}
                onDeselectAll={() => handleDeselectAllInGroup(group.items)}
                onSelectItem={handleSelect}
                onArchiveItem={handleArchive}
                onMarkReadItem={handleMarkRead}
                onOpenIssue={handleOpenIssue}
                onToggleCheck={toggleSelect}
              />
            );
          })}
        </div>
      )}

      {/* Bulk operation bar */}
      <InboxBulkBar
        selectedCount={selectedIds.size}
        onMarkReadSelected={handleBulkMarkRead}
        onArchiveSelected={handleBulkArchive}
        onClearSelection={clearSelection}
      />
    </>
  );

  const detailContent = selected?.issue_id ? (
    <ErrorBoundary resetKeys={[selected.issue_id]}>
      <IssueDetail
        key={selected.issue_id}
        issueId={selected.issue_id}
        defaultSidebarOpen={false}
        layoutId="multica_inbox_issue_detail_layout"
        highlightCommentId={selected.details?.comment_id ?? undefined}
        onDelete={() => {
          setSelectedKey("");
        }}
        onDone={() => {
          handleArchive(selected.id);
        }}
      />
    </ErrorBoundary>
  ) : selected ? (
    <div className="p-6">
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
          <p className="mt-1 whitespace-pre-wrap text-sm">
            {selected.details.original_prompt}
          </p>
        </div>
      )}
      <div className="mt-4 flex gap-2">
        {selected.type === "quick_create_failed" && (
          <Button
            size="sm"
            onClick={() => {
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

  // --- Loading state ---
  if (loading) {
    if (isMobile) {
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

    return (
      <ResizablePanelGroup
        orientation="horizontal"
        className="flex-1 min-h-0"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        <ResizablePanel
          id="list"
          defaultSize={320}
          minSize={240}
          maxSize={480}
          groupResizeBehavior="preserve-pixel-size"
        >
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

  // --- Mobile layout ---
  if (isMobile) {
    if (selected) {
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
          <div className="flex-1 min-h-0 overflow-y-auto">{detailContent}</div>
        </div>
      );
    }

    return (
      <div className="flex flex-1 flex-col min-h-0">
        {listHeader}
        <InboxToolbar searchInputRef={searchInputRef} />
        <div ref={listScrollRef} className="flex-1 min-h-0 overflow-y-auto">
          {listBody}
        </div>
      </div>
    );
  }

  // --- Tablet layout: single-panel with slide-over detail ---
  if (isTablet) {
    if (selected) {
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
          <div className="flex-1 min-h-0 overflow-y-auto">{detailContent}</div>
        </div>
      );
    }

    return (
      <div className="flex flex-1 flex-col min-h-0">
        {listHeader}
        <InboxToolbar searchInputRef={searchInputRef} />
        <div ref={listScrollRef} className="flex-1 min-h-0 overflow-y-auto">
          {listBody}
        </div>
      </div>
    );
  }

  // --- Desktop layout: resizable two-panel ---
  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel
        id="list"
        defaultSize={320}
        minSize={240}
        maxSize={480}
        groupResizeBehavior="preserve-pixel-size"
      >
        <div className="flex flex-col border-r h-full">
          {listHeader}
          <InboxToolbar searchInputRef={searchInputRef} />
          <div ref={listScrollRef} className="flex-1 min-h-0 overflow-y-auto">
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
                {items.length === 0
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
