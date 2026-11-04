import type { ProposalState } from "../company/proposal-schema";

/**
 * How a stage hands on. `auto` advances when its required artifacts are in,
 * `attest` needs the owning squad's sign-off, `gate` needs the boss's decision.
 * `unknown` stands in for a mode a newer server adds: views show it as-is and
 * never offer a control for it.
 */
export type PipelineExitMode = "auto" | "attest" | "gate" | "unknown";

export interface PipelineArtifactSpec {
  /** A knowledge kind key, such as `validation_report`. */
  kind: string;
  required: boolean;
}

export interface PipelineStage {
  key: string;
  name: string;
  owner_squad_id: string | null;
  objective: string;
  instructions: string;
  artifacts: PipelineArtifactSpec[];
  exit: { mode: PipelineExitMode; criteria: string };
  sla_days: number | null;
}

export interface Pipeline {
  id: string;
  name: string;
  is_default: boolean;
  /** In stage order. */
  stages: PipelineStage[];
}

/** `unknown` stands in for a status a newer server adds: it is not shown on the active board. */
export type InitiativeStatus = "active" | "shelved" | "operating" | "closed" | "unknown";

export interface Initiative {
  id: string;
  /** e.g. "INI-12". */
  identifier: string;
  title: string;
  thesis: string;
  pipeline_id: string;
  stage_key: string;
  owner_squad_id: string | null;
  status: InitiativeStatus;
  issue_id: string;
  build_project_id: string | null;
  stage_entered_at: string;
  days_in_stage: number;
  sla_state: "ok" | "stalled";
  artifacts_required: number;
  artifacts_present: number;
  pending_gate_proposal_id: string | null;
}

export interface InitiativeArtifact {
  kind: string;
  stage_key: string;
  required: boolean;
  knowledge_link_id: string | null;
  waived: boolean;
  waived_note: string;
}

export interface InitiativeEvent {
  id: string;
  kind: string;
  from_stage: string | null;
  to_stage: string | null;
  actor_type: string;
  actor_id: string | null;
  note: string;
  created_at: string;
}

/** A gate proposal raised for the initiative. Decided through the proposal decision endpoint. */
export interface InitiativeGate {
  id: string;
  state: ProposalState;
  /** The operate review's day after launch; null for a stage gate. */
  gate_day?: number | null;
  title: string;
  issue_identifier: string;
  created_at: string;
}

export interface InitiativeDetail {
  initiative: Initiative;
  artifacts: InitiativeArtifact[];
  events: InitiativeEvent[];
  gates: InitiativeGate[];
}

export interface ListInitiativesParams {
  pipeline_id?: string;
  status?: Exclude<InitiativeStatus, "unknown">;
  owner_squad_id?: string;
}

export interface ForceInitiativeRequest {
  to_stage: string;
  reason: string;
}

/** `POST /api/initiatives`. The server requires a thesis, so it is not optional here. */
export interface CreateInitiativeRequest {
  pipeline_id?: string;
  title: string;
  thesis: string;
}
