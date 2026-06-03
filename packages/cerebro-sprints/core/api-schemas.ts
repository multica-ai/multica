import { z } from "zod";

// All response readers route through parseWithFallback so an older/newer
// backend degrades to safe defaults instead of breaking the UI. See
// CLAUDE.md → API Response Compatibility.

const cadenceUnit = z.enum(["day", "week", "month"]);
const sprintStatus = z.enum(["planned", "active", "done", "cancelled"]);

export const sprintSettingsSchema = z.object({
  project_id: z.string(),
  workspace_id: z.string(),
  enabled: z.boolean(),
  duration_unit: cadenceUnit,
  duration_count: z.number().int().positive(),
  start_weekday: z.number().int().min(1).max(7),
  name_template: z.string(),
  auto_create_enabled: z.boolean(),
  auto_create_lead_days: z.number().int().min(0),
  move_incomplete_enabled: z.boolean(),
  timezone: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const sprintSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string(),
  name: z.string(),
  sequence_no: z.number().int(),
  status: sprintStatus,
  start_date: z.string(),
  end_date: z.string(),
  goal: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const sprintsListSchema = z.object({
  sprints: z.array(sprintSchema),
});

export const sprintIssueLinkSchema = z.object({
  issue_id: z.string(),
  sprint_id: z.string(),
  added_at: z.string(),
});

export const sprintIssuesListSchema = z.object({
  issues: z.array(sprintIssueLinkSchema),
});

export const recurringTaskSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string(),
  cadence_unit: cadenceUnit,
  cadence_count: z.number().int().positive(),
  title: z.string(),
  description: z.string().optional(),
  priority: z.string().optional(),
  assignee_type: z.enum(["member", "agent"]).optional(),
  assignee_id: z.string().optional(),
  enabled: z.boolean(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const recurringTasksListSchema = z.object({
  recurring_tasks: z.array(recurringTaskSchema),
});

export const EMPTY_SPRINTS_LIST = { sprints: [] };
export const EMPTY_SPRINT_ISSUES_LIST = { issues: [] };
export const EMPTY_RECURRING_TASKS_LIST = { recurring_tasks: [] };
