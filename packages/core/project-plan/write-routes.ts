/**
 * THE one place any project-plan write URL or HTTP verb is written down.
 *
 * These are the server's real routes, as implemented in
 * `server/internal/handler/project_plan.go`. The rest of the contract —
 * argument shapes, domain errors, which operations exist — comes from the
 * service (`server/internal/projectplan/service.go`) and
 * `server/internal/projectplan/errors.go`.
 *
 * If a route moves, this file is where the move stays a one-file edit:
 * `ApiClient`'s plan-write methods hold no path literals, and the mutation
 * hooks and every dialog reach the API only through those methods. Correcting
 * a route here propagates everywhere with no other change.
 *
 * Every route is gated on `project_plans` and answers 404 when the flag is
 * off — not 401/403/500. See `classifyPlanWriteError` for why the 404 copy has
 * to read for both "this item is gone" and "plans are off here".
 *
 * `CreateFromIssue` and `DeleteImpact` are server routes but are not part of
 * this table: issue-sourced plan creation has its own flow, and the delete
 * confirmations state the consequence in words rather than fetching a count.
 * Adding either means adding its route here.
 */

/** HTTP verb per operation. Both reorders are PATCH, not POST. */
export const PLAN_WRITE_METHODS = {
  createPlan: "POST",
  updatePlan: "PATCH",
  supersedePlan: "POST",
  deletePlan: "DELETE",
  addPhase: "POST",
  updatePhase: "PATCH",
  reorderPhases: "PATCH",
  deletePhase: "DELETE",
  addPart: "POST",
  updatePart: "PATCH",
  reorderParts: "PATCH",
  deletePart: "DELETE",
  linkIssue: "POST",
  unlinkIssue: "DELETE",
} as const;

const plans = (projectId: string) => `/api/projects/${projectId}/plans`;
const plan = (projectId: string, planId: string) => `${plans(projectId)}/${planId}`;

export const planWriteRoutes = {
  /** `Service.CreateManual` */
  createPlan: (projectId: string) => plans(projectId),
  /** `Service.UpdatePlan` */
  updatePlan: (projectId: string, planId: string) => plan(projectId, planId),
  /** `Service.Supersede` */
  supersedePlan: (projectId: string, planId: string) => `${plan(projectId, planId)}/supersede`,
  /** `Service.DeletePlan` */
  deletePlan: (projectId: string, planId: string) => plan(projectId, planId),

  /** `Service.AddPhase` */
  addPhase: (projectId: string, planId: string) => `${plan(projectId, planId)}/phases`,
  /** `Service.UpdatePhase` */
  updatePhase: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}`,
  /** `Service.ReorderPhases` */
  reorderPhases: (projectId: string, planId: string) =>
    `${plan(projectId, planId)}/phases/reorder`,
  /** `Service.DeletePhase` */
  deletePhase: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}`,

  /** `Service.AddPart` */
  addPart: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}/parts`,
  /** `Service.UpdatePart` */
  updatePart: (projectId: string, planId: string, partId: string) =>
    `${plan(projectId, planId)}/parts/${partId}`,
  /** `Service.ReorderParts` — scoped to one phase; the service reorders that phase's parts only. */
  reorderParts: (projectId: string, planId: string, phaseId: string) =>
    `${plan(projectId, planId)}/phases/${phaseId}/parts/reorder`,
  /** `Service.DeletePart` */
  deletePart: (projectId: string, planId: string, partId: string) =>
    `${plan(projectId, planId)}/parts/${partId}`,

  /** `Service.LinkIssue` — the issue id is in the path; there is no request body. */
  linkIssue: (projectId: string, planId: string, partId: string, issueId: string) =>
    `${plan(projectId, planId)}/parts/${partId}/issues/${issueId}`,
  /** `Service.UnlinkIssue` */
  unlinkIssue: (projectId: string, planId: string, partId: string, issueId: string) =>
    `${plan(projectId, planId)}/parts/${partId}/issues/${issueId}`,
} as const;
