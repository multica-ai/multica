import type { ActorFilterValue, FilterSnapshot } from "../issues/stores/view-store";
import type { IssuePriority, IssueStatus, PropertyFilterValue } from "../types";
import { isKnownPropertyFilterOp, isPropertyOperatorFilter, propertyFilterValueKey } from "../types";
import { PRIORITY_DISPLAY_ORDER } from "../issues/config";

/**
 * The open saved view's query, normalized for two jobs:
 * - membership checks (`has`): the filter menu renders view-fixed VALUES as
 *   checked-and-disabled while everything else stays selectable;
 * - resets (`raw`): removing a user-added chip returns its dimension to the
 *   view's values, never to empty.
 * The view's own conditions never appear as chips — the view name carries
 * them; chips only show what the user layered on top.
 */
export interface IssueViewBaseline {
  status: Set<string>;
  priority: Set<string>;
  /** Actor keys as `${type}:${id}`. */
  assignee: Set<string>;
  includeNoAssignee: boolean;
  creator: Set<string>;
  project: Set<string>;
  includeNoProject: boolean;
  label: Set<string>;
  /** Property definition id → fixed member keys (`propertyFilterValueKey`). */
  property: Map<string, Set<string>>;
  /** Enum-sanitized snapshot, safe to hand straight to `resetFiltersTo`. */
  raw: FilterSnapshot;
}

export function actorFilterKey(actor: ActorFilterValue): string {
  return `${actor.type}:${actor.id}`;
}

function stringArray(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

/**
 * Sanitize one property filter's member list. Strings pass through (equality,
 * "true"/"false", "__none__"); operator objects must carry a known op and a
 * string value. Anything else — a hand-edited blob or an operator a future
 * client added — is dropped: a member the store cannot represent must not
 * enter the snapshot (same rule as the enum filters above).
 */
function propertyFilterValueArray(v: unknown): PropertyFilterValue[] {
  if (!Array.isArray(v)) return [];
  return v.filter(
    (x): x is PropertyFilterValue =>
      typeof x === "string" ||
      (isPropertyOperatorFilter(x) && isKnownPropertyFilterOp(x.op)),
  );
}

function actorArray(v: unknown): ActorFilterValue[] {
  if (!Array.isArray(v)) return [];
  return v.filter(
    (x): x is ActorFilterValue =>
      !!x && typeof x === "object" && typeof (x as ActorFilterValue).id === "string",
  );
}

export function baselineFromQuery(query: Record<string, unknown>): IssueViewBaseline {
  // Unknown enum members (a newer server, a hand-edited blob) are dropped —
  // a value the store cannot represent must not enter the snapshot.
  //
  // Status is the exception: since MUL-6243 a status filter holds a status KEY,
  // and a workspace's custom keys are not enumerable from a constant. Filtering
  // against ALL_STATUSES here silently deleted every custom status filter the
  // moment a saved view was reopened, so the view came back showing more than
  // it was saved with. Any non-empty string is a representable status key.
  const statusFilters = stringArray(query.statusFilters).filter(
    (s): s is IssueStatus => s.length > 0,
  );
  const priorityFilters = stringArray(query.priorityFilters).filter(
    (p): p is IssuePriority => (PRIORITY_DISPLAY_ORDER as readonly string[]).includes(p),
  );
  const assigneeFilters = actorArray(query.assigneeFilters);
  const creatorFilters = actorArray(query.creatorFilters);
  const projectFilters = stringArray(query.projectFilters);
  const labelFilters = stringArray(query.labelFilters);
  const includeNoAssignee = query.includeNoAssignee === true;
  const includeNoProject = query.includeNoProject === true;

  const propertyFilters: Record<string, PropertyFilterValue[]> = {};
  const property = new Map<string, Set<string>>();
  if (query.propertyFilters && typeof query.propertyFilters === "object") {
    for (const [id, selected] of Object.entries(
      query.propertyFilters as Record<string, unknown>,
    )) {
      const values = propertyFilterValueArray(selected);
      if (values.length > 0) {
        propertyFilters[id] = values;
        property.set(id, new Set(values.map(propertyFilterValueKey)));
      }
    }
  }

  return {
    status: new Set(statusFilters),
    priority: new Set(priorityFilters),
    assignee: new Set(assigneeFilters.map(actorFilterKey)),
    includeNoAssignee,
    creator: new Set(creatorFilters.map(actorFilterKey)),
    project: new Set(projectFilters),
    includeNoProject,
    label: new Set(labelFilters),
    property,
    raw: {
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
    },
  };
}
