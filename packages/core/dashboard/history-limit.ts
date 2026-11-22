import { skipToken, useQuery, type QueryClient } from "@tanstack/react-query";
import { workspaceFeatureKeys, workspaceFeaturesOptions } from "../billing/paid-features";

/**
 * Dashboard history limit (MUL-9490).
 *
 * The plan caps how many calendar days back the dashboard can read. The
 * server cuts every dashboard response to it and reports it in a response
 * header; GET /features reports it ahead of time so the range menu can lock
 * what the plan does not cover. Both are null unless the limit is enforced, so
 * an observed rollout changes nothing users see.
 *
 * The features read is cached, so a plan that changes while the page is open
 * (a trial ending, a downgrade, an upgrade elsewhere) first shows up in a
 * dashboard response. That response's limit is recorded here and the features
 * read is refreshed; until the refresh lands the smaller of the two applies,
 * so the page never labels a range the server is already cutting short.
 */

export const dashboardHistoryObservedKey = (wsId: string) =>
  ["dashboard-history-observed", wsId] as const;

/** The limit a dashboard response was last served under, and when. */
export interface ObservedDashboardHistoryLimit {
  limitDays: number | null;
  observedAt: number;
}

/**
 * A dashboard response was served under a stricter limit than its query was
 * made under. Its rows cover less than the query's key promises, so the query
 * fails instead of caching them; the page has already moved to the stricter
 * limit's key by then.
 */
export class DashboardHistoryLimitTightenedError extends Error {
  constructor(
    readonly requestedUnder: number | null,
    readonly served: number,
  ) {
    super(`dashboard history limit tightened from ${requestedUnder ?? "none"} to ${served} days`);
    this.name = "DashboardHistoryLimitTightenedError";
  }
}

export function isDashboardHistoryLimitTightened(error: unknown): boolean {
  return error instanceof DashboardHistoryLimitTightenedError;
}

/** Whether `a` allows fewer days than `b`, where null means no limit. */
export function isStricterHistoryLimit(a: number | null, b: number | null): boolean {
  if (a === null) return false;
  return b === null || a < b;
}

/**
 * Called with the limit each dashboard response was served under.
 * `requestedUnder` is the limit the query was made under; any difference means
 * the client's view of the plan is out of date.
 *
 * Until a features read newer than the first such response confirms the plan,
 * later responses can only tighten what was observed, never loosen it: a
 * response served under the old, looser policy can still arrive after one
 * served under the new one, and must not reopen ranges the plan no longer
 * covers.
 */
export function reportDashboardHistoryLimit(
  client: QueryClient,
  wsId: string,
  requestedUnder: number | null,
  served: number | null,
): void {
  if (served === requestedUnder) return;
  const key = dashboardHistoryObservedKey(wsId);
  const previous = client.getQueryData<ObservedDashboardHistoryLimit>(key);
  const featuresUpdatedAt =
    client.getQueryState(workspaceFeatureKeys.all(wsId))?.dataUpdatedAt ?? 0;
  const pending = previous !== undefined && previous.observedAt > featuresUpdatedAt;
  client.setQueryData<ObservedDashboardHistoryLimit>(key, {
    limitDays: pending ? minHistoryLimit(previous.limitDays, served) : served,
    observedAt: Date.now(),
  });
  void client.invalidateQueries({ queryKey: workspaceFeatureKeys.all(wsId) });
}

/** The tighter of two limits, where null means none. */
export function minHistoryLimit(a: number | null, b: number | null): number | null {
  if (a === null) return b;
  if (b === null) return a;
  return Math.min(a, b);
}

/**
 * The limit to apply: the features read, tightened by a dashboard response
 * that arrived after it. Once the features read is newer than the
 * observation, it alone decides, so an upgrade lifts the limit again.
 */
export function resolveDashboardHistoryLimit(
  featureLimit: number | null,
  featuresUpdatedAt: number,
  observed: ObservedDashboardHistoryLimit | undefined,
): number | null {
  if (!observed || observed.observedAt <= featuresUpdatedAt) return featureLimit;
  return minHistoryLimit(featureLimit, observed.limitDays);
}

export interface DashboardHistoryLimitState {
  /** False until the features read has answered, successfully or not. */
  ready: boolean;
  /** The enforced limit in days, or null for none. */
  maxDays: number | null;
}

export function useDashboardHistoryLimit(wsId: string): DashboardHistoryLimitState {
  const features = useQuery(workspaceFeaturesOptions(wsId));
  const observed = useQuery<ObservedDashboardHistoryLimit>({
    queryKey: dashboardHistoryObservedKey(wsId),
    queryFn: skipToken,
    staleTime: Infinity,
  });
  const featureLimit = features.data?.limits?.dashboardHistoryDays ?? null;
  return {
    // A failed read counts as answered: the server still cuts every response,
    // and a dashboard that waited on it would never load.
    ready: !features.isPending,
    maxDays: resolveDashboardHistoryLimit(featureLimit, features.dataUpdatedAt, observed.data),
  };
}
