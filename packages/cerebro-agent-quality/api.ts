// FIR-3212 — client for quality/satisfaction aggregated per agent config
// version. GET /api/agents/{id}/quality.
//
// Missing data is nil server-side and must stay nullable here — a null
// average is "not measured", never 0 (FIR-3212 hard requirement). Parsed
// through lenient zod schemas with explicit fallbacks — never cast.

import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

const AgentQualitySolutionSchema = z
  .object({
    measured_runs: z.number().default(0),
    measurements: z.number().default(0),
    passes: z.number().default(0),
    scored: z.number().default(0),
    avg_score: z.number().nullish(),
    confident: z.number().default(0),
    avg_confidence: z.number().nullish(),
  })
  .loose();

const AgentQualitySatisfactionSchema = z
  .object({
    measured_runs: z.number().default(0),
    reactions: z.number().default(0),
    approvals: z.number().default(0),
    rejections: z.number().default(0),
  })
  .loose();

const AgentQualityVersionSchema = z
  .object({
    agent_context_version: z.string().default(""),
    agent_context_version_id: z.string().nullish(),
    runs: z.number().default(0),
    first_run_at: z.string().nullish(),
    last_run_at: z.string().nullish(),
    solution: AgentQualitySolutionSchema.default({
      measured_runs: 0,
      measurements: 0,
      passes: 0,
      scored: 0,
      confident: 0,
    }),
    satisfaction: AgentQualitySatisfactionSchema.default({
      measured_runs: 0,
      reactions: 0,
      approvals: 0,
      rejections: 0,
    }),
  })
  .loose();

export type AgentQualityVersion = z.infer<typeof AgentQualityVersionSchema>;

const AgentQualitySchema = z
  .object({ versions: z.array(AgentQualityVersionSchema).default([]) })
  .loose();

export async function getAgentQuality(
  agentId: string,
): Promise<AgentQualityVersion[]> {
  const path = `/api/agents/${agentId}/quality`;
  const raw = await api.cerebroRequest<unknown>(path);
  const parsed = parseWithFallback(
    raw,
    AgentQualitySchema,
    { versions: [] },
    { endpoint: path },
  ) as z.infer<typeof AgentQualitySchema>;
  return parsed.versions;
}
