"use client";

import { useLayoutEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Inbox } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { deduplicateInboxItems, inboxListOptions, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { isIssueUuid } from "@multica/core/issues/canonical-id";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { IssueContextMenuProvider } from "../../issues/actions";
import { StatusIcon } from "../../issues/components/status-icon";
import { issueHighlightMementoKey } from "../../issues/components/issue-detail";
import { getInboxDisplayTitle, isAutopilotQuotaNotice } from "../../inbox/components/inbox-display";
import { useTypeLabels } from "../../inbox/components/inbox-detail-label";
import { useTimeAgo } from "../../inbox/components/inbox-list-item";
import { useT } from "../../i18n";
import { useViewStateWriter } from "../../platform";
import { PanelBody, PanelHeader, PanelLink, PanelList, panelRowStateClass } from "../panel-nav";
import { PinnedSection, usePanelPins } from "./pinned-section";
import { TREE_CHILD_ROW_CLASS } from "./sidebar-tree";
import { routeSegment } from "./work-panel";

const EMPTY_INBOX: InboxItem[] = [];
/** Rows listed under Needs action; the page lists the rest. */
export const ATTENTION_PREVIEW_LIMIT = 10;

const itemKey = (item: InboxItem) => item.issue_id ?? item.id;

/** The issues waiting on the user, marked like the rail's Attention badge. */
function PendingCount({ value }: { value: number }) {
  if (value <= 0) return null;
  return (
    <span className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-brand px-1 text-[10px] leading-none font-medium tabular-nums text-brand-foreground">
      {value > 99 ? "99+" : value}
    </span>
  );
}

function AttentionRow({
  item,
  href,
  active,
  handled,
}: {
  item: InboxItem;
  href: string;
  active: boolean;
  /** Its issue already left Needs action; the row stays until another opens. */
  handled: boolean;
}) {
  const timeAgo = useTimeAgo();
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
        <span className="shrink-0 text-caption tabular-nums text-faint-foreground">{timeAgo(item.created_at)}</span>
      </AppLink>
    </li>
  );
}

/**
 * The Attention space: what the user follows (Pinned) and what is waiting on
 * them (Needs action). A row opens its issue on the Needs action page; the
 * group's own row opens that page's full list.
 */
export function AttentionPanel() {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname, searchParams } = useNavigation();
  const wsId = useCurrentWorkspace()?.id;
  const userId = useAuthStore((s) => s.user?.id);
  const pending = useInboxUnreadCount(wsId);

  const issueSegment = routeSegment(pathname, p.issues());
  const { data: currentIssue } = useQuery({
    ...issueDetailOptions(wsId ?? "", issueSegment ?? ""),
    enabled: false,
  });
  const currentIssueId = issueSegment ? (currentIssue?.id ?? (isIssueUuid(issueSegment) ? issueSegment : null)) : null;
  const pins = usePanelPins(wsId, userId, pathname, currentIssueId);

  const { data: rawItems = EMPTY_INBOX, isLoading } = useQuery({
    ...inboxListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);
  const onPage = pathname === p.inbox() && !searchParams.get("view");
  const selectedKey = onPage ? (searchParams.get("issue") ?? "") : "";

  // Acting on the open issue takes it out of Needs action on the server. Its
  // row stays where it was, dimmed, until another row opens.
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
  const shown = rows.slice(0, ATTENTION_PREVIEW_LIMIT);

  return (
    <>
      <PanelHeader title={t(($) => $.spaces.attention)} />
      <IssueContextMenuProvider variant="sidebar">
        <PanelBody>
          {wsId && <PinnedSection pins={pins} wsId={wsId} />}
          <section className="mt-5">
            <PanelList>
              <PanelLink
                href={p.inbox()}
                icon={<Inbox />}
                label={t(($) => $.panels.attention.needs_action)}
                active={onPage && !selectedKey}
                trailing={<PendingCount value={pending} />}
              />
            </PanelList>
            <ul className="mb-2 ml-5 border-l border-surface-border pl-[9px]">
              {shown.map((item) => {
                const key = itemKey(item);
                return (
                  <AttentionRow
                    key={key}
                    item={item}
                    href={`${p.inbox()}?${new URLSearchParams({ issue: key }).toString()}`}
                    active={key === selectedKey}
                    handled={key === handledKey}
                  />
                );
              })}
              {rows.length > shown.length && (
                <li>
                  <AppLink
                    href={p.inbox()}
                    className={cn(TREE_CHILD_ROW_CLASS, panelRowStateClass(false), "min-h-6 text-caption text-faint-foreground")}
                  >
                    {t(($) => $.panels.attention.view_all, { total: rows.length })}
                  </AppLink>
                </li>
              )}
              {!isLoading && rows.length === 0 && (
                <li className="flex h-[26px] items-center px-2 text-caption text-faint-foreground">
                  {t(($) => $.panels.attention.empty)}
                </li>
              )}
            </ul>
          </section>
        </PanelBody>
      </IssueContextMenuProvider>
    </>
  );
}
