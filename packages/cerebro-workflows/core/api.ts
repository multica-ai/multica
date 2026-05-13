import { api, parseWithFallback } from "@multica/core/api";
import {
  EMPTY_REGENERATE_INBOUND_SECRET,
  EMPTY_REGENERATE_INBOUND_TOKEN,
  EMPTY_REGENERATE_OUTBOUND_SECRET,
  EMPTY_WORKFLOW,
  EMPTY_WORKFLOWS_LIST,
  EMPTY_WORKFLOW_RUNS_LIST,
  regenerateInboundSigningSecretSchema,
  regenerateInboundTokenSchema,
  regenerateOutboundSecretSchema,
  workflowRunsListSchema,
  workflowSchema,
  workflowsListSchema,
} from "./api-schemas";
import type {
  CerebroWorkflow,
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
