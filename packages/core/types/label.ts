/**
 * Issue labels — workspace-scoped, applied as many-to-many to issues.
 *
 * Labels are lightweight metadata (name + color) distinct from projects:
 * projects group related work, labels are cross-cutting tags (bug, feature,
 * performance, …). Colors are normalized to lowercase `#RRGGBB`.
 */
export type LabelResourceType = "issue" | "agent" | "skill";

export interface Label {
  id: string;
  workspace_id: string;
  resource_type?: LabelResourceType;
  name: string;
  description?: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export interface CreateLabelRequest {
  resource_type?: LabelResourceType;
  name: string;
  description?: string;
  color: string;
}

export interface UpdateLabelRequest {
  name?: string;
  description?: string;
  color?: string;
}

export interface ListLabelsResponse {
  labels: Label[];
  total: number;
}

export interface IssueLabelsResponse {
  labels: Label[];
}

export type ResourceLabelsResponse = IssueLabelsResponse;
