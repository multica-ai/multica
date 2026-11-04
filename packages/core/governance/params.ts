import {
  GOVERNANCE_ACTOR_TYPES,
  GOVERNANCE_EVENT_KINDS,
  GOVERNANCE_SUBJECT_TYPES,
  type GovernanceActorType,
  type GovernanceEventFilters,
  type GovernanceEventKind,
} from "./types";

/**
 * The log filters a page URL selects. A value the client does not offer is
 * ignored, so the log shows every event for that filter rather than none.
 */
export function parseGovernanceEventFilters(search: URLSearchParams): GovernanceEventFilters {
  const kind = search.get("kind") ?? "";
  const actorType = search.get("actor_type") ?? "";
  const subjectType = search.get("subject_type") ?? "";
  const filters: GovernanceEventFilters = {};
  if ((GOVERNANCE_EVENT_KINDS as readonly string[]).includes(kind)) filters.kind = kind as GovernanceEventKind;
  if ((GOVERNANCE_ACTOR_TYPES as readonly string[]).includes(actorType)) {
    filters.actor_type = actorType as GovernanceActorType;
  }
  if ((GOVERNANCE_SUBJECT_TYPES as readonly string[]).includes(subjectType)) filters.subject_type = subjectType;
  return filters;
}


/**
 * The query string of the governance log, shared by the page request and the
 * CSV download so the export always matches the rows on screen. Empty filters
 * are left out, so the server reads them as "all".
 */
export function governanceEventSearch(
  filters: GovernanceEventFilters,
  paging: { cursor?: string | null; limit?: number } = {},
): URLSearchParams {
  const search = new URLSearchParams();
  if (filters.kind) search.set("kind", filters.kind);
  if (filters.actor_type) search.set("actor_type", filters.actor_type);
  if (filters.subject_type) search.set("subject_type", filters.subject_type);
  if (paging.cursor) search.set("cursor", paging.cursor);
  if (paging.limit) search.set("limit", String(paging.limit));
  return search;
}
