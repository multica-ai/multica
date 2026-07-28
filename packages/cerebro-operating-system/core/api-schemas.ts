import { z } from "zod";

const fallbackEnum = <T extends string>(values: readonly T[], fallback: T) =>
  z.string().transform((value): T => values.includes(value as T) ? value as T : fallback);
const label = (fallback: string) => z.unknown().optional().transform((value) =>
  typeof value === "string" && value.trim() ? value.trim() : fallback,
);

export const DEFAULT_TERMINOLOGY = {
  strategy: "Strategy", rock: "Goal", rocks: "Goals",
  vision_plan: "Vision Plan", meetings: "Cycles", org_chart: "Roles",
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
export const visionPlanItemSchema = z.object({
  id: z.string(), workspace_id: z.string(), section_id: z.string(), title: z.string(), description: z.string().default(""),
  part_label: z.string().optional(), owner_type: z.enum(["member", "agent"]).optional(), owner_id: z.string().optional(), owner_name: z.string().optional(),
  position: z.number().int(), state: z.enum(["active", "archived"]).catch("active"),
  goal_connections: z.array(z.object({ connection_id: z.string(), goal_id: z.string() })).default([]),
  links: z.array(z.object({
    connection_id: z.string(), target_type: z.enum(["project", "issue"]), target_id: z.string(),
    title: z.string().default(""), identifier: z.string().default(""),
  })).default([]),
  created_at: z.string(), updated_at: z.string(),
});
export const visionPlanSectionSchema = z.object({
  id: z.string(), workspace_id: z.string(), key: z.string(), name: z.string(),
  section_type: z.enum(["list", "structured", "process", "goals"]).catch("list"), position: z.number().int(),
  page_id: z.string().default(""), column_index: z.number().int().min(0).catch(0),
  items: z.array(visionPlanItemSchema).default([]), created_at: z.string(), updated_at: z.string(),
});
export const visionPlanPageSchema = z.object({
  id: z.string(), workspace_id: z.string(), key: z.string(), name: z.string(),
  column_count: z.number().int().min(1).max(3).catch(3), position: z.number().int().catch(0),
  created_at: z.string().default(""), updated_at: z.string().default(""),
});
export const visionPlanSchema = z.object({
  pages: z.array(visionPlanPageSchema).nullable().optional().transform((value) => value ?? []),
  sections: z.array(visionPlanSectionSchema),
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
const meetingCadence = fallbackEnum(["manual", "day", "week", "month", "quarter"] as const, "manual");
const meetingBinding = fallbackEnum(["none", "scorecard", "goals", "issues_list"] as const, "none");
const meetingAgendaSchema = z.object({ id: z.string(), name: z.string(), position: z.number().int().default(0), binding: meetingBinding });
const safeOptionalId = z.unknown().optional().transform((value) => typeof value === "string" && value.trim() ? value : undefined);
const meetingNoteTypeSchema = z.object({
  id: z.string(), name: z.string(), icon: z.string().optional(),
  cadence_unit: meetingCadence, cadence_count: z.number().int().positive().catch(1),
  enabled: z.boolean().catch(false), current_note_id: safeOptionalId,
  anchor_weekday: z.number().int().optional(), anchor_week_of_month: z.number().int().optional(),
  next_meeting_date: z.string().optional(),
  upcoming_dates: z.array(z.string()).nullable().optional().transform((value) => value ?? undefined),
  year_dates: z.array(z.string()).nullable().optional().transform((value) => value ?? undefined),
  participants: z.array(z.object({ type: z.enum(["member", "agent"]), id: z.string() })).nullable().optional().transform((value) => value ?? undefined),
});
export const meetingSchema = z.object({
  workspace_id: z.string(), note_type_id: z.string().optional(), note_type_name: z.string().optional(), current_note_id: safeOptionalId,
  cadence_unit: meetingCadence, cadence_count: z.number().int().positive().catch(1),
  agenda: z.array(meetingAgendaSchema).nullable().transform((value) => value ?? []),
  available_note_types: z.array(meetingNoteTypeSchema).nullable().transform((value) => value ?? []),
});
export const orgChartSeatSchema = z.object({
  id: z.string(), workspace_id: z.string(), parent_id: z.string().optional(), name: z.string(),
  responsibilities: z.array(z.string()).nullable().transform((value) => value ?? []), owner_type: z.enum(["member", "agent"]).optional(), owner_id: z.string().optional(),
  owner_name: z.string().optional(), vacant: z.boolean().default(true), position: z.number().int().default(0),
});
export const orgChartSeatListSchema = z.object({ seats: z.array(orgChartSeatSchema).nullable().transform((value) => value ?? []) });
export const EMPTY_STRATEGY = { strategy_items: [] };
export const EMPTY_STRATEGY_HISTORY = { history: [] };
export const EMPTY_ROCKS = { rocks: [] };
export const EMPTY_PERIODS = { periods: [] };
export const EMPTY_ELEMENTS = { elements: [] };
export const EMPTY_GOAL_TYPES = { goal_types: [] };
export const EMPTY_CONNECTIONS = { connections: [] };
export const EMPTY_MEETING = { workspace_id: "", cadence_unit: "manual" as const, cadence_count: 1, agenda: [], available_note_types: [] };
export const EMPTY_ORG_CHART = { seats: [] };
export const EMPTY_VISION_PLAN = { pages: [], sections: [] };
export const DEFAULT_SETTINGS = { workspace_id: "", terminology: { ...DEFAULT_TERMINOLOGY } };
