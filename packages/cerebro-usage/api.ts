import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

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
