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

/**
 * Optional automation attached to a custom status.
 *
 * v2b (FIR-1550): when an issue enters a status with triggers, the backend
 * fires them. `assign_to_*` reassigns the issue; `fire_agent_id` enqueues a
 * new run for that agent. Both fields are independent — either or both can
 * be unset. The trigger engine itself lands in the follow-up PR; the schema
 * is in place so the editor can configure and persist them now.
 */
export interface StatusTriggers {
  assign_to_id?: string;
  assign_to_type?: "member" | "agent";
  fire_agent_id?: string;
}

/** One custom status within a model. position is server-assigned by order. */
export interface StatusEntry {
  key: string;
  label: string;
  color: string;
  base_status: string;
  position: number;
  /**
   * Required for v2b — what agents read to understand what this status
   * means. The backend validates that it is non-empty on create/update.
   * Optional in the type so reads of pre-v2b rows degrade gracefully; the
   * editor enforces "required" on write.
   */
  description?: string;
  triggers?: StatusTriggers;
}

export interface CerebroStatusModel {
  id: string;
  workspace_id: string;
  name: string;
  description?: string;
  statuses: StatusEntry[];
  /** How many projects currently use this model (drives the overview panel). */
  project_count: number;
  /**
   * FIR-2800: when true, new projects are auto-assigned this model on create.
   * At most one model per workspace may have this flag set.
   */
  workspace_default: boolean;
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
    description: string;
    triggers?: StatusTriggers;
  }>;
}

/**
 * Per-issue custom status pin (v2b). Returned by GET
 * /api/cerebro/issues/{id}/custom-status. label + description are joined in
 * from the model so the UI / agents never have to look up the model
 * separately.
 */
export interface IssueCustomStatus {
  issue_id: string;
  workspace_id: string;
  status_model_id: string;
  custom_status_key: string;
  base_status: string;
  set_by_id: string;
  set_by_type: "member" | "agent" | "system";
  label?: string;
  description?: string;
  created_at: string;
  updated_at: string;
}
