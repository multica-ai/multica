import { z } from "zod";
import {
  GOAL_CONFIDENCES,
  GOAL_OVERRIDE_CONFIDENCES,
  GOAL_SCOPE_TYPES,
  GOAL_STATUSES,
  METRIC_SOURCES,
  type Goal,
  type GoalConfidence,
  type GoalDetail,
  type GoalRef,
  type GoalScopeType,
  type GoalStatus,
  type KeyResult,
  type Metric,
  type MetricReading,
  type MetricSource,
} from "./goals-types";

// Lenient on purpose (see parseWithFallback): missing fields take defaults, a
// value this client does not know degrades to `unknown`, and a malformed item is
// dropped alone so one bad goal does not hide the others.

const num = z.coerce.number().catch(0).default(0);
const nullableNum = z
  .union([z.number(), z.string()])
  .nullish()
  .transform((v) => {
    if (v == null || v === "") return null;
    const n = typeof v === "number" ? v : Number(v);
    return Number.isFinite(n) ? n : null;
  });
const optionalId = z
  .string()
  .nullish()
  .transform((id) => id || null);

function knownOr<T extends string>(values: readonly T[], fallback: T) {
  return z
    .string()
    .nullish()
    .transform((v): T => (v && (values as readonly string[]).includes(v) ? (v as T) : fallback));
}

const scopeTypeSchema = knownOr(GOAL_SCOPE_TYPES, "company") as z.ZodType<GoalScopeType>;
const statusSchema = z
  .string()
  .nullish()
  .transform((v): GoalStatus => ((GOAL_STATUSES as readonly string[]).includes(v ?? "") ? (v as GoalStatus) : "unknown"));
const confidenceSchema = knownOr(GOAL_CONFIDENCES, "unknown") as z.ZodType<GoalConfidence>;
const overrideSchema = z
  .string()
  .nullish()
  .transform((v) => ((GOAL_OVERRIDE_CONFIDENCES as readonly string[]).includes(v ?? "") ? (v as GoalConfidence) : null));
const sourceSchema = z
  .string()
  .nullish()
  .transform((v): MetricSource => ((METRIC_SOURCES as readonly string[]).includes(v ?? "") ? (v as MetricSource) : "unknown"));

const seriesPointSchema = z
  .object({ observed_at: z.string().default(""), value: num })
  .loose();

const keyResultSchema = z
  .object({
    id: z.string().default(""),
    goal_id: z.string().default(""),
    title: z.string().default(""),
    metric_key: z.string().default(""),
    scope_type: scopeTypeSchema,
    scope_id: optionalId,
    start_value: num,
    target_value: num,
    weight: num,
    current_value: nullableNum,
    observed_at: z.string().nullish().transform((v) => v || null),
    progress: nullableNum,
    series: z
      .unknown()
      .transform((v) =>
        Array.isArray(v)
          ? v.flatMap((item) => {
              const parsed = seriesPointSchema.safeParse(item);
              return parsed.success ? [parsed.data] : [];
            })
          : [],
      ),
  })
  .loose()
  .transform((kr): KeyResult => kr);

const goalSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    scope_type: scopeTypeSchema,
    scope_id: optionalId,
    parent_goal_id: optionalId,
    title: z.string().default(""),
    why: z.string().default(""),
    period_start: z.string().default(""),
    period_end: z.string().default(""),
    owner_type: z
      .string()
      .nullish()
      .transform((v): Goal["owner_type"] => (v === "member" || v === "agent" ? v : "squad")),
    owner_id: z.string().default(""),
    status: statusSchema,
    confidence: confidenceSchema,
    derived_confidence: confidenceSchema,
    confidence_override: overrideSchema,
    confidence_note: z.string().default(""),
    confidence_changed_at: z.string().default(""),
    progress: nullableNum,
    readings_missing: num,
    source_proposal_id: optionalId,
    key_results: z.unknown().transform((v) =>
      Array.isArray(v)
        ? v.flatMap((item) => {
            const parsed = keyResultSchema.safeParse(item);
            return parsed.success ? [parsed.data] : [];
          })
        : [],
    ),
    created_by_type: z.string().default(""),
    created_by_id: optionalId,
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .loose()
  .transform((g): Goal => g as Goal);

