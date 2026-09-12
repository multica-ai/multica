import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import type { ProjectPlanOverview } from "../types";
import { projectPlanActiveOptions } from "./queries";

/**
 * The four states a Plan view (Document / Pipeline / Coverage) can be in.
 * Kept as one discriminated union, rather than separate booleans, so a
 * caller cannot accidentally render two of them at once — a failed request
 * must stay distinguishable from a genuine "no plan" (a project with a plan
 * being told it has none).
 */
export type PlanOverviewState =
  | { status: "loading" }
  | { status: "error"; retry: () => void }
  | { status: "no-plan" }
  | { status: "present"; data: ProjectPlanOverview };

/**
 * Resolves a project's active plan into exactly one of the four states above.
 *
 * `data === null` (a genuine 404) and a query error are different states on
 * purpose: the former is "this project has no plan yet" (PlanEmptyState is
 * correct), the latter is "the request failed" (say so, don't fall through to
 * the no-plan copy).
 */
export function usePlanOverview(wsId: string, projectId: string | undefined): PlanOverviewState {
  const { data, isPending, isError, refetch } = useQuery({
    ...projectPlanActiveOptions(wsId, projectId ?? ""),
    enabled: !!wsId && !!projectId,
  });
  const retry = useCallback(() => {
    void refetch();
  }, [refetch]);

  return useMemo<PlanOverviewState>(() => {
    if (isPending) return { status: "loading" };
    if (isError) return { status: "error", retry };
    if (data === null || data === undefined) return { status: "no-plan" };
    return { status: "present", data };
  }, [isPending, isError, data, retry]);
}
