import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectPlanKeys } from "./queries";
import type {
  CreateManualProjectPlanRequest,
  CreateProjectPlanPartRequest,
  CreateProjectPlanPhaseRequest,
  ReorderProjectPlanRequest,
  SupersedeProjectPlanRequest,
  UpdateProjectPlanPartRequest,
  UpdateProjectPlanPhaseRequest,
  UpdateProjectPlanRequest,
} from "../types";

/**
 * Write hooks for manual plan authoring.
 *
 * Every one of these re-reads the plan overview on settle instead of patching
 * the cache optimistically. That is deliberate: `coverage_state`, all three
 * rollups, and `uncovered_parts` are computed server-side
 * (server/internal/projectplan/read.go), and a client that guessed the new
 * values after an edit would be fabricating plan data — the one thing this
 * feature must never do. Adding a part briefly shows nothing rather than
 * showing an invented "0%".
 *
 * There is no business logic here. Validation, transactions, and position
 * locking live in the service and were audited across four Tier 2 passes; a
 * hook that pre-validated would be a second, drifting copy of those rules.
 */
function usePlanInvalidation(wsId: string) {
  const qc = useQueryClient();
  return () => {
    // `all` rather than `active`: superseding rotates which plan is active and
    // retains the old version, so any cached version read is stale too.
    void qc.invalidateQueries({ queryKey: projectPlanKeys.all(wsId) });
  };
}

/** `Service.CreateManual` — a from-scratch plan on a project that has none. */
export function useCreateManualProjectPlan(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: (data: CreateManualProjectPlanRequest) =>
      api.createManualProjectPlan(projectId, data),
    onSettled: invalidate,
  });
}

/** `Service.UpdatePlan` — plan-level title/description, in place. */
export function useUpdateProjectPlan(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({ planId, data }: { planId: string; data: UpdateProjectPlanRequest }) =>
      api.updateProjectPlan(projectId, planId, data),
    onSettled: invalidate,
  });
}

/** `Service.Supersede` — archive this version, clone it forward as a new active one. */
export function useSupersedeProjectPlan(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({ planId, data }: { planId: string; data: SupersedeProjectPlanRequest }) =>
      api.supersedeProjectPlan(projectId, planId, data),
    onSettled: invalidate,
  });
}

/** `Service.DeletePlan` — removes the plan and unlinks its issues; the issues survive. */
export function useDeleteProjectPlan(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: (planId: string) => api.deleteProjectPlan(projectId, planId),
    onSettled: invalidate,
  });
}

/** `Service.AddPhase` */
export function useCreateProjectPlanPhase(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({ planId, data }: { planId: string; data: CreateProjectPlanPhaseRequest }) =>
      api.createProjectPlanPhase(projectId, planId, data),
    onSettled: invalidate,
  });
}

/** `Service.UpdatePhase` */
export function useUpdateProjectPlanPhase(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({
      planId,
      phaseId,
      data,
    }: {
      planId: string;
      phaseId: string;
      data: UpdateProjectPlanPhaseRequest;
    }) => api.updateProjectPlanPhase(projectId, planId, phaseId, data),
    onSettled: invalidate,
  });
}

/** `Service.ReorderPhases` — `ordered_ids` must be every phase, exactly once. */
export function useReorderProjectPlanPhases(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({ planId, data }: { planId: string; data: ReorderProjectPlanRequest }) =>
      api.reorderProjectPlanPhases(projectId, planId, data),
    onSettled: invalidate,
  });
}

/** `Service.DeletePhase` — cascades to the phase's parts and their issue links. */
export function useDeleteProjectPlanPhase(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({ planId, phaseId }: { planId: string; phaseId: string }) =>
      api.deleteProjectPlanPhase(projectId, planId, phaseId),
    onSettled: invalidate,
  });
}

/** `Service.AddPart` */
export function useCreateProjectPlanPart(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({
      planId,
      phaseId,
      data,
    }: {
      planId: string;
      phaseId: string;
      data: CreateProjectPlanPartRequest;
    }) => api.createProjectPlanPart(projectId, planId, phaseId, data),
    onSettled: invalidate,
  });
}

/** `Service.UpdatePart` */
export function useUpdateProjectPlanPart(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({
      planId,
      partId,
      data,
    }: {
      planId: string;
      partId: string;
      data: UpdateProjectPlanPartRequest;
    }) => api.updateProjectPlanPart(projectId, planId, partId, data),
    onSettled: invalidate,
  });
}

/** `Service.ReorderParts` — scoped to one phase; `ordered_ids` is that phase's parts. */
export function useReorderProjectPlanParts(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({
      planId,
      phaseId,
      data,
    }: {
      planId: string;
      phaseId: string;
      data: ReorderProjectPlanRequest;
    }) => api.reorderProjectPlanParts(projectId, planId, phaseId, data),
    onSettled: invalidate,
  });
}

/** `Service.DeletePart` */
export function useDeleteProjectPlanPart(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({ planId, partId }: { planId: string; partId: string }) =>
      api.deleteProjectPlanPart(projectId, planId, partId),
    onSettled: invalidate,
  });
}

/** `Service.LinkIssue` — the issue must already live in this plan's project. */
export function useLinkProjectPlanPartIssue(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({
      planId,
      partId,
      issueId,
    }: {
      planId: string;
      partId: string;
      issueId: string;
    }) => api.linkProjectPlanPartIssue(projectId, planId, partId, issueId),
    onSettled: invalidate,
  });
}

/** `Service.UnlinkIssue` — drops the membership row only; the issue is untouched. */
export function useUnlinkProjectPlanPartIssue(wsId: string, projectId: string) {
  const invalidate = usePlanInvalidation(wsId);
  return useMutation({
    mutationFn: ({
      planId,
      partId,
      issueId,
    }: {
      planId: string;
      partId: string;
      issueId: string;
    }) => api.unlinkProjectPlanPartIssue(projectId, planId, partId, issueId),
    onSettled: invalidate,
  });
}
