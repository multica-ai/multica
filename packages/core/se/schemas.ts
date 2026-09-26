import { z } from "zod";

// SE-37641: infrastructure health snapshot (produced by host-side cron from
// Prometheus and served by GET /api/se/health). Fields are defensively typed:
// the snapshot is generated outside the backend, so shape drift must not
// crash the Health page.
export const SEInfraHealthTargetSchema = z.object({
  job: z.string().default(""),
  instance: z.string().default(""),
  health: z.string().default("unknown"),
});

export const SEInfraNodeNowSchema = z.object({
  load1: z.number().nullish().transform((v) => v ?? 0),
  mem_avail_gb: z.number().nullish().transform((v) => v ?? 0),
  mem_total_gb: z.number().nullish().transform((v) => v ?? 0),
  swap_used_gb: z.number().nullish().transform((v) => v ?? 0),
  swap_total_gb: z.number().nullish().transform((v) => v ?? 0),
});

export const SEInfraSyncSchema = z.object({
  ok: z.boolean().nullish().transform((v) => v ?? false),
  last_success: z.string().nullish().transform((v) => v ?? ""),
  streak: z.number().nullish().transform((v) => v ?? 0),
});

// SE-37663: agent fleet section of GET /api/se/health. Computed live by the
// backend (no file dependency), but still parsed defensively: older backends
// omit the whole section, so every field must survive drift.
export const SEFleetRuntimeSchema = z.object({
  name: z.string().default(""),
  provider: z.string().default(""),
  status: z.string().default("unknown"),
  last_seen_at: z.string().optional(),
});

export const SEFleetAgentFailureSchema = z.object({
  agent_id: z.string().default(""),
  agent_name: z.string().default(""),
  completed: z.number().nullish().transform((v) => v ?? 0),
  failed: z.number().nullish().transform((v) => v ?? 0),
  quota_failed: z.number().nullish().transform((v) => v ?? 0),
  auth_failed: z.number().nullish().transform((v) => v ?? 0),
  first_failure_at: z.string().optional(),
  last_failure_at: z.string().optional(),
  last_error_excerpt: z.string().optional(),
});

export const SEAgentFleetSchema = z.object({
  status: z.string().default("unknown"),
  quota_streak_active: z.boolean().nullish().transform((v) => v === true),
  auth_streak_active: z.boolean().nullish().transform((v) => v === true),
  runtimes: z.array(SEFleetRuntimeSchema).nullish().transform((v) => v ?? []),
  failing_agents: z.array(SEFleetAgentFailureSchema).nullish().transform((v) => v ?? []),
});

export type SEAgentFleet = z.infer<typeof SEAgentFleetSchema>;

export const SEInfraHealthSnapshotSchema = z.object({
  generated_at: z.string().default(""),
  overall: z.string().default("unknown"),
  targets: z.array(SEInfraHealthTargetSchema).nullish().transform((v) => v ?? []),
  nodes: z.record(z.string(), SEInfraNodeNowSchema).nullish().transform((v) => v ?? {}),
  history: z.record(z.string(), z.array(z.array(z.number()))).nullish().transform((v) => v ?? {}),
  syncs: z.record(z.string(), SEInfraSyncSchema).nullish().transform((v) => v ?? {}),
  agent_fleet: SEAgentFleetSchema.nullish().transform((v) => v ?? undefined),
});

export type SEInfraHealthSnapshot = z.infer<typeof SEInfraHealthSnapshotSchema>;
