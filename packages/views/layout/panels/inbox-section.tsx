"use client";

import { type ReactNode, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, BookCheck, CheckCheck, History, ListChecks, MoreHorizontal } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { inboxTypeGroup, useInboxFilterStore } from "@multica/core/inbox/filter-store";
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useArchiveInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
  useMarkInboxUnread,
} from "@multica/core/inbox/mutations";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { AppLink, useNavigation } from "../../navigation";
import { useViewStateWriter } from "../../platform";
import { issueHighlightMementoKey } from "../../issues/components/issue-detail";
import { useT } from "../../i18n";
import { INBOX_TYPE_ICONS } from "../../inbox/components/inbox-filter-options";
import { getInboxDisplayTitle, inboxRowHref, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { ARCHIVED_VIEW_PARAM } from "../../inbox/components/inbox-view";
import { ActorAvatar } from "../../common/actor-avatar";
import { InboxDetailLabel, useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { InboxFilterMenu } from "../../inbox/components/inbox-filter-menu";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useTimeAgo } from "../../inbox/components/inbox-list-item";
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
} from "../panel-nav";
import { InboxSectionSettings, SECTION_SETTINGS_REVEAL_CLASS } from "./sidebar-settings";
import { ISSUE_ROW_LINK_CLASS, TREE_ROW_BUTTON_CLASS } from "./sidebar-tree";

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
 * by its issue reaching Done. Lifted out of the section so the panel knows
 * which issues it shows. `openKey`: see `useFilteredInboxRows`.
 */
