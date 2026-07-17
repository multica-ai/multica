import { z } from "zod";

const fallbackEnum = <T extends string>(values: readonly T[], fallback: T) =>
  z.string().transform((value): T => values.includes(value as T) ? value as T : fallback);
const label = (fallback: string) => z.unknown().optional().transform((value) =>
  typeof value === "string" && value.trim() ? value.trim() : fallback,
);

export const DEFAULT_TERMINOLOGY = {
  strategy: "Strategy", rock: "Goal", rocks: "Goals",
  vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart",
  scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map",
} as const;
const terminologySchema = z.object({
  strategy: label(DEFAULT_TERMINOLOGY.strategy), rock: label(DEFAULT_TERMINOLOGY.rock), rocks: label(DEFAULT_TERMINOLOGY.rocks),
  vision_plan: label(DEFAULT_TERMINOLOGY.vision_plan), meetings: label(DEFAULT_TERMINOLOGY.meetings), org_chart: label(DEFAULT_TERMINOLOGY.org_chart),
  scorecard: label(DEFAULT_TERMINOLOGY.scorecard), issues_list: label(DEFAULT_TERMINOLOGY.issues_list), strategy_map: label(DEFAULT_TERMINOLOGY.strategy_map),
}).catch({ ...DEFAULT_TERMINOLOGY });
export const operatingSystemSettingsSchema = z.object({
  workspace_id: z.string(), terminology: terminologySchema,
  created_at: z.string().optional(), updated_at: z.string().optional(),
});
export const operatingPeriodSchema = z.object({
  id: z.string(), workspace_id: z.string(), name: z.string(),
  unit: fallbackEnum(["month", "quarter", "custom"] as const, "quarter").catch("quarter"),
  starts_on: z.string(), ends_on: z.string(),
});
export const osElementSchema = z.object({
  key: z.string(), enabled: z.boolean(), default_enabled: z.boolean().default(false),
});
export const osElementListSchema = z.object({ elements: z.array(osElementSchema) });
export const goalTypeSchema = z.object({
  id: z.string(), workspace_id: z.string(), name: z.string(), color: z.string().default(""),
  scope_label: z.string().default(""), position: z.number().int().default(0),
  created_at: z.string().default(""), updated_at: z.string().default(""),
});
export const goalTypeListSchema = z.object({ goal_types: z.array(goalTypeSchema) });
export const operatingPeriodListSchema = z.object({ periods: z.array(operatingPeriodSchema) });
export const strategyItemSchema = z.object({
  id: z.string(), workspace_id: z.string(),
  kind: fallbackEnum(["core_value", "core_focus", "horizon_goal", "unknown"] as const, "unknown"),
  title: z.string(), description: z.string().default(""),
  horizon_unit: z.enum(["day", "week", "month", "year"]).optional(), horizon_count: z.number().int().positive().optional(),
  horizon_label: z.string().optional(), position: z.number().int(),
  state: fallbackEnum(["active", "archived", "unknown"] as const, "unknown"),
  created_at: z.string(), updated_at: z.string(),
});
const health = fallbackEnum(["on_track", "at_risk", "off_track", "unset", "unknown"] as const, "unknown");
const reportedHealth = fallbackEnum(["on_track", "at_risk", "off_track", "unset"] as const, "unset");
const rockProjectSchema = z.object({ id: z.string(), title: z.string(), issue_count: z.number().int().min(0), done_issue_count: z.number().int().min(0) });
const rockIssueSchema = z.object({ id: z.string(), identifier: z.string(), title: z.string(), status: z.string(), project_id: z.string().optional(), project_title: z.string().optional() });
export const rockCheckInSchema = z.object({
  id: z.string(), confidence: z.number().min(0).max(100), reported_health: health, note: z.string().default(""),
  created_by_type: z.string(), created_by_id: z.string(), created_at: z.string(),
});
export const rockSchema = z.object({
  id: z.string().optional(), workspace_id: z.string(), title: z.string().optional(), description: z.string().optional(),
  owner_type: z.string().optional(), owner_id: z.string().optional(), owner_name: z.string().optional(),
  period_id: z.string().default(""), period_name: z.string().default(""),
  goal_type_id: z.string().optional(), goal_type_name: z.string().optional(),
  goal_type_color: z.string().optional(), goal_type_scope_label: z.string().optional(),
  period_start: z.string(), period_end: z.string(), confidence: z.number().min(0).max(100),
  reported_health: reportedHealth,
  derived_health: z.object({ state: health, reason: z.string(), calculated_at: z.string() }),
  health_score: z.number().min(0).max(100).default(0),
  issue_count: z.number().int().min(0), done_issue_count: z.number().int().min(0), blocked_issue_count: z.number().int().min(0),
  project_count: z.number().int().min(0).default(0), projects: z.array(rockProjectSchema).default([]),
  issues: z.array(rockIssueSchema).default([]), check_ins: z.array(rockCheckInSchema).default([]),
  strategy_item_id: z.string().optional(), strategy_item_title: z.string().optional(),
  project_id: z.string().optional(), project_title: z.string().optional(), project_description: z.string().optional(), project_status: z.string().optional(),
  lead_type: z.string().optional(), lead_id: z.string().optional(), created_at: z.string(), updated_at: z.string(),
}).transform((rock) => ({
  ...rock,
  id: rock.id || rock.project_id || "",
  title: rock.title || rock.project_title || "Untitled Rock",
  period_id: rock.period_id || `${rock.period_start}:${rock.period_end}`,
  period_name: rock.period_name || "Current period",
}));
export const strategyListSchema = z.object({ strategy_items: z.array(strategyItemSchema) });
export const strategyHistoryListSchema = z.object({ history: z.array(z.object({ id: z.string(), strategy_item_id: z.string(), action: z.enum(["baseline", "created", "updated", "deleted"]), title: z.string(), snapshot: z.record(z.string(), z.unknown()), changed_at: z.string() })) });
export const rocksListSchema = z.object({ rocks: z.array(rockSchema) });
export const objectConnectionSchema = z.object({
  id: z.string(), workspace_id: z.string(), source_type: z.string(), source_id: z.string(),
  target_type: z.string(), target_id: z.string(), relationship_type: z.string(),
  provenance: z.enum(["manual", "agent", "system"]), created_by_type: z.string(),
  created_by_id: z.string(), created_at: z.string(),
});
export const objectConnectionListSchema = z.object({ connections: z.array(objectConnectionSchema) });
export const EMPTY_STRATEGY = { strategy_items: [] };
export const EMPTY_STRATEGY_HISTORY = { history: [] };
export const EMPTY_ROCKS = { rocks: [] };
export const EMPTY_PERIODS = { periods: [] };
export const EMPTY_ELEMENTS = { elements: [] };
export const EMPTY_GOAL_TYPES = { goal_types: [] };
export const EMPTY_CONNECTIONS = { connections: [] };
export const DEFAULT_SETTINGS = { workspace_id: "", terminology: { ...DEFAULT_TERMINOLOGY } };
