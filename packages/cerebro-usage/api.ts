import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

export type AnalyticsPopulation = "agent" | "gateway" | "all";
export type AnalyticsMetric =
  | "runs"
  | "input_tokens"
  | "output_tokens"
  | "cost_cents"
  | "missing_cost_runs"
  | "saved_cents"
  | "duration_seconds"
  | "quality_pass_rate"
  | "skill_invocations";
export type AnalyticsDimension =
  | "time"
  | "person"
  | "agent"
  | "project"
  | "runtime"
  | "source"
  | "provider"
  | "model"
  | "skill"
  | "status"
  | "cost_kind"
  | "quality_type"
  | "quality_category"
  | "context"
  | "run"
  | "source_id"
  | "reference"
  | "reference_label"
  | "debug_link"
  | "trace";
export type AnalyticsGrain = "none" | "hour" | "day" | "week" | "month";
export type AnalyticsOperator = "in" | "not_in" | "eq" | "gte" | "lte";

export interface AnalyticsQuery {
  population: AnalyticsPopulation;
  metrics?: AnalyticsMetric[];
  dimensions?: AnalyticsDimension[];
  filters?: { dimension: AnalyticsDimension; operator: AnalyticsOperator; values: string[] }[];
  grain?: AnalyticsGrain;
  sort?: { field: string; direction: "asc" | "desc" }[];
  page?: { limit?: number; cursor?: string };
  timezone?: string;
}

const AnalyticsCatalogSchema = z.object({
  populations: z.array(z.enum(["agent", "gateway", "all"])),
  metrics: z.array(z.enum(["runs", "input_tokens", "output_tokens", "cost_cents", "missing_cost_runs", "saved_cents", "duration_seconds", "quality_pass_rate", "skill_invocations"])),
  dimensions: z.array(z.enum(["time", "person", "agent", "project", "runtime", "source", "provider", "model", "skill", "status", "cost_kind", "quality_type", "quality_category", "context", "run", "source_id", "reference", "reference_label", "debug_link", "trace"])),
  grains: z.array(z.enum(["none", "hour", "day", "week", "month"])),
  operators: z.array(z.enum(["in", "not_in", "eq", "gte", "lte"])),
});
const AnalyticsQueryResultSchema = z.object({
  columns: z.array(z.string()),
  rows: z.array(z.record(z.string(), z.unknown())),
  next_cursor: z.string().optional(),
});

export type AnalyticsCatalog = z.infer<typeof AnalyticsCatalogSchema>;
export type AnalyticsQueryResult = z.infer<typeof AnalyticsQueryResultSchema>;
export const EMPTY_ANALYTICS_RESULT: AnalyticsQueryResult = { columns: [], rows: [] };

export function parseAnalyticsQueryResult(raw: unknown): AnalyticsQueryResult {
  return parseWithFallback(raw, AnalyticsQueryResultSchema, EMPTY_ANALYTICS_RESULT, { endpoint: "/api/analytics/query" });
}

export async function fetchAnalyticsCatalog(): Promise<AnalyticsCatalog> {
  const raw = await api.cerebroRequest<unknown>("/api/analytics/catalog");
  return parseWithFallback(raw, AnalyticsCatalogSchema, { populations: [], metrics: [], dimensions: [], grains: [], operators: [] }, { endpoint: "/api/analytics/catalog" });
}

export async function fetchAnalyticsQuery(query: AnalyticsQuery): Promise<AnalyticsQueryResult> {
  const raw = await api.cerebroRequest<unknown>("/api/analytics/query", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(query),
  });
  return parseAnalyticsQueryResult(raw);
}

const AnalyticsVisualSchema = z.object({
  id: z.string(),
  name: z.string(),
  visual_type: z.enum(["activity", "table", "bars"]),
  query: z.custom<AnalyticsQuery>((value) => typeof value === "object" && value !== null && "population" in value),
  display: z.record(z.string(), z.unknown()).default({}),
  position: z.number().int(),
  created_at: z.string(),
  updated_at: z.string(),
});
const AnalyticsVisualsSchema = z.array(AnalyticsVisualSchema);
export type PersistedAnalyticsVisual = z.infer<typeof AnalyticsVisualSchema>;
export type AnalyticsVisualInput = Pick<PersistedAnalyticsVisual, "name" | "visual_type" | "query" | "display" | "position">;