export function useInboxSectionRows(wsId: string | undefined, openKey: string | null) {
  const list = useFilteredInboxRows(wsId ?? "", openKey);
  const { rows } = list;
  const limit = useSidebarTreeStore((s) => s.inboxSettings.limit);
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const [expandedWsId, setExpandedWsId] = useState<string | null>(null);
  const expanded = expandedWsId === (wsId ?? null);
  const displayed = useMemo(() => (expanded ? rows : rows.slice(0, limit)), [expanded, rows, limit]);
  // A collapsed section shows no rows, so it lights none.
  const shownIssueIds = useMemo(
    () => new Set(collapsed ? [] : displayed.flatMap((item) => (item.issue_id ? [item.issue_id] : []))),
    [collapsed, displayed],
  );

  return {
    list,
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
  const { t: tInbox } = useT("inbox");
  const p = useWorkspacePaths();
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setInboxCollapsed);
  const showCount = useSidebarTreeStore((s) => s.inboxSettings.showCount);
  const compact = useSidebarTreeStore((s) => s.inboxSettings.compact);
  const unread = useInboxUnreadCount(wsId);
  const archive = useArchiveInbox();
  const { searchParams } = useNavigation();
  const isCompact = useIsCompact();
  const { list, rows: all, displayed, expanded, limit, setExpanded } = rows;
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
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
              compact={compact}
              onArchive={() => archive.mutate(item.id)}
            />
          ))}
        </PanelList>
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
  compact,
  onArchive,
}: {
  item: InboxItem;
  href: string;
  active: boolean;
  /** One line (type icon, title, dot or time) instead of the actor line under the title. */
  compact: boolean;
  onArchive: () => void;
}) {
  const { t } = useT("inbox");
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  const Icon = INBOX_TYPE_ICONS[inboxTypeGroup(item.type)];
  const title = inboxRowTitle(item, typeLabels);
  const writeViewState = useViewStateWriter();
  const requestLanding = useInboxSelectionStore((s) => s.requestLanding);
  // Every click is a fresh intent to land on what the notification is about,
  // even on the issue already open, or one whose landing ran before.
  const land = () => {
    if (!item.issue_id) return;
    writeViewState(issueHighlightMementoKey(item.issue_id), undefined);
    requestLanding(item.issue_id);
  };
  const unread = item.read !== true;
  const archiveLabel = t(($) => $.list.archive_tooltip);
  // Hidden while the archive button stands in its place.
  const yields = "group-hover/inbox:invisible group-focus-within/inbox:invisible";
  const time = (
    <span className={cn("shrink-0 text-caption tabular-nums text-faint-foreground", yields)}>
      {timeAgo(item.created_at)}
    </span>
  );
  const dot = <span data-mc="unread-dot" aria-hidden className="size-1.5 shrink-0 rounded-full bg-brand" />;
  const titleText = (
    <span className={cn("min-w-0 flex-1 truncate", unread && "font-medium text-foreground")}>{title}</span>
  );

  return (
    <li data-inbox-row data-read={unread ? "false" : "true"} className="group/inbox relative">
      <HoverCard>
        <HoverCardTrigger
          delay={500}
          closeDelay={100}
          render={<div />}
          {...panelRowAnchor(active)}
          className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "relative", !compact && "items-start py-[5px]")}
        >
          {compact ? (
            <span title={typeLabels[item.type]} className="flex shrink-0 items-center">
              <Icon aria-hidden className="size-4" />
            </span>
          ) : (
            <span className="mt-0.5 flex shrink-0">
              <InboxActorAvatar item={item} size="xs" />
            </span>
          )}
          <AppLink
            href={href}
            newTabTitle={title}
            draggable={false}
            aria-current={active ? "page" : undefined}
            onNavigateInPlace={land}
            className={cn(ISSUE_ROW_LINK_CLASS, compact ? "gap-2.5" : "flex-col items-stretch gap-0")}
          >
            {compact ? (
              <>
                {titleText}
                {unread ? <span className={cn("flex shrink-0 items-center", yields)}>{dot}</span> : time}
              </>
            ) : (
              <>
                <span className="flex min-w-0 items-center gap-2 leading-5">
                  {titleText}
                  {time}
                </span>
                <span className="flex min-w-0 items-center gap-2 text-caption leading-4 text-muted-foreground">
                  <span className="min-w-0 flex-1 truncate">
                    <InboxActorLine item={item} />
                  </span>
                  {unread && dot}
                </span>
              </>
            )}
          </AppLink>
        </HoverCardTrigger>
        <HoverCardContent side="right" align="start" sideOffset={12} alignOffset={-8} className="w-72 p-3">
          <InboxRowCard item={item} href={href} title={title} onOpen={land} onArchive={onArchive} />
        </HoverCardContent>
      </HoverCard>
      <TooltipHint label={archiveLabel}>
        <button
          type="button"
          aria-label={archiveLabel}
          className={cn(
            "absolute right-1.5 z-10 hidden size-5 items-center justify-center rounded-md text-muted-foreground group-hover/inbox:flex group-focus-within/inbox:flex hover:bg-surface-selected hover:text-foreground",
            // On the title's line, where the time was.
            compact ? "top-1/2 -translate-y-1/2" : "top-[5px]",
          )}
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
}: {
  item: InboxItem;
  href: string;
  title: string;
  onOpen: () => void;
  onArchive: () => void;
}) {
  const { t } = useT("layout");
  const { t: tInbox } = useT("inbox");
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(item.workspace_id);
  const statusLabelOf = useStatusLabel(item.workspace_id);
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
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
        <AppLink href={href} onNavigateInPlace={onOpen} className={buttonVariants({ size: "sm", variant: "secondary" })}>
          {t(($) => $.panels.work.inbox_open)}
        </AppLink>
        <Button
          size="sm"
          variant="ghost"
          className="text-muted-foreground"
          onClick={() => (unread ? markRead.mutate(item.id) : markUnread.mutate(item.id))}
        >
          {unread ? tInbox(($) => $.context_menu.mark_read) : tInbox(($) => $.context_menu.mark_unread)}
        </Button>
        <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={onArchive}>
          {tInbox(($) => $.context_menu.archive)}
        </Button>
      </div>
    </div>
  );
}
