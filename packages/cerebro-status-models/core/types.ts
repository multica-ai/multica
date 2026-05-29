// Cerebro workflow v2a status models (FIR-1550) — shared types.
//
// A status model is a reusable, workspace-level status pipeline a project can
// opt into. Every custom status binds to one of the 7 upstream base statuses,
// so the rest of Multica keeps reading the base status unchanged.

/** The 7 upstream base statuses a custom status can map to. */
export const BASE_STATUSES = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
] as const;

export type BaseStatus = (typeof BASE_STATUSES)[number];

/** One custom status within a model. position is server-assigned by order. */
export interface StatusEntry {
  key: string;
  label: string;
  color: string;
  base_status: string;
  position: number;
}

export interface CerebroStatusModel {
  id: string;
  workspace_id: string;
  name: string;
  description?: string;
  statuses: StatusEntry[];
  /** How many projects currently use this model (drives the overview panel). */
  project_count: number;
  created_by_id: string;
  created_by_type: string;
  created_at: string;
  updated_at: string;
}

export interface StatusModelsListResponse {
  status_models: CerebroStatusModel[];
}

/** A project → model link. Absence of a link = project uses default statuses. */
export interface ProjectStatusAssignment {
  project_id: string;
  workspace_id: string;
  status_model_id: string;
  created_at: string;
  updated_at: string;
}

export interface AssignmentsListResponse {
  assignments: ProjectStatusAssignment[];
}

/** Payload for create/update. position is derived server-side from order. */
export interface StatusModelWriteInput {
  name: string;
  description?: string;
  statuses: Array<{
    key: string;
    label: string;
    color: string;
    base_status: string;
  }>;
}
