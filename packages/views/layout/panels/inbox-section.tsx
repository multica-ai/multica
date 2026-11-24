"use client";

import { memo, type ReactNode, type Ref, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useReducedMotion } from "@multica/core/appearance";
import { AlarmClock, Archive, ArchiveRestore, BellOff, BookCheck, CheckCheck, CircleDot, Filter, ListChecks, MoreHorizontal } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { inboxFilterCount, inboxTypeGroup, useInboxFilterStore } from "@multica/core/inbox/filter-store";
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
  useMarkInboxUnread,
} from "@multica/core/inbox/mutations";
import { deduplicateInboxItems, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { inboxRowTime, inboxSnoozeReturnedAt } from "@multica/core/inbox/snooze";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useInboxSelectionStore, type LastInboxCleared } from "@multica/core/inbox/selection-store";
import { inboxRowKey, nextInboxRow } from "@multica/core/inbox/open-row";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem, SnoozedInboxIssue } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { useLocale, useT } from "../../i18n";
import { INBOX_TYPE_ICONS } from "../../inbox/components/inbox-filter-options";
import { getInboxDisplayTitle, inboxRowHref, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { inboxTabFromParam, inboxTabHref, type InboxTab, type InboxView } from "../../inbox/components/inbox-view";
import { InboxTabs } from "../../inbox/components/inbox-tabs";
import { InboxArchiveAllDialog } from "../../inbox/components/inbox-archive-all-dialog";
import { InboxContextMenuProvider, useInboxContextMenu } from "../../inbox/components/inbox-context-menu";
import type { InboxRowActions } from "../../inbox/components/inbox-item-actions";
import { InboxDeleteDialog } from "../../inbox/components/inbox-delete-dialog";
import { formatSnoozeTime } from "../../inbox/components/inbox-snooze-time";
import {
  useArchivedInboxRows,
  useInboxTabPrefetch,
  useSnoozedInboxRows,
} from "../../inbox/components/use-inbox-tab-rows";
import { useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { InboxActorAvatar, InboxActorLine } from "../../inbox/components/inbox-actor";
import { InboxFilterMenu } from "../../inbox/components/inbox-filter-menu";
import { InboxSnoozeMenu } from "../../inbox/components/inbox-snooze-menu";
import { useInboxSnooze } from "../../inbox/components/use-inbox-snooze";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useInboxIssuePrefetch } from "../../inbox/components/use-inbox-issue-prefetch";
import { useTimeAgo } from "../../inbox/components/inbox-list-item";
import { useInboxUnsubscribable, useUnsubscribeArchive } from "../../inbox/components/use-unsubscribe-archive";
import {
  clearedLabelForItem,
  collectBatchArchiveIds,
  useArchiveInboxBatchWithUndo,
  useArchiveInboxWithUndo,
  useLeaveInboxRow,
  useRestoreInbox,
} from "../../inbox/components/use-leave-inbox-row";
import { useKeptInboxRowText } from "../../inbox/components/inbox-kept-actions";
import { StatusIcon } from "../../issues/components";
import { useStatusLabel } from "../../issues/utils/status-label";
import {
  PANEL_ROW_CLASS,
  PanelLink,
  PanelList,
  PanelSection,
  panelRowAnchor,
  panelRowStateClass,
  usePanelScrollRef,
} from "../panel-nav";
import { InboxSectionSettings, SECTION_SETTINGS_REVEAL_CLASS } from "./sidebar-settings";
import {
  ISSUE_ROW_LINK_CLASS,
  IssueAgentBadge,
  SIDEBAR_SHIMMER_CLASS,
  TREE_ROW_BUTTON_CLASS,
  useIssueAgentState,
} from "./sidebar-tree";

/** The Inbox rows' place in the clicked-issue record, apart from every sidebar source. */
export const INBOX_LIST = "inbox";

/**
 * The Inbox section's rows: one per issue, newest first, read and unread
 * alike, under the Inbox filters. A row leaves only when archived, by hand or
 * by its issue reaching Done; the open one stays in place until the user
 * moves on (`keepOpenInboxRow`). Lifted out of the section so the panel knows
 * which issues it shows. `openKey`: see `useFilteredInboxRows`.
 */
export function useInboxSectionRows(wsId: string | undefined, openKey: string | null) {
  const list = useFilteredInboxRows(wsId ?? "", openKey);
  const { rows } = list;
  const limit = useSidebarTreeStore((s) => s.inboxSettings.limit);
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const [expandedWsId, setExpandedWsId] = useState<string | null>(null);
  const expanded = expandedWsId === (wsId ?? null);
  // Newer notifications must not push the open row past the limit: the row
  // would leave, and the open issue's highlight with it.
  const displayed = useMemo(() => {
    if (expanded) return rows;
    const open = openKey ? rows.findIndex((item) => (item.issue_id ?? item.id) === openKey) : -1;
    return open >= limit ? [...rows.slice(0, limit - 1), rows[open]!] : rows.slice(0, limit);
  }, [expanded, rows, limit, openKey]);

  return {
    list,
    rows,
    displayed,
    expanded,
    limit,
    /** A collapsed section shows no rows, so it lights none. */
    collapsed,
    setExpanded: (next: boolean) => setExpandedWsId(next ? (wsId ?? null) : null),
  };
}

type InboxSectionRows = ReturnType<typeof useInboxSectionRows>;

/**
 * The Work panel's Inbox: its three lists as tabs in the header, the live
 * Inbox, the issues snoozed out of it, and the archive. Every row opens on
 * the Inbox page, on its tab.
 */
export function InboxSection({
  wsId,
  rows,
  pathname,
}: {
  wsId: string;
  rows: InboxSectionRows;
  pathname: string;
}) {
  const { t } = useT("layout");
  const { t: tInbox } = useT("inbox");
  const p = useWorkspacePaths();
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setInboxCollapsed);
  const showCount = useSidebarTreeStore((s) => s.inboxSettings.showCount);
  const compact = useSidebarTreeStore((s) => s.inboxSettings.compact);
  const unread = useInboxUnreadCount(wsId);
  const archive = useArchiveInboxWithUndo();
  const restoreInbox = useRestoreInbox();
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
  const leave = useLeaveInboxRow();
  const leaveRow = useInboxSelectionStore((s) => s.leaveRow);
  const recordTransition = useInboxSelectionStore((s) => s.recordTransition);
  const unsubscribable = useInboxUnsubscribable(wsId);
  const unsubscribeArchive = useUnsubscribeArchive();
  const { snooze } = useInboxSnooze();
  const [deleteTarget, setDeleteTarget] = useState<{ item: InboxItem; view: InboxView } | null>(null);
  const [retainedDeleteTarget, setRetainedDeleteTarget] = useState<{ item: InboxItem; view: InboxView } | null>(null);
  if (deleteTarget !== null && retainedDeleteTarget?.item.id !== deleteTarget.item.id) {
    setRetainedDeleteTarget(deleteTarget);
  }
  const activeDeleteTarget = deleteTarget ?? retainedDeleteTarget;
  const { searchParams, replace } = useNavigation();
  const isCompact = useIsCompact();
  const { list, rows: all, displayed, expanded, limit, setExpanded } = rows;
  const keptText = useKeptInboxRowText(wsId);
  const keptAgents = useIssueAgentState(
    wsId,
    list.kept?.reason === "archived" ? (list.kept.item.issue_id ?? "") : "",
  );
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
  // Follow the Inbox page's URL whenever navigation arrives at a new `/inbox`
  // state, while letting the user switch sidebar tabs freely without closing
  // an issue/notification currently open on the right.
  const onInboxPage = pathname === p.inbox();
  const pageTab = inboxTabFromParam(searchParams.get("view"));
  // What the Inbox page has open, keyed as its tab groups rows.
  const pageKey = onInboxPage ? searchParams.get("issue") : null;
  const inboxUrl = onInboxPage ? `${pageTab}:${pageKey ?? ""}` : null;
  const storedTab = useInboxSelectionStore((s) => s.sectionTabByWorkspace[wsId] ?? "inbox");
  const syncedUrl = useInboxSelectionStore((s) => s.sectionTabSyncedUrlByWorkspace[wsId]);
  const setSectionTab = useInboxSelectionStore((s) => s.setSectionTab);
  const clearSectionTabSync = useInboxSelectionStore((s) => s.clearSectionTabSync);
  const tab = onInboxPage && syncedUrl !== inboxUrl ? pageTab : storedTab;
  useEffect(() => {
    if (onInboxPage && inboxUrl !== null) {
      if (syncedUrl !== inboxUrl) setSectionTab(wsId, pageTab, inboxUrl);
    } else if (syncedUrl !== undefined) {
      clearSectionTabSync(wsId);
    }
  }, [onInboxPage, inboxUrl, syncedUrl, pageTab, setSectionTab, clearSectionTabSync, wsId]);
  const selectTab = (next: InboxTab) => {
    // The open tab is the section's title: selecting it again folds the
    // section, as a click on another section's title does.
    if (next === tab) {
      setCollapsed(!collapsed);
      return;
    }
    setSectionTab(wsId, next, inboxUrl ?? undefined);
    if (collapsed) setCollapsed(false);
    // Keep an empty `/inbox` canvas in sync with the tab, but never dismiss an
    // open issue/notification just because the user browsed another tab.
    if (onInboxPage && !pageKey && next !== pageTab) replace(inboxTabHref(p, next));
  };
  const openRowKey = pageTab === "inbox" ? pageKey : null;
  const snoozed = useSnoozedInboxRows(wsId);
  const archived = useArchivedInboxRows(wsId, tab === "archived" && !collapsed);
  const prefetchTab = useInboxTabPrefetch(wsId, archived);
  const leaveIfOpen = (item: InboxItem, cleared: LastInboxCleared) => {
    const key = inboxRowKey(item);
    if (key === openRowKey) leave(key, nextInboxRow(all, key), cleared);
  };
  const prefetchRow = useInboxIssuePrefetch(wsId, collapsed ? [] : all, (item) => inboxRowHref(p, item), openRowKey);
  // What arrived on each issue while it was snoozed, for the rows back from one.
  const updatesWhileSnoozed = useMemo(() => {
    const counts = new Map<string, number>();
    for (const item of list.notifications) {
      if (!item.issue_id || !item.snoozed_at) continue;
      if (new Date(item.created_at).getTime() <= new Date(item.snoozed_at).getTime()) continue;
      counts.set(item.issue_id, (counts.get(item.issue_id) ?? 0) + 1);
    }
    return counts;
  }, [list.notifications]);

  const actionsRef = useRef<HTMLButtonElement>(null);
  const [filterOpen, setFilterOpen] = useState(false);
  // The filter is the open tab's: a switch, from here or the Inbox page's
  // URL, closes it rather than reopening it on the next tab.
  const [filterTab, setFilterTab] = useState(tab);
  if (filterTab !== tab) {
    setFilterTab(tab);
    setFilterOpen(false);
  }

  const scrollRef = usePanelScrollRef();
  // Rows are memoized and re-render only when their own props change, not on
  // every navigation that re-renders this section (MUL-9609); the handlers
  // keep one identity and read this render's state when they run.
  const archivedOpenKey = pageTab === "archived" ? pageKey : null;
  const restoreArchivedItem = (item: InboxItem) => {
    const key = inboxRowKey(item);
    if (key === archivedOpenKey) {
      leaveRow(wsId, key);
      const next = nextInboxRow(archived.items, key);
      const nextKey = next ? inboxRowKey(next) : "";
      recordTransition(wsId, nextKey, "next");
      replace(inboxTabHref(p, "archived", nextKey || null));
    }
    void restoreInbox(item.id);
  };
  const markItemRead = (id: string) => {
    useInboxSelectionStore.getState().clearManualUnread(wsId, id);
    markRead.mutate(id, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message ? err.message : tInbox(($) => $.errors.mark_read_failed),
        ),
    });
  };
  const markItemUnread = (item: InboxItem) => {
    const key = inboxRowKey(item);
    if (key === openRowKey) useInboxSelectionStore.getState().markManualUnread(wsId, item.id);
    markUnread.mutate(item.id, {
      onError: (err) => {
        useInboxSelectionStore.getState().clearManualUnread(wsId, item.id);
        toast.error(
          err instanceof Error && err.message ? err.message : tInbox(($) => $.errors.mark_unread_failed),
        );
      },
    });
  };
  const contextView: InboxView = tab === "archived" ? "archived" : "inbox";
  const latest = {
    prefetch: (item: InboxItem) => void prefetchRow(item),
    markRead: (item: InboxItem) => markItemRead(item.id),
    markUnread: (item: InboxItem) => markItemUnread(item),
    archive: (item: InboxItem) => {
      const key = inboxRowKey(item);
      archive(item.id, key);
      leaveIfOpen(item, {
        kind: "archived",
        itemId: item.id,
        key,
        label: clearedLabelForItem(item),
        href: inboxRowHref(p, item),
        clearedAt: Date.now(),
      });
    },
    unsubscribe: (item: InboxItem) => {
      const key = inboxRowKey(item);
      unsubscribeArchive({ id: item.id, issueId: item.issue_id! });
      leaveIfOpen(item, {
        kind: "unsubscribed",
        itemId: item.id,
        issueId: item.issue_id!,
        key,
        label: clearedLabelForItem(item),
        href: inboxRowHref(p, item),
        undoable: true,
        clearedAt: Date.now(),
      });
    },
    snooze: (item: InboxItem, until: Date) => {
      const key = inboxRowKey(item);
      snooze(item, until);
      leaveIfOpen(item, {
        kind: "snoozed",
        itemId: item.id,
        key,
        label: clearedLabelForItem(item),
        href: inboxRowHref(p, item),
        until: until.toISOString(),
        wasRead: item.read === true,
        clearedAt: Date.now(),
      });
    },
    contextMarkRead: (id: string) => markItemRead(id),
    contextMarkUnread: (id: string) => {
      const target = all.find((item) => item.id === id);
      if (target) markItemUnread(target);
      else {
        markUnread.mutate(id, {
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message ? err.message : tInbox(($) => $.errors.mark_unread_failed),
            ),
        });
      }
    },
    contextAction: (id: string) => {
      if (contextView === "archived") {
        const target = archived.items.find((item) => item.id === id);
        if (target) restoreArchivedItem(target);
        return;
      }
      const target = all.find((item) => item.id === id);
      if (target) latestRef.current.archive(target);
    },
    contextDelete: (id: string) => {
      const source = contextView === "archived" ? archived.items : all;
      const target = source.find((item) => item.id === id);
      if (target) setDeleteTarget({ item: target, view: contextView });
    },
    contextUnsubscribeArchive: (id: string) => {
      const target = all.find((item) => item.id === id);
      if (target?.issue_id) latestRef.current.unsubscribe(target);
    },
    canUnsubscribe: unsubscribable,
  };
  const latestRef = useRef(latest);
  useLayoutEffect(() => {
    latestRef.current = latest;
  });
  const handlers = useMemo<InboxRowHandlers>(() => ({
    prefetch: (item) => latestRef.current.prefetch(item),
    markRead: (item) => latestRef.current.markRead(item),
    markUnread: (item) => latestRef.current.markUnread(item),
    archive: (item) => latestRef.current.archive(item),
    unsubscribe: (item) => latestRef.current.unsubscribe(item),
    snooze: (item, until) => latestRef.current.snooze(item, until),
  }), []);
  // Keep one stable identity for the context menu provider too: a fresh object
  // every render would churn the context value and re-render every memoized row.
  const contextMenuActions = useMemo<InboxRowActions>(() => ({
    onMarkRead: (id) => latestRef.current.contextMarkRead(id),
    onMarkUnread: (id) => latestRef.current.contextMarkUnread(id),
    onAction: (id) => latestRef.current.contextAction(id),
    onDelete: (id) => latestRef.current.contextDelete(id),
    onUnsubscribeArchive: (id) => latestRef.current.contextUnsubscribeArchive(id),
    canUnsubscribe: (item) => latestRef.current.canUnsubscribe(item),
  }), []);
  // Server-side `archive-all` ignores client filters and leaves snoozed issues
  // alone, so the confirmation dialog counts every unfiltered active row group.
  const totalActiveCount = useMemo(
    () => deduplicateInboxItems(list.notifications).length,
    [list.notifications],
  );
  const renderRow = (item: InboxItem, measure?: RowMeasure) => (
    <InboxRow
      key={item.id}
      item={item}
      // Two props rather than the object: a fresh object every render would
      // defeat the row's memo (the measuring ref keeps one identity).
      measureIndex={measure?.index}
      measureRef={measure?.ref}
      href={inboxRowHref(p, item)}
      active={!!openRowKey && inboxRowKey(item) === openRowKey}
      compact={compact}
      kept={list.kept?.item.id === item.id ? keptText.line(list.kept, keptAgents) : null}
      settled={list.kept?.item.id === item.id && list.kept.reason !== "filtered"}
      updatesWhileSnoozed={item.issue_id ? (updatesWhileSnoozed.get(item.issue_id) ?? 0) : 0}
      unsubscribable={!!item.issue_id && unsubscribable(item)}
      handlers={handlers}
    />
  );

  // Every tab shows rows or a note once it has loaded; until then it shows nothing.
  const tabLoading =
    tab === "inbox"
      ? displayed.length === 0 && !list.filtered && list.load === "loading"
      : tab === "snoozed"
        ? snoozed.rows.length === 0 && snoozed.load === "loading"
        : archived.items.length === 0 && !archived.query.isError && !archived.query.isSuccess;

  return (
    <PanelSection
      label={t(($) => $.nav.inbox)}
      heading={
        <InboxTabs
          tab={tab}
          onSelect={selectTab}
          onPrefetch={prefetchTab}
          unread={showCount ? unread : undefined}
          snoozed={snoozed.rows.length}
          expanded={!collapsed}
        />
      }
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <div className="relative flex items-center gap-1">
          {tab !== "snoozed" && (
            <InboxFilterMenu
              wsId={wsId}
              items={tab === "archived" ? archived.items : list.notifications}
              priorityFilterSupport={tab === "archived" ? "supported" : list.priorityFilterSupport}
              issueFieldsSupport={tab === "archived" ? archived.issueFieldsSupport : list.issueFieldsSupport}
              activeIssueIds={list.activeIssueIds}
              archived={tab === "archived"}
              sectionTrigger={{
                className: cn(TREE_ROW_BUTTON_CLASS, "text-muted-foreground"),
                // Shown only while filters are set: a filtered list says so
                // even while the section is not hovered.
                activeClassName: "w-auto gap-0.5 px-1 text-brand hover:text-brand",
              }}
              opener={{ open: filterOpen, onClose: () => setFilterOpen(false), anchor: actionsRef }}
            />
          )}
          <InboxSectionSettings className={SECTION_SETTINGS_REVEAL_CLASS} />
          {tab !== "snoozed" && (
            <InboxSectionActions
              wsId={wsId}
              rows={all}
              totalActiveCount={totalActiveCount}
              openRowKey={openRowKey}
              bulk={tab === "inbox"}
              triggerRef={actionsRef}
              filterOpen={filterOpen}
              onOpenFilter={() => setFilterOpen(true)}
              className={SECTION_SETTINGS_REVEAL_CLASS}
            />
          )}
        </div>
      }
    >
      <InboxTabBody tab={tab} loading={tabLoading}>
        <InboxContextMenuProvider view={contextView} actions={contextMenuActions}>
        {tab === "snoozed" ? (
          <SnoozedSectionList
            wsId={wsId}
            list={snoozed}
            limit={limit}
            openKey={pageTab === "snoozed" ? pageKey : null}
          />
        ) : tab === "archived" ? (
          <ArchivedSectionList
            wsId={wsId}
            archived={archived}
            limit={limit}
            openKey={archivedOpenKey}
          />
        ) : displayed.length > 0 ? (
          displayed.length > VIRTUALIZE_AFTER && scrollRef ? (
            <VirtualInboxList items={displayed} compact={compact} renderRow={renderRow} />
          ) : (
            <PanelList data-sidebar-list={INBOX_LIST} className={compact ? undefined : "space-y-0.5"}>
              <AnimatePresence initial={false}>
                {displayed.map((item) => renderRow(item))}
              </AnimatePresence>
            </PanelList>
          )
        ) : list.load === "failed" ? (
          // A list that never loaded is not an empty Inbox: say so and offer
          // another try rather than "No notifications".
          <div role="alert" className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
            <span className="min-w-0 flex-1 truncate">{tInbox(($) => $.errors.load_failed)}</span>
            <button
              type="button"
              disabled={list.retrying}
              className="shrink-0 rounded-md px-1 font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50"
              onClick={list.retry}
            >
              {tInbox(($) => $.list.retry)}
            </button>
          </div>
        ) : list.filtered ? (
          <div className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
            <span className="min-w-0 flex-1 truncate">{tInbox(($) => $.filters.empty)}</span>
            <button
              type="button"
              className="shrink-0 rounded-md px-1 font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-ring"
              onClick={() => clearFilters(wsId)}
            >
              {tInbox(($) => $.filters.clear)}
            </button>
          </div>
        ) : list.load === "loaded" ? (
          <p className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
            {t(($) => $.panels.work.inbox_empty)}
          </p>
        ) : null}
        </InboxContextMenuProvider>
        {activeDeleteTarget && (
          <InboxDeleteDialog
            key={activeDeleteTarget.item.id}
            open={deleteTarget !== null}
            item={activeDeleteTarget.item}
            view={activeDeleteTarget.view}
            onClose={() => setDeleteTarget(null)}
            onOpenChangeComplete={(nextOpen) => {
              if (!nextOpen) {
                setRetainedDeleteTarget((prev) =>
                  prev?.item.id === activeDeleteTarget.item.id ? null : prev,
                );
              }
            }}
            onDeleted={() => {
              const { item: deleted, view: deletedView } = activeDeleteTarget;
              const key = inboxRowKey(deleted);
              if (deletedView === "archived") {
                if (key === archivedOpenKey) {
                  leaveRow(wsId, key);
                  const next = nextInboxRow(archived.items, key);
                  const nextKey = next ? inboxRowKey(next) : "";
                  recordTransition(wsId, nextKey, "next");
                  replace(inboxTabHref(p, "archived", nextKey || null));
                }
              } else if (key === openRowKey) {
                leave(key, nextInboxRow(all, key));
              }
              toast.success(tInbox(($) => $.toasts.deleted));
            }}
          />
        )}
        {tab === "inbox" && all.length > limit && (
          <button
            type="button"
            aria-expanded={expanded}
            className={cn(PANEL_ROW_CLASS, panelRowStateClass(false), "pl-[2.375rem] text-caption")}
            onClick={() => setExpanded(!expanded)}
          >
            {expanded
              ? t(($) => $.sidebar.show_fewer_pins)
              : t(($) => $.sidebar.show_more_pins, { count: all.length - limit })}
          </button>
        )}
        {/* The sidebar sits behind a drawer on a phone, so the full list keeps its page there. */}
        {isCompact && (
          <PanelList>
            <PanelLink
              href={inboxTabHref(p, tab)}
              icon={<span className="size-4" aria-hidden />}
              label={t(($) => $.panels.work.inbox_all)}
              active={onInboxPage && !pageKey && pageTab === tab}
              className="text-caption"
            />
          </PanelList>
        )}
      </InboxTabBody>
    </PanelSection>
  );
}

