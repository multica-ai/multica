import type { CreateIssueRequest, Issue, IssueProperty, IssuePropertyValues } from "../types";
import { hasUnknownActorRef, isKnownPropertyType } from "../types/property";

type ParentContext = Pick<Issue, "id"> & Partial<Pick<Issue,
  "project_id" | "assignee_type" | "assignee_id"
>> & { identifier?: string | null };

/** Contextual defaults, not a backend inheritance contract. */
export function subIssueCreateDefaults(parent: ParentContext) {
  return {
    parent_issue_id: parent.id,
    parent_issue_identifier: parent.identifier ?? undefined,
    ...(parent.project_id ? { project_id: parent.project_id } : {}),
    // Omit the pair for an unassigned parent: preserve the last-used owner.
    ...(parent.assignee_type && parent.assignee_id
      ? { assignee_type: parent.assignee_type, assignee_id: parent.assignee_id }
      : {}),
  };
}

export type ParentCopyField = "priority" | "labels" | "start_date" | "due_date" | `property:${string}`;
export type ParentFieldCopy = Pick<CreateIssueRequest, "priority" | "label_ids" | "start_date" | "due_date" | "properties">;

export function copyableParentProperties(parent: Issue, catalog: IssueProperty[]) {
  return catalog.filter((property) =>
    !property.archived && isKnownPropertyType(property.type)
    && Object.hasOwn(parent.properties ?? {}, property.id)
    && !(property.type === "actor" && hasUnknownActorRef(parent.properties?.[property.id])),
  );
}

/** A detached snapshot of only explicitly selected, currently available fields. */
export function copyParentIssueFields(parent: Issue, fields: ParentCopyField[], catalog: IssueProperty[]): ParentFieldCopy {
  const selected = new Set(fields);
  const copy: ParentFieldCopy = {};
  if (selected.has("priority")) copy.priority = parent.priority;
  if (selected.has("labels")) copy.label_ids = (parent.labels ?? []).map((label) => label.id);
  if (selected.has("start_date") && parent.start_date) copy.start_date = parent.start_date;
  if (selected.has("due_date") && parent.due_date) copy.due_date = parent.due_date;
  const properties: IssuePropertyValues = {};
  for (const property of copyableParentProperties(parent, catalog)) {
    if (!selected.has(`property:${property.id}`)) continue;
    const value = parent.properties[property.id];
    if (value === undefined) continue;
    properties[property.id] = Array.isArray(value) ? [...value] : value;
  }
  if (Object.keys(properties).length > 0) copy.properties = properties;
  return copy;
}
