import { z } from "zod";

const fallbackEnum = <T extends string>(values: readonly T[], fallback: T) =>
  z.string().transform((value): T => values.includes(value as T) ? value as T : fallback);
const label = (fallback: string) => z.unknown().transform((value) =>
  typeof value === "string" && value.trim() ? value.trim() : fallback,
);

const terminologySchema = z.object({
  strategy: label("Strategy"), rock: label("Rock"), rocks: label("Rocks"),
}).catch({ strategy: "Strategy", rock: "Rock", rocks: "Rocks" });
export const operatingSystemSettingsSchema = z.object({
  workspace_id: z.string(), terminology: terminologySchema,
  created_at: z.string().optional(), updated_at: z.string().optional(),
});
export const strategyItemSchema = z.object({
  id: z.string(), workspace_id: z.string(),
  kind: fallbackEnum(["core_value", "core_focus", "horizon_goal", "unknown"] as const, "unknown"),
  title: z.string(), description: z.string().default(""),
  horizon_unit: z.enum(["day", "week", "month", "year"]).optional(), horizon_count: z.number().int().positive().optional(),
  position: z.number().int(), state: fallbackEnum(["active", "archived", "unknown"] as const, "unknown"),
  created_at: z.string(), updated_at: z.string(),
});
const health = fallbackEnum(["on_track", "at_risk", "off_track", "unset", "unknown"] as const, "unknown");
export const rockSchema = z.object({
  project_id: z.string(), workspace_id: z.string(), project_title: z.string(), project_description: z.string().optional(),
  project_status: z.string(), lead_type: z.string().optional(), lead_id: z.string().optional(),
  period_start: z.string(), period_end: z.string(), confidence: z.number().min(0).max(100),
  reported_health: fallbackEnum(["on_track", "at_risk", "off_track", "unset"] as const, "unset"),
  derived_health: z.object({ state: health, reason: z.string(), calculated_at: z.string() }),
  issue_count: z.number().int().min(0), done_issue_count: z.number().int().min(0), blocked_issue_count: z.number().int().min(0),
  created_at: z.string(), updated_at: z.string(),
});
export const strategyListSchema = z.object({ strategy_items: z.array(strategyItemSchema) });
export const rocksListSchema = z.object({ rocks: z.array(rockSchema) });
export const objectConnectionSchema = z.object({
  id: z.string(), workspace_id: z.string(), source_type: z.string(), source_id: z.string(),
  target_type: z.string(), target_id: z.string(), relationship_type: z.string(),
  provenance: z.enum(["manual", "agent", "system"]), created_by_type: z.string(),
  created_by_id: z.string(), created_at: z.string(),
});
export const objectConnectionListSchema = z.object({ connections: z.array(objectConnectionSchema) });
export const EMPTY_STRATEGY = { strategy_items: [] };
export const EMPTY_ROCKS = { rocks: [] };
export const EMPTY_CONNECTIONS = { connections: [] };
export const DEFAULT_SETTINGS = { workspace_id: "", terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" } };
