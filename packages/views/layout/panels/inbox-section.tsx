"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { inboxTypeGroup } from "@multica/core/inbox/filter-store";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
import { deduplicateInboxItems, inboxListOptions, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useWorkspacePaths, type WorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { INBOX_TYPE_ICONS } from "../../inbox/components/inbox-filter-options";
import { getInboxDisplayTitle, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { useTimeAgo } from "../../inbox/components/inbox-list-item";
import { PANEL_ROW_CLASS, PanelCount, PanelLink, PanelList, PanelSection, panelRowAnchor, panelRowStateClass } from "../panel-nav";
import { InboxSectionSettings, SECTION_SETTINGS_REVEAL_CLASS } from "./sidebar-settings";
import { ISSUE_ROW_LINK_CLASS } from "./sidebar-tree";

const EMPTY_INBOX: InboxItem[] = [];

/** The Inbox rows' place in the clicked-issue record, apart from every sidebar source. */
export const INBOX_LIST = "inbox";

/**
 * Where a notification row leads: its issue, at the comment it is about when
 * it names one. An issue-less notification opens in the full Inbox.
 */
function inboxRowHref(p: WorkspacePaths, item: InboxItem): string {
  if (!item.issue_id) return `${p.inbox()}?${new URLSearchParams({ issue: item.id }).toString()}`;
  const issueHref = p.issueDetail(item.issue_id);
  const commentId = item.details?.comment_id;
  return commentId ? `${issueHref}#comment-${commentId}` : issueHref;
}

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
  const { rows: all, displayed, expanded, limit, setExpanded } = rows;

  return (
    <PanelSection
      label={t(($) => $.nav.inbox)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <>
          {showCount && <PanelCount value={unread} className="px-1 font-medium text-brand" />}
          <InboxSectionSettings className={SECTION_SETTINGS_REVEAL_CLASS} />
        </>
      }
    >
      {displayed.length > 0 && (
        <PanelList data-sidebar-list={INBOX_LIST}>
          {displayed.map((item) => (
            <InboxRow
              key={item.id}
              item={item}
              href={inboxRowHref(p, item)}
              active={!!item.issue_id && item.issue_id === activeIssueId}
              onArchive={() => archive.mutate(item.id)}
            />
          ))}
        </PanelList>
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
      <PanelList className="mt-px">
        <PanelLink
          href={p.inbox()}
          icon={<span className="size-4" aria-hidden />}
          label={all.length > 0 ? t(($) => $.panels.work.inbox_all) : t(($) => $.panels.work.inbox_empty)}
          active={pathname === p.inbox()}
          className="text-caption"
        />
      </PanelList>
    </PanelSection>
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
      <div {...panelRowAnchor(active)} className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "relative pr-2")}>
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
