import { api, parseWithFallback } from "@multica/core/api";
import {
  EMPTY_ACTIVATE_WORKFLOW,
  EMPTY_ACTIVE_WORKFLOW_FOR_ISSUE,
  EMPTY_LOOP_STATE,
  EMPTY_REGENERATE_INBOUND_SECRET,
  EMPTY_REGENERATE_INBOUND_TOKEN,
  EMPTY_REGENERATE_OUTBOUND_SECRET,
  EMPTY_ISSUE_LOOP_RUNS,
  EMPTY_WORKFLOW,
  EMPTY_WORKFLOWS_LIST,
  EMPTY_WORKFLOW_RUNS_LIST,
  activateWorkflowSchema,
  activeWorkflowForIssueSchema,
  issueLoopRunsSchema,
  loopStateSchema,
  regenerateInboundSigningSecretSchema,
  regenerateInboundTokenSchema,
  regenerateOutboundSecretSchema,
  workflowRunsListSchema,
  workflowSchema,
  workflowsListSchema,
} from "./api-schemas";
import type {
  ActivateWorkflowResponse,
  ActiveWorkflowForIssueResponse,
  CerebroWorkflow,
  IssueLoopRunsResponse,
  LoopStateResponse,
  RegenerateInboundSigningSecretResponse,
  RegenerateInboundTokenResponse,
  RegenerateOutboundSecretResponse,
  WorkflowRunsListResponse,
  WorkflowsListResponse,
  WorkflowWriteInput,
} from "./types";

// All cerebro-workflows reads route through parseWithFallback so an older
// backend that's missing phase-3 fields (or a future one that drops fields
// the UI relies on) degrades to safe defaults instead of white-screening the
// editor or the runs page. See CLAUDE.md "API Response Compatibility".

export async function fetchWorkflows(): Promise<WorkflowsListResponse> {
  const raw = await api.listCerebroWorkflows<unknown>();
  return parseWithFallback(raw, workflowsListSchema, EMPTY_WORKFLOWS_LIST, {
    endpoint: "listCerebroWorkflows",
  });
}

export async function fetchWorkflow(id: string): Promise<CerebroWorkflow> {
  const raw = await api.getCerebroWorkflow<unknown>(id);
  return parseWithFallback(raw, workflowSchema, EMPTY_WORKFLOW, {
    endpoint: "getCerebroWorkflow",
  });
}

export async function createWorkflow(payload: WorkflowWriteInput): Promise<CerebroWorkflow> {
  const raw = await api.createCerebroWorkflow<unknown>(payload);
  return parseWithFallback(raw, workflowSchema, EMPTY_WORKFLOW, {
    endpoint: "createCerebroWorkflow",
  });
}

export async function updateWorkflow(
  id: string,
  payload: WorkflowWriteInput,
): Promise<CerebroWorkflow> {
  const raw = await api.updateCerebroWorkflow<unknown>(id, payload);
  return parseWithFallback(raw, workflowSchema, EMPTY_WORKFLOW, {
    endpoint: "updateCerebroWorkflow",
  });
}

export async function toggleWorkflow(id: string, enabled: boolean): Promise<{ enabled: boolean }> {
  return api.toggleCerebroWorkflow<{ enabled: boolean }>(id, enabled);
}

export async function deleteWorkflow(id: string): Promise<void> {
  await api.deleteCerebroWorkflow(id);
}

export async function fetchWorkflowRuns(filter: {
  workflowId?: string | null;
  limit?: number;
  offset?: number;
}): Promise<WorkflowRunsListResponse> {
  const raw = await api.listCerebroWorkflowRuns<unknown>(filter);
  return parseWithFallback(raw, workflowRunsListSchema, EMPTY_WORKFLOW_RUNS_LIST, {
    endpoint: "listCerebroWorkflowRuns",
  });
}

