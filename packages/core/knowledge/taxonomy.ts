import { z } from "zod";
import type {
  KnowledgeEvalCase,
  KnowledgeEvalCases,
  KnowledgeEvalRun,
  KnowledgeEvalRuns,
  KnowledgeHealth,
  KnowledgeKind,
  KnowledgeKinds,
  KnowledgePublishMode,
  KnowledgePublishPolicies,
  KnowledgePublishPolicy,
} from "./taxonomy-types";

// Knowledge taxonomy (spec 3.3): kinds, publish policies, the Librarian's
// health, and retrieval eval. Schemas are lenient and fall back per field, so a
// newer server adding a mode or a job degrades instead of breaking the page.
// The response-level fallbacks are empty, never a list that looks like data.

const KNOWN_PUBLISH_MODES = ["manual", "auto_after_review", "auto"] as const;
const KNOWN_EVAL_STATES = ["ready", "offline"] as const;

const stringList = z
  .array(z.string())
  .nullish()
  .transform((v) => v ?? []);

// A mode the client does not know is "unknown", never "manual" or "auto": the
// UI must not show a policy it cannot read as one it can.
const PublishModeSchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgePublishMode =>
    (KNOWN_PUBLISH_MODES as readonly string[]).includes(s) ? (s as KnowledgePublishMode) : "unknown",
  );

const EvalStateSchema = z
  .string()
  .default("unknown")
  .transform((s): "ready" | "offline" | "unknown" =>
    (KNOWN_EVAL_STATES as readonly string[]).includes(s) ? (s as "ready" | "offline") : "unknown",
  );

export const KnowledgeKindSchema = z
  .object({
    key: z.string().default(""),
    name: z.string().default(""),
    default_base_key: z.string().default(""),
    template: z.string().default(""),
    review_days: z.number().default(0),
    required_sections: stringList,
  })
  .loose();

export const KnowledgeKindsSchema = z
  .object({
    kinds: z.array(KnowledgeKindSchema).nullish().transform((v) => v ?? []),
  })
  .loose();

export const KnowledgePublishPolicySchema = z
  .object({
    scope: z.string().default(""),
    key: z.string().default(""),
    mode: PublishModeSchema,
    updated_at: z.string().default(""),
  })
  .loose();

export const KnowledgePublishPoliciesSchema = z
  .object({
    policies: z.array(KnowledgePublishPolicySchema).nullish().transform((v) => v ?? []),
    // The mode of a scope without a row. Absent from an older server: manual.
    default_mode: PublishModeSchema.default("manual"),
  })
  .loose();

const LibrarianFindingSchema = z
  .object({
    link_id: z.string().default(""),
    title: z.string().default(""),
    knowledge_base: z.string().default(""),
    knowledge_kind: z.string().default(""),
    squad_id: z.string().default(""),
    reason: z.string().default(""),
    missing_sections: stringList,
    other_link_id: z.string().default(""),
  })
  .loose();

const LibrarianReportSchema = z
  .object({
    job: z.string().default(""),
    ran_at: z.string().default(""),
    counts: z
      .record(z.string(), z.number())
      .nullish()
      .transform((v) => v ?? {}),
    findings: z.array(LibrarianFindingSchema).nullish().transform((v) => v ?? []),
    truncated: z.boolean().default(false),
  })
  .loose();

export const KnowledgeHealthSchema = z
  .object({
    jobs: z
      .array(
        z
          .object({
            job: z.string().default(""),
            ran_at: z.string().default(""),
            report: LibrarianReportSchema,
          })
          .loose(),
      )
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeEvalCaseSchema = z
  .object({
    id: z.string().default(""),
    question: z.string().default(""),
    expected_kind: z.string().default(""),
    expected_document_id: z.string().default(""),
    owner_squad_id: z.string().nullable().default(null),
    created_by_type: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const KnowledgeEvalCasesSchema = z
  .object({
    cases: z.array(KnowledgeEvalCaseSchema).nullish().transform((v) => v ?? []),
  })
  .loose();

const EvalResultSchema = z
  .object({
    case_id: z.string().default(""),
    question: z.string().default(""),
    expected_kind: z.string().default(""),
    expected_document_id: z.string().default(""),
    hit: z.boolean().default(false),
    rank: z.number().default(0),
  })
  .loose();

export const KnowledgeEvalRunSchema = z
  .object({
    id: z.string().default(""),
    state: EvalStateSchema,
    total: z.number().default(0),
    hits: z.number().default(0),
    hit_rate: z.number().default(0),
    ran_at: z.string().default(""),
    results: z.array(EvalResultSchema).nullish().transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeEvalRunsSchema = z
  .object({
    runs: z.array(KnowledgeEvalRunSchema).nullish().transform((v) => v ?? []),
  })
  .loose();

/** Response fallbacks: an unreadable answer shows nothing, never stale data. */
export const FALLBACK_KNOWLEDGE_KINDS: KnowledgeKinds = { kinds: [] };
export const FALLBACK_KNOWLEDGE_KIND: KnowledgeKind = {
  key: "",
  name: "",
  default_base_key: "",
  template: "",
  review_days: 0,
  required_sections: [],
};
export const FALLBACK_KNOWLEDGE_POLICY: KnowledgePublishPolicy = {
  scope: "",
  key: "",
  mode: "unknown",
  updated_at: "",
};
export const FALLBACK_KNOWLEDGE_HEALTH: KnowledgeHealth = { jobs: [] };
export const FALLBACK_KNOWLEDGE_POLICIES: KnowledgePublishPolicies = {
  policies: [],
  default_mode: "manual",
};
export const FALLBACK_KNOWLEDGE_EVAL_CASES: KnowledgeEvalCases = { cases: [] };
export const FALLBACK_KNOWLEDGE_EVAL_RUNS: KnowledgeEvalRuns = { runs: [] };

/** An unreadable single case or run: an empty row the UI renders as nothing. */
export const FALLBACK_KNOWLEDGE_EVAL_CASE: KnowledgeEvalCase = {
  id: "",
  question: "",
  expected_kind: "",
  expected_document_id: "",
  owner_squad_id: null,
  created_by_type: "",
  created_at: "",
};

export const FALLBACK_KNOWLEDGE_EVAL_RUN: KnowledgeEvalRun = {
  id: "",
  state: "unknown",
  total: 0,
  hits: 0,
  hit_rate: 0,
  ran_at: "",
  results: [],
};
