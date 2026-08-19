/**
 * Product-facing views that live inside Tag's single Tasks route.
 *
 * The underlying API still calls these resources issues, inbox items, and
 * autopilots. Keeping the mapping here lets host adapters collapse their
 * legacy URLs without making the API or WebSocket vocabulary product-facing.
 */
export const TASK_CENTER_TABS = [
  "tasks",
  "projects",
  "mine",
  "activity",
  "automations",
] as const;

export type TaskCenterTab = (typeof TASK_CENTER_TABS)[number];

const TASK_CENTER_TAB_SET = new Set<string>(TASK_CENTER_TABS);

export function taskCenterTabFromSearch(search: URLSearchParams): TaskCenterTab {
  const tab = search.get("tab");
  return tab && TASK_CENTER_TAB_SET.has(tab) ? (tab as TaskCenterTab) : "tasks";
}

export function taskCenterPath(workspaceSlug: string, tab: TaskCenterTab): string {
  return `/${encodeURIComponent(workspaceSlug)}/issues?tab=${tab}`;
}
