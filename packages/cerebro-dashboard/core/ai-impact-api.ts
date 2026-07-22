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
