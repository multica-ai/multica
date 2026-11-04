import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { governanceKeys } from "./queries";
import type { ReviewFilters } from "./reviews-schema";

/** The scorecards the caller may read (owners, admins and the subject's leader). */
export function scorecardsOptions(wsId: string, filters: ReviewFilters = {}) {
  return queryOptions({
    queryKey: [...governanceKeys.all(wsId), "scorecards", filters.subject_type ?? "", filters.subject_id ?? ""] as const,
    queryFn: () => api.listScorecards(filters),
    enabled: !!wsId,
  });
}

/** The reviews the caller may read, newest first. */
export function reviewsOptions(wsId: string, filters: ReviewFilters = {}) {
  return queryOptions({
    queryKey: [...governanceKeys.all(wsId), "reviews", filters.subject_type ?? "", filters.subject_id ?? ""] as const,
    queryFn: () => api.listReviews(filters),
    enabled: !!wsId,
  });
}