/**
 * The open tab's list. A switch fades the old rows out and the new ones in;
 * while the next tab loads the list keeps the height it had, then eases to
 * the new one, so the sections below it do not jump.
 */
function InboxTabBody({ tab, loading, children }: { tab: InboxTab; loading: boolean; children: ReactNode }) {
  const reduceMotion = useReducedMotion();
  const ref = useRef<HTMLDivElement>(null);
  // The height the list keeps from the switch until the next tab's rows are in.
  const [held, setHeld] = useState<number | null>(null);
  // The tab whose rows are in. A tab's rows mount once the previous rows have
  // faded out, or at once when none were in (that tab was still loading).
  const [shown, setShown] = useState<InboxTab | null>(loading ? null : tab);
  const switchedTo = useRef(tab);

  useLayoutEffect(() => {
    if (switchedTo.current === tab) return;
    switchedTo.current = tab;
    if (!ref.current) return;
    // Measured before paint, while the old rows are still in.
    setHeld(ref.current.offsetHeight);
  }, [tab, reduceMotion]);

  if (reduceMotion) return <>{children}</>;
  const holding = held !== null && (shown !== tab || loading);
  return (
    <motion.div
      ref={ref}
      data-inbox-body
      initial={false}
      animate={{ height: holding ? held : "auto" }}
      transition={{ duration: holding ? 0 : UI_MOTION_DURATION.standard, ease: UI_EASE_OUT }}
      onAnimationComplete={() => {
        if (!holding) setHeld(null);
      }}
      className={cn(held !== null && "overflow-hidden")}
    >
      <AnimatePresence mode="wait" initial={false}>
        {!loading && (
          <motion.div
            key={tab}
            ref={(node) => {
              if (node) setShown(tab);
            }}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1, transition: { duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT } }}
            exit={{ opacity: 0, transition: { duration: UI_MOTION_DURATION.micro, ease: UI_EASE_IN } }}
          >
            {children}
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

/**
 * Past this many rows (only reachable through "Show N more") the section
 * mounts just the rows near the panel's viewport. Every mounted row
 * re-renders whenever the open issue changes, so a fully mounted list of
 * ~1,000 notifications made each switch cost seconds.
 */
const VIRTUALIZE_AFTER = 50;

/** Estimated row heights and the gaps `space-y-px` / `space-y-0.5` put between rows. */
const ROW_ESTIMATE = { compact: 32, full: 50 } as const;
const ROW_GAP = { compact: 1, full: 2 } as const;

/** What a virtualized row hands its `<li>` so the list can measure it. */
interface RowMeasure {
  index: number;
  ref: Ref<HTMLLIElement>;
}

/**
 * The Inbox rows, windowed inside the Work panel's own scroll container. The
 * list stays in normal flow: padding stands in for the rows above and below
 * the window, so the sections after it keep their place.
 */
function VirtualInboxList({
  items,
  compact,
  renderRow,
}: {
  items: readonly InboxItem[];
  compact: boolean;
  renderRow: (item: InboxItem, measure: RowMeasure) => ReactNode;
}) {
  const scrollRef = usePanelScrollRef()!;
  const [list, setList] = useState<HTMLUListElement | null>(null);
  // Where the list starts within the scroll container's content. The sections
  // above it (Pinned) move it as they collapse, expand, mount or unmount.
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!list || !scroller) return;
    const measure = () =>
      setScrollMargin(list.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop);
    const resize = new ResizeObserver(measure);
    const observe = () => {
      resize.disconnect();
      resize.observe(scroller);
      for (const child of scroller.children) {
        if (child.contains(list)) break;
        resize.observe(child);
      }
      measure();
    };
    observe();
    const mutations = new MutationObserver(observe);
    mutations.observe(scroller, { childList: true });
    return () => {
      mutations.disconnect();
      resize.disconnect();
    };
  }, [list, scrollRef]);

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => (compact ? ROW_ESTIMATE.compact : ROW_ESTIMATE.full),
    gap: compact ? ROW_GAP.compact : ROW_GAP.full,
    getItemKey: (index) => items[index]!.id,
    scrollMargin,
    // Rows past the viewport stay mounted, so Tab can always reach the next one.
    overscan: 10,
  });
  const virtualRows = virtualizer.getVirtualItems();
  const first = virtualRows[0];
  const last = virtualRows[virtualRows.length - 1];
  const paddingTop = first ? first.start - scrollMargin : 0;
  const paddingBottom = last ? virtualizer.getTotalSize() - (last.end - scrollMargin) : 0;

  return (
    <PanelList
      ref={setList}
      data-sidebar-list={INBOX_LIST}
      className={compact ? undefined : "space-y-0.5"}
      style={{ paddingTop, paddingBottom }}
    >
      {virtualRows.map((row) => renderRow(items[row.index]!, { index: row.index, ref: virtualizer.measureElement }))}
    </PanelList>
  );
}

