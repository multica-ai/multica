export type EvalStatus = "draft" | "active" | "paused" | "retired";
export type EvalRunStatus = "queued" | "running" | "passed" | "failed" | "error";
export type EvalBindingPhase = "plan" | "delivery" | "monitor";

export interface CerebroEval {
  id: string;
  workspace_id: string;
  key: string;
  version: string;
  title: string;
  description: string;
  status: EvalStatus;
  owner: Record<string, unknown>;
  objective: string;
  target: Record<string, unknown>;
  datasets: unknown[];
  graders: unknown[];
  thresholds: unknown[];
  runner: Record<string, unknown>;
  source: Record<string, unknown>;
  created_by_id: string;
  created_by_type: "member" | "agent";
  created_at: string;
  updated_at: string;
}

export type EvalWriteInput = Omit<
  CerebroEval,
  "id" | "workspace_id" | "created_by_id" | "created_by_type" | "created_at" | "updated_at"
>;

export interface EvalRun {
  id: string;
  workspace_id: string;
  eval_id: string;
  eval_version: string;
  target_version: string;
  workflow_id?: string;
  issue_id?: string;
  status: EvalRunStatus;
  results: Record<string, unknown>;
  evidence_artifact_id?: string;
  cost_cents: number;
  latency_ms: number;
  created_at: string;
}

export interface EvalBinding {
  id: string;
  workspace_id: string;
  workflow_id: string;
  eval_id: string;
  phase: EvalBindingPhase;
  blocking: boolean;
  eval_key: string;
  eval_version: string;
  eval_title: string;
  created_at: string;
}
