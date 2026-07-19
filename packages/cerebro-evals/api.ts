import { api, parseWithFallback } from "@multica/core/api";
import { evalBindingSchema, evalBindingsListSchema, evalRunSchema, evalRunsListSchema, evalScheduleSchema, evalSchema, evalsListSchema } from "./schemas";
import type { CerebroEval, EvalBinding, EvalRun, EvalSchedule, EvalScheduleInput, EvalWriteInput } from "./types";

export async function fetchEvals(): Promise<CerebroEval[]> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/evals");
  return parseWithFallback(raw, evalsListSchema, { evals: [] }, { endpoint: "listCerebroEvals" }).evals as CerebroEval[];
}

export async function createEval(input: EvalWriteInput): Promise<CerebroEval> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/evals", { method: "POST", body: JSON.stringify(input) });
  const parsed = parseWithFallback(raw, evalSchema, null, { endpoint: "createCerebroEval" });
  if (!parsed) throw new Error("Invalid create eval response");
  return parsed as CerebroEval;
}

export async function updateEval(id: string, input: EvalWriteInput): Promise<CerebroEval> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/evals/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
  const parsed = parseWithFallback(raw, evalSchema, null, { endpoint: "updateCerebroEval" });
  if (!parsed) throw new Error("Invalid update eval response");
  return parsed as CerebroEval;
}

export async function deleteEval(id: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/evals/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function fetchEvalRuns(evalId: string): Promise<EvalRun[]> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/evals/${encodeURIComponent(evalId)}/runs`);
  return parseWithFallback(raw, evalRunsListSchema, { runs: [] }, { endpoint: "listCerebroEvalRuns" }).runs as EvalRun[];
}

export async function fetchEvalBindings(workflowId?: string): Promise<EvalBinding[]> {
  const query = workflowId ? `?workflow_id=${encodeURIComponent(workflowId)}` : "";
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/evals/bindings${query}`);
  return parseWithFallback(raw, evalBindingsListSchema, { bindings: [] }, { endpoint: "listCerebroEvalBindings" }).bindings as EvalBinding[];
}

export async function createEvalBinding(input: { workflow_id: string; eval_id: string; phase: string; blocking: boolean }): Promise<EvalBinding> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/evals/bindings", { method: "POST", body: JSON.stringify(input) });
  const parsed = parseWithFallback(raw, evalBindingSchema, null, { endpoint: "createCerebroEvalBinding" });
  if (!parsed) throw new Error("Invalid create eval binding response");
  return parsed as EvalBinding;
}

export async function deleteEvalBinding(id: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/evals/bindings/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function fetchEvalSchedule(evalId: string): Promise<EvalSchedule | null> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/evals/${encodeURIComponent(evalId)}/schedule`);
  if (raw === null) return null;
  const parsed = parseWithFallback(raw, evalScheduleSchema, null, { endpoint: "getCerebroEvalSchedule" });
  return parsed as EvalSchedule | null;
}

export async function upsertEvalSchedule(evalId: string, input: EvalScheduleInput): Promise<EvalSchedule> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/evals/${encodeURIComponent(evalId)}/schedule`, {
    method: "PUT", body: JSON.stringify(input),
  });
  const parsed = parseWithFallback(raw, evalScheduleSchema, null, { endpoint: "upsertCerebroEvalSchedule" });
  if (!parsed) throw new Error("Invalid eval schedule response");
  return parsed as EvalSchedule;
}

export async function deleteEvalSchedule(evalId: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/evals/${encodeURIComponent(evalId)}/schedule`, { method: "DELETE" });
}

export async function runEvalNow(evalId: string): Promise<EvalRun> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/evals/${encodeURIComponent(evalId)}/run`, { method: "POST" });
  const parsed = parseWithFallback(raw, evalRunSchema, null, { endpoint: "runCerebroEvalNow" });
  if (!parsed) throw new Error("Invalid eval run response");
  return parsed as EvalRun;
}
