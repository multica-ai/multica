import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { buildIssueStatusCatalog, issueStatusListOptions, type IssueStatusCatalog } from "./queries";

/**
 * The workspace status catalog, resolved and memoized (MUL-6243).
 *
 * Takes `wsId` explicitly rather than reading it from context, per the repo's
 * state rules — the catalog is per-workspace, and switching workspaces does not
 * remount the app, so a module-level snapshot would go stale.
 */
export function useIssueStatuses(wsId: string): IssueStatusCatalog {
  const { data, isPending, isError, refetch } = useQuery({
    ...issueStatusListOptions(wsId),
    enabled: Boolean(wsId),
  });
  // `refetch` travels with the catalog, not just its failure flag: a surface
  // that blocks on a failed catalog needs something to offer the user, and
  // reaching back into React Query from the view layer would duplicate the
  // query key. (MUL-6243)
  const retry = useCallback(() => {
    void refetch();
  }, [refetch]);
  // pending/error are carried through, not dropped: a surface that routes a
  // CUSTOM status key cannot distinguish "catalog not here yet" from "catalog
  // failed" without them, and both need different UI — a spinner and a retry.
  // (MUL-6243)
  return useMemo(
    () => buildIssueStatusCatalog(data, { isPending, isError, retry }),
    [data, isPending, isError, retry],
  );
}
