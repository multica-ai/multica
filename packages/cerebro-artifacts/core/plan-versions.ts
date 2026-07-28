import { queryOptions } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";

// FIR-3659 — the Workpad panel shows how many versions the plan has. A plan is
// a note (artifact), and every save snapshots a version, so the count comes from
// GET /api/notes/{planId}/versions (a bare array of version rows). Defensive
// parse (API Response Compatibility rule): on any shape drift this yields [] so
// the panel simply omits the version line rather than throwing.

export const PlanVersionSchema = z.object({
  id: z.string().default(""),
  version_no: z.number().default(0),
  author_type: z.string().default(""),
  author_id: z.string().default(""),
  created_at: z.string().default(""),
});

export type PlanVersion = z.infer<typeof PlanVersionSchema>;

export const PlanVersionsListSchema = z.array(PlanVersionSchema).catch([]);

export function safeParsePlanVersions(raw: unknown): PlanVersion[] {
  const result = PlanVersionsListSchema.safeParse(raw ?? []);
  if (result.success) return result.data;
  return [];
}

export const planVersionKeys = {
  all: (wsId: string) => ["plan-versions", wsId] as const,
  forPlan: (wsId: string, planId: string) =>
    [...planVersionKeys.all(wsId), planId] as const,
};

export function planVersionsOptions(wsId: string, planId: string) {
  return queryOptions({
    queryKey: planVersionKeys.forPlan(wsId, planId),
    queryFn: async () => safeParsePlanVersions(await api.listNoteVersions(planId)),
    enabled: Boolean(wsId && planId),
    staleTime: 15 * 1000,
  });
}
