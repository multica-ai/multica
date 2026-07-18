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

// The typed shape of an eval run's `results` JSONB, produced by the Go engine
// (runner.Report / runner.CaseReport, FIR-3496 Fase 2). The See-why screen reads
// this to show, per task, what was asked, expected, produced, and why it passed
// or failed. Kept in lockstep with server/internal/cerebro/evals/runner/runner.go
// and scoring.go — snake_case keys match the Go json tags.
export interface EvalCaseReport {
  case_id: string;
  situation: string;
  expected: string;
  produced: string;
  passed: boolean;
  critical: boolean;
  reason: string;
  error?: string;
}

export interface EvalRunOutcome {
  total: number;
  passed: number;
  pass_rate: number;
  critical_total: number;
  critical_failed: number;
  min_pass_rate: number;
  threshold_met: boolean;
  critical_rule_met: boolean;
  status: string;
}

export interface EvalReport {
  cases: EvalCaseReport[];
  outcome: EvalRunOutcome;
  cost_cents: number;
  latency_ms: number;
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
