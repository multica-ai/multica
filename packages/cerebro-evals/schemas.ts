import { z } from "zod";

export const evalSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  key: z.string(),
  version: z.string(),
  title: z.string(),
  description: z.string().default(""),
  status: z.string(),
  owner: z.record(z.string(), z.unknown()).default({}),
  objective: z.string(),
  target: z.record(z.string(), z.unknown()).default({}),
  datasets: z.array(z.unknown()).default([]),
  graders: z.array(z.unknown()).default([]),
  thresholds: z.array(z.unknown()).default([]),
  runner: z.record(z.string(), z.unknown()).default({}),
  source: z.record(z.string(), z.unknown()).default({}),
  created_by_id: z.string(),
  created_by_type: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).passthrough();

export const evalsListSchema = z.object({ evals: z.array(evalSchema).default([]) }).passthrough();

export const evalRunSchema = z.object({
  id: z.string(), workspace_id: z.string(), eval_id: z.string(), eval_version: z.string(),
  target_version: z.string().default(""), workflow_id: z.string().optional(), issue_id: z.string().optional(),
  status: z.string(), results: z.record(z.string(), z.unknown()).default({}), evidence_artifact_id: z.string().optional(),
  cost_cents: z.number().default(0), latency_ms: z.number().default(0), created_at: z.string(),
}).passthrough();

export const evalRunsListSchema = z.object({ runs: z.array(evalRunSchema).default([]) }).passthrough();

export const evalBindingSchema = z.object({
  id: z.string(), workspace_id: z.string(), workflow_id: z.string(), eval_id: z.string(),
  phase: z.string(), blocking: z.boolean(), eval_key: z.string(), eval_version: z.string(),
  eval_title: z.string(), created_at: z.string(),
}).passthrough();

export const evalBindingsListSchema = z.object({ bindings: z.array(evalBindingSchema).default([]) }).passthrough();