/**
 * The section's ⋯ menu: the bulk actions on the Inbox tab, and on every tab
 * the section's display settings.
 */
function InboxSectionActions({
  wsId,
  rows,
  totalActiveCount,
  openRowKey,
  bulk,
  triggerRef,
  filterOpen,
  onOpenFilter,
  className,
}: {
  wsId: string;
  rows: readonly InboxItem[];
  totalActiveCount: number;
  openRowKey: string | null;
  /** Offers the bulk actions, which act on the live Inbox. */
  bulk: boolean;
  triggerRef: Ref<HTMLButtonElement>;
  /** The filter hangs from this menu's button, which stays shown meanwhile. */
  filterOpen: boolean;
  onOpenFilter: () => void;
  className?: string;
}) {
  const { t } = useT("layout");
  const { t: tInbox } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const qc = useQueryClient();
  const leave = useLeaveInboxRow();
  const archiveWithUndo = useArchiveInboxBatchWithUndo();
  const { categoryOf } = useIssueStatuses(wsId);
  // A batch archive is the user's own Inbox action: when it takes the open
  // notification, that one moves on to the next row the batch leaves.
  const archiveBatch = (
    archive: () => Promise<{ count: number }>,
    archives: (item: InboxItem) => boolean,
    failed: string,
  ) => {
    const open = openRowKey ? rows.find((item) => inboxRowKey(item) === openRowKey) : undefined;
    const takesOpen = !!open && archives(open);
    const takenIds = takesOpen ? collectBatchArchiveIds(qc, wsId, archives) : [];
    void archiveWithUndo(archive, archives, failed, takesOpen ? openRowKey : null);
    if (takesOpen) {
      leave(
        openRowKey!,
        nextInboxRow(rows, openRowKey!, archives),
        takenIds.length > 0
          ? {
              kind: "batch_archived",
              count: takenIds.length,
              itemIds: takenIds,
              key: openRowKey,
              clearedAt: Date.now(),
            }
          : undefined,
      );
    }
  };
  const isRead = (item: InboxItem) => item.read === true;
  const isFinished = (item: InboxItem) => {
    const category = item.issue_status ? categoryOf(item.issue_status) : null;
    return category === "done" || category === "closed";
  };
  const markAllRead = useMarkAllInboxRead();
  const archiveAll = useArchiveAllInbox();
  const archiveAllRead = useArchiveAllReadInbox();
  const archiveCompleted = useArchiveCompletedInbox();
  const [archiveAllConfirmOpen, setArchiveAllConfirmOpen] = useState(false);
  const label = t(($) => $.panels.work.inbox_actions);
  const onError = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);

  return (
    <>
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            ref={triggerRef}
            type="button"
            aria-label={label}
            className={cn(
              TREE_ROW_BUTTON_CLASS,
              // The slot of the other sections' create button.
              "size-[var(--button-height-xs)]",
              "text-muted-foreground data-popup-open:bg-surface-selected data-popup-open:text-foreground data-popup-open:opacity-100",
              className,
              filterOpen && "bg-surface-selected text-foreground opacity-100",
            )}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="right" align="start" className="w-auto min-w-48">
        <DropdownMenuItem onClick={onOpenFilter}>
          <Filter className="size-4" />
          {tIssues(($) => $.filters.tooltip)}
        </DropdownMenuItem>
        {bulk && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() =>
                markAllRead.mutate(undefined, { onError: onError(tInbox(($) => $.errors.mark_all_read_failed)) })
              }
            >
              <CheckCheck className="size-4" />
              {tInbox(($) => $.menu.mark_all_read)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() =>
                archiveBatch(
                  () => archiveAllRead.mutateAsync(),
                  isRead,
                  tInbox(($) => $.errors.archive_all_read_failed),
                )
              }
            >
              <BookCheck className="size-4" />
              {tInbox(($) => $.menu.archive_all_read)}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() =>
                archiveBatch(
                  () => archiveCompleted.mutateAsync(),
                  isFinished,
                  tInbox(($) => $.errors.archive_completed_failed),
                )
              }
            >
              <ListChecks className="size-4" />
              {tInbox(($) => $.menu.archive_completed)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setArchiveAllConfirmOpen(true)}>
              <Archive className="size-4" />
              {tInbox(($) => $.menu.archive_all)}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
    <InboxArchiveAllDialog
      open={archiveAllConfirmOpen}
      count={totalActiveCount}
      onClose={() => setArchiveAllConfirmOpen(false)}
      onConfirm={() =>
        archiveBatch(
          () => archiveAll.mutateAsync(),
          () => true,
          tInbox(($) => $.errors.archive_all_failed),
        )
      }
    />
    </>
  );
}

