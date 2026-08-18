import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { buildIssueStatusCatalog, issueStatusListOptions, type IssueStatusCatalog } from "./queries";

/**
 * The workspace status catalog, resolved and memoized (MUL-6243).
 *
 * Takes `wsId` explicitly rather than reading it from context, per the repo's
 * state rules — the catalog is per-workspace, and switching workspaces does not
 * remount the app, so a module-level snapshot would go stale.
 */
export function useIssueStatuses(wsId: string): IssueStatusCatalog {
  const { data } = useQuery({ ...issueStatusListOptions(wsId), enabled: Boolean(wsId) });
  return useMemo(() => buildIssueStatusCatalog(data), [data]);
}
