/**
 * Issue labels — workspace-global or project-scoped, applied as many-to-many
 * to issues.
 *
 * Labels are lightweight metadata (name + color) distinct from projects:
 * projects group related work, labels are cross-cutting tags (bug, feature,
 * performance, …). Colors are normalized to lowercase `#RRGGBB`.
 */
export interface Label {
  id: string;
  workspace_id: string;
  project_id: string | null;
  name: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
  created_at: string;
  updated_at: string;
}

export interface CreateLabelRequest {
  name: string;
  color: string;
  project_id?: string | null;
}

export interface UpdateLabelRequest {
  name?: string;
  color?: string;
}

export interface ListLabelsResponse {
  labels: Label[];
  total: number;
}

export interface IssueLabelsResponse {
  labels: Label[];
}
