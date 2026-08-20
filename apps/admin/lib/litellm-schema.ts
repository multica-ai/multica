import { z } from "zod";

// External-boundary schemas for LiteLLM's admin API. Kept lenient per
// packages/core/api/schema.ts's parseWithFallback convention: unknown extra
// fields are ignored, and every field we actually read is optional/nullable
// so a partial or drifted response degrades gracefully instead of throwing.

export const LiteLlmKeySchema = z.object({
  key_alias: z.string().nullish(),
  team_id: z.string().nullish(),
  spend: z.number().nullish(),
});

export const LiteLlmKeyListSchema = z.object({
  keys: z.array(LiteLlmKeySchema).default([]),
  metadata: z
    .object({
      total_pages: z.number().nullish(),
    })
    .nullish(),
});

// /team/list entries. `team_alias` here is the org's real, human-assigned
// team name (e.g. "Digital Acquisition"). Note the raw /key/list response
// also has a field named `team_alias`, but it's always null on every key
// created via Gandalf's workspace_create path, so LiteLlmKeySchema
// deliberately doesn't model it — the key only carries `team_id`, and this
// list is what resolves that id to a display name.
export const LiteLlmTeamSchema = z.object({
  team_id: z.string().nullish(),
  team_alias: z.string().nullish(),
});

export const LiteLlmTeamListSchema = z.union([
  z.array(LiteLlmTeamSchema),
  z.object({ teams: z.array(LiteLlmTeamSchema).default([]) }),
]);

export const LiteLlmModelMetricsSchema = z.object({
  spend: z.number().nullish(),
  total_tokens: z.number().nullish(),
});

// The real /team/daily/activity response nests each model's numbers under
// `metrics`, but also has top-level `spend`/`total_tokens` on some payload
// shapes — model both explicitly rather than a z.union with a bare
// z.record(z.unknown()) fallback, since zod's object parsing strips
// unrecognized keys: a union would silently drop `metrics` because the
// all-nullish LiteLlmModelMetricsSchema branch matches (and wins) first,
// even when the real data lives one level deeper.
export const LiteLlmModelEntrySchema = z.object({
  metrics: LiteLlmModelMetricsSchema.nullish(),
  spend: z.number().nullish(),
  total_tokens: z.number().nullish(),
});

export const LiteLlmDayResultSchema = z.object({
  date: z.string().nullish(),
  group_by_day: z.string().nullish(),
  breakdown: z
    .object({
      models: z.record(z.string(), LiteLlmModelEntrySchema).nullish(),
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
export type LiteLlmTeam = z.infer<typeof LiteLlmTeamSchema>;
export type LiteLlmTeamActivity = z.infer<typeof LiteLlmTeamActivitySchema>;
export type LiteLlmDayResult = z.infer<typeof LiteLlmDayResultSchema>;
