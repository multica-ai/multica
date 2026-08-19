import { z } from "zod";

// External-boundary schemas for LiteLLM's admin API. Kept lenient per
// packages/core/api/schema.ts's parseWithFallback convention: unknown extra
// fields are ignored, and every field we actually read is optional/nullable
// so a partial or drifted response degrades gracefully instead of throwing.

export const LiteLlmKeySchema = z.object({
  key_alias: z.string().nullish(),
  team_alias: z.string().nullish(),
  team_id: z.string().nullish(),
});

export const LiteLlmKeyListSchema = z.object({
  keys: z.array(LiteLlmKeySchema).default([]),
  metadata: z
    .object({
      total_pages: z.number().nullish(),
    })
    .nullish(),
});

export const LiteLlmModelMetricsSchema = z.object({
  spend: z.number().nullish(),
  total_tokens: z.number().nullish(),
});

export const LiteLlmDayResultSchema = z.object({
  date: z.string().nullish(),
  group_by_day: z.string().nullish(),
  breakdown: z
    .object({
      models: z.record(z.string(), z.union([LiteLlmModelMetricsSchema, z.record(z.string(), z.unknown())])).nullish(),
    })
    .nullish(),
});

export const LiteLlmTeamActivitySchema = z.object({
  results: z.array(LiteLlmDayResultSchema).default([]),
  metadata: z
    .object({
      total_pages: z.number().nullish(),
    })
    .nullish(),
});

export type LiteLlmKey = z.infer<typeof LiteLlmKeySchema>;
export type LiteLlmKeyList = z.infer<typeof LiteLlmKeyListSchema>;
export type LiteLlmTeamActivity = z.infer<typeof LiteLlmTeamActivitySchema>;
