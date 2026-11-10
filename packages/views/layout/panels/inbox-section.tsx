"use client";

import { useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Inbox } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { deduplicateInboxItems, inboxListOptions, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { StatusIcon } from "../../issues/components/status-icon";
import { issueHighlightMementoKey } from "../../issues/components/issue-detail";
import { getInboxDisplayTitle, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { useT } from "../../i18n";
import { useViewStateWriter } from "../../platform";
import { PanelList, panelRowStateClass } from "../panel-nav";
import { TREE_CHILD_ROW_CLASS, TreeRow } from "./sidebar-tree";

const EMPTY_INBOX: InboxItem[] = [];
/** Issues listed under the Inbox row; the page lists the rest. */
export const INBOX_PREVIEW_LIMIT = 10;

const itemKey = (item: InboxItem) => item.issue_id ?? item.id;

/** The issues waiting on the user, marked like the rail's Work badge. */
function PendingCount({ value }: { value: number }) {
  if (value <= 0) return null;
  return (
    <span className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-brand px-1 text-[10px] leading-none font-medium tabular-nums text-brand-foreground">
      {value > 99 ? "99+" : value}
    </span>
  );
}

function InboxRow({
  item,
  href,
  active,
  handled,
  onKeyDown,
}: {
  item: InboxItem;
  href: string;
  active: boolean;
  /** Its issue already left the Inbox; the row stays until another opens. */
  handled: boolean;
  onKeyDown: (event: React.KeyboardEvent) => void;
}) {
  const typeLabels = useTypeLabels();
  const statuses = useIssueStatuses(item.workspace_id);
  const writeViewState = useViewStateWriter();
  const title = isAutopilotQuotaNotice(item.type) ? typeLabels[item.type] : getInboxDisplayTitle(item);
  const unread = !item.read && !handled;
  return (
    <li>
      <AppLink
        href={href}
        newTabTitle={title}
        aria-current={active ? "page" : undefined}
        data-handled={handled || undefined}
        className={cn(TREE_CHILD_ROW_CLASS, panelRowStateClass(active), handled && "opacity-50")}
        onKeyDown={onKeyDown}
        // Like a click on the page's own row: land on the comment again even
        // when this issue was opened before.
        onClick={() => {
          if (item.issue_id) writeViewState(issueHighlightMementoKey(item.issue_id), undefined);
        }}
      >
        {item.issue_status ? (
          <StatusIcon
            status={item.issue_status}
            category={statuses.categoryOf(item.issue_status)}
            color={statuses.colorOf(item.issue_status)}
            icon={statuses.iconOf(item.issue_status)}
            className="size-3 shrink-0"
          />
        ) : (
          <Inbox className="size-3 shrink-0" />
        )}
        <span className={cn("min-w-0 flex-1 truncate", unread && "text-foreground")}>{title}</span>
        {unread && <span className="size-1.5 shrink-0 rounded-full bg-brand" />}
      </AppLink>
    </li>
  );
}

/**
 * The top of the Work panel: the Inbox row, with the first issues waiting on
 * the user listed under it. A listed issue opens on the Inbox page; the row
 * itself opens the page's full list.
 */
export function InboxSection({ wsId }: { wsId: string }) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname, searchParams } = useNavigation();
  const pending = useInboxUnreadCount(wsId);
  const collapsed = useSidebarTreeStore((s) => s.inboxCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setInboxCollapsed);
  const listId = useId();
  const linkRef = useRef<HTMLAnchorElement>(null);

  const onPage = pathname === p.inbox() && !searchParams.get("view");
  const selectedKey = onPage ? (searchParams.get("issue") ?? "") : "";
  // The page's own list is on screen: the same issues listed twice, side by
  // side, would only compete. The row and its count stay.
  const onList = onPage && !selectedKey;
  const listed = !collapsed && !onList;

  const { data: rawItems = EMPTY_INBOX, isLoading } = useQuery({
    ...inboxListOptions(wsId),
    enabled: listed,
  });
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);

  // Acting on the open issue takes it out of the Inbox on the server. Its row
  // stays where it was, dimmed, until another row opens.
  const [held, setHeld] = useState<{ key: string; item: InboxItem; index: number } | null>(null);
  const index = selectedKey ? items.findIndex((item) => itemKey(item) === selectedKey) : -1;
  useLayoutEffect(() => {
    if (index !== -1) setHeld({ key: selectedKey, item: items[index]!, index });
  }, [index, items, selectedKey]);
  const handledKey = index === -1 && held?.key === selectedKey ? held.key : null;
  const rows = useMemo(() => {
    if (!held || !handledKey) return items;
    const at = Math.min(held.index, items.length);
    return [...items.slice(0, at), held.item, ...items.slice(at)];
  }, [items, held, handledKey]);
  const shown = rows.slice(0, INBOX_PREVIEW_LIMIT);

  const label = t(($) => $.nav.inbox);
  const collapseFromRow = (event: React.KeyboardEvent) => {
    if (event.key !== "ArrowLeft") return;
    event.preventDefault();
    setCollapsed(true);
    linkRef.current?.focus();
  };

  return (
    <section>
      <PanelList>
        <li>
          <TreeRow
            ref={linkRef}
            href={p.inbox()}
            label={label}
            icon={<Inbox className="size-4 shrink-0" />}
            active={onPage && !selectedKey}
            expanded={listed}
            onExpandedChange={onList ? undefined : (next) => setCollapsed(!next)}
            listId={listId}
            toggleLabel={t(($) => $.panels.work.project_issues, { name: label })}
            trailing={<PendingCount value={pending} />}
          />
          {listed && (
            <ul id={listId} className="mb-2 ml-5 border-l border-surface-border pl-[9px]">
              {shown.map((item) => {
                const key = itemKey(item);
                return (
                  <InboxRow
                    key={key}
                    item={item}
                    href={`${p.inbox()}?${new URLSearchParams({ issue: key }).toString()}`}
                    active={key === selectedKey}
                    handled={key === handledKey}
                    onKeyDown={collapseFromRow}
                  />
                );
              })}
              {rows.length > shown.length && (
                <li>
                  <AppLink
                    href={p.inbox()}
                    onKeyDown={collapseFromRow}
                    className={cn(TREE_CHILD_ROW_CLASS, panelRowStateClass(false), "min-h-6 text-caption text-faint-foreground")}
                  >
                    {t(($) => $.panels.work.inbox_view_all, { total: rows.length })}
                  </AppLink>
                </li>
              )}
              {!isLoading && rows.length === 0 && (
                <li className="flex h-[26px] items-center px-2 text-caption text-faint-foreground">
                  {t(($) => $.panels.work.inbox_empty)}
                </li>
              )}
            </ul>
          )}
        </li>
      </PanelList>
    </section>
  );
}
