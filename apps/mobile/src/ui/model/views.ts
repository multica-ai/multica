import type { IssueView } from "@multica/core/api/schemas";
import { baselineFromQuery, issueViewFilterCount } from "@multica/core/issue-views/baseline";
import { BUILTIN_VIEW_KEYS, builtinViewItemId, savedViewItemId, type SidebarViewItem } from "@multica/core/issue-views/sidebar-views";
import { builtinSidebarRequest, viewSidebarNeedsUnread, viewSidebarRequest, type SidebarIssuesRequest } from "@multica/core/issues/sidebar-list";
import type { SidebarIssueSettings } from "@multica/core/issues/stores/sidebar-tree-store";
import type { IssueStatusCategory } from "@multica/core/types";
import type { Strings } from "../strings";

/** Built-in lists show every open status, most recently touched first. */
const BUILTIN_SETTINGS: SidebarIssueSettings = {
  sortBy: "updated_at",
  sortDirection: "desc",
  hiddenStatuses: null,
  assignee: "anyone",
  showSubIssues: true,
};

export function viewItemLabel(item: SidebarViewItem, strings: Strings): string {
  return item.kind === "builtin" ? strings.nav.builtin[item.key] : item.view.name;
}

/** Whether listing the item needs the viewer's unread issue ids. */
export function viewItemNeedsUnread(item: SidebarViewItem): boolean {
  return item.kind === "view" && viewSidebarNeedsUnread(item.view);
}

/** The id set a built-in lists: issues agents are running on, or issues the viewer snoozed. */
export function viewItemIdSource(item: SidebarViewItem): "working" | "snoozed" | null {
  return item.kind === "builtin" && (item.key === "working" || item.key === "snoozed") ? item.key : null;
}

export function workingIssueIds(agents: readonly { issue_ids: readonly string[] }[]): string[] {
  return [...new Set(agents.flatMap((agent) => agent.issue_ids))].sort();
}

export function snoozedIssueIds(entries: readonly { issue_id: string }[]): string[] {
  return entries.map((entry) => entry.issue_id).sort();
}

/** The table request that lists the item, or null when its scope can't be listed. */
export function viewItemRequest(
  item: SidebarViewItem,
  statuses: readonly { key: string; category: IssueStatusCategory }[],
  today: string,
  unreadIssueIds: readonly string[],
  issueIds: readonly string[] = [],
): SidebarIssuesRequest | null {
  if (item.kind === "view") return viewSidebarRequest(item.view, today, unreadIssueIds);
  return builtinSidebarRequest(item.key, BUILTIN_SETTINGS, statuses, issueIds);
}

/** How many filters a saved view sets, split out by status for the summary under its title. */
export function viewFilterSummary(view: Pick<IssueView, "query">): { statuses: number; others: number } {
  const statuses = baselineFromQuery(view.query).raw.statusFilters.length;
  return { statuses, others: issueViewFilterCount(view.query) - (statuses > 0 ? 1 : 0) };
}

export interface ViewRowModel {
  /** The sidebar item id, `builtin:<key>` or `view:<id>`. */
  id: string;
  label: string;
  detail: string;
  starred: boolean;
}

/** "Workspace view · 3 statuses · +2 filters" */
export function viewDetail(item: SidebarViewItem, userId: string | null | undefined, strings: Strings): string {
  if (item.kind === "builtin") return strings.views.builtin;
  const { statuses, others } = viewFilterSummary(item.view);
  return [
    item.view.owner_id === userId ? strings.views.mine : strings.views.shared,
    statuses > 0 ? strings.views.filters.status(statuses) : null,
    others > 0 ? strings.views.filters.more(others) : null,
  ]
    .filter(Boolean)
    .join(" · ");
}

/** Starred items in menu order, then every other built-in and view. */
export function viewRows(
  starred: readonly SidebarViewItem[],
  views: readonly IssueView[],
  userId: string | null | undefined,
  strings: Strings,
): { starred: ViewRowModel[]; others: ViewRowModel[] } {
  const starredIds = new Set(starred.map((item) => item.id));
  const everything: SidebarViewItem[] = [
    ...BUILTIN_VIEW_KEYS.map((key) => ({ id: builtinViewItemId(key), kind: "builtin" as const, key })),
    ...views.map((view) => ({ id: savedViewItemId(view.id), kind: "view" as const, view })),
  ];
  const toRow = (item: SidebarViewItem, isStarred: boolean): ViewRowModel => ({
    id: item.id,
    label: viewItemLabel(item, strings),
    detail: viewDetail(item, userId, strings),
    starred: isStarred,
  });
  return {
    starred: starred.map((item) => toRow(item, true)),
    others: everything.filter((item) => !starredIds.has(item.id)).map((item) => toRow(item, false)),
  };
}

/** The item behind an id, whether or not it is starred; null once the view is gone. */
export function findViewItem(id: string, views: readonly IssueView[]): SidebarViewItem | null {
  const builtin = BUILTIN_VIEW_KEYS.find((key) => builtinViewItemId(key) === id);
  if (builtin) return { id, kind: "builtin", key: builtin };
  const view = views.find((candidate) => savedViewItemId(candidate.id) === id);
  return view ? { id, kind: "view", view } : null;
}
