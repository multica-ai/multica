"use client";

import { type ReactNode, type Ref, useLayoutEffect, useMemo, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlarmClock, Archive, BellOff, BookCheck, CheckCheck, History, ListChecks, MoreHorizontal } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
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
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { inboxTypeGroup, useInboxFilterStore } from "@multica/core/inbox/filter-store";
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
} from "@multica/core/inbox/mutations";
import { snoozedInboxOptions, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { inboxRowTime, inboxSnoozeReturnedAt } from "@multica/core/inbox/snooze";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { inboxRowKey, nextInboxRow } from "@multica/core/inbox/open-row";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { INBOX_TYPE_ICONS } from "../../inbox/components/inbox-filter-options";
import { getInboxDisplayTitle, inboxRowHref, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { ARCHIVED_VIEW_PARAM } from "../../inbox/components/inbox-view";
import { ActorAvatar } from "../../common/actor-avatar";
import { InboxDetailLabel, useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { InboxFilterMenu } from "../../inbox/components/inbox-filter-menu";
import { InboxSnoozeMenu } from "../../inbox/components/inbox-snooze-menu";
import { useInboxSnooze } from "../../inbox/components/use-inbox-snooze";
import { useBuiltinViewNavigation } from "../../issue-views/builtin-views";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useInboxIssuePrefetch } from "../../inbox/components/use-inbox-issue-prefetch";
import { useTimeAgo } from "../../inbox/components/inbox-list-item";
import { useInboxUnsubscribable, useUnsubscribeArchive } from "../../inbox/components/use-unsubscribe-archive";
import {
  useArchiveInboxBatchWithUndo,
  useArchiveInboxWithUndo,
  useLeaveInboxRow,
} from "../../inbox/components/use-leave-inbox-row";
import { useKeptInboxRowText } from "../../inbox/components/inbox-kept-actions";
import { StatusIcon } from "../../issues/components";
import { useStatusLabel } from "../../issues/utils/status-label";
import {
  PANEL_ROW_CLASS,
  PanelCount,
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

/**
 * The unread count sits in the panel's number column (the rows' trailing
 * edge, 12px in from the label row's) and yields that spot to the section's
 * buttons while they show. Where they always show (coarse pointers) it stays
 * in line before them.
 */
const HEADER_COUNT_CLASS = cn(
  "pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 font-medium text-brand transition-opacity duration-150",
  "group-hover/section:opacity-0 group-focus-within/section:opacity-0",
  "[@media(pointer:coarse)]:pointer-events-auto [@media(pointer:coarse)]:static [@media(pointer:coarse)]:translate-y-0 [@media(pointer:coarse)]:px-1 [@media(pointer:coarse)]:group-focus-within/section:opacity-100",
);

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

export function InboxSection({
  wsId,
  rows,
  pathname,
  activeIssueId,
}: {
  wsId: string;
  rows: InboxSectionRows;
  pathname: string;
  /** The open issue, when it was opened from this section. */
  activeIssueId: string | null;
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
  const leave = useLeaveInboxRow();
  const unsubscribable = useInboxUnsubscribable(wsId);
  const unsubscribeArchive = useUnsubscribeArchive();
  const { snooze } = useInboxSnooze();
  const { searchParams } = useNavigation();
  const isCompact = useIsCompact();
  const { list, rows: all, displayed, expanded, limit, setExpanded } = rows;
  const keptText = useKeptInboxRowText(wsId);
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
  // The notification the Inbox page has open, keyed as it groups rows.
  const openKey = pathname === p.inbox() ? searchParams.get("issue") : null;
  // The open notification, wherever it is open; acting on its row moves on.
  const openRowKey = activeIssueId ?? openKey;
  const leaveIfOpen = (item: InboxItem) => {
    const key = inboxRowKey(item);
    if (key === openRowKey) leave(key, nextInboxRow(all, key));
  };
  const prefetchRow = useInboxIssuePrefetch(wsId, collapsed ? [] : displayed, (item) => inboxRowHref(p, item));
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

  const scrollRef = usePanelScrollRef();
  const renderRow = (item: InboxItem, measure?: RowMeasure) => (
    <InboxRow
      key={item.id}
      item={item}
      measure={measure}
      href={inboxRowHref(p, item)}
      onPrefetch={() => void prefetchRow(item)}
      active={
        (!!item.issue_id && item.issue_id === activeIssueId) ||
        (!!openKey && (item.issue_id ?? item.id) === openKey)
      }
      compact={compact}
      kept={list.kept?.item.id === item.id ? keptText.line(list.kept) : null}
      settled={list.kept?.item.id === item.id && list.kept.reason !== "filtered"}
      updatesWhileSnoozed={item.issue_id ? (updatesWhileSnoozed.get(item.issue_id) ?? 0) : 0}
      onArchive={() => {
        archive(item.id, inboxRowKey(item));
        leaveIfOpen(item);
      }}
      onUnsubscribe={
        item.issue_id && unsubscribable(item)
          ? () => {
              unsubscribeArchive({ id: item.id, issueId: item.issue_id! });
              leaveIfOpen(item);
            }
          : undefined
      }
      onSnooze={(until) => {
        snooze(item, until);
        leaveIfOpen(item);
      }}
    />
  );

  return (
    <PanelSection
      label={t(($) => $.nav.inbox)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <div className="relative flex items-center gap-1">
          {showCount && <PanelCount value={unread} className={HEADER_COUNT_CLASS} />}
          <InboxFilterMenu
            wsId={wsId}
            items={list.notifications}
            priorityFilterSupport={list.priorityFilterSupport}
            issueFieldsSupport={list.issueFieldsSupport}
            activeIssueIds={list.activeIssueIds}
            sectionTrigger={{
              className: cn(TREE_ROW_BUTTON_CLASS, "text-muted-foreground", SECTION_SETTINGS_REVEAL_CLASS),
              // A filtered list says so even while the section is not hovered.
              activeClassName: "w-auto gap-0.5 px-1 text-brand opacity-100 hover:text-brand",
            }}
          />
          <InboxSectionSettings className={SECTION_SETTINGS_REVEAL_CLASS} />
          <InboxSectionActions
            wsId={wsId}
            rows={all}
            openRowKey={openRowKey}
            className={SECTION_SETTINGS_REVEAL_CLASS}
          />
        </div>
      }
    >
      {displayed.length > 0 ? (
        displayed.length > VIRTUALIZE_AFTER && scrollRef ? (
          <VirtualInboxList items={displayed} compact={compact} renderRow={renderRow} />
        ) : (
          <PanelList data-sidebar-list={INBOX_LIST} className={compact ? undefined : "space-y-0.5"}>
            {displayed.map((item) => renderRow(item))}
          </PanelList>
        )
      ) : list.filtered ? (
        <div className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
          <span className="min-w-0 flex-1 truncate">{tInbox(($) => $.filters.empty)}</span>
          <button
            type="button"
            className="shrink-0 rounded-md px-1 font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
            onClick={() => clearFilters(wsId)}
          >
            {tInbox(($) => $.filters.clear)}
          </button>
        </div>
      ) : (
        <p className={cn(PANEL_ROW_CLASS, "pl-[2.375rem] text-caption text-faint-foreground")}>
          {t(($) => $.panels.work.inbox_empty)}
        </p>
      )}
      {all.length > limit && (
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
            href={p.inbox()}
            icon={<span className="size-4" aria-hidden />}
            label={t(($) => $.panels.work.inbox_all)}
            active={
              pathname === p.inbox() && !openKey && searchParams.get("view") !== ARCHIVED_VIEW_PARAM
            }
            className="text-caption"
          />
        </PanelList>
      )}
    </PanelSection>
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
  // Where the list starts within the scroll container's content.
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!list || !scroller) return;
    const measure = () =>
      setScrollMargin(list.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(scroller);
    return () => observer.disconnect();
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

function InboxSectionActions({
  wsId,
  rows,
  openRowKey,
  className,
}: {
  wsId: string;
  rows: readonly InboxItem[];
  openRowKey: string | null;
  className?: string;
}) {
  const { t } = useT("layout");
  const { t: tInbox } = useT("inbox");
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
    void archiveWithUndo(archive, archives, failed, takesOpen ? openRowKey : null);
    if (takesOpen) leave(openRowKey!, nextInboxRow(rows, openRowKey!, archives));
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
  const { push } = useNavigation();
  const p = useWorkspacePaths();
  const builtinNav = useBuiltinViewNavigation();
  const { data: snoozed } = useQuery(snoozedInboxOptions(wsId));
  const label = t(($) => $.panels.work.inbox_actions);
  const onError = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={label}
            className={cn(
              TREE_ROW_BUTTON_CLASS,
              // The slot of the other sections' create button.
              "size-[var(--button-height-xs)]",
              "text-muted-foreground data-popup-open:bg-surface-selected data-popup-open:text-foreground data-popup-open:opacity-100",
              className,
            )}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="right" align="start" className="w-auto min-w-48">
        <DropdownMenuItem
          onClick={() => {
            builtinNav.prepare("snoozed");
            push(builtinNav.hrefFor("snoozed"));
          }}
        >
          <AlarmClock className="size-4" />
          {t(($) => $.panels.work.inbox_view_snoozed)}
          {!!snoozed?.length && (
            <DropdownMenuShortcut className="tracking-normal tabular-nums">{snoozed.length}</DropdownMenuShortcut>
          )}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => push(`${p.inbox()}?${new URLSearchParams({ view: ARCHIVED_VIEW_PARAM })}`)}>
          <History className="size-4" />
          {t(($) => $.panels.work.inbox_view_archived)}
        </DropdownMenuItem>
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
        <DropdownMenuItem
          onClick={() =>
            archiveBatch(
              () => archiveAll.mutateAsync(),
              () => true,
              tInbox(($) => $.errors.archive_all_failed),
            )
          }
        >
          <Archive className="size-4" />
          {tInbox(($) => $.menu.archive_all)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function InboxRow({
  item,
  measure,
  href,
  onPrefetch,
  active,
  compact,
  kept,
  settled,
  updatesWhileSnoozed,
  onArchive,
  onUnsubscribe,
  onSnooze,
}: {
  item: InboxItem;
  /** Set when the row is one of a virtualized list's window. */
  measure?: RowMeasure;
  href: string;
  /** Warms the issue this row opens, so the click renders from cache. */
  onPrefetch: () => void;
  active: boolean;
  /** One line (status, title, agents, dot or time) instead of the actor line under the title. */
  compact: boolean;
  /** Why the open row is kept in place (`keepOpenInboxRow`), in place of its actor line. */
  kept: string | null;
  /** Kept after it left the Inbox, so there is nothing left to archive or snooze. */
  settled: boolean;
  /** Notifications that arrived while the issue was snoozed. */
  updatesWhileSnoozed: number;
  onArchive: () => void;
  /** Unsubscribe & archive; only for issues that are not the viewer's own. */
  onUnsubscribe?: () => void;
  onSnooze: (until: Date) => void;
}) {
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
  const dot = <span data-mc="unread-dot" aria-hidden className="size-1.5 shrink-0 rounded-full bg-brand" />;
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

  return (
    <li
      ref={measure?.ref}
      data-index={measure?.index}
      data-inbox-row
      data-read={unread ? "false" : "true"}
      className="group/inbox relative"
      onPointerEnter={onPrefetch}
      onFocus={onPrefetch}
    >
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
          // Enter on the focused link opens the issue too. Focus does not
          // move, so the card stays shut without the dismissed flag.
          onKeyDown={(event) => {
            if (event.key === "Enter") setCardOpen(false);
          }}
          {...panelRowAnchor(active)}
          className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "relative", !compact && "items-start py-1.5")}
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
                  !agents && time
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
                  {unread && dot}
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
          className={cn("w-72 p-3", cardDismissed && "data-closed:duration-0")}
        >
          <InboxRowCard
            item={item}
            href={href}
            title={title}
            onOpen={() => {
              dismissCard();
              land();
            }}
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
                snoozeOpen && "flex bg-surface-selected text-foreground",
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
            className={cn(rowButtonClass(compact), "right-1.5", snoozeOpen && "flex")}
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
    </li>
  );
}

/** A row's hover button. */
function rowButtonClass(compact: boolean) {
  return cn(
    "absolute z-10 hidden size-5 items-center justify-center rounded-md text-muted-foreground group-hover/inbox:flex group-focus-within/inbox:flex hover:bg-surface-selected hover:text-foreground",
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

function InboxActorAvatar({ item, size }: { item: InboxItem; size: "xs" | "sm" }) {
  return (
    <ActorAvatar
      actorType={item.actor_type ?? item.recipient_type}
      actorId={item.actor_id ?? item.recipient_id}
      size={size}
    />
  );
}

/** Who did what: the actor's name, then what the notification says happened. */
function InboxActorLine({ item, detail }: { item: InboxItem; detail?: ReactNode }) {
  const { getActorName } = useActorName();
  const name =
    item.actor_type && item.actor_type !== "system" && item.actor_id
      ? getActorName(item.actor_type, item.actor_id)
      : null;
  return (
    <>
      {name && <span className="font-medium">{name} </span>}
      {detail ?? <InboxDetailLabel item={item} />}
    </>
  );
}

/** The whole notification, shown while a row is hovered. */
function InboxRowCard({
  item,
  href,
  title,
  onOpen,
  onArchive,
  onUnsubscribe,
  onSnooze,
}: {
  item: InboxItem;
  href: string;
  title: string;
  onOpen: () => void;
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
  const markRead = useMarkInboxRead();
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
        {unread && (
          <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={() => markRead.mutate(item.id)}>
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
        {onUnsubscribe && (
          <IconButton
            label={tInbox(($) => $.context_menu.unsubscribe_archive)}
            className="ml-auto text-muted-foreground"
            onClick={onUnsubscribe}
          >
            <BellOff />
          </IconButton>
        )}
      </div>
    </div>
  );
}
