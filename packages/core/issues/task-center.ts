/**
 * Product-facing views that live inside Tag's single Tasks route.
 *
 * The underlying API still calls these resources issues, inbox items, and
 * autopilots. Keeping the mapping here lets host adapters collapse their
 * legacy URLs without making the API or WebSocket vocabulary product-facing.
 */
export const TASK_CENTER_TABS = [
  "tasks",
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

/**
 * Maps legacy top-level product routes into the Tasks center. The route stays
 * workspace-neutral so web, desktop, and the Tag host can apply it at their
 * own navigation boundary.
 */
export function remapTaskCenterPath(path: string): string {
  const url = new URL(path, "https://multica.local");
  const match = /^\/([^/]+)\/(inbox|autopilots|my-issues)\/?$/.exec(url.pathname);
  if (!match) return path;

  const [, workspaceSlug, legacySegment] = match;
  const tab: TaskCenterTab =
    legacySegment === "inbox"
      ? "activity"
      : legacySegment === "autopilots"
        ? "automations"
        : "mine";
  url.pathname = `/${workspaceSlug}/issues`;
  url.searchParams.set("tab", tab);
  return `${url.pathname}${url.search}${url.hash}`;
}

export function taskCenterPath(workspaceSlug: string, tab: TaskCenterTab): string {
  return `/${encodeURIComponent(workspaceSlug)}/issues?tab=${tab}`;
}