/** What a row does with its notification; one stable set shared by every row. */
interface InboxRowHandlers {
  /** Warms the issue a row opens, so the click renders from cache. */
  prefetch: (item: InboxItem) => void;
  markRead: (item: InboxItem) => void;
  markUnread: (item: InboxItem) => void;
  archive: (item: InboxItem) => void;
  /** Unsubscribe & archive. */
  unsubscribe: (item: InboxItem) => void;
  snooze: (item: InboxItem, until: Date) => void;
}

const InboxRow = memo(function InboxRow({
  item,
  measureIndex,
  measureRef,
  href,
  active,
  compact,
  kept,
  settled,
  updatesWhileSnoozed,
  unsubscribable,
  handlers,
}: {
  item: InboxItem;
  /** Set when the row is one of a virtualized list's window (`RowMeasure`). */
  measureIndex?: number;
  measureRef?: Ref<HTMLLIElement>;
  href: string;
  active: boolean;
  /** One line (status, title, agents, dot or time) instead of the actor line under the title. */
  compact: boolean;
  /** Why the open row is kept in place (`keepOpenInboxRow`), in place of its actor line. */
  kept: string | null;
  /** Kept after it left the Inbox, so there is nothing left to archive or snooze. */
  settled: boolean;
  /** Notifications that arrived while the issue was snoozed. */
  updatesWhileSnoozed: number;
  /** Offers Unsubscribe & archive; only for issues that are not the viewer's own. */
  unsubscribable: boolean;
  handlers: InboxRowHandlers;
}) {
  const onPrefetch = () => handlers.prefetch(item);
  const onMarkRead = () => handlers.markRead(item);
  const onMarkUnread = () => handlers.markUnread(item);
  const onArchive = () => handlers.archive(item);
  const onUnsubscribe = unsubscribable ? () => handlers.unsubscribe(item) : undefined;
  const onSnooze = (until: Date) => handlers.snooze(item, until);
  const openContextMenu = useInboxContextMenu();
  const { t } = useT("inbox");
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  const Icon = INBOX_TYPE_ICONS[inboxTypeGroup(item.type)];
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(item.workspace_id);
  const statusLabelOf = useStatusLabel(item.workspace_id);
  const status = item.issue_status;
  const title = inboxRowTitle(item, typeLabels);
  const requestLanding = useInboxSelectionStore((s) => s.requestLanding);
  // Every click is a fresh intent to land on what the notification is about,
  // even on the issue already open, or one whose landing ran before.
  const land = () => {
    if (item.issue_id) requestLanding(item.issue_id);
  };
  // The card previews a notification you have not opened. A press opens it,
  // so the card goes at once, with no exit animation to compete with the
  // issue's first render, and a hover delay already running must not bring it
  // back over the opened issue. It may return once the pointer leaves the row.
  const [cardOpen, setCardOpen] = useState(false);
  const [cardDismissed, setCardDismissed] = useState(false);
  const dismissCard = () => {
    setCardDismissed(true);
    setCardOpen(false);
  };
  const agents = useIssueAgentState(item.workspace_id, item.issue_id ?? "");
  const running = !!agents?.running;
  const unread = item.read !== true && !kept;
  const archiveLabel = t(($) => $.list.archive_tooltip);
  const snoozeLabel = t(($) => $.snooze.action);
  const [snoozeOpen, setSnoozeOpen] = useState(false);
  // Back from a snooze, until it is read: the row says so in place of the actor line.
  const returned = unread && inboxSnoozeReturnedAt(item) !== null;
  const at = new Date(inboxRowTime(item)).toISOString();
  // Hidden while the row's buttons stand in its place.
  const yields = cn(
    !settled && "group-hover/inbox:invisible group-focus-within/inbox:invisible",
    snoozeOpen && "invisible",
  );
  const timeClass = "shrink-0 text-caption tabular-nums text-faint-foreground";
  const time = <span className={cn(timeClass, yields)}>{timeAgo(at)}</span>;
  // Takes the time's place while an agent runs or waits on the issue.
  const agentBadge = agents && (
    <span className={cn("flex shrink-0 items-center", yields)}>
      <IssueAgentBadge agents={agents} />
    </span>
  );
  const isPresent = useIsPresent();
  const reduceMotion = useReducedMotion();
  const dot = (
    <AnimatePresence initial={false}>
      {unread && (
        <motion.span
          key="unread-dot"
          data-mc="unread-dot"
          aria-hidden
          initial={false}
          animate={{ opacity: 1, scale: 1, width: 6 }}
          exit={{
            opacity: 0,
            scale: 0,
            width: 0,
            transition: { duration: reduceMotion ? 0 : 0.12, ease: UI_EASE_OUT },
          }}
          className="h-1.5 shrink-0 rounded-full bg-brand"
        />
      )}
    </AnimatePresence>
  );
  // Leads the row like a Pinned or tree issue row. Display only: the link
  // covers the row, and the hover card already names the status.
  const statusIcon = status && (
    <span data-inbox-status={status} aria-hidden className={cn("flex shrink-0", !compact && "mt-[3px]")}>
      <StatusIcon
        status={status}
        category={categoryOf(status)}
        color={colorOf(status)}
        icon={iconOf(status)}
        className="size-3.5"
      />
    </span>
  );
  const statusText = status && <span className="sr-only">, {statusLabelOf(status)}</span>;
  const titleText = (
    <ShimmerText
      active={running}
      className={cn(
        "min-w-0 flex-1 truncate",
        SIDEBAR_SHIMMER_CLASS,
        unread && "font-medium",
        unread && !running && "text-foreground",
        kept && !running && "text-muted-foreground",
      )}
    >
      {title}
    </ShimmerText>
  );

  const rowChildren = (
    <>
      <HoverCard
        open={cardOpen && !snoozeOpen}
        onOpenChange={(open) => {
          if (open && cardDismissed) return;
          setCardOpen(open);
        }}
      >
        <HoverCardTrigger
          delay={500}
          closeDelay={100}
          render={<div />}
          onPointerDown={dismissCard}
          onPointerLeave={() => setCardDismissed(false)}
          onContextMenu={
            !settled && openContextMenu
              ? (event) => {
                  dismissCard();
                  openContextMenu(item, event);
                }
              : undefined
          }
          // Enter on the focused link opens the issue too. Focus does not
          // move, so the card stays shut without the dismissed flag.
          onKeyDown={(event) => {
            if (event.key === "Enter") setCardOpen(false);
          }}
          {...panelRowAnchor(active)}
          className={cn(
            PANEL_ROW_CLASS,
            panelRowStateClass(active),
            "relative select-none data-[popup-open]:bg-surface-hover",
            !compact && "items-start py-1.5",
          )}
        >
          {statusIcon ||
            (compact ? (
              <span title={typeLabels[item.type]} className="flex shrink-0 items-center">
                <Icon aria-hidden className="size-4" />
              </span>
            ) : (
              <span className="mt-0.5 flex shrink-0">
                <InboxActorAvatar item={item} size="xs" />
              </span>
            ))}
          <AppLink
            href={href}
            newTabTitle={title}
            data-issue-id={item.issue_id ?? undefined}
            draggable={false}
            aria-current={active ? "page" : undefined}
            onNavigateInPlace={land}
            className={cn(
              ISSUE_ROW_LINK_CLASS,
              compact ? "gap-2.5" : "flex-col items-stretch gap-0",
              // Room for the snooze button beside the archive one.
              item.issue_id && !settled && "group-focus-within/inbox:pr-5 group-hover/inbox:pr-5",
              snoozeOpen && "pr-5",
            )}
          >
            {compact ? (
              <>
                {titleText}
                {statusText}
                {agentBadge}
                {kept ? (
                  <>
                    <span className="sr-only">, {kept}</span>
                    {settled && !agents && (
                      <span title={kept} className={cn("flex shrink-0 items-center text-faint-foreground", yields)}>
                        <Archive aria-hidden className="size-3" />
                      </span>
                    )}
                  </>
                ) : unread ? (
                  <span className={cn("flex shrink-0 items-center", yields)}>{dot}</span>
                ) : (
                  <>
                    <span className={cn("flex shrink-0 items-center", yields)}>{dot}</span>
                    {!agents && time}
                  </>
                )}
              </>
            ) : (
              <>
                <span className="flex min-w-0 items-center gap-2 leading-5">
                  {titleText}
                  {statusText}
                  {agentBadge || time}
                </span>
                <span className="mt-0.5 flex min-w-0 items-center gap-2 text-caption leading-4 text-muted-foreground">
                  <span className="flex min-w-0 flex-1 items-center gap-1.5">
                    {/* The status took the avatar's column; it stays with the name. */}
                    {statusIcon && <InboxActorAvatar item={item} size="xs" />}
                    <span className="min-w-0 flex-1 truncate">
                      {kept ? (
                        kept
                      ) : returned ? (
                        <InboxReturnedLine updates={updatesWhileSnoozed} />
                      ) : (
                        <InboxActorLine item={item} />
                      )}
                    </span>
                  </span>
                  {/* The agents hold the title line's time slot, so the time moves down. */}
                  {agents && <span className={timeClass}>{timeAgo(at)}</span>}
                  {dot}
                </span>
              </>
            )}
          </AppLink>
        </HoverCardTrigger>
        <HoverCardContent
          side="right"
          align="start"
          sideOffset={12}
          alignOffset={-8}
          className={cn("w-72 p-3", cardDismissed && "data-closed:animation-duration-0")}
        >
          <InboxRowCard
            item={item}
            href={href}
            title={title}
            onOpen={() => {
              dismissCard();
              land();
            }}
            onMarkRead={settled ? undefined : onMarkRead}
            onMarkUnread={settled ? undefined : onMarkUnread}
            onArchive={settled ? undefined : onArchive}
            onUnsubscribe={settled ? undefined : onUnsubscribe}
            onSnooze={
              item.issue_id && !settled
                ? () => {
                    setCardOpen(false);
                    setSnoozeOpen(true);
                  }
                : undefined
            }
          />
        </HoverCardContent>
      </HoverCard>
      {item.issue_id && !settled && (
        <InboxSnoozeMenu
          open={snoozeOpen}
          onOpenChange={setSnoozeOpen}
          onSnooze={onSnooze}
          side="right"
          align="start"
          trigger={
            <button
              type="button"
              aria-label={snoozeLabel}
              title={snoozeLabel}
              className={cn(
                rowButtonClass(compact),
                "right-7",
                snoozeOpen && "pointer-events-auto opacity-100 bg-surface-selected text-foreground",
              )}
              onClick={(event) => event.stopPropagation()}
            >
              <AlarmClock className="size-3" />
            </button>
          }
        />
      )}
      {!settled && (
        <TooltipHint label={archiveLabel}>
          <button
            type="button"
            aria-label={archiveLabel}
            className={cn(rowButtonClass(compact), "right-1.5", snoozeOpen && "pointer-events-auto opacity-100")}
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onArchive();
            }}
          >
            <Archive className="size-3" />
          </button>
        </TooltipHint>
      )}
    </>
  );

  if (measureRef) {
    return (
      <li
        ref={measureRef}
        data-index={measureIndex}
        data-inbox-row
        data-read={unread ? "false" : "true"}
        className="group/inbox relative"
        onPointerEnter={onPrefetch}
        onFocus={onPrefetch}
      >
        {rowChildren}
      </li>
    );
  }

  return (
    <motion.li
      data-inbox-row={isPresent ? "" : undefined}
      data-read={unread ? "false" : "true"}
      aria-hidden={!isPresent || undefined}
      initial={false}
      exit={{
        opacity: 0,
        height: 0,
        marginTop: 0,
        marginBottom: 0,
        overflow: "hidden",
        transition: { duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
      className={cn("group/inbox relative", !isPresent && "pointer-events-none")}
      onPointerEnter={onPrefetch}
      onFocus={onPrefetch}
    >
      {rowChildren}
    </motion.li>
  );
});

/** A tab's rows past the section's limit wait behind "Show N more", keeping the open row visible. */
function useSectionLimit<T>(rows: readonly T[], limit: number, openIndex = -1) {
  const [expanded, setExpanded] = useState(false);
  const shown = expanded
    ? rows
    : openIndex >= limit
      ? [...rows.slice(0, limit - 1), rows[openIndex]!]
      : rows.slice(0, limit);
  return {
    shown,
    more: rows.length > limit,
    hidden: Math.max(0, rows.length - shown.length),
    expanded,
    toggle: () => setExpanded((value) => !value),
  };
}

function SectionMoreButton({ expanded, hidden, onToggle }: { expanded: boolean; hidden: number; onToggle: () => void }) {
  const { t } = useT("layout");
  return (
    <button
      type="button"
      aria-expanded={expanded}
      className={cn(PANEL_ROW_CLASS, panelRowStateClass(false), "pl-[2.375rem] text-caption")}
      onClick={onToggle}
    >
      {expanded ? t(($) => $.sidebar.show_fewer_pins) : t(($) => $.sidebar.show_more_pins, { count: hidden })}
    </button>
  );
}

/** A quiet line in place of a tab's rows: empty, or failed with another try. */
function SectionNote({ children, retry }: { children: ReactNode; retry?: { label: string; busy: boolean; run: () => void } }) {
  return (
    <div role={retry ? "alert" : undefined} className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {retry && (
        <button
          type="button"
          disabled={retry.busy}
          className="shrink-0 rounded-md px-1 font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50"
          onClick={retry.run}
        >
          {retry.label}
        </button>
      )}
    </div>
  );
}

/** The Snoozed tab: each snoozed issue and when it comes back, soonest first. */
function SnoozedSectionList({
  wsId,
  list,
  limit,
  openKey,
}: {
  wsId: string;
  list: ReturnType<typeof useSnoozedInboxRows>;
  limit: number;
  openKey: string | null;
}) {
  const { t } = useT("inbox");
  const { unsnooze } = useInboxSnooze();
  const openIndex = openKey ? list.rows.findIndex((entry) => entry.issue_id === openKey) : -1;
  const { shown, more, hidden, expanded, toggle } = useSectionLimit(list.rows, limit, openIndex);
  if (list.rows.length === 0) {
    if (list.load === "failed") {
      return (
        <SectionNote retry={{ label: t(($) => $.list.retry), busy: list.retrying, run: list.retry }}>
          {t(($) => $.errors.load_failed)}
        </SectionNote>
      );
    }
    return list.load === "loaded" ? <SectionNote>{t(($) => $.snooze.view_empty)}</SectionNote> : null;
  }
  return (
    <>
      <PanelList data-sidebar-list={INBOX_LIST}>
        <AnimatePresence initial={false}>
          {shown.map((entry) => (
            <SnoozedSectionRow
              key={entry.issue_id}
              wsId={wsId}
              entry={entry}
              active={entry.issue_id === openKey}
              onUnsnooze={() => unsnooze(entry.item_id, entry.issue_id)}
            />
          ))}
        </AnimatePresence>
      </PanelList>
      {more && <SectionMoreButton expanded={expanded} hidden={hidden} onToggle={toggle} />}
    </>
  );
}

function SnoozedSectionRow({
  wsId,
  entry,
  active,
  onUnsnooze,
}: {
  wsId: string;
  entry: SnoozedInboxIssue;
  active: boolean;
  onUnsnooze: () => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const p = useWorkspacePaths();
  // The list carries the title and status; only an older server's leaves the row to fetch the issue.
  const { data: issue } = useQuery({ ...issueDetailOptions(wsId, entry.issue_id), enabled: !entry.issue_title });
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const backAt = formatSnoozeTime(new Date(entry.snoozed_until), new Date(), locale);
  const title = issue?.title ?? entry.issue_title ?? entry.issue_identifier ?? "";
  const status = issue?.status ?? entry.issue_status;
  const unsnoozeLabel = t(($) => $.snooze.unsnooze);
  const isPresent = useIsPresent();
  const reduceMotion = useReducedMotion();
  return (
    <motion.li
      data-inbox-snoozed-row={isPresent ? "" : undefined}
      aria-hidden={!isPresent || undefined}
      initial={false}
      exit={{
        opacity: 0,
        height: 0,
        marginTop: 0,
        marginBottom: 0,
        overflow: "hidden",
        transition: { duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
      className={cn("group/inbox relative", !isPresent && "pointer-events-none")}
    >
      <div {...panelRowAnchor(active)} className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "relative")}>
        <span aria-hidden className="flex size-3.5 shrink-0">
          {status && (
            <StatusIcon
              status={status}
              category={categoryOf(status)}
              color={colorOf(status)}
              icon={iconOf(status)}
              className="size-3.5"
            />
          )}
        </span>
        <AppLink
          href={inboxTabHref(p, "snoozed", entry.issue_id)}
          newTabTitle={title}
          data-issue-id={entry.issue_id}
          draggable={false}
          aria-current={active ? "page" : undefined}
          className={cn(ISSUE_ROW_LINK_CLASS, "gap-2.5 group-focus-within/inbox:pr-5 group-hover/inbox:pr-5")}
        >
          <span className="min-w-0 flex-1 truncate">{title}</span>
          <span
            title={t(($) => $.snooze.back_at, { time: backAt })}
            className="shrink-0 text-caption tabular-nums text-faint-foreground group-hover/inbox:invisible group-focus-within/inbox:invisible"
          >
            {backAt}
          </span>
        </AppLink>
      </div>
      <TooltipHint label={unsnoozeLabel}>
        <button
          type="button"
          aria-label={unsnoozeLabel}
          className={cn(rowButtonClass(true), "right-1.5")}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onUnsnooze();
          }}
        >
          <AlarmClock className="size-3" />
        </button>
      </TooltipHint>
    </motion.li>
  );
}

/** The Archived tab, a page at a time under the Inbox filters. */
function ArchivedSectionList({
  wsId,
  archived,
  limit,
  openKey,
}: {
  wsId: string;
  archived: ReturnType<typeof useArchivedInboxRows>;
  limit: number;
  openKey: string | null;
}) {
  const { t } = useT("inbox");
  const p = useWorkspacePaths();
  const { replace } = useNavigation();
  const { query, items, filters } = archived;
  const filtered = inboxFilterCount(filters) > 0;
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
  const leaveRow = useInboxSelectionStore((s) => s.leaveRow);
  const recordTransition = useInboxSelectionStore((s) => s.recordTransition);
  const restoreInbox = useRestoreInbox();
  const openIndex = openKey ? items.findIndex((item) => inboxRowKey(item) === openKey) : -1;
  const { shown, more, hidden, expanded, toggle } = useSectionLimit(items, limit, openIndex);
  // Restoring the open row moves the page on to the next archived one, as
  // its own bar's restore does.
  const restore = (item: InboxItem) => {
    const key = inboxRowKey(item);
    if (key === openKey) {
      leaveRow(wsId, key);
      const next = nextInboxRow(items, key);
      const nextKey = next ? inboxRowKey(next) : "";
      recordTransition(wsId, nextKey, "next");
      replace(inboxTabHref(p, "archived", nextKey || null));
    }
    void restoreInbox(item.id);
  };
  if (items.length === 0) {
    if (query.isError) {
      return (
        <SectionNote retry={{ label: t(($) => $.list.retry), busy: query.isFetching, run: () => void query.refetch() }}>
          {t(($) => $.errors.load_failed)}
        </SectionNote>
      );
    }
    if (filtered && query.isSuccess) {
      return (
        <div className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
          <span className="min-w-0 flex-1 truncate">{t(($) => $.filters.empty)}</span>
          <button
            type="button"
            className="shrink-0 rounded-md px-1 font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-ring"
            onClick={() => clearFilters(wsId)}
          >
            {t(($) => $.filters.clear)}
          </button>
        </div>
      );
    }
    return query.isSuccess ? <SectionNote>{t(($) => $.list.archived_empty)}</SectionNote> : null;
  }
  return (
    <>
      <PanelList data-sidebar-list={INBOX_LIST}>
        <AnimatePresence initial={false}>
          {shown.map((item) => (
            <ArchivedSectionRow
              key={item.id}
              item={item}
              active={inboxRowKey(item) === openKey}
              onRestore={() => restore(item)}
            />
          ))}
        </AnimatePresence>
      </PanelList>
      {more && <SectionMoreButton expanded={expanded} hidden={hidden} onToggle={toggle} />}
      {(!more || expanded) && query.hasNextPage && (
        <button
          type="button"
          disabled={query.isFetchingNextPage}
          className={cn(PANEL_ROW_CLASS, panelRowStateClass(false), "pl-[2.375rem] text-caption disabled:opacity-50")}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage
            ? t(($) => $.list.loading_more)
            : query.isFetchNextPageError
              ? t(($) => $.list.retry)
              : t(($) => $.list.load_more)}
        </button>
      )}
    </>
  );
}

