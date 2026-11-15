"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, BookCheck, CheckCheck, History, ListChecks, MoreHorizontal } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { inboxTypeGroup } from "@multica/core/inbox/filter-store";
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useArchiveInbox,
  useMarkAllInboxRead,
} from "@multica/core/inbox/mutations";
import { deduplicateInboxItems, inboxListOptions, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { INBOX_TYPE_ICONS } from "../../inbox/components/inbox-filter-options";
import { getInboxDisplayTitle, inboxRowHref, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { ARCHIVED_VIEW_PARAM } from "../../inbox/components/inbox-view";
import { useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { useTimeAgo } from "../../inbox/components/inbox-list-item";
import { PANEL_ROW_CLASS, PanelCount, PanelList, PanelSection, panelRowAnchor, panelRowStateClass } from "../panel-nav";
import { InboxSectionSettings, SECTION_SETTINGS_REVEAL_CLASS } from "./sidebar-settings";
import { ISSUE_ROW_LINK_CLASS, TREE_ROW_BUTTON_CLASS } from "./sidebar-tree";

const EMPTY_INBOX: InboxItem[] = [];

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
 * alike. A row leaves only when archived, by hand or by its issue reaching
 * Done. Lifted out of the section so the panel knows which issues it shows.
 */
export function useInboxSectionRows(wsId: string | undefined) {
  const { data: raw = EMPTY_INBOX } = useQuery({ ...inboxListOptions(wsId ?? ""), enabled: !!wsId });
  const limit = useSidebarTreeStore((s) => s.inboxSettings.limit);
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const [expandedWsId, setExpandedWsId] = useState<string | null>(null);
  const expanded = expandedWsId === (wsId ?? null);
  const rows = useMemo(() => deduplicateInboxItems(raw), [raw]);
  const displayed = useMemo(() => (expanded ? rows : rows.slice(0, limit)), [expanded, rows, limit]);
  // A collapsed section shows no rows, so it lights none.
  const shownIssueIds = useMemo(
    () => new Set(collapsed ? [] : displayed.flatMap((item) => (item.issue_id ? [item.issue_id] : []))),
    [collapsed, displayed],
  );

  return {
    rows,
    displayed,
    expanded,
    limit,
    shownIssueIds,
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
  const p = useWorkspacePaths();
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setInboxCollapsed);
  const showCount = useSidebarTreeStore((s) => s.inboxSettings.showCount);
  const unread = useInboxUnreadCount(wsId);
  const archive = useArchiveInbox();
  const { searchParams } = useNavigation();
  const { rows: all, displayed, expanded, limit, setExpanded } = rows;
  // The notification the Inbox page has open, keyed as it groups rows.
  const openKey = pathname === p.inbox() ? searchParams.get("issue") : null;

  return (
    <PanelSection
      label={t(($) => $.nav.inbox)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <div className="relative flex items-center gap-1">
          {showCount && <PanelCount value={unread} className={HEADER_COUNT_CLASS} />}
          <InboxSectionSettings className={SECTION_SETTINGS_REVEAL_CLASS} />
          <InboxSectionActions className={SECTION_SETTINGS_REVEAL_CLASS} />
        </div>
      }
    >
      {displayed.length > 0 ? (
        <PanelList data-sidebar-list={INBOX_LIST}>
          {displayed.map((item) => (
            <InboxRow
              key={item.id}
              item={item}
              href={inboxRowHref(p, item)}
              active={
                (!!item.issue_id && item.issue_id === activeIssueId) ||
                (!!openKey && (item.issue_id ?? item.id) === openKey)
              }
              onArchive={() => archive.mutate(item.id)}
            />
          ))}
        </PanelList>
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
    </PanelSection>
  );
}

function InboxSectionActions({ className }: { className?: string }) {
  const { t } = useT("layout");
  const { t: tInbox } = useT("inbox");
  const markAllRead = useMarkAllInboxRead();
  const archiveAll = useArchiveAllInbox();
  const archiveAllRead = useArchiveAllReadInbox();
  const archiveCompleted = useArchiveCompletedInbox();
  const { push } = useNavigation();
  const p = useWorkspacePaths();
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
            archiveAllRead.mutate(undefined, { onError: onError(tInbox(($) => $.errors.archive_all_read_failed)) })
          }
        >
          <BookCheck className="size-4" />
          {tInbox(($) => $.menu.archive_all_read)}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            archiveCompleted.mutate(undefined, { onError: onError(tInbox(($) => $.errors.archive_completed_failed)) })
          }
        >
          <ListChecks className="size-4" />
          {tInbox(($) => $.menu.archive_completed)}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            archiveAll.mutate(undefined, { onError: onError(tInbox(($) => $.errors.archive_all_failed)) })
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
  href,
  active,
  onArchive,
}: {
  item: InboxItem;
  href: string;
  active: boolean;
  onArchive: () => void;
}) {
  const { t } = useT("inbox");
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  const Icon = INBOX_TYPE_ICONS[inboxTypeGroup(item.type)];
  const title = isAutopilotQuotaNotice(item.type) ? typeLabels[item.type] : getInboxDisplayTitle(item);
  const unread = item.read !== true;
  const archiveLabel = t(($) => $.list.archive_tooltip);

  return (
    <li data-inbox-row data-read={unread ? "false" : "true"} className="group/inbox relative">
      <div {...panelRowAnchor(active)} className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "relative")}>
        <span title={typeLabels[item.type]} className="flex shrink-0 items-center">
          <Icon aria-hidden className="size-4" />
        </span>
        <AppLink
          href={href}
          newTabTitle={title}
          draggable={false}
          aria-current={active ? "page" : undefined}
          className={cn(ISSUE_ROW_LINK_CLASS, "gap-2.5")}
        >
          <span className={cn("min-w-0 flex-1 truncate", unread && "font-medium text-foreground")}>{title}</span>
          {/* Yields its slot to the archive button on hover. */}
          <span className="flex shrink-0 items-center group-hover/inbox:invisible group-focus-within/inbox:invisible">
            {unread ? (
              <span data-mc="unread-dot" aria-hidden className="size-1.5 rounded-full bg-brand" />
            ) : (
              <span className="text-caption tabular-nums text-faint-foreground">{timeAgo(item.created_at)}</span>
            )}
          </span>
        </AppLink>
      </div>
      <TooltipHint label={archiveLabel}>
        <button
          type="button"
          aria-label={archiveLabel}
          className="absolute top-1/2 right-1.5 z-10 hidden size-5 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground group-hover/inbox:flex group-focus-within/inbox:flex hover:bg-surface-selected hover:text-foreground"
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onArchive();
          }}
        >
          <Archive className="size-3" />
        </button>
      </TooltipHint>
    </li>
  );
}
