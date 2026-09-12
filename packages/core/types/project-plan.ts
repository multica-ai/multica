// A project's decomposed plan: phases -> parts -> issues, plus the
// dependency edges and rollups the three Plan views (Document, Pipeline,
// Coverage) render. Mirrors `projectplan.Overview` in
// server/internal/projectplan/read.go — every number here comes from that
// read model, never computed or guessed client-side.

/** Coverage state of one plan part, computed server-side from its linked issues. */
export type ProjectPlanCoverageState =
  | "no_tasks_yet"
  | "covered_no_active_tasks"
  | "not_started"
  | "in_progress"
  | "complete";

export interface ProjectPlan {
  id: string;
  workspace_id: string;
  project_id: string;
  version: number;
  kind: string;
  origin: string;
  title: string;
  description: string;
  attributes: unknown;
  source_issue_id: string | null;
  superseded: boolean;
  superseded_at: string | null;
  created_by_type: string;
  created_by_id: string;
  created_at: string;
  updated_at: string;
}

export interface ProjectPlanRollup {
  tasks_done: number;
  tasks_total: number;
  percent: number;
  parts_covered: number;
  parts_total: number;
  parts_without_tasks: number;
}

export interface ProjectPlanTaskRollup {
  tasks_done: number;
  tasks_total: number;
  percent: number;
}

export interface ProjectPlanIssueDetail {
  id: string | null;
  number: number;
  identifier: string;
  title: string;
  status: string;
  status_category: string;
  assignee_type: string | null;
  assignee_id: string | null;
  deleted: boolean;
}

export interface ProjectPlanPart {
  id: string;
  title: string;
  description: string;
  acceptance_criteria: string;
  attributes: unknown;
  position: number;
  coverage_state: ProjectPlanCoverageState;
  rollup: ProjectPlanTaskRollup;
  issues: ProjectPlanIssueDetail[];
  created_at: string;
  updated_at: string;
}

export interface ProjectPlanPhase {
  id: string;
  title: string;
  description: string;
  attributes: unknown;
  position: number;
  rollup: ProjectPlanTaskRollup;
  parts: ProjectPlanPart[];
  created_at: string;
  updated_at: string;
}

export interface ProjectPlanDependencyNode {
  type: string;
  id: string;
  title: string;
  phase_id?: string | null;
  phase_title?: string | null;
  missing: boolean;
}

export interface ProjectPlanDependency {
  id: string;
  blocked: ProjectPlanDependencyNode;
  blocking: ProjectPlanDependencyNode;
}

export interface ProjectPlanUncoveredPart {
  id: string;
  title: string;
  position: number;
  phase_id: string;
  phase_title: string;
}

export interface ProjectPlanOverview {
  plan: ProjectPlan;
  rollup: ProjectPlanRollup;
  phases: ProjectPlanPhase[];
  dependencies: ProjectPlanDependency[];
  uncovered_parts: ProjectPlanUncoveredPart[];
}

// ---------------------------------------------------------------------------
// Write payloads (manual plan authoring)
//
// Derived from the audited service surface in
// server/internal/projectplan/service.go, not from a guessed HTTP contract:
// each type below mirrors one exported method's params struct, minus the
// fields the server owns.
//
// Deliberately absent everywhere:
//   * `created_by` — the handler takes the actor from the authenticated
//     request; a client-supplied creator would be forgeable.
//   * `workspace_id` / `plan_id` / `project_id` — path-scoped, never body.
//   * `origin`, `source_issue_id`, and the other `source_*` columns —
//     `CreateManual` hardcodes origin="manual" with a NULL source, which is
//     what `project_plan_source_provenance_check`
//     (server/migrations/467_project_plan.up.sql:45) requires. A manually
//     authored plan must never claim issue provenance, so there is no field
//     here through which it could.
// ---------------------------------------------------------------------------

/** `projectplan.CreateManualParams`. `kind` is "prd" — the only kind this release accepts. */
export interface CreateManualProjectPlanRequest {
  kind: string;
  title: string;
  description: string;
}

/** `projectplan.PlanPatch`. Every field optional: omitted means "leave alone". */
export interface UpdateProjectPlanRequest {
  title?: string;
  description?: string;
}

/**
 * `projectplan.SupersedeParams.Patch`. Archives the active version and clones
 * its structure forward into a new active version, applying this patch to the
 * new one.
 */
export interface SupersedeProjectPlanRequest {
  title?: string;
  description?: string;
}

/** `projectplan.CreatePhaseParams`. */
export interface CreateProjectPlanPhaseRequest {
  title: string;
  description: string;
  position: number;
}

/** `projectplan.PhasePatch`. */
export interface UpdateProjectPlanPhaseRequest {
  title?: string;
  description?: string;
  position?: number;
}

/** `projectplan.CreatePartParams`. */
export interface CreateProjectPlanPartRequest {
  title: string;
  description: string;
  acceptance_criteria: string;
  position: number;
}

/** `projectplan.PartPatch`. */
export interface UpdateProjectPlanPartRequest {
  title?: string;
  description?: string;
  acceptance_criteria?: string;
  position?: number;
}

/**
 * `ReorderPhases` / `ReorderParts`. The service's `validateExactOrder` rejects
 * anything that is not a permutation of every current sibling — so this must
 * always be the complete ordered set, never a partial move.
 */
export interface ReorderProjectPlanRequest {
  ordered_ids: string[];
}
