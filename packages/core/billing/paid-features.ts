import { queryOptions, useQuery, type QueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { PaidFeature, WorkspaceFeatureState } from "../types";

/**
 * Paid features (MUL-9482).
 *
 * The server says, per workspace, whether each paid feature is `available` or
 * `locked`. Locked never takes anything away: existing configuration keeps
 * working and can always be reduced or removed, but nothing can be added. A
 * component turns that into one of three UI states for the resource it shows:
 *
 * - `available`: use it normally.
 * - `read_only`: locked, and this resource already uses the feature. Show
 *   "Read-only. Upgrade to edit." and hide only the add/widen actions.
 * - `unavailable`: locked, and this resource does not use the feature yet.
 *   Show the upgrade prompt where the feature would be turned on.
 */
export type PaidFeatureAccessState = "available" | "read_only" | "unavailable";

export const FEATURE_LOCKED_CODE = "feature_locked";

export const workspaceFeatureKeys = {
  all: (wsId: string) => ["workspace-features", wsId] as const,
};

/**
 * Short stale time plus refetch on focus: a plan change or a trial ending
 * happens outside this tab (Stripe, another device, a clock), and the read is
 * cheap.
 */
export function workspaceFeaturesOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceFeatureKeys.all(wsId),
    queryFn: () => api.getWorkspaceFeatures(wsId),
    enabled: wsId.length > 0,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function resolvePaidFeatureAccess(
  state: WorkspaceFeatureState,
  hasExistingConfig: boolean,
): PaidFeatureAccessState {
  if (state === "available") return "available";
  return hasExistingConfig ? "read_only" : "unavailable";
}

export interface PaidFeatureAccess {
  /** Null while the state is loading or could not be read. */
  state: PaidFeatureAccessState | null;
  isPending: boolean;
  isError: boolean;
  /**
   * Whether add/widen actions are offered. False while loading so a locked
   * workspace never flashes an editable form; true when the read failed,
   * because the server still decides on write and a failed read must not
   * show an upgrade prompt to a workspace that may well have the feature.
   */
  canAdd: boolean;
  /** Reducing or removing configuration is never restricted by plan. */
  canReduce: true;
}

/**
 * The paid-feature access for one resource in one workspace.
 * `hasExistingConfig` describes that resource only (this project is private),
 * never the workspace as a whole.
 */
export function usePaidFeatureAccess(
  wsId: string,
  feature: PaidFeature,
  { hasExistingConfig }: { hasExistingConfig: boolean },
): PaidFeatureAccess {
  const query = useQuery(workspaceFeaturesOptions(wsId));
  if (query.isPending) {
    return { state: null, isPending: true, isError: false, canAdd: false, canReduce: true };
  }
  if (query.isError) {
    return { state: null, isPending: false, isError: true, canAdd: true, canReduce: true };
  }
  const state = resolvePaidFeatureAccess(
    query.data?.features?.[feature] ?? "available",
    hasExistingConfig,
  );
  return { state, isPending: false, isError: false, canAdd: state === "available", canReduce: true };
}

/** The server refused to add to a paid feature the workspace's plan lacks. */
export function isFeatureLockedError(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 403) return false;
  const body = error.body;
  return (
    typeof body === "object" &&
    body !== null &&
    (body as { code?: unknown }).code === FEATURE_LOCKED_CODE
  );
}

/**
 * A feature_locked refusal means this client's view of the plan was stale:
 * refetch it so the form switches to its read-only or upgrade state.
 */
export function refreshFeaturesOnLockedError(
  queryClient: QueryClient,
  wsId: string,
  error: unknown,
): void {
  if (isFeatureLockedError(error)) {
    void queryClient.invalidateQueries({ queryKey: workspaceFeatureKeys.all(wsId) });
  }
}
