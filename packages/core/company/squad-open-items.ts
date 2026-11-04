import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import type { Issue } from "../types";

/** Rows the room panel shows; the rest are counted, not listed. */
export const SQUAD_OPEN_ITEMS_LIMIT = 20;

export const squadOpenItemsKeys = {
  squad: (wsId: string, squadId: string) => [...issueKeys.all(wsId), "squad-open-items", squadId] as const,
};

/** A squad's unfinished issues, newest first, with the total for the "+N" count. */
export function squadOpenItemsOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: squadOpenItemsKeys.squad(wsId, squadId),
    queryFn: async (): Promise<{ issues: Issue[]; total: number }> => {
      const res = await api.listIssues({
        assignee_id: squadId,
        assignee_types: ["squad"],
        open_only: true,
        limit: SQUAD_OPEN_ITEMS_LIMIT,
      });
      return { issues: res.issues, total: res.total };
    },
    enabled: !!wsId && !!squadId,
  });
}