function ArchivedSectionRow({
  item,
  active,
  onRestore,
}: {
  item: InboxItem;
  active: boolean;
  onRestore: () => void;
}) {
  const { t } = useT("inbox");
  const p = useWorkspacePaths();
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(item.workspace_id);
  const Icon = INBOX_TYPE_ICONS[inboxTypeGroup(item.type)];
  const title = inboxRowTitle(item, typeLabels);
  const status = item.issue_status;
  const restoreLabel = t(($) => $.detail.restore);
  const requestLanding = useInboxSelectionStore((s) => s.requestLanding);
  const openContextMenu = useInboxContextMenu();
  const isPresent = useIsPresent();
  const reduceMotion = useReducedMotion();
  return (
    <motion.li
      data-inbox-archived-row={isPresent ? "" : undefined}
      aria-hidden={!isPresent || undefined}
      initial={false}
      exit={{
        opacity: 0,
        height: 0,
        marginTop: 0,
        marginBottom: 0,
        overflow: "hidden",
        transition: { duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
      className={cn("group/inbox relative", !isPresent && "pointer-events-none")}
    >
      <div
        {...panelRowAnchor(active)}
        onContextMenu={openContextMenu ? (event) => openContextMenu(item, event) : undefined}
        className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "relative select-none data-[popup-open]:bg-surface-hover")}
      >
        {status ? (
          <span aria-hidden className="flex shrink-0">
            <StatusIcon
              status={status}
              category={categoryOf(status)}
              color={colorOf(status)}
              icon={iconOf(status)}
              className="size-3.5"
            />
          </span>
        ) : (
          <span title={typeLabels[item.type]} className="flex shrink-0 items-center">
            <Icon aria-hidden className="size-4" />
          </span>
        )}
        <AppLink
          href={inboxRowHref(p, item, "archived")}
          newTabTitle={title}
          data-issue-id={item.issue_id ?? undefined}
          draggable={false}
          onNavigateInPlace={() => {
            if (item.issue_id) requestLanding(item.issue_id);
          }}
          aria-current={active ? "page" : undefined}
          className={cn(ISSUE_ROW_LINK_CLASS, "gap-2.5 group-focus-within/inbox:pr-5 group-hover/inbox:pr-5")}
        >
          <span className="min-w-0 flex-1 truncate text-muted-foreground">{title}</span>
          <span className="shrink-0 text-caption tabular-nums text-faint-foreground group-hover/inbox:invisible group-focus-within/inbox:invisible">
            {timeAgo(new Date(inboxRowTime(item)).toISOString())}
          </span>
        </AppLink>
      </div>
      <TooltipHint label={restoreLabel}>
        <button
          type="button"
          aria-label={restoreLabel}
          className={cn(rowButtonClass(true), "right-1.5")}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onRestore();
          }}
        >
          <ArchiveRestore className="size-3" />
        </button>
      </TooltipHint>
    </motion.li>
  );
}