export async function fetchAnalyticsVisuals(): Promise<PersistedAnalyticsVisual[]> {
  const raw = await api.cerebroRequest<unknown>("/api/analytics/visuals");
  return parseWithFallback(raw, AnalyticsVisualsSchema, [], { endpoint: "/api/analytics/visuals" });
}

export async function createAnalyticsVisual(input: AnalyticsVisualInput): Promise<PersistedAnalyticsVisual> {
  const raw = await api.cerebroRequest<unknown>("/api/analytics/visuals", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return AnalyticsVisualSchema.parse(raw);
}

export async function updateAnalyticsVisual(id: string, input: AnalyticsVisualInput): Promise<PersistedAnalyticsVisual> {
  const raw = await api.cerebroRequest<unknown>(`/api/analytics/visuals/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return AnalyticsVisualSchema.parse(raw);
}

export async function deleteAnalyticsVisual(id: string): Promise<void> {
  await api.cerebroRequest(`/api/analytics/visuals/${id}`, { method: "DELETE" });
}

const SkillUsageRowSchema = z
  .object({
    skill_id: z.string().nullable().default(null),
    skill_name: z.string().min(1),
    invocation_count: z.number().int().nonnegative(),
    run_count: z.number().int().nonnegative(),
    last_used_at: z.string().nullable().default(null),
  })
  .loose();

const SkillUsageRowsSchema = z.array(SkillUsageRowSchema);

export type SkillUsageRow = z.infer<typeof SkillUsageRowSchema>;

const ExplorerSummarySchema = z.object({ runs:z.number().int().nonnegative(), tokens:z.number().nonnegative(), actual_cost_cents:z.number().nonnegative(), calculated_cost_runs:z.number().int().nonnegative(), missing_cost_runs:z.number().int().nonnegative() });
const FacetSchema = z.object({ value:z.string(), count:z.number().int().nonnegative() });
const RunSchema = z.object({ id:z.string(), created_at:z.string(), status:z.string(), project:z.string(), agent:z.string(), runtime:z.string(), model:z.string(), provider:z.string(), trigger:z.string(), input_tokens:z.number(), output_tokens:z.number(), cost_cents:z.number(), cost_kind:z.string(), duration_seconds:z.number(), trace_url:z.string().nullable() }).loose();
const SavingSchema = z.object({ type:z.string(), state:z.string(), saved_cents:z.number(), saved_units:z.number(), affected_runs:z.number() });
const ExplorerSchema = z.object({ summary:ExplorerSummarySchema, facets:z.record(z.string(),z.array(FacetSchema)), runs:z.array(RunSchema), total:z.number().int().nonnegative(), savings:z.array(SavingSchema) }).loose();
export type UsageExplorer = z.infer<typeof ExplorerSchema>;
export const EMPTY_EXPLORER: UsageExplorer = { summary:{runs:0,tokens:0,actual_cost_cents:0,calculated_cost_runs:0,missing_cost_runs:0}, facets:{}, runs:[], total:0, savings:[] };
export function parseUsageExplorer(raw: unknown): UsageExplorer { return parseWithFallback(raw, ExplorerSchema, EMPTY_EXPLORER, { endpoint:"/api/dashboard/usage/explorer" }); }
export async function fetchUsageExplorer(query:string):Promise<UsageExplorer>{
  const params = new URLSearchParams(query);
  if (params.get("grain") === "day") params.set("grain", "daily");
  if (params.get("grain") === "week") params.set("grain", "weekly");
  const path=`/api/dashboard/usage/explorer?${params}`;
  return parseUsageExplorer(await api.cerebroRequest<unknown>(path));
}

export async function fetchSkillUsage(
  days: number,
  projectId: string | null,
  include: string[] = [],
  exclude: string[] = [],
): Promise<SkillUsageRow[]> {
  const params = new URLSearchParams({ days: String(days) });
  if (projectId) params.set("project_id", projectId);
  [...new Set(include)].sort().forEach((value) => params.append("skill", value));
  [...new Set(exclude)].sort().forEach((value) => params.append("exclude.skill", value));
  const path = `/api/dashboard/usage/skills?${params}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, SkillUsageRowsSchema, [], { endpoint: path });
}
