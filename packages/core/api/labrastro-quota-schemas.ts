import { z } from "zod";
import type { AgentRuntime } from "../types";

// Account-level GLM (Zhipu) Coding Plan quota snapshot from the server-side
// collector. Windows mirror the provider's limit entries (TOKENS_LIMIT 5h
// token window, TIME_LIMIT monthly tool credits, CREDIT_LIMIT pools).
// window_minutes is the window's real period (300 = 5h, 10080 = weekly)
// derived from the provider's unit/number; absent on windows the provider
// did not label — several windows of one type differ only by it.
export const GlmQuotaWindowSchema = z.object({
  type: z.string(),
  window_minutes: z.number().optional(),
  used_percent: z.number().optional(),
  usage: z.number().optional(),
  current_value: z.number().optional(),
  remaining: z.number().optional(),
  resets_at: z.number().optional(),
});
export type GlmQuotaWindow = z.infer<typeof GlmQuotaWindowSchema>;

export const GlmQuotaSnapshotSchema = z.object({
  level: z.string().optional().catch(undefined),
  // One unreadable provider window must not hide the other balances or
  // fabricate a healthy quota for the unreadable window.
  windows: z.array(GlmQuotaWindowSchema.nullable().catch(null))
    .transform((windows) => windows.filter((window) => window !== null)),
  observed_at: z.number(),
});
export type GlmQuotaSnapshot = z.infer<typeof GlmQuotaSnapshotSchema>;

export const GlmQuotaStatusSchema = z.object({
  enabled: z.boolean(),
  quota: GlmQuotaSnapshotSchema.nullable().optional().catch(null),
  stale: z.boolean().optional().catch(undefined),
  last_error: z.string().optional().catch(undefined),
  anchor_device: z.string().optional().catch(undefined),
});
export type GlmQuotaStatus = z.infer<typeof GlmQuotaStatusSchema>;

// ---------------------------------------------------------------------------
// Runtime plan-quota schemas. The snapshot rides along on each runtime
// object from `GET /api/runtimes` as an optional field. They are NOT wired
// through parseWithFallback at the endpoint — one malformed snapshot must not
// take down the whole runtime list — so the views layer sanitizes each
// runtime individually (see parsePlanQuota in core/runtimes/plan-quota).
// ---------------------------------------------------------------------------

// Strict per-window shape: a window whose fields fail to parse is dropped by
// the sanitizer, so the schema itself does not tolerate drift.
export const RuntimePlanQuotaWindowSchema = z.object({
  name: z.string().default(""),
  used_percent: z.number().nullable().default(null),
  window_minutes: z.number().nullable().default(null),
  resets_at: z.number().nullable().default(null),
  // Optional quota-pool label for providers that keep several independent
  // pools per account (antigravity reports a gemini pool and a claude_gpt
  // pool, each with 5h and weekly windows). Reporters with a single pool
  // omit it.
  group: z.string().nullish().default(null),
}).loose();

// `windows` stays `unknown[]` here on purpose: the sanitizer re-parses each
// window with RuntimePlanQuotaWindowSchema and drops the malformed ones
// instead of failing the whole snapshot.
export const RuntimePlanQuotaSchema = z.object({
  provider: z.string().default(""),
  status: z.string().default("ok"),
  windows: z.array(z.unknown()).default([]),
  observed_at: z.number().default(0),
  source: z.string().default(""),
}).loose();

// Machine-level CPU/memory sample. `stale` is computed by the server at read
// time and defaults to false for older backends. Unlike plan_quota (helper-
// parsed per runtime, see the note above), system_stats is validated at the
// API boundary via RuntimeListItemSchema: a malformed sample degrades to null
// for that row only. parseSystemStats (core/runtimes/host-metrics) remains as
// the views layer's defensive re-check (it also rejects captured_at <= 0).
export const RuntimeSystemStatsSchema = z.object({
  cpu_percent: z.number().nullable().default(null),
  memory_percent: z.number().nullable().default(null),
  captured_at: z.number().default(0),
  stale: z.boolean().default(false),
}).loose();

// Jev health snapshot for adaptive thinking (contract §3), riding the
// runtime list as "thinking_adapt". Loose by design: fields default rather
// than fail, and `status` stays a plain string so an unrecognized status
// from a newer daemon passes through instead of losing the snapshot — the
// UI renders anything outside the four known values as a neutral state.
export const RuntimeThinkingAdaptSchema = z.object({
  status: z.string().catch(""),
  active_source: z.string().default(""),
  last_latency_ms: z.number().default(0),
  decisions_ok: z.number().default(0),
  decisions_fallback: z.number().default(0),
  since: z.string().nullable().catch(null),
  updated_at: z.string().nullable().catch(null),
}).loose();

// Runtime list items are parsed loosely at the boundary: every field passes
// through untouched except system_stats, which is schema-validated per row —
// a malformed one becomes null (the UI's not-reported state) without taking
// down the rest of the list. A wholly malformed response falls back to [].
export const RuntimeListItemSchema = z.object({
  shared_with: z.array(z.string().uuid()).catch([]),
  system_stats: RuntimeSystemStatsSchema.nullable().catch(null),
  thinking_adapt: RuntimeThinkingAdaptSchema.nullable().catch(null),
}).loose();

export const RuntimeListSchema = z.array(RuntimeListItemSchema);

// Fallback for a wholly malformed runtime list response (not an array of
// objects at all): the caller renders an empty list rather than crashing.
export const EMPTY_RUNTIME_LIST: AgentRuntime[] = [];
