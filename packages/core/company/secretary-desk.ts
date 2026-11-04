import { queryOptions } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import type { Issue } from "../types";

/** Page size per request; the server caps a page at this many rows. */
const DESK_PAGE_SIZE = 100;

type DeskIssueParams = Omit<NonNullable<Parameters<typeof api.listIssues>[0]>, "limit" | "offset">;

/**
 * Reads every row the filter matches, page by page, so the desk never drops
 * older requests or directives. `total` from the server is authoritative.
 */
async function listAllDeskIssues(params: DeskIssueParams): Promise<Issue[]> {
  const issues: Issue[] = [];
  for (;;) {
    const page = await api.listIssues({ ...params, limit: DESK_PAGE_SIZE, offset: issues.length });
    issues.push(...page.issues);
    if (page.issues.length === 0 || issues.length >= page.total) return issues;
  }
}

// Under the issue key tree, so issue realtime invalidation refreshes the desk.
export const secretaryDeskKeys = {
  directives: (wsId: string) => [...issueKeys.all(wsId), "secretary-desk", "directives"] as const,
  requests: (wsId: string, secretaryId: string) =>
    [...issueKeys.all(wsId), "secretary-desk", "requests", secretaryId] as const,
};

/** Issues the boss's chat messages created as directives (origin_type = "directive"). */
export function secretaryDeskDirectivesOptions(wsId: string) {
  return queryOptions({
    queryKey: secretaryDeskKeys.directives(wsId),
    queryFn: async () => {
      return listAllDeskIssues({ origin_type: "directive" });
    },
    enabled: !!wsId,
  });
}

/**
 * Squad requests whose parent issue is assigned to the Secretary. The list API
 * cannot filter by parent, so both sides are read and joined here.
 */
export function secretaryDeskRequestsOptions(wsId: string, secretaryId: string | null) {
  return queryOptions({
    queryKey: secretaryDeskKeys.requests(wsId, secretaryId ?? ""),
    queryFn: async (): Promise<Issue[]> => {
      if (!secretaryId) return [];
      const [parents, requests] = await Promise.all([
        listAllDeskIssues({ assignee_id: secretaryId, assignee_types: ["agent"] }),
        listAllDeskIssues({ origin_type: "squad_request" }),
      ]);
      const parentIds = new Set(parents.map((issue) => issue.id));
      return requests.filter(
        (issue) => issue.parent_issue_id !== null && parentIds.has(issue.parent_issue_id),
      );
    },
    enabled: !!wsId && !!secretaryId,
  });
}

const directiveMetadataSchema = z.object({
  directive: z.object({ quote: z.string().trim().min(1) }),
});

/** The boss's words a directive stored in metadata.directive.quote, or null. */
export function directiveQuote(metadata: unknown): string | null {
  const parsed = directiveMetadataSchema.safeParse(metadata);
  return parsed.success ? parsed.data.directive.quote : null;
}

export type SecretaryDeskItem = {
  issue: Issue;
  kind: "directive" | "request";
  quote: string | null;
};

/** One list, newest first. A directive and a request are told apart by their source. */
export function secretaryDeskItems(
  directives: readonly Issue[],
  requests: readonly Issue[],
): SecretaryDeskItem[] {
  const items: SecretaryDeskItem[] = [
    ...directives.map((issue) => ({ issue, kind: "directive" as const, quote: directiveQuote(issue.metadata) })),
    ...requests.map((issue) => ({ issue, kind: "request" as const, quote: null })),
  ];
  return items.sort((a, b) => Date.parse(b.issue.created_at) - Date.parse(a.issue.created_at));
}
