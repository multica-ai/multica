import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

const MetricFamilySchema = z
  .union([
    z.enum(["Adoption", "Output", "Outcome", "Quality", "Economics", "Risk"]),
    z.literal("Unknown"),
  ])
  .catch("Unknown");
const EvidenceStatusSchema = z
  .union([z.enum(["Measured", "Estimated", "Missing"]), z.literal("Unknown")])
  .catch("Unknown");
const MetricDirectionSchema = z
  .union([z.enum(["increase", "decrease"]), z.literal("Unknown")])
  .catch("Unknown");
const DecisionSchema = z
  .union([z.enum(["Scale", "Observe", "Stop"]), z.literal("Unknown")])
  .catch("Unknown");

const EvidenceSchema = z
  .object({
    function_id: z.string().catch(""),
    function_name: z.string().catch(""),
    operating_loop_id: z.string().catch(""),
    operating_loop_name: z.string().catch(""),
    metric_id: z.string().catch(""),
    metric_name: z.string().catch(""),
    metric_family: MetricFamilySchema,
    metric_unit: z.string().catch(""),
    metric_direction: MetricDirectionSchema,
    period_start: z.string().catch(""),
    period_end: z.string().catch(""),
    value: z.number().catch(0),
    evidence_status: EvidenceStatusSchema,
    confidence: z.number().catch(0),
    source: z.string().catch(""),
    method: z.string().catch(""),
  })
  .loose();

const OverviewResponseSchema = z
  .object({
    families: z
      .array(
        z
          .object({
            family: MetricFamilySchema,
            evidence: z.array(EvidenceSchema).default([]),
          })
          .loose(),
      )
      .default([]),
  })
  .loose();

const FunctionResponseSchema = z
  .object({
    functions: z
      .array(
        z
          .object({
            id: z.string().catch(""),
            name: z.string().catch(""),
            operating_loops: z
              .array(
                z
                  .object({
                    id: z.string().catch(""),
                    name: z.string().catch(""),
                    decision: DecisionSchema,
                  })
                  .loose(),
              )
              .default([]),
          })
          .loose(),
      )
      .default([]),
  })
  .loose();

const QualityRiskResponseSchema = z
  .object({
    decisions: z
      .array(
        z
          .object({
            function_id: z.string().catch(""),
            function_name: z.string().catch(""),
            operating_loop_id: z.string().catch(""),
            operating_loop_name: z.string().catch(""),
            decision: DecisionSchema,
          })
          .loose(),
      )
      .default([]),
  })
  .loose();

export type AIImpactOverviewResponse = z.infer<typeof OverviewResponseSchema>;
export type AIImpactFunctionsResponse = z.infer<typeof FunctionResponseSchema>;
export type AIImpactQualityRiskResponse = z.infer<typeof QualityRiskResponseSchema>;

export const PeopleImpactPeriodSchema = z.enum(["hour", "day", "week", "month"]);
export type PeopleImpactPeriod = z.infer<typeof PeopleImpactPeriodSchema>;

const PeopleResponseSchema = z.object({
  period: PeopleImpactPeriodSchema.catch("day"),
  people: z.array(z.object({
    id: z.string(),
    type: z.enum(["member", "agent"]),
    name: z.string(),
    activity: z.array(z.object({ bucket: z.string(), count: z.number() })).default([]),
    usage: z.object({
      runs: z.number().nullable().default(null),
      issues: z.number().nullable().default(null),
      projects: z.number().nullable().default(null),
      chats: z.number().nullable().default(null),
      channels: z.number().nullable().default(null),
    }),
    outcomes: z.object({
      needs_solved: z.object({ solved: z.number(), measurable: z.number() }).nullable().default(null),
      solution_quality: z.number().nullable().default(null),
      frustration_free: z.number().nullable().default(null),
      prompt_effectiveness: z.number().nullable().default(null),
      skill_activity: z.number().nullable().default(null),
      cost_cents: z.number().nullable().default(null),
    }),
    confidence: z.number().nullable().default(null),
    sample_size: z.number().default(0),
  })).default([]),
}).loose();
export type AIImpactPeopleResponse = z.infer<typeof PeopleResponseSchema>;

const EMPTY_OVERVIEW: AIImpactOverviewResponse = { families: [] };
const EMPTY_FUNCTIONS: AIImpactFunctionsResponse = { functions: [] };
const EMPTY_QUALITY_RISK: AIImpactQualityRiskResponse = { decisions: [] };

async function fetchAIImpactReadModel<T>(
  path: string,
  schema: z.ZodType<T>,
  fallback: T,
): Promise<T> {
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, schema, fallback, { endpoint: path });
}

export function fetchAIImpactOverview(): Promise<AIImpactOverviewResponse> {
  const path = "/api/cerebro/ai-impact/overview/summary";
  return fetchAIImpactReadModel(path, OverviewResponseSchema, EMPTY_OVERVIEW);
}

export function fetchAIImpactFunctions(): Promise<AIImpactFunctionsResponse> {
  const path = "/api/cerebro/ai-impact/functions/summary";
  return fetchAIImpactReadModel(path, FunctionResponseSchema, EMPTY_FUNCTIONS);
}

export function fetchAIImpactQualityRisk(): Promise<AIImpactQualityRiskResponse> {
  const path = "/api/cerebro/ai-impact/quality-risk/decisions";
  return fetchAIImpactReadModel(path, QualityRiskResponseSchema, EMPTY_QUALITY_RISK);
}

export async function fetchAIImpactPeople(
  period: PeopleImpactPeriod,
): Promise<AIImpactPeopleResponse> {
  const path = `/api/cerebro/ai-impact/people?period=${period}`;
  return fetchAIImpactReadModel(path, PeopleResponseSchema, { period, people: [] });
}
