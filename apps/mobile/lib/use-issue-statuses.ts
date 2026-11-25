/**
 * The workspace status catalog, resolved and memoized (MUL-6243).
 *
 * Mobile-owned mirror of `packages/core/issue-statuses/hooks.ts`. Two
 * deliberate differences:
 *
 * 1. **No `wsId` parameter.** Every mobile request is scoped by the
 *    `X-Workspace-Slug` header of the CURRENT workspace (see `data/api.ts`), so
 *    a catalog fetched under some other workspace's id would cache that
 *    workspace's key with this workspace's rows. Web passes an id because its
 *    inbox is cross-workspace; mobile's inbox is a single-workspace list, so
 *    reading the store is both simpler and the only correct option.
 * 2. **No `isPending` / `isError` / `retry`.** Web needs those because its
 *    board routes a status FILTER to server-side column branches and must hold
 *    its loading state rather than guess. Mobile filters client-side over an
 *    already-fetched list and groups on the concrete status key, so
 *    nothing here blocks on the catalog — a catalog that never arrives degrades
 *    to built-in labels or the raw custom key, without dropping any rows.
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueStatusListOptions } from "@/data/queries/issue-statuses";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  buildIssueStatusCatalog,
  isBuiltInIssueStatus,
  type IssueStatusCatalog,
} from "@/lib/issue-status";
import { useT } from "@/lib/i18n";

export function useIssueStatuses(): IssueStatusCatalog {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useT();
  const { data } = useQuery(issueStatusListOptions(wsId));
  return useMemo(() => {
    const catalog = buildIssueStatusCatalog(data);
    return {
      ...catalog,
      labelOf: (key: string) => {
        const label = catalog.labelOf(key);
        // Only i18next keys are translatable. A custom status name may itself
        // contain a colon, so its catalog name must pass through unchanged.
        return isBuiltInIssueStatus(key) ? t(label) : label;
      },
    };
  }, [data, t]);
}
