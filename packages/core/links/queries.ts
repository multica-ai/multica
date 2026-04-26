import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueLink, IssueBlocker } from "../types";

export const linkKeys = {
  all: (wsId: string) => ["links", wsId] as const,
  forIssue: (wsId: string, issueId: string) =>
    [...linkKeys.all(wsId), "issue", issueId] as const,
  blockersForIssue: (wsId: string, issueId: string) =>
    [...linkKeys.all(wsId), "blockers", issueId] as const,
};

/** Stand-alone query for the dedicated /links endpoint. The Issue DTO
 *  already embeds links (L-PR#2), so most callers should read from
 *  `issue.links` rather than fetching this. Use this when you need the
 *  freshest server-truth list (e.g. after an out-of-band agent action). */
export function issueLinksOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: linkKeys.forIssue(wsId, issueId),
    queryFn: () => api.listIssueLinks(issueId, wsId),
    select: (data: IssueLink[]) => data,
  });
}

/** Open blockers for an issue. Used for the "blocked by" warning shown when
 *  a user tries to move an issue to an active status while incoming open
 *  blocks links exist. */
export function issueBlockersOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: linkKeys.blockersForIssue(wsId, issueId),
    queryFn: () => api.listIssueBlockers(issueId, wsId),
    select: (data: IssueBlocker[]) => data,
  });
}