/** A row's hover button. */
function rowButtonClass(compact: boolean) {
  return cn(
    "pointer-events-none absolute z-10 flex size-5 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity duration-100 group-hover/inbox:pointer-events-auto group-hover/inbox:opacity-100 group-focus-within/inbox:pointer-events-auto group-focus-within/inbox:opacity-100 hover:bg-surface-selected hover:text-foreground motion-reduce:transition-none",
    // On the title's line, where the time was.
    compact ? "top-1/2 -translate-y-1/2" : "top-1.5",
  );
}

/** In place of the actor line on a row back from a snooze. */
function InboxReturnedLine({ updates }: { updates: number }) {
  const { t } = useT("inbox");
  return (
    <span className="inline-flex min-w-0 items-center gap-1 text-brand">
      <AlarmClock aria-hidden className="size-3 shrink-0" />
      <span className="truncate">
        {t(($) => $.snooze.returned)}
        {updates > 0 && <span className="text-muted-foreground"> · {t(($) => $.snooze.updates, { count: updates })}</span>}
      </span>
    </span>
  );
}

function inboxRowTitle(item: InboxItem, typeLabels: ReturnType<typeof useTypeLabels>) {
  return isAutopilotQuotaNotice(item.type) ? typeLabels[item.type] : getInboxDisplayTitle(item);
}

