import { workspaceWakeupsOptions } from "@multica/core/issues/wakeups";

/** The workspace's issue wakeups live on the autopilots route under `?tab=wakeups`. */
export const WAKEUPS_TAB = "wakeups";

export function isWakeupsTab(searchParams: URLSearchParams): boolean {
  return searchParams.get("tab") === WAKEUPS_TAB;
}

export function wakeupsHref(autopilotsPath: string): string {
  return `${autopilotsPath}?tab=${WAKEUPS_TAB}`;
}

/**
 * Per-scope counts plus the newest rule the platform paused. The sidebar's
 * count and the list's paused banner read this one query, so they share a
 * request.
 */
export function wakeupCountsOptions(wsId: string) {
  return workspaceWakeupsOptions(wsId, {
    scope: "paused",
    kind: "all",
    source: "",
    search: "",
    agent_id: "",
    offset: 0,
    limit: 1,
  });
}
