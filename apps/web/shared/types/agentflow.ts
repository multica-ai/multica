export type AgentflowStatus = "active" | "paused" | "archived";
export type AgentflowConcurrencyPolicy = "skip_if_active" | "coalesce" | "always_run";
export type AgentflowTriggerKind = "schedule" | "webhook" | "api";
export type AgentflowRunStatus = "received" | "executing" | "completed" | "failed" | "skipped" | "coalesced";
export type AgentflowRunSourceKind = "schedule" | "webhook" | "api" | "manual";

export interface Agentflow {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  agent_id: string;
  status: AgentflowStatus;
  concurrency_policy: AgentflowConcurrencyPolicy;
  variables: unknown[];
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface AgentflowTrigger {
  id: string;
  agentflow_id: string;
  kind: AgentflowTriggerKind;
  enabled: boolean;
  cron_expression: string | null;
  timezone: string | null;
  next_run_at: string | null;
  public_id?: string | null;
  last_fired_at: string | null;
  created_at: string;
}

export interface AgentflowRun {
  id: string;
  agentflow_id: string;
  trigger_id: string | null;
  source_kind: AgentflowRunSourceKind;
  status: AgentflowRunStatus;
  linked_issue_id: string | null;
  payload: unknown | null;
  agent_output: string | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
}

export interface CreateAgentflowRequest {
  title: string;
  description?: string;
  agent_id: string;
  status?: AgentflowStatus;
  concurrency_policy?: AgentflowConcurrencyPolicy;
  variables?: unknown[];
}

export interface UpdateAgentflowRequest {
  title?: string;
  description?: string;
  agent_id?: string;
  status?: AgentflowStatus;
  concurrency_policy?: AgentflowConcurrencyPolicy;
  variables?: unknown[];
}

export interface CreateAgentflowTriggerRequest {
  kind: AgentflowTriggerKind;
  enabled?: boolean;
  cron_expression?: string;
  timezone?: string;
  next_run_at?: string;
}

export interface UpdateAgentflowTriggerRequest {
  enabled?: boolean;
  cron_expression?: string;
  timezone?: string;
  next_run_at?: string;
}
