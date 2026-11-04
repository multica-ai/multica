import { z } from "zod";
import type {
  Initiative,
  InitiativeDetail,
  InitiativeStatus,
  Pipeline,
  PipelineExitMode,
} from "./types";
import type { ProposalState } from "../company/proposal-schema";

// Lenient on purpose (see parseWithFallback): optional fields default, unknown
// fields pass through, and an enum this client does not know degrades to its
// `unknown` value instead of failing the whole pipeline or board.

const KNOWN_EXIT_MODES: readonly PipelineExitMode[] = ["auto", "attest", "gate"];
const KNOWN_INITIATIVE_STATUSES: readonly InitiativeStatus[] = ["active", "shelved", "operating", "closed"];
const KNOWN_PROPOSAL_STATES: readonly ProposalState[] = ["pending", "approved", "rejected", "revising", "expired"];

const exitModeSchema = z
  .string()
  .default("auto")
  .transform((mode): PipelineExitMode =>
    (KNOWN_EXIT_MODES as readonly string[]).includes(mode) ? (mode as PipelineExitMode) : "unknown",
  );

const initiativeStatusSchema = z
  .string()
  .default("active")
  .transform((status): InitiativeStatus =>
    (KNOWN_INITIATIVE_STATUSES as readonly string[]).includes(status)
      ? (status as InitiativeStatus)
      : "unknown",
  );

const proposalStateSchema = z
  .string()
  .default("unknown")
  .transform((state): ProposalState =>
    (KNOWN_PROPOSAL_STATES as readonly string[]).includes(state) ? (state as ProposalState) : "unknown",
  );

const optionalId = z
  .string()
  .nullish()
  .transform((id) => id || null);

export const PipelineArtifactSpecSchema = z
  .object({
    kind: z.string(),
    required: z.boolean().default(true),
  })
  .loose();

export const PipelineStageSchema = z
  .object({
    key: z.string(),
    name: z.string().default(""),
    owner_squad_id: optionalId,
    objective: z.string().default(""),
    instructions: z.string().default(""),
    artifacts: z.array(PipelineArtifactSpecSchema).default([]),
    exit: z
      .object({
        mode: exitModeSchema,
        criteria: z.string().default(""),
      })
      .loose()
      .default({ mode: "auto", criteria: "" }),
    sla_days: z
      .number()
      .nullish()
      .transform((days) => days ?? null),
  })
  .loose();

export const PipelineSchema = z
  .object({
    id: z.string(),
    name: z.string().default(""),
    is_default: z.boolean().default(false),
    stages: z.array(PipelineStageSchema).default([]),
  })
  .loose();

/** `GET /api/pipelines` unwraps to the list of pipelines. */
export const PipelinesResponseSchema = z
  .object({
    pipelines: z.array(PipelineSchema).default([]),
  })
  .loose()
  .transform((body): Pipeline[] => body.pipelines);

export const InitiativeSchema = z
  .object({
    id: z.string(),
    identifier: z.string().default(""),
    title: z.string().default(""),
    thesis: z.string().default(""),
    pipeline_id: z.string().default(""),
    stage_key: z.string().default(""),
    owner_squad_id: optionalId,
    status: initiativeStatusSchema,
    issue_id: z.string().default(""),
    build_project_id: optionalId,
    stage_entered_at: z.string().default(""),
    days_in_stage: z.number().default(0),
    sla_state: z
      .string()
      .default("ok")
      .transform((state): Initiative["sla_state"] => (state === "stalled" ? "stalled" : "ok")),
    artifacts_required: z.number().default(0),
    artifacts_present: z.number().default(0),
    pending_gate_proposal_id: optionalId,
  })
  .loose();

/** `GET /api/initiatives` unwraps to the list of initiatives. */
export const InitiativesResponseSchema = z
  .object({
    initiatives: z.array(InitiativeSchema).default([]),
  })
  .loose()
  .transform((body): Initiative[] => body.initiatives);

export const InitiativeArtifactSchema = z
  .object({
    kind: z.string(),
    stage_key: z.string().default(""),
    required: z.boolean().default(true),
    knowledge_link_id: optionalId,
    waived: z.boolean().default(false),
    waived_note: z.string().default(""),
  })
  .loose();

export const InitiativeEventSchema = z
  .object({
    id: z.string(),
    kind: z.string().default(""),
    from_stage: z.string().nullish().transform((stage) => stage ?? null),
    to_stage: z.string().nullish().transform((stage) => stage ?? null),
    actor_type: z.string().default(""),
    actor_id: optionalId,
    note: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const InitiativeGateSchema = z
  .object({
    id: z.string(),
    state: proposalStateSchema,
    gate_day: z.number().int().nullish().transform((day) => day ?? null),
    title: z.string().default(""),
    issue_identifier: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const InitiativeDetailSchema = z
  .object({
    initiative: InitiativeSchema,
    artifacts: z.array(InitiativeArtifactSchema).default([]),
    events: z.array(InitiativeEventSchema).default([]),
    gates: z.array(InitiativeGateSchema).default([]),
  })
  .loose() satisfies z.ZodType<InitiativeDetail>;

/** What an unreadable detail response degrades to: an id-less initiative that the panel reads as not loaded. */
export const FALLBACK_INITIATIVE: Initiative = {
  id: "",
  identifier: "",
  title: "",
  thesis: "",
  pipeline_id: "",
  stage_key: "",
  owner_squad_id: null,
  status: "unknown",
  issue_id: "",
  build_project_id: null,
  stage_entered_at: "",
  days_in_stage: 0,
  sla_state: "ok",
  artifacts_required: 0,
  artifacts_present: 0,
  pending_gate_proposal_id: null,
};

export const FALLBACK_INITIATIVE_DETAIL: InitiativeDetail = {
  initiative: FALLBACK_INITIATIVE,
  artifacts: [],
  events: [],
  gates: [],
};

export const EMPTY_PIPELINES: Pipeline[] = [];
export const EMPTY_INITIATIVES: Initiative[] = [];
