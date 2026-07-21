import type { Issue, UpdateIssueRequest } from "../../types";
import type { MyIssuesFilter } from "../queries";

/**
 * Whether an issue belongs to a filtered list cache.
 *
 * `"unknown"` means the client cannot decide from the entity alone — the
 * filter's predicate lives server-side (agent-ownership graph behind
 * `involves_user_id`, the my:all relation union) or the entity is missing the
 * field the filter keys on. Callers patch on `true`, remove on `false`, and
 * patch + invalidate on `"unknown"` — the "certain → patch, uncertain →
 * invalidate" rule applied to list membership.
 */
export type IssueMembership = true | false | "unknown";

/**
 * The field groups a write can touch that move an issue in or out of a
 * filtered list (assignee / project) or shift per-status bucket totals
 * (status). Creator is not here: it is immutable after create.
 */
export interface IssueChangedDims {
  assignee: boolean;
  project: boolean;
  status: boolean;
}

/**
 * Derive the changed dimensions from a mutation patch. `base` (the freshest
 * cached pre-write entity, usually the detail cache) sharpens the answer:
 * writing the same value an issue already has changes nothing. Without a
 * base, any written membership field counts as changed — conservative, at
 * worst one extra list refetch on settle.
 */
export function issueChangedDims(
  patch: Partial<Issue> | UpdateIssueRequest,
  base?: Issue,
): IssueChangedDims {
  const has = (field: string) =>
    Object.prototype.hasOwnProperty.call(patch, field);
  const p = patch as Partial<Issue>;
  return {
    assignee:
      (has("assignee_id") && (!base || base.assignee_id !== p.assignee_id)) ||
      (has("assignee_type") && (!base || base.assignee_type !== p.assignee_type)),
    project: has("project_id") && (!base || base.project_id !== p.project_id),
    status: has("status") && p.status !== undefined && (!base || base.status !== p.status),
  };
}

/**
 * Does this list's server contract depend on any of the changed dimensions?
 * `scope` is the myList scope segment from the query key (`undefined` for the
 * unfiltered workspace list). When this returns false the write cannot move
 * the issue in or out of the list, so a plain field patch is a complete
 * reconcile.
 */
export function listFilterDependsOn(
  scope: string | undefined,
  filter: MyIssuesFilter,
  changed: IssueChangedDims,
): boolean {
  // my:all is the union of assigned / created / involved — the assigned and
  // involved legs key on the assignee.
  if (scope === "all") return changed.assignee;
  if (
    changed.assignee &&
    (filter.assignee_id !== undefined ||
      filter.assignee_ids !== undefined ||
      filter.assignee_types !== undefined ||
      filter.involves_user_id !== undefined)
  ) {
    return true;
  }
  if (changed.project && filter.project_id !== undefined) return true;
  // creator_id filters never react to updates — creator is immutable.
  return false;
}

/**
 * Judge an issue against a list's server contract. AND semantics across
 * filter fields: any definitive miss is `false`; a predicate the client
 * cannot evaluate (or a field the partial entity is missing) degrades the
 * answer to `"unknown"` instead of guessing.
 */
export function issueMatchesListFilter(
  issue: Partial<Issue>,
  scope: string | undefined,
  filter: MyIssuesFilter,
): IssueMembership {
  // my:all — union across relations; the involved leg needs the server's
  // agent-ownership graph, so membership is never decidable client-side.
  if (scope === "all") return "unknown";

  let unknown = false;

  if (filter.assignee_id !== undefined) {
    if (issue.assignee_id === undefined) unknown = true;
    else if (issue.assignee_id !== filter.assignee_id) return false;
  }
  if (filter.assignee_ids !== undefined) {
    if (issue.assignee_id === undefined) unknown = true;
    else if (
      issue.assignee_id === null ||
      !filter.assignee_ids.includes(issue.assignee_id)
    ) {
      return false;
    }
  }
  if (filter.assignee_types !== undefined) {
    if (issue.assignee_type === undefined) unknown = true;
    else if (
      issue.assignee_type === null ||
      !filter.assignee_types.includes(issue.assignee_type)
    ) {
      return false;
    }
  }
  if (filter.creator_id !== undefined) {
    if (issue.creator_id === undefined) unknown = true;
    else if (issue.creator_id !== filter.creator_id) return false;
  }
  if (filter.project_id !== undefined) {
    if (issue.project_id === undefined) unknown = true;
    else if (issue.project_id !== filter.project_id) return false;
  }
  if (filter.involves_user_id !== undefined) {
    // Indirect-assignee predicate (owned agents / squads) — server-only.
    unknown = true;
  }

  return unknown ? "unknown" : true;
}