const goalRefSchema = z
  .object({
    id: z.string().default(""),
    title: z.string().default(""),
    status: statusSchema,
    scope_type: scopeTypeSchema,
    scope_id: optionalId,
  })
  .loose()
  .transform((r): GoalRef => r);

export const GoalSchema = goalSchema;

/** What an unreadable goal degrades to: an empty goal the views treat as missing. */
export const FALLBACK_GOAL: Goal = goalSchema.parse({});

/** A goal list: a malformed goal is dropped alone. */
export const GoalsResponseSchema = z
  .object({
    goals: z.unknown().transform((v) =>
      Array.isArray(v)
        ? v.flatMap((item) => {
            const parsed = goalSchema.safeParse(item);
            return parsed.success ? [parsed.data] : [];
          })
        : [],
    ),
  })
  .loose()
  .transform((r) => r.goals);

export const GoalDetailSchema = z
  .object({
    goal: goalSchema.catch(FALLBACK_GOAL),
    child_goals: z.unknown().transform((v) =>
      Array.isArray(v)
        ? v.flatMap((item) => {
            const parsed = goalRefSchema.safeParse(item);
            return parsed.success ? [parsed.data] : [];
          })
        : [],
    ),
  })
  .loose()
  .transform((d): GoalDetail => d);

/** What an unreadable goal detail degrades to. */
export const FALLBACK_GOAL_DETAIL: GoalDetail = GoalDetailSchema.parse({});

const metricSchema = z
  .object({
    key: z.string().default(""),
    name: z.string().default(""),
    unit: z.string().default(""),
    source: sourceSchema,
    direction: z.string().nullish().transform((v) => (v === "decrease" ? "decrease" : "increase")),
    description: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose()
  .transform((m): Metric => m as Metric);

export const MetricsResponseSchema = z
  .object({
    metrics: z.unknown().transform((v) =>
      Array.isArray(v)
        ? v.flatMap((item) => {
            const parsed = metricSchema.safeParse(item);
            return parsed.success ? [parsed.data] : [];
          })
        : [],
    ),
  })
  .loose()
  .transform((r) => r.metrics);

const readingSchema = z
  .object({
    id: z.string().default(""),
    metric_key: z.string().default(""),
    scope_type: scopeTypeSchema,
    scope_id: optionalId,
    // A reading with no number is dropped, not shown as zero.
    value: z.coerce.number().refine((n) => Number.isFinite(n), "value must be a number"),
    observed_at: z.string().default(""),
    actor_type: z.string().default(""),
    actor_id: optionalId,
    note: z.string().default(""),
  })
  .loose()
  .transform((r): MetricReading => r as MetricReading);

export const MetricReadingsResponseSchema = z
  .object({
    readings: z.unknown().transform((v) =>
      Array.isArray(v)
        ? v.flatMap((item) => {
            const parsed = readingSchema.safeParse(item);
            return parsed.success ? [parsed.data] : [];
          })
        : [],
    ),
  })
  .loose()
  .transform((r) => r.readings);

/** A single goal in a response under `goal`; an unreadable one is the empty fallback. */
export const GoalResponseSchema = z
  .object({ goal: goalSchema.catch(FALLBACK_GOAL) })
  .loose()
  .transform((r) => r.goal);

/** What an unreadable reading degrades to: an empty placeholder no view shows. */
export const FALLBACK_READING: MetricReading = {
  id: "",
  metric_key: "",
  scope_type: "company",
  scope_id: null,
  value: 0,
  observed_at: "",
  actor_type: "",
  actor_id: null,
  note: "",
};

/** A single reading in a response under `reading`. */
export const ReadingResponseSchema = z
  .object({ reading: readingSchema.catch(FALLBACK_READING) })
  .loose()
  .transform((r) => r.reading);

export const FALLBACK_GOAL_LIST: Goal[] = [];
export const FALLBACK_METRIC_LIST: Metric[] = [];