/** The whole notification, shown while a row is hovered. */
function InboxRowCard({
  item,
  href,
  title,
  onOpen,
  onMarkRead,
  onMarkUnread,
  onArchive,
  onUnsubscribe,
  onSnooze,
}: {
  item: InboxItem;
  href: string;
  title: string;
  onOpen: () => void;
  onMarkRead?: () => void;
  onMarkUnread?: () => void;
  /** Left out for a row kept after it left the Inbox. */
  onArchive?: () => void;
  onUnsubscribe?: () => void;
  /** Opens the row's snooze menu; issue rows only. */
  onSnooze?: () => void;
}) {
  const { t } = useT("layout");
  const { t: tInbox } = useT("inbox");
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(item.workspace_id);
  const statusLabelOf = useStatusLabel(item.workspace_id);
  const { data: issue } = useQuery({ ...issueDetailOptions(item.workspace_id, item.issue_id ?? ""), enabled: !!item.issue_id });
  const unread = item.read !== true;
  // A comment gets its own block, so the line above names only the kind.
  const comment = item.type === "new_comment" && item.body ? item.body : null;
  const status = item.issue_status;

  return (
    <div data-inbox-card className="flex flex-col gap-2">
      <div className="flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
        <InboxActorAvatar item={item} size="sm" />
        <span className="min-w-0 flex-1 truncate">
          <InboxActorLine item={item} detail={comment ? typeLabels[item.type] : undefined} />
        </span>
        <span className="shrink-0 tabular-nums">{timeAgo(item.created_at)}</span>
      </div>
      <p className="font-medium leading-snug">{title}</p>
      {comment && (
        <p className="line-clamp-4 rounded-md bg-muted px-2 py-1.5 text-caption leading-5 whitespace-pre-wrap">
          {comment}
        </p>
      )}
      {status && (
        <div className="flex items-center gap-1.5 text-caption text-muted-foreground">
          <StatusIcon
            status={status}
            category={categoryOf(status)}
            color={colorOf(status)}
            icon={iconOf(status)}
            className="size-3.5 shrink-0"
          />
          <span className="truncate">
            {issue?.identifier ? `${issue.identifier} · ` : ""}
            {statusLabelOf(status)}
          </span>
        </div>
      )}
      <div className="-mx-1 mt-1 flex items-center gap-1 border-t border-border pt-2">
        <AppLink
          href={href}
          data-issue-id={item.issue_id ?? undefined}
          onNavigateInPlace={onOpen}
          className={buttonVariants({ size: "sm", variant: "secondary" })}
        >
          {t(($) => $.panels.work.inbox_open)}
        </AppLink>
        {unread && onMarkRead && (
          <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={onMarkRead}>
            {tInbox(($) => $.context_menu.mark_read)}
          </Button>
        )}
        {onSnooze && (
          <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={onSnooze}>
            {tInbox(($) => $.snooze.action)}
          </Button>
        )}
        {onArchive && (
          <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={onArchive}>
            {tInbox(($) => $.context_menu.archive)}
          </Button>
        )}
        {((!unread && onMarkUnread) || onUnsubscribe) && (
          <div className="ml-auto flex items-center gap-0.5">
            {!unread && onMarkUnread && (
              <IconButton
                label={tInbox(($) => $.context_menu.mark_unread)}
                className="text-muted-foreground"
                onClick={onMarkUnread}
              >
                <CircleDot />
              </IconButton>
            )}
            {onUnsubscribe && (
              <IconButton
                label={tInbox(($) => $.context_menu.unsubscribe_archive)}
                className="text-muted-foreground"
                onClick={onUnsubscribe}
              >
                <BellOff />
              </IconButton>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
