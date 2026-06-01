import { z } from "zod";
import type {
  AssignmentsListResponse,
  CerebroStatusModel,
  ProjectStatusAssignment,
  StatusModelsListResponse,
} from "./types";

// Zod schemas for the cerebro-status-models REST surface — the boundary
// defense against API drift that CLAUDE.md "API Response Compatibility"
// mandates. Schemas are LENIENT: base_status stays z.string() so a future
// upstream status downgrades gracefully, and every object .passthrough()s
// unknown fields rather than stripping them.

const statusTriggersSchema = z
  .object({
    assign_to_id: z.string().optional(),
    assign_to_type: z.enum(["member", "agent"]).optional(),
    fire_agent_id: z.string().optional(),
  })
  .passthrough();

const statusEntrySchema = z
  .object({
    key: z.string(),
    label: z.string(),
    color: z.string().default(""),
    base_status: z.string(),
    position: z.number().default(0),
    description: z.string().optional(),
    triggers: statusTriggersSchema.optional(),
  })
  .passthrough();

export const statusModelSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    name: z.string(),
    description: z.string().optional(),
    statuses: z.array(statusEntrySchema).default([]),
    project_count: z.number().default(0),
    created_by_id: z.string(),
    created_by_type: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .passthrough();

export const statusModelsListSchema = z
  .object({
    status_models: z.array(statusModelSchema).default([]),
  })
  .passthrough();

export const projectAssignmentSchema = z
  .object({
    project_id: z.string(),
    workspace_id: z.string(),
    status_model_id: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .passthrough();

export const assignmentsListSchema = z
  .object({
    assignments: z.array(projectAssignmentSchema).default([]),
  })
  .passthrough();

// Safe fallbacks returned when a response fails validation, so the UI never
// white-screens on a drifted or partial body.
export const EMPTY_STATUS_MODELS_LIST: StatusModelsListResponse = { status_models: [] };
export const EMPTY_ASSIGNMENTS_LIST: AssignmentsListResponse = { assignments: [] };
export const EMPTY_STATUS_MODEL: CerebroStatusModel = {
  id: "",
  workspace_id: "",
  name: "",
  statuses: [],
  project_count: 0,
  created_by_id: "",
  created_by_type: "",
  created_at: "",
  updated_at: "",
};
export const EMPTY_PROJECT_ASSIGNMENT: ProjectStatusAssignment = {
  project_id: "",
  workspace_id: "",
  status_model_id: "",
  created_at: "",
  updated_at: "",
};

// v2b: per-issue custom status pin schema (FIR-1550).
export const issueCustomStatusSchema = z
  .object({
    issue_id: z.string(),
    workspace_id: z.string(),
    status_model_id: z.string(),
    custom_status_key: z.string(),
    base_status: z.string(),
    set_by_id: z.string().default(""),
    set_by_type: z.enum(["member", "agent", "system"]).default("system"),
    label: z.string().optional(),
    description: z.string().optional(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .passthrough();