// Phase 3 (JEH-1108). Three regenerate endpoints — each mints a fresh
// token/secret server-side and returns the plaintext exactly once. The
// schemas default missing string fields to "" so a malformed response
// renders an empty input rather than crashing on `.length`.
export async function regenerateInboundToken(
  id: string,
): Promise<RegenerateInboundTokenResponse> {
  const raw = await api.regenerateCerebroInboundToken<unknown>(id);
  return parseWithFallback(
    raw,
    regenerateInboundTokenSchema,
    EMPTY_REGENERATE_INBOUND_TOKEN,
    { endpoint: "regenerateCerebroInboundToken" },
  );
}

export async function regenerateInboundSigningSecret(
  id: string,
): Promise<RegenerateInboundSigningSecretResponse> {
  const raw = await api.regenerateCerebroInboundSigningSecret<unknown>(id);
  return parseWithFallback(
    raw,
    regenerateInboundSigningSecretSchema,
    EMPTY_REGENERATE_INBOUND_SECRET,
    { endpoint: "regenerateCerebroInboundSigningSecret" },
  );
}

export async function regenerateOutboundSecret(
  id: string,
): Promise<RegenerateOutboundSecretResponse> {
  const raw = await api.regenerateCerebroOutboundSecret<unknown>(id);
  return parseWithFallback(
    raw,
    regenerateOutboundSecretSchema,
    EMPTY_REGENERATE_OUTBOUND_SECRET,
    { endpoint: "regenerateCerebroOutboundSecret" },
  );
}

// FIR-2283 — Issue workflow control strip + approval. Routed through the
// generic cerebroRequest primitive (no dedicated client.ts method needed —
// see CEREBRO-PATCH(cerebro-api-request)).
export async function fetchLoopState(
  workflowId: string,
  issueId: string,
): Promise<LoopStateResponse> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/workflows/${workflowId}/loop-state?issue_id=${encodeURIComponent(issueId)}`,
  );
  return parseWithFallback(raw, loopStateSchema, EMPTY_LOOP_STATE, {
    endpoint: "loopState",
  });
}

export async function approveHumanCheck(
  workflowId: string,
  checkId: string,
  input: { issue_id: string; approved: boolean; note?: string },
): Promise<void> {
  await api.cerebroRequest<unknown>(
    `/api/cerebro/workflows/${workflowId}/human-checks/${checkId}/approve`,
    { method: "POST", body: JSON.stringify(input) },
  );
}

// FIR-2283 v2 point 8 — "per-issue workflow activation". Activates a saved
// Issue workflow recipe (workflowId) on one specific issue: the recipe stays
// a reusable template, this compiles an independent set of rules for that
// issue alone.
export async function activateWorkflow(
  workflowId: string,
  issueId: string,
): Promise<ActivateWorkflowResponse> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/workflows/${workflowId}/activate`,
    { method: "POST", body: JSON.stringify({ issue_id: issueId }) },
  );
  return parseWithFallback(raw, activateWorkflowSchema, EMPTY_ACTIVATE_WORKFLOW, {
    endpoint: "activateWorkflow",
  });
}

// fetchActiveWorkflowForIssue reads back which recipe (if any) issueId is
// currently running.
export async function fetchActiveWorkflowForIssue(
  issueId: string,
): Promise<ActiveWorkflowForIssueResponse> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/workflows/for-issue/${encodeURIComponent(issueId)}`,
  );
  return parseWithFallback(raw, activeWorkflowForIssueSchema, EMPTY_ACTIVE_WORKFLOW_FOR_ISSUE, {
    endpoint: "activeWorkflowForIssue",
  });
}

// FIR-2283 v2 point 7 — the issues that have run through workflowId's loop.
export async function fetchWorkflowLoopRuns(
  workflowId: string,
): Promise<IssueLoopRunsResponse> {
  const raw = await api.cerebroRequest<unknown>(
    `/api/cerebro/workflows/${encodeURIComponent(workflowId)}/loop-runs`,
  );
  return parseWithFallback(raw, issueLoopRunsSchema, EMPTY_ISSUE_LOOP_RUNS, {
    endpoint: "workflowLoopRuns",
  });
}
